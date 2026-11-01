package captions

import (
	"errors"
	"strings"
	"testing"
)

func TestSRTConverts(t *testing.T) {
	in := "\xef\xbb\xbf1\r\n00:00:01,000 --> 00:00:02,500\r\nHello\r\nworld\r\n\r\n2\r\n00:00:03,5 --> 00:00:04,000\r\nBye\r\n"
	got, err := ToVTT([]byte(in), true)
	if err != nil {
		t.Fatal(err)
	}
	want := "WEBVTT\n\n00:00:01.000 --> 00:00:02.500\nHello\nworld\n\n00:00:03.500 --> 00:00:04.000\nBye\n"
	if string(got) != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSRTWithoutCueNumbers(t *testing.T) {
	got, err := ToVTT([]byte("0:00:01,000 --> 0:00:02,000\nHi\n"), true)
	if err != nil || !strings.Contains(string(got), "00:00:01.000 --> 00:00:02.000\nHi") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSRTArrowInTextIsNeutralised(t *testing.T) {
	got, err := ToVTT([]byte("1\n00:00:01,000 --> 00:00:02,000\na --> b\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), "-->") != 1 {
		t.Fatalf("cue text must not contain a second timing arrow: %q", got)
	}
}

func TestSRTWithNoCuesRejected(t *testing.T) {
	for _, in := range []string{"", "hello world", "1\nnot a timing\ntext\n", "1\n00:00:01,000 --> 00:00:02,000\n\n"} {
		if _, err := ToVTT([]byte(in), true); !errors.Is(err, ErrFormat) {
			t.Errorf("%q: want ErrFormat, got %v", in, err)
		}
	}
}

func TestVTT(t *testing.T) {
	ok := "WEBVTT\n\n00:01.000 --> 00:02.000\nHi\n"
	if got, err := ToVTT([]byte(ok), false); err != nil || string(got) != ok {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := ToVTT([]byte("WEBVTT - title\n\n00:00:01.000 --> 00:00:02.000\nHi"), false); err != nil {
		t.Errorf("header with a title must be accepted: %v", err)
	}
	for _, bad := range []string{"", "Hi", "WEBVTTX\n\n00:01.000 --> 00:02.000\nHi", "WEBVTT\n\nno cues here\n", "<html>WEBVTT"} {
		if _, err := ToVTT([]byte(bad), false); !errors.Is(err, ErrFormat) {
			t.Errorf("%q: want ErrFormat, got %v", bad, err)
		}
	}
}

func TestEncodingAndSize(t *testing.T) {
	if _, err := ToVTT([]byte("WEBVTT\n\x00"), false); !errors.Is(err, ErrEncoding) {
		t.Errorf("NUL byte: %v", err)
	}
	if _, err := ToVTT([]byte("WEBVTT\n\xff\xfe"), false); !errors.Is(err, ErrEncoding) {
		t.Errorf("invalid UTF-8: %v", err)
	}
	if _, err := ToVTT(make([]byte, MaxBytes+1), false); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
}
