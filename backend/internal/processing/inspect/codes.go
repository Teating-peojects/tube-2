// Package inspect looks at an uploaded media file before anything expensive happens to it: it runs ffprobe safely,
// turns its JSON into a typed Info and checks the result against a Policy. A file that breaks the policy yields a
// *Rejection with a safe error code and a fixed message; raw ffprobe output only ever goes into Rejection.Detail,
// which is for restricted logs and is never shown to a user or stored in the database.
package inspect

import (
	"errors"
	"fmt"
)

// Safe error codes. Every code matches the database CHECK on video_processing_jobs.error_code.
const (
	CodeUnsupportedMedia  = "unsupported_media"   // not a video container we accept (wrong format, text file, ...)
	CodeCorruptInput      = "corrupt_input"       // looks like an accepted container but cannot be read
	CodeNoVideoStream     = "no_video_stream"     // audio-only, image-only, or cover-art-only
	CodeTooLong           = "too_long"            // longer than the policy allows
	CodeTooShort          = "too_short"           // shorter than the policy allows
	CodeTooLarge          = "too_large"           // file bigger than the policy allows
	CodeResolutionTooHigh = "resolution_too_high" // larger than 1080p (the database limit)
	CodeDimensionsInvalid = "dimensions_invalid"  // zero, negative or absurd dimensions
	CodeFPSTooHigh        = "fps_too_high"        // frame rate above the limit
	CodeTooManyStreams    = "too_many_streams"    // stream-count bomb
	CodeUnsupportedCodec  = "unsupported_codec"   // video or audio codec outside the allow-list
	CodeShortTooLarge     = "short_too_large"     // <= 60 s but > 200 MiB: cannot be stored as SHORT
	CodeProcessingTimeout = "processing_timeout"  // the inspection itself took too long
)

// AllCodes lists every code this package can return.
var AllCodes = []string{
	CodeUnsupportedMedia, CodeCorruptInput, CodeNoVideoStream, CodeTooLong, CodeTooShort, CodeTooLarge,
	CodeResolutionTooHigh, CodeDimensionsInvalid, CodeFPSTooHigh, CodeTooManyStreams, CodeUnsupportedCodec,
	CodeShortTooLarge, CodeProcessingTimeout,
}

var messages = map[string]string{
	CodeUnsupportedMedia:  "This file is not a supported video.",
	CodeCorruptInput:      "The video file is damaged or incomplete.",
	CodeNoVideoStream:     "The file has no video track.",
	CodeTooLong:           "The video is longer than the allowed duration.",
	CodeTooShort:          "The video is too short.",
	CodeTooLarge:          "The file is larger than the allowed size.",
	CodeResolutionTooHigh: "The video resolution is higher than 1080p.",
	CodeDimensionsInvalid: "The video dimensions are not valid.",
	CodeFPSTooHigh:        "The video frame rate is too high.",
	CodeTooManyStreams:    "The file contains too many tracks.",
	CodeUnsupportedCodec:  "The video or audio format is not supported.",
	CodeShortTooLarge:     "Videos of 60 seconds or less must be 200 MiB or smaller.",
	CodeProcessingTimeout: "Checking the file took too long.",
}

// Rejection is a file the policy refuses. Message is fixed per code and safe to show; Detail is not.
type Rejection struct {
	Code    string
	Message string
	Detail  string // sanitised, for restricted logs only
}

func (r *Rejection) Error() string { return r.Code + ": " + r.Message }

func reject(code, format string, args ...any) *Rejection {
	return &Rejection{Code: code, Message: messages[code], Detail: fmt.Sprintf(format, args...)}
}

// AsRejection returns the *Rejection inside err, if any.
func AsRejection(err error) (*Rejection, bool) {
	var r *Rejection
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}
