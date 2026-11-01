package pagination

import (
	"encoding/base64"
	"strings"
	"testing"
)

type c struct {
	K string `json:"k"`
	N int64  `json:"n"`
}

func TestRoundTrip(t *testing.T) {
	s, err := Encode(c{K: "latest", N: 42})
	if err != nil {
		t.Fatal(err)
	}
	var out c
	if err := Decode(s, &out); err != nil || out.K != "latest" || out.N != 42 {
		t.Fatalf("round trip failed: %v %+v", err, out)
	}
	if strings.ContainsAny(s, "+/=") {
		t.Fatalf("cursor must be URL-safe: %s", s)
	}
}

func TestDecodeRejectsBadInput(t *testing.T) {
	enc := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	bad := map[string]string{
		"empty":         "",
		"not base64":    "!!!",
		"not json":      enc("nope"),
		"unknown field": enc(`{"k":"a","n":1,"evil":true}`),
		"two values":    enc(`{"k":"a"} {"k":"b"}`),
		"wrong type":    enc(`{"k":1}`),
		"too long":      strings.Repeat("a", 400),
	}
	for name, s := range bad {
		var out c
		if err := Decode(s, &out); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
