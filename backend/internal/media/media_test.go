package media

import "testing"

func TestAssetURLs(t *testing.T) {
	key := "thumbs/a.jpg"
	if got := New("").Asset(&key); got != nil {
		t.Fatalf("no CDN configured must give nil, got %q", *got)
	}
	u := New("https://cdn.example.com/")
	if got := u.Asset(&key); got == nil || *got != "https://cdn.example.com/thumbs/a.jpg" {
		t.Fatalf("unexpected url %v", got)
	}
	if u.Asset(nil) != nil || u.AssetString("") != nil {
		t.Fatal("empty keys must give nil")
	}
	slash := "/leading/slash.jpg"
	if got := u.Asset(&slash); *got != "https://cdn.example.com/leading/slash.jpg" {
		t.Fatalf("double slash: %s", *got)
	}
}

func TestQuality(t *testing.T) {
	h := func(v int32) *int32 { return &v }
	cases := []struct {
		in   *int32
		want string
	}{{nil, "SD"}, {h(360), "SD"}, {h(480), "SD"}, {h(719), "SD"}, {h(720), "HD"}, {h(1080), "HD"}, {h(2160), "4K"}}
	for _, c := range cases {
		if got := Quality(c.in); got != c.want {
			t.Errorf("Quality(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}
