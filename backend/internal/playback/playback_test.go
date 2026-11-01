package playback

import (
	"strings"
	"testing"
	"time"
)

const (
	vid  = "0190a5d2-7c1e-7b3a-9d4f-1a2b3c4d5e6f"
	vid2 = "0190a5d2-7c1e-7b3a-9d4f-ffffffffffff"
)

var key = []byte(strings.Repeat("k", 32))

func signer(t *testing.T, now time.Time) *Signer {
	s, err := NewSigner(key, "https://stream.example.com/", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	return s
}

func TestRoundTrip(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := signer(t, now)
	u := s.URL(vid, "hls/"+vid+"/master.m3u8")
	if !strings.HasPrefix(u, "https://stream.example.com/") || !strings.HasSuffix(u, "/hls/"+vid+"/master.m3u8") {
		t.Fatalf("unexpected URL %s", u)
	}
	token := strings.Split(strings.TrimPrefix(u, "https://stream.example.com/"), "/")[0]
	for _, obj := range []string{"hls/" + vid + "/master.m3u8", "hls/" + vid + "/720p/index.m3u8", "hls/" + vid + "/1080p/seg_00042.ts"} {
		got, err := Verify(key, token, obj, now.Add(time.Minute))
		if err != nil || got != vid {
			t.Errorf("Verify(%s) = %q, %v", obj, got, err)
		}
	}
}

func TestRejects(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := signer(t, now)
	token := s.Token(vid)
	master := "hls/" + vid + "/master.m3u8"

	if _, err := Verify(key, token, master, now.Add(2*time.Hour)); err == nil {
		t.Error("expired token must fail")
	}
	if _, err := Verify(key, token, "hls/"+vid2+"/master.m3u8", now); err == nil {
		t.Error("token for another video must fail")
	}
	if _, err := Verify([]byte(strings.Repeat("x", 32)), token, master, now); err == nil {
		t.Error("wrong key must fail")
	}
	// tampered expiry
	parts := strings.SplitN(token, ".", 2)
	if _, err := Verify(key, "9999999999."+parts[1], master, now); err == nil {
		t.Error("extended expiry must fail")
	}
	for _, bad := range []string{"", ".", "abc", "1.", ".abc", "x.y"} {
		if _, err := Verify(key, bad, master, now); err == nil {
			t.Errorf("token %q must fail", bad)
		}
	}
}

func TestOnlyServableObjects(t *testing.T) {
	bad := []string{
		"src/u/" + vid + "/source.mp4",
		"hls/" + vid + "/../../etc/passwd",
		"hls/" + vid + "/master.m3u8/extra",
		"hls/" + vid + "/4k/index.m3u8",
		"hls/" + vid + "/720p/seg_1.ts",
		"hls/not-a-uuid/master.m3u8",
		"pub/videos/" + vid + "/thumb.jpg",
		"/hls/" + vid + "/master.m3u8",
		"hls/" + vid + "/master.m3u8\n",
	}
	for _, k := range bad {
		if _, ok := ObjectVideo(k); ok {
			t.Errorf("%q must not be servable", k)
		}
	}
	s := signer(t, time.Now())
	if s.URL(vid, "hls/"+vid2+"/master.m3u8") != "" {
		t.Error("URL must refuse a master key that belongs to another video")
	}
}

func TestShortKeyRejected(t *testing.T) {
	if _, err := NewSigner([]byte("short"), "https://x", 0); err == nil {
		t.Fatal("short key must be rejected")
	}
}
