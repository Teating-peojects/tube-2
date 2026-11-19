package inspect

import (
	"encoding/binary"
	"os"
	"strings"
)

// isoTruncated walks the top-level boxes of an MP4/MOV file and reports whether one of them claims more bytes than the
// file holds. ffprobe does not notice this when the index (moov) sits in front of the media data (the common
// "faststart" layout): the header still describes the whole video although its tail is missing.
// A box with size 0 runs to the end of the file, which is valid; a box smaller than its own header is invalid.
func isoTruncated(path string, fileSize int64) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()
	var hdr [16]byte
	off := int64(0)
	for boxes := 0; off < fileSize; boxes++ {
		if boxes > 100000 {
			return true, "too many top-level boxes"
		}
		if fileSize-off < 8 {
			return true, "box header cut off at the end of the file"
		}
		if _, err := f.ReadAt(hdr[:8], off); err != nil {
			return true, "unreadable box header"
		}
		size := int64(binary.BigEndian.Uint32(hdr[:4]))
		header := int64(8)
		switch {
		case size == 0:
			return false, "" // extends to the end of the file
		case size == 1:
			if fileSize-off < 16 {
				return true, "64-bit box header cut off"
			}
			if _, err := f.ReadAt(hdr[8:16], off+8); err != nil {
				return true, "unreadable 64-bit box header"
			}
			u := binary.BigEndian.Uint64(hdr[8:16])
			if u > uint64(fileSize)+1 { // also catches values that would overflow int64
				return true, "box " + string(hdr[4:8]) + " claims more bytes than the file holds"
			}
			size, header = int64(u), 16
		}
		if size < header {
			return true, "box with an impossible size"
		}
		if off+size > fileSize {
			return true, "box " + printable(hdr[4:8]) + " runs past the end of the file"
		}
		off += size
	}
	return false, ""
}

func printable(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7f {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('?')
		}
	}
	return sb.String()
}

// truncationHint recognises the messages ffprobe prints (at error level) when the end of a file is missing.
func truncationHint(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "ended prematurely") || strings.Contains(s, "truncated") || strings.Contains(s, "unexpected end")
}

// vint reads an EBML variable-length number. For element sizes the marker bit is removed; unknown reports the
// reserved "all value bits set" size that live recordings use.
func vint(b []byte, keepMarker bool) (val uint64, n int, unknown, ok bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false, false
	}
	n = 1
	for mask := byte(0x80); b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 || len(b) < n {
		return 0, 0, false, false
	}
	first := b[0]
	if !keepMarker {
		first &= byte(0xFF >> uint(n))
	}
	val = uint64(first)
	allOnes := first == byte(0xFF>>uint(n))
	for _, c := range b[1:n] {
		val = val<<8 | uint64(c)
		allOnes = allOnes && c == 0xFF
	}
	return val, n, allOnes && !keepMarker, true
}

// mkvTruncated checks a Matroska/WebM file the same way: the EBML header, then the Segment, whose declared size
// must fit in the file. A Segment of unknown size (live recordings) cannot be judged here.
func mkvTruncated(path string, fileSize int64) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()
	b := make([]byte, 256)
	n, _ := f.ReadAt(b, 0)
	b = b[:n]
	id, idLen, _, ok := vint(b, true)
	if !ok || id != 0x1A45DFA3 {
		return false, ""
	}
	size, sizeLen, _, ok := vint(b[idLen:], false)
	if !ok || size > uint64(fileSize) {
		return true, "EBML header larger than the file"
	}
	pos := int64(idLen+sizeLen) + int64(size)
	if pos > fileSize {
		return true, "EBML header runs past the end of the file"
	}
	if pos >= int64(len(b)) {
		return false, "" // header unusually large: leave it to ffprobe
	}
	sid, sidLen, _, ok := vint(b[pos:], true)
	if !ok || sid != 0x18538067 {
		return false, ""
	}
	ssize, ssizeLen, unknown, ok := vint(b[pos+int64(sidLen):], false)
	if !ok || unknown {
		return false, ""
	}
	if ssize > uint64(fileSize) || pos+int64(sidLen+ssizeLen)+int64(ssize) > fileSize {
		return true, "Segment runs past the end of the file"
	}
	return false, ""
}
