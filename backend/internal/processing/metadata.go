package processing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"

	"github.com/redxtube/api/internal/processing/inspect"
	"github.com/redxtube/api/internal/storage"
)

// Source is the part of the object storage the metadata step needs (storage.API satisfies it).
type Source interface {
	Head(ctx context.Context, key string) (storage.ObjectInfo, error)
	Download(ctx context.Context, key string, w io.Writer) (int64, error)
}

// MediaProber inspects a local file (inspect.Prober satisfies it).
type MediaProber interface {
	Probe(ctx context.Context, path string) (*inspect.Info, error)
}

// Extra safe codes of this step.
const CodeSourceMissing = "source_missing"

// diskReserve is kept free on top of the file size so a nearly full disk is not driven to zero.
const diskReserve = 256 << 20

// MetadataProcessor is the METADATA job: it fetches the original, inspects it, checks the input policy, records the
// technical metadata and queues the single HLS job (decision D3). It never reads a file name, only content.
type MetadataProcessor struct {
	Store     *Store
	Src       Source
	Prober    MediaProber
	Policy    inspect.Policy
	TempDir   string // default os.TempDir()
	Log       *slog.Logger
	FreeBytes func(dir string) (uint64, error) // default: the real free space (tests replace it)
}

type capWriter struct {
	w   io.Writer
	max int64
	n   int64
}

var errTooManyBytes = errors.New("download exceeded the size announced by the storage")

func (c *capWriter) Write(p []byte) (int, error) {
	if c.n+int64(len(p)) > c.max {
		return 0, errTooManyBytes
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func (m *MetadataProcessor) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

// Process implements Processor.
func (m *MetadataProcessor) Process(ctx context.Context, job Job, p Progress) error {
	log := m.log().With("job", job.ID, "video", job.VideoID)
	key, dbSize, err := m.Store.original(ctx, job.VideoID)
	if err != nil {
		return err
	}
	p.Report("checking", 5)
	head, err := m.Src.Head(ctx, key)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return Permanent(CodeSourceMissing, err)
	case err != nil:
		return fmt.Errorf("storage head: %w", err) // transient
	case head.Size <= 0 || head.Size > m.Policy.MaxSizeBytes:
		return Permanent(inspect.CodeTooLarge, fmt.Errorf("object size %d", head.Size))
	case dbSize != nil && *dbSize != head.Size:
		return Permanent(inspect.CodeCorruptInput, fmt.Errorf("stored object has %d bytes, the upload recorded %d", head.Size, *dbSize))
	}

	base := m.TempDir
	if base == "" {
		base = os.TempDir()
	}
	free := m.FreeBytes
	if free == nil {
		free = freeBytes
	}
	if avail, err := free(base); err != nil {
		return fmt.Errorf("temp dir: %w", err)
	} else if avail < uint64(head.Size)+diskReserve {
		return fmt.Errorf("not enough free disk in the temp dir (%d bytes free, %d needed)", avail, head.Size) // transient: try later
	}
	dir, err := os.MkdirTemp(base, "job-")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)             // on every exit path, including panics and cancellation
	path := filepath.Join(dir, "input") // no extension: the name must never steer the demuxer

	p.Report("downloading", 10)
	if err := m.download(ctx, key, path, head.Size); err != nil {
		return err
	}

	p.Report("probing", 60)
	info, err := m.Prober.Probe(ctx, path)
	if err == nil {
		err = m.Policy.Validate(info)
	}
	if err != nil {
		if rej, ok := inspect.AsRejection(err); ok {
			log.Warn("input refused", "code", rej.Code, "detail", rej.Detail) // restricted log: detail is sanitised, no paths
			return Permanent(rej.Code, errors.New(rej.Message))
		}
		return err // ffprobe missing, context cancelled...: not the file's fault
	}

	p.Report("saving", 90)
	if err := m.Store.SaveInspection(ctx, job, info); err != nil {
		return err
	}
	log.Info("metadata saved", "container", info.Container, "type", info.VideoType())
	return nil
}

func (m *MetadataProcessor) download(ctx context.Context, key, path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cw := &capWriter{w: f, max: size}
	_, dlErr := m.Src.Download(ctx, key, cw)
	closeErr := f.Close()
	switch {
	case errors.Is(dlErr, errTooManyBytes):
		return Permanent(inspect.CodeCorruptInput, dlErr)
	case dlErr != nil:
		return fmt.Errorf("storage download: %w", dlErr) // transient
	case closeErr != nil:
		return closeErr
	case cw.n != size:
		return fmt.Errorf("storage download ended after %d of %d bytes", cw.n, size) // transient
	}
	return nil
}

// original reads where the uploaded file lives. A video that is gone, deleted or no longer PROCESSING ends the job.
func (s *Store) original(ctx context.Context, videoID string) (key string, size *int64, err error) {
	var status string
	var deleted bool
	var k *string
	err = s.pool.QueryRow(ctx, `SELECT original_storage_key, file_size_bytes, status::text, deleted_at IS NOT NULL
FROM videos WHERE id = $1::uuid`, videoID).Scan(&k, &size, &status, &deleted)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil, Permanent(CodeVideoGone, err)
	case err != nil:
		return "", nil, err
	case deleted || status != "PROCESSING":
		return "", nil, Permanent(CodeVideoGone, fmt.Errorf("video is %s (deleted: %v)", status, deleted))
	case k == nil || *k == "":
		return "", nil, Permanent(CodeSourceMissing, errors.New("the video has no original"))
	}
	return *k, size, nil
}

// SaveInspection records the technical metadata of the original and queues the HLS job, in one transaction. It is
// fenced by the lease (a worker that lost its job writes nothing) and idempotent: running it again changes the
// metadata in place and never queues a second active HLS job.
func (s *Store) SaveInspection(ctx context.Context, job Job, info *inspect.Info) error {
	v := info.Video
	if v == nil {
		return errors.New("processing: SaveInspection needs a video stream")
	}
	var fps *float64
	if v.FPS > 0 {
		r := math.Round(v.FPS*1000) / 1000
		fps = &r
	}
	var bitrate *int64
	if info.BitRate > 0 {
		bitrate = &info.BitRate
	}
	var audio *string
	if info.Audio != nil {
		audio = &info.Audio.Codec
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		status, owner, _, _, _, err := lock(ctx, tx, job.ID)
		if err != nil {
			return err
		}
		if status != Processing || owner != job.WorkerID {
			return ErrLeaseLost
		}
		if _, err := tx.Exec(ctx, `INSERT INTO video_metadata (video_id, width, height, fps, video_codec, audio_codec, bitrate, container, has_audio)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (video_id) DO UPDATE SET width = EXCLUDED.width, height = EXCLUDED.height, fps = EXCLUDED.fps,
  video_codec = EXCLUDED.video_codec, audio_codec = EXCLUDED.audio_codec, bitrate = EXCLUDED.bitrate,
  container = EXCLUDED.container, has_audio = EXCLUDED.has_audio, updated_at = now()`,
			job.VideoID, v.DisplayWidth, v.DisplayHeight, fps, v.Codec, audio, bitrate, info.Container, info.Audio != nil); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO video_processing_jobs (video_id, job_type, processing_version)
SELECT v.id, 'HLS', $2 FROM videos v WHERE v.id = $1::uuid AND v.status = 'PROCESSING' AND v.deleted_at IS NULL
ON CONFLICT (video_id, job_type) WHERE status IN ('QUEUED', 'PROCESSING') DO NOTHING`, job.VideoID, job.ProcessingVersion)
		return err
	})
}
