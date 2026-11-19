package inspect

import "time"

// Policy is the configurable input policy. Zero values are not "unlimited": use DefaultPolicy and change fields.
type Policy struct {
	MaxSizeBytes      int64
	MinDuration       time.Duration
	MaxDuration       time.Duration
	MaxStreams        int // all streams of any kind, a stream-count bomb guard
	MaxSide           int // longest side the file may declare (coded or display), an absurd-dimension guard
	MaxDisplayLong    int // display size limit, the database "max 1080p" rule: short side <= MaxDisplayShort
	MaxDisplayShort   int // and long side <= MaxDisplayLong
	MinSide           int // smallest accepted side; anything below is not a usable picture
	MaxFPS            float64
	ShortMaxSeconds   int   // SHORT iff duration (rounded up) <= this
	ShortMaxSizeBytes int64 // a SHORT must not be larger than this (database CHECK)
	Containers        []string
	VideoCodecs       []string
	AudioCodecs       []string
}

// DefaultPolicy matches the database limits (videos_size_limits, videos_duration_range, video_metadata_max_1080p,
// video_metadata_fps) and the containers Phase 5 accepts. The duration cap of 4 hours is a product choice; the
// database allows up to 24 hours.
func DefaultPolicy() Policy {
	return Policy{
		MaxSizeBytes:      5 << 30,
		MinDuration:       500 * time.Millisecond,
		MaxDuration:       4 * time.Hour,
		MaxStreams:        16,
		MaxSide:           16384,
		MaxDisplayLong:    1920,
		MaxDisplayShort:   1080,
		MinSide:           16,
		MaxFPS:            240,
		ShortMaxSeconds:   60,
		ShortMaxSizeBytes: 200 << 20,
		Containers:        []string{"mp4", "matroska"},
		VideoCodecs:       []string{"h264", "hevc", "vp8", "vp9", "av1", "mpeg4"},
		AudioCodecs:       []string{"aac", "mp3", "opus", "vorbis", "ac3", "eac3", "flac", "alac", "pcm_s16le", "pcm_s24le"},
	}
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Validate checks an Info against the policy and returns a *Rejection or nil. The order is cheapest and most
// specific first, so a hostile file is refused before anything is computed from its claims.
func (p Policy) Validate(i *Info) error {
	switch {
	case i.SizeBytes > p.MaxSizeBytes:
		return reject(CodeTooLarge, "size %d > %d", i.SizeBytes, p.MaxSizeBytes)
	case i.SizeBytes <= 0:
		return reject(CodeCorruptInput, "empty file")
	case i.Streams > p.MaxStreams:
		return reject(CodeTooManyStreams, "%d streams > %d", i.Streams, p.MaxStreams)
	case !has(p.Containers, i.Container):
		return reject(CodeUnsupportedMedia, "container %q (%v)", i.Container, i.FormatNames)
	case i.Video == nil:
		return reject(CodeNoVideoStream, "no real video stream among %d streams", i.Streams)
	}
	v := i.Video
	switch {
	case !has(p.VideoCodecs, v.Codec):
		return reject(CodeUnsupportedCodec, "video codec %q", v.Codec)
	case i.Audio != nil && !has(p.AudioCodecs, i.Audio.Codec):
		return reject(CodeUnsupportedCodec, "audio codec %q", i.Audio.Codec)
	case v.Width <= 0 || v.Height <= 0 || v.DisplayWidth <= 0 || v.DisplayHeight <= 0:
		return reject(CodeDimensionsInvalid, "%dx%d (display %dx%d)", v.Width, v.Height, v.DisplayWidth, v.DisplayHeight)
	case max(v.Width, v.Height) > p.MaxSide || max(v.DisplayWidth, v.DisplayHeight) > 4*p.MaxSide:
		return reject(CodeResolutionTooHigh, "%dx%d (display %dx%d) beyond sanity limit", v.Width, v.Height, v.DisplayWidth, v.DisplayHeight)
	case min(v.DisplayWidth, v.DisplayHeight) < p.MinSide:
		return reject(CodeDimensionsInvalid, "display %dx%d below minimum side %d", v.DisplayWidth, v.DisplayHeight, p.MinSide)
	case min(v.DisplayWidth, v.DisplayHeight) > p.MaxDisplayShort || max(v.DisplayWidth, v.DisplayHeight) > p.MaxDisplayLong:
		return reject(CodeResolutionTooHigh, "display %dx%d", v.DisplayWidth, v.DisplayHeight)
	case v.FPS > p.MaxFPS:
		return reject(CodeFPSTooHigh, "fps %.3f > %.0f", v.FPS, p.MaxFPS)
	case i.DurationSeconds <= 0:
		return reject(CodeCorruptInput, "no usable duration")
	case i.DurationSeconds < p.MinDuration.Seconds():
		return reject(CodeTooShort, "duration %.3fs", i.DurationSeconds)
	case i.DurationSeconds > p.MaxDuration.Seconds():
		return reject(CodeTooLong, "duration %.1fs", i.DurationSeconds)
	case i.DurationSecondsInt() <= p.ShortMaxSeconds && i.SizeBytes > p.ShortMaxSizeBytes:
		return reject(CodeShortTooLarge, "%ds and %d bytes", i.DurationSecondsInt(), i.SizeBytes)
	}
	return nil
}
