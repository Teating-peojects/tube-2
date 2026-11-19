package inspect

// Info is what ffprobe says about a file, normalised. SizeBytes is the real size on disk, not a claim of the file.
type Info struct {
	Container       string // "mp4" (mov family) or "matroska" (mkv/webm) or the raw first format name
	FormatNames     []string
	DurationSeconds float64
	SizeBytes       int64
	BitRate         int64
	Streams         int // all streams, any kind
	VideoStreams    int // real video streams (cover art excluded)
	AudioStreams    int
	Video           *VideoStream // first real video stream, nil when there is none
	Audio           *AudioStream // first audio stream, nil for a silent video (valid)
}

// VideoStream describes the first real video stream.
type VideoStream struct {
	Index         int
	Codec         string
	Profile       string
	PixFmt        string
	Width, Height int // coded size as stored
	// DisplayWidth/DisplayHeight are what a viewer sees: the sample aspect ratio is applied to the width (so
	// non-square pixels stretch it) and width and height swap for a 90/270 degree rotation.
	DisplayWidth, DisplayHeight int
	SARNum, SARDen              int     // 1:1 when unknown
	Rotation                    int     // degrees clockwise needed to show the picture upright: 0, 90, 180 or 270
	FPS                         float64 // average frame rate (0 = unknown)
	BitRate                     int64
}

// AudioStream describes the first audio stream.
type AudioStream struct {
	Index      int
	Codec      string
	SampleRate int
	Channels   int
	Layout     string
	BitRate    int64
}

// VideoType is the database video_type for this file: SHORT iff the duration, rounded up to whole seconds, is at
// most 60 (the same integer the finalize step stores in videos.duration_seconds).
func (i *Info) VideoType() string {
	if i.DurationSecondsInt() <= 60 {
		return "SHORT"
	}
	return "LONG"
}

// DurationSecondsInt is the duration rounded up to a whole second (at least 1 for any positive duration).
func (i *Info) DurationSecondsInt() int {
	s := int(i.DurationSeconds)
	if float64(s) < i.DurationSeconds {
		s++
	}
	return s
}
