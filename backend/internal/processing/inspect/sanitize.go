package inspect

import "strings"

// Sanitize makes tool output safe for a restricted log line: the given paths are replaced by placeholders,
// control characters are dropped, runs of whitespace collapse, and the result is truncated.
func Sanitize(s string, paths ...string) string {
	for _, p := range paths {
		if p != "" {
			s = strings.ReplaceAll(s, p, "<path>")
		}
	}
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == ' ':
			space = true
			continue
		case r < 0x20 || r == 0x7f:
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
		if b.Len() >= 400 {
			break
		}
	}
	return b.String()
}
