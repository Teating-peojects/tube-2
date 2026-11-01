package handler_test

// Integration tests for the read API against a real PostgreSQL database
// created from db/migrations. See fixtures_test.go for how to run them.

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func (e *env) get(path string) (int, map[string]any, []byte) {
	e.t.Helper()
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	return rr.Code, m, rr.Body.Bytes()
}

func ids(m map[string]any) []string {
	var out []string
	for _, it := range m["items"].([]any) {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// walk follows nextCursor to the end and returns every id in order.
func (e *env) walk(path string) []string {
	e.t.Helper()
	var all []string
	cursor := ""
	for i := 0; i < 50; i++ {
		p := path
		if cursor != "" {
			sep := "?"
			if strings.Contains(p, "?") {
				sep = "&"
			}
			p += sep + "cursor=" + url.QueryEscape(cursor)
		}
		code, m, raw := e.get(p)
		if code != 200 {
			e.t.Fatalf("GET %s = %d %s", p, code, raw)
		}
		all = append(all, ids(m)...)
		next, _ := m["nextCursor"].(string)
		if next == "" {
			return all
		}
		cursor = next
	}
	e.t.Fatal("pagination did not terminate")
	return nil
}

const recent = "now() - interval '1 day'"

// A catalogue covering every way a video can be non-public.
func (e *env) visibilityFixture() {
	e.creator(1, "Zebra Studio", "ACTIVE", false, 500)
	e.creator(2, "Suspended Creator", "SUSPENDED", false, 900)
	e.creator(3, "Deleted Creator", "ACTIVE", true, 900)
	e.video(vopt{n: 1, creator: 1, title: "zebra public", published: recent, views: 100})
	e.video(vopt{n: 2, creator: 1, title: "zebra unlisted", visibility: "UNLISTED", published: recent, views: 100})
	e.video(vopt{n: 3, creator: 1, title: "zebra private", visibility: "PRIVATE", published: recent, views: 100})
	e.video(vopt{n: 4, creator: 1, title: "zebra draft", status: "PROCESSING", visibility: "PUBLIC", views: 100})
	e.video(vopt{n: 5, creator: 1, title: "zebra removed", status: "REMOVED", published: recent, views: 100})
	e.video(vopt{n: 6, creator: 1, title: "zebra soft deleted", published: recent, views: 100, deleted: true})
	e.video(vopt{n: 7, creator: 2, title: "zebra suspended creator", published: recent, views: 100})
	e.video(vopt{n: 8, creator: 3, title: "zebra deleted creator", published: recent, views: 100})
	e.video(vopt{n: 9, creator: 1, title: "zebra short", kind: "SHORT", dur: 30, published: recent, views: 100})
}

func TestOnlyPublicVideosAreEverListed(t *testing.T) {
	e := setup(t)
	e.visibilityFixture()

	hidden := []string{vid(2), vid(3), vid(4), vid(5), vid(6), vid(7), vid(8)}
	endpoints := []string{
		"/api/v1/videos", "/api/v1/videos?category=popular", "/api/v1/videos?category=new",
		"/api/v1/videos?category=trending", "/api/v1/videos?category=shorts", "/api/v1/videos?category=live",
		"/api/v1/shorts", "/api/v1/live",
		"/api/v1/trending?tab=now", "/api/v1/trending?tab=most-viewed", "/api/v1/trending?tab=fast-growing", "/api/v1/trending?tab=recent",
		"/api/v1/channels/" + cid(1) + "/videos?kind=videos", "/api/v1/channels/" + cid(1) + "/videos?kind=shorts",
		"/api/v1/channels/" + cid(1) + "/videos?kind=live",
	}
	for _, ep := range endpoints {
		code, m, raw := e.get(ep)
		if code != 200 {
			t.Fatalf("%s = %d %s", ep, code, raw)
		}
		got := ids(m)
		for _, h := range hidden {
			if contains(got, h) {
				t.Errorf("%s leaked non-public video %s", ep, h)
			}
		}
	}

	// the main feed shows exactly the plain public video (shorts have their own tab)
	_, m, _ := e.get("/api/v1/videos")
	if got := ids(m); len(got) != 1 || got[0] != vid(1) {
		t.Fatalf("feed = %v", got)
	}
	_, m, _ = e.get("/api/v1/shorts")
	if got := ids(m); len(got) != 1 || got[0] != vid(9) {
		t.Fatalf("shorts = %v", got)
	}
	// live streaming has no tables yet: the endpoint exists and is empty
	_, m, _ = e.get("/api/v1/live")
	if got := ids(m); len(got) != 0 {
		t.Fatalf("live = %v", got)
	}
}

func TestVideoDetailVisibility(t *testing.T) {
	e := setup(t)
	e.visibilityFixture()
	want := map[int]int{1: 200, 2: 200 /* unlisted: reachable by link */, 3: 404, 4: 404, 5: 404, 6: 404, 7: 404, 8: 404, 9: 200}
	for n, status := range want {
		if code, _, raw := e.get("/api/v1/videos/" + vid(n)); code != status {
			t.Errorf("video %d: got %d want %d (%s)", n, code, status, raw)
		}
	}
	code, m, _ := e.get("/api/v1/videos/" + vid(1))
	if code != 200 || m["title"] != "zebra public" || m["reaction"] != nil {
		t.Fatalf("detail body: %v", m)
	}
	if m["thumbnailUrl"] != "https://cdn.test/t/"+vid(1)+".jpg" || m["quality"] != "HD" {
		t.Fatalf("thumbnail/quality: %v / %v", m["thumbnailUrl"], m["quality"])
	}
	creator := m["creator"].(map[string]any)
	if creator["name"] != "Zebra Studio" || creator["verified"] != true || creator["isSubscribed"] != false {
		t.Fatalf("creator: %v", creator)
	}
	if code, _, _ := e.get("/api/v1/videos/" + vid(999)); code != 404 {
		t.Errorf("unknown id = %d", code)
	}
}

func TestVideoDetailStreamAndCaptions(t *testing.T) {
	e := setup(t)
	e.creator(1, "A", "ACTIVE", false, 1)
	e.video(vopt{n: 1, creator: 1, title: "has hls", published: recent})
	_, m, _ := e.get("/api/v1/videos/" + vid(1))
	// the stream URL is the processed master playlist (hls_master_key) on the CDN
	if m["streamUrl"] != "https://cdn.test/videos/"+vid(1)+"/hls/master.m3u8" {
		t.Fatalf("stream: %v", m["streamUrl"])
	}
	e.exec(`INSERT INTO video_captions (video_id, language, label, storage_key, status) VALUES ($1, 'en', 'English', 'caps/en.vtt', 'READY'), ($1, 'bn', 'Bangla', 'caps/bn.vtt', 'PENDING')`, vid(1))
	_, m, _ = e.get("/api/v1/videos/" + vid(1))
	caps := m["captions"].([]any)
	if len(caps) != 1 || caps[0].(map[string]any)["url"] != "https://cdn.test/caps/en.vtt" || caps[0].(map[string]any)["lang"] != "en" {
		t.Fatalf("only READY captions are exposed: %v", caps)
	}
}

func TestKeysetPaginationWithTiedSortKeys(t *testing.T) {
	e := setup(t)
	e.creator(1, "Tie Studio", "ACTIVE", false, 1)
	// 30 videos with IDENTICAL published_at and view_count: only the id breaks ties.
	for i := 1; i <= 30; i++ {
		e.video(vopt{n: i, creator: 1, title: fmt.Sprintf("tie %d", i), published: "TIMESTAMPTZ '2026-03-01 10:00:00+00'", views: 777})
	}
	for _, ep := range []string{"/api/v1/videos", "/api/v1/videos?category=popular", "/api/v1/channels/" + cid(1) + "/videos?kind=videos&sort=popular"} {
		got := e.walk(ep)
		seen := map[string]bool{}
		for _, id := range got {
			if seen[id] {
				t.Fatalf("%s returned %s twice", ep, id)
			}
			seen[id] = true
		}
		if len(got) != 30 {
			t.Fatalf("%s returned %d of 30 (skipped rows)", ep, len(got))
		}
	}
}

func TestCategoryAndPopularOrdering(t *testing.T) {
	e := setup(t)
	e.creator(1, "A", "ACTIVE", false, 1)
	e.video(vopt{n: 1, creator: 1, title: "m1", category: "music", published: recent, views: 10})
	e.video(vopt{n: 2, creator: 1, title: "m2", category: "music", published: recent, views: 900})
	e.video(vopt{n: 3, creator: 1, title: "g1", category: "gaming", published: recent, views: 500})
	_, m, _ := e.get("/api/v1/videos?category=music")
	if got := ids(m); len(got) != 2 || contains(got, vid(3)) {
		t.Fatalf("music = %v", got)
	}
	_, m, _ = e.get("/api/v1/videos?category=popular")
	if got := ids(m); len(got) != 3 || got[0] != vid(2) || got[1] != vid(3) || got[2] != vid(1) {
		t.Fatalf("popular order = %v", got)
	}
	if _, m, _ = e.get("/api/v1/videos?category=does-not-exist"); len(ids(m)) != 0 {
		t.Fatal("unknown category must be empty")
	}
	if code, _, _ := e.get("/api/v1/videos?category=" + url.QueryEscape("bad slug;")); code != 422 {
		t.Fatalf("malformed category = %d, want 422", code)
	}
}

func TestSearchOnlyFindsPublicAndFiltersWork(t *testing.T) {
	e := setup(t)
	e.visibilityFixture()
	e.video(vopt{n: 20, creator: 1, title: "zebra long documentary", dur: 2400, published: recent, views: 5})
	e.video(vopt{n: 21, creator: 1, title: "zebra old upload", published: "now() - interval '400 days'", views: 9000})

	_, m, _ := e.get("/api/v1/search?q=zebra&type=videos&sort=views")
	got := ids2(m, "video")
	for _, h := range []int{2, 3, 4, 5, 6, 7, 8} {
		if contains(got, vid(h)) {
			t.Errorf("search leaked video %d", h)
		}
	}
	if len(got) == 0 || got[0] != vid(21) {
		t.Fatalf("sort=views should put the 9000-view video first: %v", got)
	}

	_, m, _ = e.get("/api/v1/search?q=zebra&type=videos&duration=long")
	if got := ids2(m, "video"); len(got) != 1 || got[0] != vid(20) {
		t.Fatalf("duration=long: %v", got)
	}
	_, m, _ = e.get("/api/v1/search?q=zebra&type=videos&duration=short")
	if got := ids2(m, "video"); contains(got, vid(20)) || !contains(got, vid(9)) {
		t.Fatalf("duration=short: %v", got)
	}
	_, m, _ = e.get("/api/v1/search?q=zebra&type=videos&date=month")
	if got := ids2(m, "video"); contains(got, vid(21)) || !contains(got, vid(1)) {
		t.Fatalf("date=month: %v", got)
	}
	_, m, _ = e.get("/api/v1/search?q=zebra&type=live")
	if got := ids2(m, "video"); len(got) != 0 {
		t.Fatalf("type=live must be empty (no live streams exist): %v", got)
	}
	_, m, _ = e.get("/api/v1/search?q=zebra&sort=latest")
	if it := m["items"].([]any); it[0].(map[string]any)["kind"] != "channel" {
		t.Fatalf("matching channel must come first: %v", it[0])
	}
	// partial words match titles
	_, m, _ = e.get("/api/v1/search?q=docum&type=videos")
	if got := ids2(m, "video"); len(got) != 1 || got[0] != vid(20) {
		t.Fatalf("partial match: %v", got)
	}
	// suspended / deleted creators never appear as channels (usernames are creator_1..3)
	_, m, _ = e.get("/api/v1/search?q=creator&type=channels")
	var chans []string
	for _, it := range m["items"].([]any) {
		chans = append(chans, it.(map[string]any)["channel"].(map[string]any)["id"].(string))
	}
	if len(chans) != 1 || chans[0] != cid(1) {
		t.Fatalf("only the active creator may be found (by username): %v", chans)
	}
}

func ids2(m map[string]any, kind string) []string {
	var out []string
	for _, it := range m["items"].([]any) {
		o := it.(map[string]any)
		if o["kind"] == kind {
			out = append(out, o[kind].(map[string]any)["id"].(string))
		}
	}
	return out
}

func TestSearchHostileInput(t *testing.T) {
	e := setup(t)
	e.creator(1, "A", "ACTIVE", false, 1)
	e.video(vopt{n: 1, creator: 1, title: "100% real_deal", published: recent})
	e.video(vopt{n: 2, creator: 1, title: "something else", published: recent})
	for _, q := range []string{"'; DROP TABLE videos; --", `\`, "%", "_", "a & b | !c", strings.Repeat("x", 5000), "\x00", "(((", "bad\xffutf8", "\u202e\x00x"} {
		code, _, raw := e.get("/api/v1/search?q=" + url.QueryEscape(q))
		if code != 200 {
			t.Errorf("q=%q -> %d %s", q, code, raw)
		}
		code, _, raw = e.get("/api/v1/search/suggestions?q=" + url.QueryEscape(q))
		if code != 200 {
			t.Errorf("suggest q=%q -> %d %s", q, code, raw)
		}
	}
	// '%' and '_' are literals, not wildcards: only the title containing '%' matches
	_, m, _ := e.get("/api/v1/search?q=" + url.QueryEscape("%") + "&type=videos")
	if got := ids2(m, "video"); len(got) != 1 || got[0] != vid(1) {
		t.Fatalf("literal %%: %v", got)
	}
	_, m, _ = e.get("/api/v1/search?q=" + url.QueryEscape("_") + "&type=videos")
	if got := ids2(m, "video"); len(got) != 1 || got[0] != vid(1) {
		t.Fatalf("literal _: %v", got)
	}
	if _, m, _ := e.get("/api/v1/videos/" + url.PathEscape("' OR 1=1 --")); m["code"] != "video_not_found" {
		t.Fatalf("injection id: %v", m)
	}
}

func TestSearchPagingAcrossManyResults(t *testing.T) {
	e := setup(t)
	e.creator(1, "Pager Studio", "ACTIVE", false, 1)
	for i := 1; i <= 25; i++ {
		e.video(vopt{n: i, creator: 1, title: fmt.Sprintf("pagerword %d", i), published: recent, views: int64(i)})
	}
	var all []string
	cursor := ""
	var total float64
	for page := 0; page < 10; page++ {
		p := "/api/v1/search?q=pagerword&type=videos&sort=views"
		if cursor != "" {
			p += "&cursor=" + url.QueryEscape(cursor)
		}
		code, m, raw := e.get(p)
		if code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		all = append(all, ids2(m, "video")...)
		total = m["total"].(float64)
		next, _ := m["nextCursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	seen := map[string]bool{}
	for _, id := range all {
		if seen[id] {
			t.Fatalf("duplicate %s", id)
		}
		seen[id] = true
	}
	if len(all) != 25 || total != 25 {
		t.Fatalf("got %d results, total=%v", len(all), total)
	}
	if all[0] != vid(25) || all[24] != vid(1) {
		t.Fatalf("not sorted by views desc: first=%s last=%s", all[0], all[24])
	}
}

func TestSuggestions(t *testing.T) {
	e := setup(t)
	e.visibilityFixture()
	code, m, raw := e.get("/api/v1/search/suggestions?q=zeb")
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	var s []string
	for _, x := range m["suggestions"].([]any) {
		s = append(s, x.(string))
	}
	if len(s) == 0 || s[0] != "Zebra Studio" {
		t.Fatalf("prefix-matching channel name should lead: %v", s)
	}
	for _, x := range s {
		if strings.Contains(x, "private") || strings.Contains(x, "draft") || strings.Contains(x, "removed") || strings.Contains(x, "suspended") {
			t.Fatalf("suggestion leaked hidden title %q", x)
		}
	}
	if _, m, _ = e.get("/api/v1/search/suggestions?q=z"); len(m["suggestions"].([]any)) != 0 {
		t.Fatal("1-char query must return nothing")
	}
}

func TestTrendingRanksByRecentActivityNotTotalViews(t *testing.T) {
	e := setup(t)
	e.creator(1, "T", "ACTIVE", false, 1)
	e.video(vopt{n: 1, creator: 1, title: "old giant", published: "now() - interval '300 days'", views: 5000000})
	e.video(vopt{n: 2, creator: 1, title: "rising", published: "now() - interval '2 days'", views: 800})
	e.video(vopt{n: 3, creator: 1, title: "quiet", published: "now() - interval '2 days'", views: 900})
	e.exec(`INSERT INTO video_views (video_id, session_id, created_at) SELECT $1::uuid, gen_random_uuid(), now() - (g * interval '10 minutes') FROM generate_series(1, 60) g`, vid(2))
	e.exec(`INSERT INTO video_views (video_id, session_id, created_at) SELECT $1::uuid, gen_random_uuid(), now() - interval '30 hours' FROM generate_series(1, 3) g`, vid(2))

	_, m, _ := e.get("/api/v1/trending?tab=now")
	if got := ids(m); got[0] != vid(2) {
		t.Fatalf("now: the video with recent views must outrank the old giant: %v", got)
	}
	_, m, _ = e.get("/api/v1/trending?tab=fast-growing")
	if got := ids(m); got[0] != vid(2) {
		t.Fatalf("fast-growing: %v", got)
	}
	_, m, _ = e.get("/api/v1/trending?tab=most-viewed")
	if got := ids(m); got[0] != vid(1) {
		t.Fatalf("most-viewed must use total views: %v", got)
	}
	_, m, _ = e.get("/api/v1/trending?tab=recent")
	if got := ids(m); contains(got, vid(1)) {
		t.Fatalf("recent tab must exclude 300-day-old upload: %v", got)
	}
	if code, _, _ := e.get("/api/v1/trending?tab=bogus"); code != 422 {
		t.Fatalf("bogus tab = %d", code)
	}
}

func TestChannelEndpoints(t *testing.T) {
	e := setup(t)
	e.visibilityFixture()
	e.exec(`UPDATE user_profiles SET website = 'https://zebra.example', social_links = $2::jsonb, banner_storage_key = 'b/1.jpg' WHERE user_id = $1::uuid`, cid(1),
		`[{"label":"X","url":"javascript:alert(1)"},{"label":"Insta","url":"https://i.example/z"}]`)

	code, m, raw := e.get("/api/v1/channels/" + cid(1))
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	// public videos of creator 1: ids 1 and 9 (short) => 2; unlisted/private/draft/removed/deleted excluded
	if m["videoCount"] != float64(2) || m["totalViews"] != float64(200) || m["subscribers"] != float64(500) {
		t.Fatalf("counts leak or are wrong: %v", m)
	}
	if m["bannerUrl"] != "https://cdn.test/b/1.jpg" {
		t.Fatalf("banner: %v", m["bannerUrl"])
	}
	links := m["links"].([]any)
	if len(links) != 2 {
		t.Fatalf("javascript: link must be dropped: %v", links)
	}
	if code, _, _ := e.get("/api/v1/channels/" + cid(2)); code != 404 {
		t.Errorf("suspended channel = %d, want 404", code)
	}
	if code, _, _ := e.get("/api/v1/channels/" + cid(3)); code != 404 {
		t.Errorf("deleted channel = %d, want 404", code)
	}
	if code, _, _ := e.get("/api/v1/channels/bad-id"); code != 404 {
		t.Errorf("bad id = %d", code)
	}
	if code, _, _ := e.get("/api/v1/channels/" + cid(1) + "/videos?kind=nope"); code != 422 {
		t.Errorf("bad kind = %d", code)
	}

	code, _, raw = e.get("/api/v1/channels/popular")
	var list []map[string]any
	_ = json.Unmarshal(raw, &list)
	if code != 200 || len(list) != 1 || list[0]["name"] != "Zebra Studio" {
		t.Fatalf("popular must list only active creators with public videos: %s", raw)
	}
}

func TestChannelPlaylistsOnlyPublicWithPublicCounts(t *testing.T) {
	e := setup(t)
	e.visibilityFixture()
	e.exec(`INSERT INTO playlists (id, user_id, title, visibility) VALUES
	  ('e0000000-0000-7000-8000-000000000001', $1::uuid, 'Public list', 'PUBLIC'),
	  ('e0000000-0000-7000-8000-000000000002', $1::uuid, 'Private list', 'PRIVATE'),
	  ('e0000000-0000-7000-8000-000000000003', $1::uuid, 'Unlisted list', 'UNLISTED')`, cid(1))
	// public list holds: private video first, then public video, then unlisted
	e.exec(`INSERT INTO playlist_videos (playlist_id, video_id, position) VALUES
	  ('e0000000-0000-7000-8000-000000000001', $1, 0),
	  ('e0000000-0000-7000-8000-000000000001', $2, 1),
	  ('e0000000-0000-7000-8000-000000000001', $3, 2)`, vid(3), vid(1), vid(2))

	code, _, raw := e.get("/api/v1/channels/" + cid(1) + "/playlists")
	var pls []map[string]any
	_ = json.Unmarshal(raw, &pls)
	if code != 200 || len(pls) != 1 || pls[0]["title"] != "Public list" || pls[0]["visibility"] != "public" {
		t.Fatalf("only the PUBLIC playlist may be listed: %s", raw)
	}
	if pls[0]["videoCount"] != float64(1) {
		t.Fatalf("video count must only include public videos, got %v", pls[0]["videoCount"])
	}
	if pls[0]["thumbnailUrl"] != "https://cdn.test/t/"+vid(1)+".jpg" {
		t.Fatalf("thumbnail must come from the first PUBLIC video, got %v", pls[0]["thumbnailUrl"])
	}
	// playlist appears in search; the private one does not
	_, m, _ := e.get("/api/v1/search?q=list&type=playlists")
	items := m["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["playlist"].(map[string]any)["title"] != "Public list" {
		t.Fatalf("playlist search: %v", items)
	}
}

func TestSitemapAndMisc(t *testing.T) {
	e := setup(t)
	e.visibilityFixture()
	code, _, raw := e.get("/api/v1/sitemap/videos?limit=100")
	var list []map[string]string
	_ = json.Unmarshal(raw, &list)
	if code != 200 {
		t.Fatalf("%d", code)
	}
	var got []string
	for _, x := range list {
		got = append(got, x["id"])
	}
	for _, h := range []int{2, 3, 4, 5, 6, 7, 8} {
		if contains(got, vid(h)) {
			t.Errorf("sitemap leaked %d (unlisted/private/etc must not be indexed)", h)
		}
	}
	if !contains(got, vid(1)) {
		t.Fatal("public video missing from sitemap")
	}
	// absurd limits fall back to a safe default instead of erroring or scanning everything
	if code, _, _ := e.get("/api/v1/sitemap/videos?limit=999999999"); code != 200 {
		t.Fatalf("huge limit = %d", code)
	}
	if code, _, _ := e.get("/api/v1/sitemap/videos?limit=abc"); code != 200 {
		t.Fatalf("bad limit = %d", code)
	}
	code, _, raw = e.get("/api/v1/reports/reasons")
	if code != 200 || !strings.Contains(string(raw), `"underage"`) {
		t.Fatalf("reasons: %d %s", code, raw)
	}
	if code, m, _ := e.get("/api/v1/videos?cursor=garbage"); code != 400 || m["code"] != "invalid_cursor" {
		t.Fatalf("bad cursor: %d %v", code, m)
	}
}

func TestEmptyDatabaseReturnsEmptyListsNotNull(t *testing.T) {
	e := setup(t)
	for _, ep := range []string{"/api/v1/videos", "/api/v1/shorts", "/api/v1/live", "/api/v1/trending?tab=now", "/api/v1/search?q=anything"} {
		code, _, raw := e.get(ep)
		if code != 200 || !strings.Contains(string(raw), `"items":[]`) || !strings.Contains(string(raw), `"nextCursor":null`) {
			t.Errorf("%s: %d %s", ep, code, raw)
		}
	}
	if _, _, raw := e.get("/api/v1/channels/popular"); strings.TrimSpace(string(raw)) != "[]" {
		t.Errorf("popular on empty db: %s", raw)
	}
}
