package worker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/storage"
)

const (
	cacheImmutable = "public, max-age=31536000, immutable"
	cachePlaylist  = "public, max-age=300"
	uploadWorkers  = 6
)

// Pipeline runs one job. It is safe for concurrent use.
type Pipeline struct {
	Store   Store
	Storage storage.API
	Log     *slog.Logger
	WorkDir string
	FFmpeg  string
	FFprobe string
	Threads int
	// Notify pushes a realtime event after a commit (optional).
	Notify func(ctx context.Context, userID, videoID string)
}

// Run executes the job and returns nil only when the job was completed in the database.
func (p *Pipeline) Run(ctx context.Context, j Job) error {
	v, err := p.Store.Video(ctx, j.VideoID)
	switch {
	case errors.Is(err, ErrVideoGone):
		return &JobError{Code: "video_unavailable", Message: "The video no longer exists", Permanent: true, Silent: true, Err: err}
	case err != nil:
		return err
	case v.Moderation == "REMOVED" || v.Status != "PROCESSING":
		return &JobError{Code: "video_unavailable", Message: "The video can no longer be processed", Permanent: true, Silent: true}
	case v.OriginalKey == "":
		return permanent("no_original", "The original file is missing")
	}
	switch j.Type {
	case "METADATA":
		return p.metadata(ctx, j, v)
	case "THUMBNAIL":
		return p.thumbnails(ctx, j, v)
	case "TRANSCODE":
		return p.transcode(ctx, j, v)
	case "HLS":
		return p.master(ctx, j, v)
	}
	return permanent("unknown_job", "Unknown job type "+j.Type)
}

func (p *Pipeline) dir(videoID string) string { return filepath.Join(p.WorkDir, videoID) }

// original returns the local path of the original, downloading it when it is not cached. With
// hash=true it always downloads fresh and returns the SHA-256 of what storage really holds.
func (p *Pipeline) original(ctx context.Context, videoID string, v VideoRow, hash bool) (string, []byte, error) {
	ext := strings.ToLower(filepath.Ext(v.OriginalKey))
	d := p.dir(videoID)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", nil, transient("workdir", err)
	}
	path := filepath.Join(d, "original"+ext)
	if !hash {
		if st, err := os.Stat(path); err == nil && st.Size() == v.Size {
			return path, nil, nil
		}
	}
	tmp := path + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, transient("workdir", err)
	}
	h := sha256.New()
	n, err := p.Storage.Download(ctx, v.OriginalKey, io.MultiWriter(f, h))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		if errors.Is(err, storage.ErrNotFound) {
			return "", nil, permanent("original_missing", "The uploaded file is missing")
		}
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		return "", nil, transient("download_failed", err)
	}
	if n != v.Size {
		_ = os.Remove(tmp)
		return "", nil, permanent("size_mismatch", "The uploaded file does not match its declared size")
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", nil, transient("workdir", err)
	}
	return path, h.Sum(nil), nil
}

func (p *Pipeline) metadata(ctx context.Context, j Job, v VideoRow) error {
	path, sum, err := p.original(ctx, j.VideoID, v, true)
	if err != nil {
		return err
	}
	pr, err := ProbeFile(ctx, p.FFprobe, path, filepath.Ext(v.OriginalKey))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var je *JobError
		if errors.As(err, &je) {
			return je
		}
		return transient("ffprobe_failed", err)
	}
	vt, secs, je := pr.Validate(v.Size)
	if je != nil {
		return je
	}
	blocked, err := p.Store.SaveMetadata(ctx, j, MetadataResult{Probe: pr, VideoType: vt, Seconds: secs, SHA256: sum})
	if err != nil {
		return err
	}
	if blocked {
		p.Log.WarnContext(ctx, "upload matched the content-hash blocklist", "video", j.VideoID)
		return &JobError{Code: "blocked_content", Message: "This video was removed by moderation", Permanent: true, Silent: true}
	}
	return nil
}

func even2(n int) int { return even(n) }

func (p *Pipeline) thumbnails(ctx context.Context, j Job, v VideoRow) error {
	meta, err := p.Store.Meta(ctx, j.VideoID)
	if err != nil {
		return err
	}
	path, _, err := p.original(ctx, j.VideoID, v, false)
	if err != nil {
		return err
	}
	tdir := filepath.Join(p.dir(j.VideoID), "thumbs")
	if err := os.MkdirAll(tdir, 0o700); err != nil {
		return transient("workdir", err)
	}
	w := min(thumbMaxWidth, meta.Width)
	h := even2(meta.Height * w / meta.Width)
	var items []ThumbOut
	for i, at := range thumbTimes(float64(meta.Duration)) {
		out := filepath.Join(tdir, fmt.Sprintf("%d.jpg", i))
		err := runFFmpeg(ctx, p.FFmpeg, thumbArgs(path, out, at, thumbMaxWidth), 2*time.Minute)
		if st, serr := os.Stat(out); err != nil || serr != nil || st.Size() == 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// seeking past the last decodable frame: fall back to the very first frame
			if err2 := runFFmpeg(ctx, p.FFmpeg, thumbArgs(path, out, 0, thumbMaxWidth), 2*time.Minute); err2 != nil {
				return asJobError(err2)
			}
		}
		f, err := os.Open(out)
		if err != nil {
			return transient("thumbnail", err)
		}
		st, _ := f.Stat()
		key := "thumbs/" + j.VideoID + "/" + auth.NewUUIDv7() + ".jpg"
		err = p.Storage.Put(ctx, key, "image/jpeg", cacheImmutable, f, st.Size())
		_ = f.Close()
		if err != nil {
			return storageErr(ctx, err)
		}
		items = append(items, ThumbOut{Key: key, W: w, H: h})
	}
	old, err := p.Store.ReplaceThumbnails(ctx, j, items, len(items)/2)
	if err != nil {
		return err
	}
	for _, k := range old { // best effort: orphans are harmless, they are never referenced
		_ = p.Storage.Delete(context.WithoutCancel(ctx), k)
	}
	return nil
}

func storageErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return transient("storage_failed", err)
}

func (p *Pipeline) transcode(ctx context.Context, j Job, v VideoRow) error {
	meta, err := p.Store.Meta(ctx, j.VideoID)
	if err != nil {
		return err
	}
	path, _, err := p.original(ctx, j.VideoID, v, false)
	if err != nil {
		return err
	}
	pr := Probe{Width: meta.Width, Height: meta.Height, FPS: meta.FPS, HasAudio: meta.HasAudio}
	// Start from a clean prefix: a previous failed attempt may have left segments behind.
	if _, err := p.Storage.DeletePrefix(ctx, "hls/"+j.VideoID+"/"); err != nil {
		return storageErr(ctx, err)
	}
	for _, r := range Plan(meta.Width, meta.Height) {
		out := filepath.Join(p.dir(j.VideoID), "hls", r.Name)
		_ = os.RemoveAll(out)
		if err := os.MkdirAll(out, 0o700); err != nil {
			return transient("workdir", err)
		}
		if err := runFFmpeg(ctx, p.FFmpeg, hlsArgs(path, out, r, pr, p.Threads), encodeTimeout(float64(meta.Duration))); err != nil {
			return asJobErrorCtx(ctx, err)
		}
		prefix := "hls/" + j.VideoID + "/" + r.Name + "/"
		n, err := p.uploadDir(ctx, out, prefix)
		if err != nil {
			return err
		}
		if err := p.Store.SaveVariant(ctx, j.VideoID, VariantOut{R: r, Key: prefix, Bytes: n}); err != nil {
			return err
		}
		_ = os.RemoveAll(out) // free the disk before the next rendition
	}
	return p.Store.FinishTranscode(ctx, j)
}

func asJobErrorCtx(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return asJobError(err)
}

// uploadDir uploads segments in parallel and the playlist last, so a playlist never points at a
// segment that is not there yet. It returns the total bytes.
func (p *Pipeline) uploadDir(ctx context.Context, dir, prefix string) (int64, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, transient("workdir", err)
	}
	var segs []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".ts") {
			segs = append(segs, e.Name())
		}
	}
	sort.Strings(segs)
	if len(segs) == 0 {
		return 0, transient("no_segments", errors.New("ffmpeg produced no segments"))
	}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int64
		first error
		sem   = make(chan struct{}, uploadWorkers)
	)
	put := func(name, ctype, cache string) error {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return transient("workdir", err)
		}
		defer f.Close()
		st, _ := f.Stat()
		if err := p.Storage.Put(ctx, prefix+name, ctype, cache, f, st.Size()); err != nil {
			return storageErr(ctx, err)
		}
		mu.Lock()
		total += st.Size()
		mu.Unlock()
		return nil
	}
	for _, s := range segs {
		sem <- struct{}{}
		wg.Add(1)
		go func(s string) {
			defer func() { <-sem; wg.Done() }()
			mu.Lock()
			failed := first != nil
			mu.Unlock()
			if failed {
				return
			}
			if err := put(s, "video/mp2t", cacheImmutable); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	if first != nil {
		return 0, first
	}
	if err := put("index.m3u8", "application/vnd.apple.mpegurl", cachePlaylist); err != nil {
		return 0, err
	}
	return total, nil
}

func (p *Pipeline) master(ctx context.Context, j Job, v VideoRow) error {
	meta, err := p.Store.Meta(ctx, j.VideoID)
	if err != nil {
		return err
	}
	plan := Plan(meta.Width, meta.Height)
	ready, err := p.Store.Variants(ctx, j.VideoID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, r := range ready {
		have[r] = true
	}
	for _, r := range plan {
		if !have[r.Name] {
			return transient("variants_missing", fmt.Errorf("rendition %s is not ready", r.Name))
		}
	}
	key := "hls/" + j.VideoID + "/master.m3u8"
	body := MasterPlaylist(plan, meta.HasAudio)
	if err := p.Storage.PutBytes(ctx, key, "application/vnd.apple.mpegurl", cachePlaylist, []byte(body)); err != nil {
		return storageErr(ctx, err)
	}
	creator, err := p.Store.FinishHLS(ctx, j, key)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(p.dir(j.VideoID))
	if p.Notify != nil && creator != "" {
		p.Notify(context.WithoutCancel(ctx), creator, j.VideoID)
	}
	return nil
}
