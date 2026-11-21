package service

// failureMessages maps the stable failure codes written by the media worker (the part of
// video_processing_jobs.error_message before the colon) to text a creator can act on. Only codes in this
// table are ever shown: anything else (ffmpeg failed, storage problem, scanner unavailable...) is an
// internal condition and gets the generic sentence, so no internal detail can reach a client.
var failureMessages = map[string]string{
	"too_long":              "That video is longer than the maximum allowed length. Trim it and upload it again.",
	"too_large":             "That file is larger than the maximum allowed size.",
	"short_too_large":       "Videos up to 60 seconds can be at most 200 MB.",
	"resolution_too_high":   "That video's resolution is above the maximum of 1080p. Export it at 1080p or lower.",
	"resolution_too_low":    "That video is too small to process.",
	"frame_rate_too_high":   "That video's frame rate is higher than the maximum allowed.",
	"unsupported_container": "Only MP4, MOV, MKV and WebM files are supported.",
	"unsupported_codec":     "That video's codec isn't supported. Export it as H.264 or HEVC.",
	"no_video_stream":       "That file doesn't contain a video track.",
	"invalid_file":          "That file couldn't be read as a video. It may be damaged.",
	"invalid_duration":      "We couldn't read the length of that video. It may be damaged.",
	"too_many_streams":      "That file has too many tracks. Export a simpler file.",
	"size_mismatch":         "The uploaded file doesn't match its declared size. Please upload it again.",
	"original_missing":      "The uploaded file is missing. Please upload it again.",
	"cancelled":             "Processing was cancelled.",
}

const genericProcessingFailure = "We couldn't process this video. Try uploading it again."

// failureMessage returns the creator-facing sentence for a worker failure code.
func failureMessage(code string) string {
	if m, ok := failureMessages[code]; ok {
		return m
	}
	return genericProcessingFailure
}
