package inspect

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

type rawStream struct {
	Index             int    `json:"index"`
	CodecType         string `json:"codec_type"`
	CodecName         string `json:"codec_name"`
	Profile           string `json:"profile"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	PixFmt            string `json:"pix_fmt"`
	RFrameRate        string `json:"r_frame_rate"`
	AvgFrameRate      string `json:"avg_frame_rate"`
	SampleAspectRatio string `json:"sample_aspect_ratio"`
	BitRate           string `json:"bit_rate"`
	Duration          string `json:"duration"`
	Channels          int    `json:"channels"`
	ChannelLayout     string `json:"channel_layout"`
	SampleRate        string `json:"sample_rate"`
	Disposition       struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
	Tags         map[string]string `json:"tags"`
	SideDataList []struct {
		Type     string   `json:"side_data_type"`
		Rotation *float64 `json:"rotation"`
	} `json:"side_data_list"`
}

type rawProbe struct {
	Streams []rawStream `json:"streams"`
	Format  struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
}

func num(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func integer(s string) int64 {
	f := num(s)
	if f < 0 || f > math.MaxInt64/2 {
		return 0
	}
	return int64(f)
}

// ratio parses "30000/1001" or "4:3". Zero denominators and "N/A" give ok == false.
func ratio(s string) (n, d float64, ok bool) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, "/:")
	if i < 0 {
		v := num(s)
		return v, 1, v > 0
	}
	n, d = num(s[:i]), num(s[i+1:])
	return n, d, n > 0 && d > 0
}

func fps(s rawStream) float64 {
	// avg_frame_rate is the real average; r_frame_rate is a timebase guess that is absurd (90000) for many
	// variable-frame-rate files, so it is only a fallback.
	for _, v := range []string{s.AvgFrameRate, s.RFrameRate} {
		if n, d, ok := ratio(v); ok {
			return n / d
		}
	}
	return 0
}

// rotation returns degrees clockwise, normalised to 0/90/180/270. ffprobe reports the display matrix as an angle
// counter-clockwise (av_display_rotation_get) and the legacy "rotate" tag as clockwise.
func rotation(s rawStream) int {
	deg := 0.0
	found := false
	for _, sd := range s.SideDataList {
		if sd.Rotation != nil {
			deg, found = -*sd.Rotation, true
			break
		}
	}
	if !found {
		if t, ok := s.Tags["rotate"]; ok {
			deg = num(t)
		}
	}
	r := int(math.Round(deg/90.0)) * 90 // snap odd angles to the nearest quarter turn
	return ((r % 360) + 360) % 360
}

// Parse turns the JSON of `ffprobe -print_format json -show_format -show_streams` into an Info (SizeBytes is
// left for the caller, who knows the real file size).
func Parse(data []byte) (*Info, error) {
	var raw rawProbe
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	info := &Info{Streams: len(raw.Streams)}
	for _, n := range strings.Split(raw.Format.FormatName, ",") {
		if n = strings.TrimSpace(n); n != "" {
			info.FormatNames = append(info.FormatNames, n)
		}
	}
	info.Container = container(info.FormatNames)
	info.DurationSeconds = num(raw.Format.Duration)
	info.BitRate = integer(raw.Format.BitRate)

	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic == 1 {
				continue // cover art is not a video track
			}
			info.VideoStreams++
			if info.Video == nil {
				info.Video = videoStream(s)
			}
		case "audio":
			info.AudioStreams++
			if info.Audio == nil {
				info.Audio = &AudioStream{Index: s.Index, Codec: s.CodecName, SampleRate: int(integer(s.SampleRate)), Channels: s.Channels,
					Layout: s.ChannelLayout, BitRate: integer(s.BitRate)}
			}
		}
	}
	if info.DurationSeconds <= 0 { // some containers only carry stream durations
		for _, s := range raw.Streams {
			if d := num(s.Duration); d > info.DurationSeconds {
				info.DurationSeconds = d
			}
		}
	}
	return info, nil
}

func container(names []string) string {
	for _, n := range names {
		switch n {
		case "mov", "mp4":
			return "mp4"
		case "matroska", "webm":
			return "matroska"
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func videoStream(s rawStream) *VideoStream {
	v := &VideoStream{Index: s.Index, Codec: s.CodecName, Profile: s.Profile, PixFmt: s.PixFmt, Width: s.Width, Height: s.Height,
		SARNum: 1, SARDen: 1, FPS: fps(s), BitRate: integer(s.BitRate), Rotation: rotation(s)}
	sar := 1.0
	if n, d, ok := ratio(s.SampleAspectRatio); ok {
		v.SARNum, v.SARDen, sar = int(n), int(d), n/d
	}
	w, h := float64(s.Width), float64(s.Height)
	if sar > 1 {
		w = math.Round(w * sar)
	} else if sar < 1 {
		h = math.Round(h / sar)
	}
	v.DisplayWidth, v.DisplayHeight = int(w), int(h)
	if v.Rotation == 90 || v.Rotation == 270 {
		v.DisplayWidth, v.DisplayHeight = v.DisplayHeight, v.DisplayWidth
	}
	return v
}
