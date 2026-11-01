// Package captions validates uploaded subtitle files and turns them into
// WebVTT. The schema promises that every stored caption is a WebVTT file, so an
// uploaded .srt is converted here and nothing else is ever stored.
//
// Captions are shown by the browser's own <track> support, which renders cue
// text as plain text plus a small set of safe tags, never as script. Even so
// the file is parsed, size-limited and rewritten (not passed through blindly).
package captions

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxBytes is the largest caption file accepted (matches the frontend limit).
const MaxBytes = 1 << 20

// MaxCues bounds the work and the size of the output.
const MaxCues = 20000

var (
	ErrTooLarge = errors.New("captions: file too large")
	ErrEncoding = errors.New("captions: file is not valid UTF-8 text")
	ErrFormat   = errors.New("captions: not a valid caption file")
)

var (
	srtTiming = regexp.MustCompile(`^(\d{1,3}:\d{2}:\d{2}[,.]\d{1,3})\s*-->\s*(\d{1,3}:\d{2}:\d{2}[,.]\d{1,3})`)
	vttTiming = regexp.MustCompile(`^((?:\d{1,3}:)?\d{2}:\d{2}\.\d{3})\s*-->\s*((?:\d{1,3}:)?\d{2}:\d{2}\.\d{3})`)
	allDigits = regexp.MustCompile(`^\d+$`)
)

// ToVTT validates raw and returns WebVTT. srt selects the SubRip parser.
func ToVTT(raw []byte, srt bool) ([]byte, error) {
	if len(raw) > MaxBytes {
		return nil, ErrTooLarge
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // UTF-8 byte order mark
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, ErrEncoding
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	if srt {
		return srtToVTT(text)
	}
	return cleanVTT(text)
}

func cleanVTT(text string) ([]byte, error) {
	if !strings.HasPrefix(text, "WEBVTT") {
		return nil, ErrFormat
	}
	if len(text) > 6 && text[6] != ' ' && text[6] != '\t' && text[6] != '\n' {
		return nil, ErrFormat // "WEBVTTX" is not a valid signature
	}
	cues := 0
	for _, line := range strings.Split(text, "\n") {
		if vttTiming.MatchString(line) {
			cues++
		}
	}
	if cues == 0 || cues > MaxCues {
		return nil, ErrFormat
	}
	return []byte(strings.TrimRight(text, "\n") + "\n"), nil
}

func srtToVTT(text string) ([]byte, error) {
	var out strings.Builder
	out.WriteString("WEBVTT\n\n")
	cues := 0
	for _, block := range strings.Split(strings.TrimSpace(text), "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		i := 0
		if i < len(lines) && allDigits.MatchString(strings.TrimSpace(lines[i])) {
			i++ // cue number
		}
		if i >= len(lines) {
			continue
		}
		m := srtTiming.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if m == nil {
			continue
		}
		var body []string
		for _, l := range lines[i+1:] {
			// "-->" inside cue text would be read as a new timing line
			body = append(body, strings.ReplaceAll(strings.TrimRight(l, " \t"), "-->", "--&gt;"))
		}
		if strings.TrimSpace(strings.Join(body, "")) == "" {
			continue
		}
		cues++
		if cues > MaxCues {
			return nil, ErrFormat
		}
		out.WriteString(vttTime(m[1]) + " --> " + vttTime(m[2]) + "\n" + strings.Join(body, "\n") + "\n\n")
	}
	if cues == 0 {
		return nil, ErrFormat
	}
	return []byte(strings.TrimRight(out.String(), "\n") + "\n"), nil
}

// vttTime turns 00:01:02,5 into 00:01:02.500 (VTT needs a dot and 3 digits).
func vttTime(s string) string {
	s = strings.Replace(s, ",", ".", 1)
	dot := strings.IndexByte(s, '.')
	frac := s[dot+1:]
	for len(frac) < 3 {
		frac += "0"
	}
	head := s[:dot]
	if len(head) < 8 { // H:MM:SS -> HH:MM:SS
		head = strings.Repeat("0", 8-len(head)) + head
	}
	return head + "." + frac
}
