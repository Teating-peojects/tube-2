package worker

import (
	"regexp"
	"strings"
	"unicode"
)

var diagPath = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[/\\][^\s:'"()\[\]]+){2,}`)

// sanitizeDiag makes ffmpeg/scanner output safe to log and store: absolute paths (they reveal the
// scratch layout and file names), control characters and runs of whitespace are removed, the length
// is capped. It never goes into an API response: users only ever see Code/Message of a JobError.
func sanitizeDiag(s string) string {
	s = diagPath.ReplaceAllString(s, "<path>")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 400 {
		s = s[:400] + "..."
	}
	return s
}
