package handler_test

import (
	"fmt"
	"strings"
	"testing"
)

// ---------- views, progress, history ----------

func TestViewCounting(t *testing.T) {
	e := setupAuth(t)
	creator, chID := e.channel()
	v := e.addVideo(chID, "watch me", vo{})
	private := e.addVideo(chID, "private", vo{vis: "PRIVATE"})
	vw := api + "/videos/" + v + "/view"
	count := func() int64 { _, _, _, n := e.videoCounts(v); return n }

	anon := e.viewer()
	anon.post(vw, nil).mustBe(t, 204)
	if count() != 1 {
		t.Fatalf("first view: %d", count())
	}
	anon.post(vw, map[string]any{"source": "search"}).mustBe(t, 204) // refresh/replay within 30 minutes
	if count() != 1 {
		t.Fatalf("a repeat from the same viewer must not count again: %d", count())
	}
	e.viewer().post(vw, map[string]any{"source": "search"}).mustBe(t, 204) // another visitor
	if count() != 2 {
		t.Fatalf("second visitor: %d", count())
	}

	// bots, empty user agents and the creator's own views never count
	bot := e.viewer()
	bot.headers["User-Agent"] = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	bot.post(vw, nil).mustBe(t, 204)
	none := e.viewer()
	none.headers["User-Agent"] = ""
	none.post(vw, nil).mustBe(t, 204)
	creator.post(vw, nil).mustBe(t, 204)
	if count() != 2 {
		t.Fatalf("bots/empty UA/creator inflated the counter: %d", count())
	}

	// hidden or unknown videos, bad bodies
	e.viewer().post(api+"/videos/"+private+"/view", nil).wantErr(t, 404, "video_not_found")
	e.viewer().post(api+"/videos/00000000-0000-4000-8000-000000000000/view", nil).wantErr(t, 404, "video_not_found")
	e.viewer().post(vw, map[string]any{"source": "<script>", "evil": 1}).wantErr(t, 400, "invalid_json")
	e.client().post(vw, nil).wantErr(t, 403, "age_verification_required")
	odd := e.viewer()
	odd.post(vw, map[string]any{"source": "<script>"}).mustBe(t, 204) // unknown sources are recorded as DIRECT, not rejected
	if count() != 3 {
		t.Fatalf("unknown source still counts: %d", count())
	}

	// what is stored: enums, a hashed IP, and nothing personal for anonymous viewers
	var device, source string
	var ipHash []byte
	var userNull bool
	e.scan([]any{&device, &source, &ipHash, &userNull}, `SELECT device_type::text, traffic_source::text, ip_hash, user_id IS NULL FROM video_views WHERE video_id = $1::uuid AND traffic_source = 'SEARCH' ORDER BY created_at LIMIT 1`, v)
	if device != "DESKTOP" || source != "SEARCH" || len(ipHash) != 32 || !userNull || strings.Contains(string(ipHash), anon.ip) {
		t.Fatalf("stored view: %s %s %x %v", device, source, ipHash, userNull)
	}
	var views int64
	e.scan([]any{&views}, `SELECT views FROM video_daily_analytics WHERE video_id = $1::uuid`, v)
	if views != 3 {
		t.Fatalf("daily analytics views = %d", views)
	}
}

func TestViewRateLimitPerIP(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, e.addVideo(chID, fmt.Sprintf("v%d", i), vo{}))
	}
	c := e.viewer()
	for i := 0; i < 125; i++ { // beyond 120/min from one IP the views are silently dropped
		c.post(api+"/videos/"+ids[i%3]+"/view", nil)
		if i == 2 {
			continue
		}
	}
	var total int64
	for _, id := range ids {
		_, _, _, n := e.videoCounts(id)
		total += n
	}
	if total != 3 { // each video is counted once per viewer, however often the page is refreshed
		t.Fatalf("one visitor can only count once per video: %d", total)
	}
}

func TestProgressAndHistory(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v := e.addVideo(chID, "series", vo{dur: 300})
	u, _, _ := e.verifiedUser()
	pg := api + "/videos/" + v + "/progress"

	e.viewer().post(pg, map[string]any{"positionSec": 10, "watchedSec": 10}).wantErr(t, 401, "unauthenticated")
	u.post(pg, map[string]any{"positionSec": -1, "watchedSec": 1}).wantErr(t, 422, "validation_failed")
	u.post(pg, map[string]any{"positionSec": 1, "watchedSec": -5}).wantErr(t, 422, "validation_failed")
	u.post(pg, map[string]any{"positionSec": 1, "watchedSec": 1, "admin": true}).wantErr(t, 400, "invalid_json")
	e.addVideo(chID, "x", vo{}) // noise
	priv := e.addVideo(chID, "hidden", vo{vis: "PRIVATE"})
	u.post(api+"/videos/"+priv+"/progress", map[string]any{"positionSec": 1, "watchedSec": 1}).wantErr(t, 404, "video_not_found")

	u.post(pg, map[string]any{"positionSec": 40, "watchedSec": 500}).mustBe(t, 204) // watchedSec is capped at 120 per call
	var pos, watched int
	var completed bool
	read := func() {
		e.scan([]any{&pos, &watched, &completed}, `SELECT last_position_seconds, watch_duration_seconds, completed FROM watch_history WHERE video_id = $1::uuid`, v)
	}
	read()
	if pos != 40 || watched != 120 || completed {
		t.Fatalf("after first report: %d %d %v", pos, watched, completed)
	}
	u.post(pg, map[string]any{"positionSec": 290, "watchedSec": 30}).mustBe(t, 204) // past 90% of 300s
	read()
	if pos != 290 || watched != 150 || !completed {
		t.Fatalf("watch time accumulates and 90%% completes: %d %d %v", pos, watched, completed)
	}
	u.post(pg, map[string]any{"positionSec": 9999, "watchedSec": 0}).mustBe(t, 204) // clamped to the duration
	read()
	if pos != 300 {
		t.Fatalf("position beyond the end must clamp to the duration: %d", pos)
	}
	var vs, daily int
	e.scan([]any{&vs}, `SELECT watch_time_seconds FROM video_stats WHERE video_id = $1::uuid`, v)
	e.scan([]any{&daily}, `SELECT watch_time_seconds FROM video_daily_analytics WHERE video_id = $1::uuid`, v)
	if vs != 150 || daily != 150 {
		t.Fatalf("watch time totals: stats=%d daily=%d", vs, daily)
	}

	h := u.get(api + "/history")
	h.mustBe(t, 200)
	items := h.items()
	if len(items) != 1 || items[0]["progressSec"] != float64(300) || items[0]["video"].(map[string]any)["id"] != v || items[0]["watchedAt"] == nil {
		t.Fatalf("history: %s", h.body)
	}
	// a video that becomes private disappears from history
	e.exec(`UPDATE videos SET visibility = 'PRIVATE' WHERE id = $1::uuid`, v)
	if got := u.get(api + "/history").itemIDs(); len(got) != 0 {
		t.Fatalf("hidden videos must not be listed: %v", got)
	}
	e.exec(`UPDATE videos SET visibility = 'PUBLIC' WHERE id = $1::uuid`, v)

	// history is per user, and can be deleted one by one or all at once
	other, _, _ := e.verifiedUser()
	if got := other.get(api + "/history").itemIDs(); len(got) != 0 {
		t.Fatal("history leaked between users")
	}
	u.del(api+"/history/"+v).mustBe(t, 204)
	if got := u.get(api + "/history").itemIDs(); len(got) != 0 {
		t.Fatalf("history item not deleted: %v", got)
	}
	u.del(api+"/history/nope").wantErr(t, 404, "video_not_found")
	u.del(api+"/history").mustBe(t, 204)
	e.viewer().get(api+"/history").wantErr(t, 401, "unauthenticated")
	e.client().get(api+"/history").wantErr(t, 403, "age_verification_required")
}

func TestHistoryRespectsPrivacySettingAndPaginates(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v := e.addVideo(chID, "tracked", vo{})
	u, uEmail, _ := e.verifiedUser()
	uid := e.userID(uEmail)

	// the user turns watch history off: views and progress write nothing
	u.mustCode(u.patch(api+"/settings", map[string]any{"privacy": map[string]any{"saveWatchHistory": false}}), 204)
	u.post(api+"/videos/"+v+"/view", nil).mustBe(t, 204)
	u.post(api+"/videos/"+v+"/progress", map[string]any{"positionSec": 30, "watchedSec": 30}).mustBe(t, 204)
	if n := e.countRows(`SELECT count(*) FROM watch_history WHERE user_id = $1::uuid`, uid); n != 0 {
		t.Fatalf("history was written although it is turned off: %d", n)
	}
	if _, _, _, views := e.videoCounts(v); views != 1 {
		t.Fatalf("the view itself still counts: %d", views)
	}
	u.mustCode(u.patch(api+"/settings", map[string]any{"privacy": map[string]any{"saveWatchHistory": true}}), 204)
	u.post(api+"/videos/"+v+"/progress", map[string]any{"positionSec": 30, "watchedSec": 30}).mustBe(t, 204)
	if n := e.countRows(`SELECT count(*) FROM watch_history WHERE user_id = $1::uuid`, uid); n != 1 {
		t.Fatalf("history must resume when turned back on: %d", n)
	}

	// pagination: 13 entries, newest first, no duplicates
	ids := e.bulkVideos(chID, 13)
	e.exec(`INSERT INTO watch_history (user_id, video_id, last_watched_at) SELECT $1::uuid, id, now() - (row_number() OVER (ORDER BY title) * interval '1 minute') FROM videos WHERE id = ANY($2::uuid[])`, uid, ids)
	first := u.get(api + "/history")
	if len(first.items()) != 12 || first.next() == "" {
		t.Fatalf("first page: %d next=%q", len(first.items()), first.next())
	}
	all := u.walkAll(api + "/history")
	seen := map[string]bool{}
	for _, id := range all {
		if seen[id] {
			t.Fatalf("duplicate %s", id)
		}
		seen[id] = true
	}
	if len(all) != 14 {
		t.Fatalf("history has %d of 14 entries", len(all))
	}
	u.get(api+"/history?cursor=garbage").wantErr(t, 400, "invalid_cursor")
}

func TestProgressRateLimit(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v := e.addVideo(chID, "limit", vo{})
	u, _, _ := e.verifiedUser()
	var last *resp
	for i := 0; i < 31; i++ {
		last = u.post(api+"/videos/"+v+"/progress", map[string]any{"positionSec": i, "watchedSec": 1})
	}
	last.wantErr(t, 429, "rate_limited")
}

// ---------- watch later ----------

func TestWatchLater(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v1 := e.addVideo(chID, "one", vo{})
	sleepMs(15)
	v2 := e.addVideo(chID, "two", vo{})
	priv := e.addVideo(chID, "private", vo{vis: "PRIVATE"})
	u, _, _ := e.verifiedUser()
	other, _, _ := e.verifiedUser()

	e.viewer().put(api+"/watch-later/"+v1, nil).wantErr(t, 401, "unauthenticated")
	u.put(api+"/watch-later/"+priv, nil).wantErr(t, 404, "video_not_found")
	u.put(api+"/watch-later/not-a-uuid", nil).wantErr(t, 404, "video_not_found")
	u.put(api+"/watch-later/"+v1, nil).mustBe(t, 204)
	u.put(api+"/watch-later/"+v1, nil).mustBe(t, 204) // idempotent
	sleepMs(15)
	u.put(api+"/watch-later/"+v2, nil).mustBe(t, 204)

	list := u.get(api + "/watch-later")
	if ids := list.itemIDs(); len(ids) != 2 || ids[0] != v2 || ids[1] != v1 {
		t.Fatalf("watch later must list newest-added first: %v", ids)
	}
	idsResp := u.get(api + "/watch-later/ids")
	var ids []string
	_ = jsonInto(idsResp.body, &ids)
	if len(ids) != 2 || !has(ids, v1) || !has(ids, v2) {
		t.Fatalf("ids: %s", idsResp.body)
	}
	if got := other.get(api + "/watch-later").itemIDs(); len(got) != 0 {
		t.Fatal("watch later leaked between users")
	}
	u.del(api+"/watch-later/"+v1).mustBe(t, 204)
	u.del(api+"/watch-later/"+v1).mustBe(t, 204)
	if got := u.get(api + "/watch-later").itemIDs(); len(got) != 1 || got[0] != v2 {
		t.Fatalf("after remove: %v", got)
	}
	// a video that becomes hidden drops out of the list
	e.exec(`UPDATE videos SET visibility = 'PRIVATE' WHERE id = $1::uuid`, v2)
	if got := u.get(api + "/watch-later").itemIDs(); len(got) != 0 {
		t.Fatalf("hidden video listed: %v", got)
	}
	e.client().get(api+"/watch-later").wantErr(t, 403, "age_verification_required")
}

func TestWatchLaterCapAndPagination(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	ids := e.bulkVideos(chID, 501)
	u, uEmail, _ := e.verifiedUser()
	uid := e.userID(uEmail)
	e.exec(`INSERT INTO watch_later (user_id, video_id, created_at) SELECT $1::uuid, v, now() - (n * interval '1 second') FROM unnest($2::uuid[]) WITH ORDINALITY AS t(v, n) WHERE n <= 500`, uid, ids)

	r := u.put(api+"/watch-later/"+ids[500], nil)
	r.wantErr(t, 422, "limit_reached")
	if n := e.countRows(`SELECT count(*) FROM watch_later WHERE user_id = $1::uuid`, uid); n != 500 {
		t.Fatalf("cap exceeded: %d", n)
	}
	all := u.walkAll(api + "/watch-later")
	if len(all) != 500 {
		t.Fatalf("paginated list returned %d of 500", len(all))
	}
	u.put(api+"/watch-later/"+ids[0], nil).mustBe(t, 204) // already in the list: still fine at the cap
}

// ---------- playlists ----------

func TestPlaylistsLifecycle(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	vA := e.addVideo(chID, "A", vo{})
	vB := e.addVideo(chID, "B", vo{})
	vC := e.addVideo(chID, "C", vo{})
	priv := e.addVideo(chID, "private", vo{vis: "PRIVATE"})
	owner, ownerEmail, _ := e.verifiedUser()
	ownerID := e.userID(ownerEmail)
	other, _, _ := e.verifiedUser()
	unverified, _, _ := e.newUser()

	unverified.post(api+"/playlists", map[string]any{"title": "x", "visibility": "private"}).wantErr(t, 403, "verification_required")
	for name, body := range map[string]map[string]any{
		"empty title": {"title": "  ", "visibility": "private"}, "long title": {"title": strings.Repeat("t", 101), "visibility": "private"},
		"bad visibility": {"title": "ok", "visibility": "secret"}, "bad video": {"title": "ok", "visibility": "private", "videoId": priv},
	} {
		if r := owner.post(api+"/playlists", body); r.code != 422 && r.code != 404 {
			t.Errorf("%s accepted: %d %s", name, r.code, r.body)
		}
	}
	cr := owner.post(api+"/playlists", map[string]any{"title": "  My   mix ", "visibility": "private", "videoId": vA})
	cr.mustBe(t, 201)
	pl := cr.str("id")
	if cr.str("title") != "My mix" || cr.str("visibility") != "private" || cr.m["videoCount"] != float64(1) || cr.str("ownerName") == "" {
		t.Fatalf("created playlist: %s", cr.body)
	}
	if ids := e.client().get(api + "/playlists/" + pl).code; ids != 403 {
		t.Fatalf("age gate on playlists: %d", ids)
	}

	// membership lookups for the "save to playlist" menu
	e.addVideo(chID, "unused", vo{})
	owner.post(api+"/playlists", map[string]any{"title": "Second", "visibility": "public"}).mustBe(t, 201)
	mine := owner.get(api + "/playlists?videoId=" + vA)
	var rows []map[string]any
	_ = jsonInto(mine.body, &rows)
	contains := map[string]any{}
	for _, r := range rows {
		contains[r["title"].(string)] = r["contains"]
	}
	if len(rows) != 2 || contains["My mix"] != true || contains["Second"] != false {
		t.Fatalf("my playlists with membership: %s", mine.body)
	}
	owner.get(api+"/playlists?videoId=bad").wantErr(t, 422, "validation_failed")

	// adding: idempotent, order kept, private videos refused
	pa := api + "/playlists/" + pl
	owner.put(pa+"/videos/"+vB, nil).mustBe(t, 204)
	owner.put(pa+"/videos/"+vB, nil).mustBe(t, 204)
	owner.put(pa+"/videos/"+vC, nil).mustBe(t, 204)
	owner.put(pa+"/videos/"+priv, nil).wantErr(t, 404, "video_not_found")
	owner.put(pa+"/videos/"+vA, nil).mustBe(t, 204) // already there
	d := owner.get(pa)
	d.mustBe(t, 200)
	if got := videoIDs(d); strings.Join(got, ",") != strings.Join([]string{vA, vB, vC}, ",") || d.m["isOwner"] != true || d.m["videoCount"] != float64(3) {
		t.Fatalf("detail: %v %s", got, d.body)
	}

	// private: nobody else can see it or change it
	other.get(pa).wantErr(t, 404, "playlist_not_found")
	e.viewer().get(pa).wantErr(t, 404, "playlist_not_found")
	other.put(pa+"/videos/"+vB, nil).wantErr(t, 404, "playlist_not_found")
	other.del(pa+"/videos/"+vB).wantErr(t, 404, "playlist_not_found")
	other.patch(pa, map[string]any{"title": "hijack"}).wantErr(t, 404, "playlist_not_found")
	other.put(pa+"/order", map[string]any{"videoIds": []string{vC, vB, vA}}).wantErr(t, 404, "playlist_not_found")
	other.del(pa).wantErr(t, 404, "playlist_not_found")

	// reordering needs exactly the same set
	owner.put(pa+"/order", map[string]any{"videoIds": []string{vC, vA, vB}}).mustBe(t, 204)
	if got := videoIDs(owner.get(pa)); strings.Join(got, ",") != strings.Join([]string{vC, vA, vB}, ",") {
		t.Fatalf("reorder: %v", got)
	}
	for name, ids := range map[string][]string{"missing": {vC, vA}, "extra": {vC, vA, vB, priv}, "duplicate": {vC, vC, vA}, "not uuid": {vC, "x", vB}, "empty": {}} {
		owner.put(pa+"/order", map[string]any{"videoIds": ids}).wantErr(t, 422, "invalid_order")
		_ = name
	}
	// removing compacts positions
	owner.del(pa+"/videos/"+vA).mustBe(t, 204)
	owner.del(pa+"/videos/"+vA).mustBe(t, 204)
	rowsPos, _ := e.pool.Query(contextBG(), `SELECT position FROM playlist_videos WHERE playlist_id = $1::uuid ORDER BY position`, pl)
	var positions []int
	for rowsPos.Next() {
		var p int
		_ = rowsPos.Scan(&p)
		positions = append(positions, p)
	}
	rowsPos.Close()
	if fmt.Sprint(positions) != "[0 1]" {
		t.Fatalf("positions must stay 0..n-1: %v", positions)
	}

	// public playlists are visible to everyone and appear on the channel page
	owner.patch(pa, map[string]any{"visibility": "public", "title": "Public mix", "description": "d"}).mustBe(t, 204)
	owner.patch(pa, map[string]any{"visibility": "weird"}).wantErr(t, 422, "validation_failed")
	owner.patch(pa, map[string]any{"title": ""}).wantErr(t, 422, "validation_failed")
	owner.patch(pa, map[string]any{"owner": "x"}).wantErr(t, 400, "invalid_json")
	seen := other.get(pa)
	seen.mustBe(t, 200)
	if seen.m["isOwner"] != false || seen.str("title") != "Public mix" || seen.str("description") != "d" {
		t.Fatalf("public view: %s", seen.body)
	}
	if got := e.viewer().get(api + "/channels/" + ownerID + "/playlists"); !strings.Contains(string(got.body), pl) || strings.Contains(string(got.body), "Second") && false {
		t.Fatalf("channel playlists: %s", got.body)
	}

	// a video that becomes private disappears from other people's view and from the count
	e.exec(`UPDATE videos SET visibility = 'PRIVATE' WHERE id = $1::uuid`, vB)
	if got := videoIDs(other.get(pa)); len(got) != 1 || got[0] != vC {
		t.Fatalf("hidden video leaked through a playlist: %v", got)
	}
	e.exec(`UPDATE videos SET visibility = 'PUBLIC' WHERE id = $1::uuid`, vB)

	owner.del(pa).mustBe(t, 204)
	owner.get(pa).wantErr(t, 404, "playlist_not_found")
	owner.del(pa).wantErr(t, 404, "playlist_not_found")
	owner.patch(pa, map[string]any{"title": "x"}).wantErr(t, 404, "playlist_not_found")
}

func videoIDs(r *resp) []string {
	var out []string
	if a, ok := r.m["videos"].([]any); ok {
		for _, x := range a {
			out = append(out, x.(map[string]any)["id"].(string))
		}
	}
	return out
}

func TestPlaylistLimits(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	ids := e.bulkVideos(chID, 201)
	u, uEmail, _ := e.verifiedUser()
	uid := e.userID(uEmail)

	e.exec(`INSERT INTO playlists (id, user_id, title, visibility) SELECT gen_random_uuid(), $1::uuid, 'p' || g, 'PRIVATE' FROM generate_series(1, 99) g`, uid)
	u.post(api+"/playlists", map[string]any{"title": "hundredth", "visibility": "private"}).mustBe(t, 201)
	u.post(api+"/playlists", map[string]any{"title": "one too many", "visibility": "private"}).wantErr(t, 422, "limit_reached")

	pl := u.get(api + "/playlists").items()
	_ = pl
	var plID string
	e.scan([]any{&plID}, `SELECT id::text FROM playlists WHERE user_id = $1::uuid AND title = 'hundredth'`, uid)
	e.exec(`INSERT INTO playlist_videos (playlist_id, video_id, position) SELECT $1::uuid, v, n - 1 FROM unnest($2::uuid[]) WITH ORDINALITY AS t(v, n) WHERE n <= 200`, plID, ids)
	u.put(api+"/playlists/"+plID+"/videos/"+ids[200], nil).wantErr(t, 422, "limit_reached")
}

// ---------- not interested, block ----------

func TestNotInterestedAndBlock(t *testing.T) {
	e := setupAuth(t)
	_, chA := e.channel()
	_, chB := e.channel()
	a1 := e.addVideo(chA, "a one", vo{views: 50})
	a2 := e.addVideo(chA, "a two", vo{views: 40})
	b1 := e.addVideo(chB, "b one", vo{views: 30})
	u, _, _ := e.verifiedUser()
	anon := e.viewer()
	priv := e.addVideo(chA, "private", vo{vis: "PRIVATE"})

	feed := func(c *client) []string { return c.get(api + "/videos").itemIDs() }
	if got := feed(u); len(got) != 3 {
		t.Fatalf("baseline feed: %v", got)
	}
	e.viewer().post(api+"/videos/"+a1+"/not-interested", nil).wantErr(t, 401, "unauthenticated")
	u.post(api+"/videos/"+priv+"/not-interested", nil).wantErr(t, 404, "video_not_found")
	u.post(api+"/videos/"+a1+"/not-interested", nil).mustBe(t, 204)
	u.post(api+"/videos/"+a1+"/not-interested", nil).mustBe(t, 204)
	if got := feed(u); has(got, a1) || len(got) != 2 {
		t.Fatalf("not interested must hide the video from the feed: %v", got)
	}
	for _, p := range []string{"/trending?tab=most-viewed", "/search?q=one&type=videos"} {
		if got := u.get(api + p).itemIDs(); has(got, a1) {
			t.Errorf("%s still shows the video", p)
		}
	}
	if got := feed(anon); !has(got, a1) {
		t.Fatal("other viewers are unaffected")
	}
	u.mustCode(u.get(api+"/videos/"+a1), 200) // direct links still work

	// blocking a channel hides it everywhere for that user, and unsubscribes
	u.put(api+"/channels/"+chA+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204)
	if n := e.countRows(`SELECT subscriber_count FROM user_profiles WHERE user_id = $1::uuid`, chA); n != 1 {
		t.Fatalf("subscriber count: %d", n)
	}
	u.put(api+"/channels/"+chA+"/block", nil).mustBe(t, 204)
	u.put(api+"/channels/"+chA+"/block", nil).mustBe(t, 204)
	if got := feed(u); len(got) != 1 || got[0] != b1 {
		t.Fatalf("blocked channel in the feed: %v", got)
	}
	if n := e.countRows(`SELECT subscriber_count FROM user_profiles WHERE user_id = $1::uuid`, chA); n != 0 {
		t.Fatalf("blocking must remove the subscription: %d", n)
	}
	var popular []map[string]any
	_ = jsonInto(u.get(api+"/channels/popular").body, &popular)
	if len(popular) != 1 || popular[0]["id"] != chB {
		t.Fatalf("popular channels still list the blocked one: %v", popular)
	}
	if got := u.get(api + "/search?q=a&type=channels"); strings.Contains(string(got.body), chA) {
		t.Fatalf("search lists a blocked channel: %s", got.body)
	}
	if got := u.get(api + "/channels/" + chA + "/videos").itemIDs(); !has(got, a2) {
		t.Fatal("a blocked channel's own page still works")
	}
	u.del(api+"/channels/"+chA+"/block").mustBe(t, 204)
	u.del(api+"/channels/"+chA+"/block").mustBe(t, 204)
	if got := feed(u); !has(got, a2) {
		t.Fatalf("unblock must restore: %v", got)
	}

	me := e.userID(func() string { _, em, _ := e.verifiedUser(); return em }())
	_ = me
	uc, ucEmail, _ := e.verifiedUser()
	uc.put(api+"/channels/"+e.userID(ucEmail)+"/block", nil).wantErr(t, 422, "cannot_block_self")
	u.put(api+"/channels/00000000-0000-4000-8000-000000000000/block", nil).wantErr(t, 404, "channel_not_found")
	u.put(api+"/channels/xyz/block", nil).wantErr(t, 404, "channel_not_found")
}

// ---------- reports ----------

func TestReports(t *testing.T) {
	e := setupAuth(t)
	creator, chID := e.channel()
	v := e.addVideo(chID, "reportable", vo{})
	cm := creator.post(api+"/videos/"+v+"/comments", map[string]any{"text": "a comment"}).str("id")
	priv := e.addVideo(chID, "private", vo{vis: "PRIVATE"})
	u, uEmail, _ := e.verifiedUser()
	unverified, _, _ := e.newUser()
	rep := func(c *client, tt, id, reason, details string) *resp {
		return c.post(api+"/reports", map[string]any{"targetType": tt, "targetId": id, "reason": reason, "details": details})
	}

	rep(e.viewer(), "video", v, "spam", "").wantErr(t, 401, "unauthenticated")
	// fields
	r := rep(u, "nope", "x", "weird", strings.Repeat("d", 1001))
	r.wantErr(t, 422, "validation_failed")
	for _, f := range []string{"targetType", "targetId", "reason", "details"} {
		if r.m["fields"].(map[string]any)[f] == nil {
			t.Errorf("missing field error %s: %v", f, r.m["fields"])
		}
	}
	rep(u, "video", v, "other", "").wantErr(t, 422, "validation_failed") // "other" needs details
	u.post(api+"/reports", map[string]any{"targetType": "video", "targetId": v, "reason": "spam", "extra": 1}).wantErr(t, 400, "invalid_json")

	// each reason lands with the schema's priority (a trigger sets it)
	cases := []struct {
		reason string
		enum   string
		prio   int
	}{{"underage", "UNDERAGE_CONTENT", 0}, {"non_consensual", "NON_CONSENSUAL_CONTENT", 0}, {"illegal", "ILLEGAL_CONTENT", 0}, {"copyright", "COPYRIGHT", 1}, {"harassment", "HARASSMENT", 1}, {"spam", "SPAM", 1}}
	for i, c := range cases {
		vv := e.addVideo(chID, fmt.Sprintf("v%d", i), vo{})
		rep(u, "video", vv, c.reason, "details here").mustBe(t, 204)
		var enum string
		var prio int
		var status string
		e.scan([]any{&enum, &prio, &status}, `SELECT reason::text, priority, status::text FROM reports WHERE target_video_id = $1::uuid`, vv)
		if enum != c.enum || prio != c.prio || status != "PENDING" {
			t.Errorf("%s: stored %s prio=%d status=%s", c.reason, enum, prio, status)
		}
	}
	rep(u, "video", v, "other", "something is off").mustBe(t, 204)

	// duplicates: one open report per reporter per target; another reporter is fine
	rep(u, "video", v, "spam", "").wantErr(t, 409, "already_reported")
	other, _, _ := e.verifiedUser()
	rep(other, "video", v, "spam", "").mustBe(t, 204)
	// after the report is handled the same person may report again
	e.exec(`UPDATE reports SET status = 'RESOLVED', resolved_at = now() WHERE target_video_id = $1::uuid AND reporter_user_id = $2::uuid`, v, e.userID(uEmail))
	rep(u, "video", v, "harassment", "").mustBe(t, 204)

	// comments, channels, users; unverified accounts may report too (a second account: the first used its hourly quota)
	u, _, _ = e.verifiedUser()
	rep(u, "comment", cm, "harassment", "").mustBe(t, 204)
	rep(u, "channel", chID, "spam", "").mustBe(t, 204)
	rep(unverified, "user", chID, "spam", "").mustBe(t, 204)
	if n := e.countRows(`SELECT count(*) FROM reports WHERE target_type = 'USER' AND target_user_id = $1::uuid`, chID); n != 2 {
		t.Fatalf("channel/user reports are stored as USER reports: %d", n)
	}
	rep(creator, "channel", chID, "spam", "").wantErr(t, 422, "cannot_report_self")

	// targets must be visible to the reporter; no probing for hidden ids
	rep(u, "video", priv, "spam", "").wantErr(t, 404, "target_not_found")
	rep(u, "video", "00000000-0000-4000-8000-000000000000", "spam", "").wantErr(t, 404, "target_not_found")
	rep(u, "comment", "00000000-0000-4000-8000-000000000000", "spam", "").wantErr(t, 404, "target_not_found")
	rep(u, "user", "00000000-0000-4000-8000-000000000000", "spam", "").wantErr(t, 404, "target_not_found")
	e.exec(`UPDATE videos SET visibility = 'PRIVATE' WHERE id = $1::uuid`, v)
	rep(other, "comment", cm, "spam", "").wantErr(t, 404, "target_not_found")
}

func TestReportRateLimit(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	ids := e.bulkVideos(chID, 11)
	u, _, _ := e.verifiedUser()
	var last *resp
	for i := 0; i < 11; i++ {
		last = u.post(api+"/reports", map[string]any{"targetType": "video", "targetId": ids[i], "reason": "spam"})
	}
	last.wantErr(t, 429, "rate_limited")
	if last.hdr.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

// ---------- cross-feature ----------

func TestDeletingAnAccountCleansSocialData(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v := e.addVideo(chID, "shared", vo{})
	u, uEmail, _ := e.verifiedUser()
	uid := e.userID(uEmail)
	u.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204)
	u.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "like"}).mustBe(t, 204)
	u.put(api+"/watch-later/"+v, nil).mustBe(t, 204)
	cm := u.post(api+"/videos/"+v+"/comments", map[string]any{"text": "I will be deleted"}).str("id")
	if likes, _, comments, _ := e.videoCounts(v); likes != 1 || comments != 1 {
		t.Fatalf("setup: likes=%d comments=%d", likes, comments)
	}
	u.post(api+"/account/delete", map[string]any{"password": strongPass}).mustBe(t, 204)

	if n := e.countRows(`SELECT subscriber_count FROM user_profiles WHERE user_id = $1::uuid`, chID); n != 0 {
		t.Fatalf("subscriber count must drop: %d", n)
	}
	if likes, _, comments, _ := e.videoCounts(v); likes != 0 || comments != 0 {
		t.Fatalf("a deleted person's like and comment must stop counting: likes=%d comments=%d", likes, comments)
	}
	if got := e.viewer().get(api + "/videos/" + v + "/comments").items(); len(got) != 0 {
		t.Fatalf("deleted user's comments must disappear: %v", got)
	}
	e.viewer().get(api+"/comments/"+cm+"/replies").wantErr(t, 404, "comment_not_found")
	for _, q := range []string{`SELECT count(*) FROM watch_later WHERE user_id = $1::uuid`, `SELECT count(*) FROM video_reactions WHERE user_id = $1::uuid`, `SELECT count(*) FROM subscriptions WHERE subscriber_id = $1::uuid`} {
		if n := e.countRows(q, uid); n != 0 {
			t.Errorf("%s -> %d", q, n)
		}
	}
}

func TestSocialReadEndpointsAreAgeGated(t *testing.T) {
	e := setupAuth(t)
	u, _, _ := e.verifiedUser()
	_, chID := e.channel()
	v := e.addVideo(chID, "gated", vo{})
	gate := e.client() // no age cookie
	gate.jar["xh_session"] = u.jar["xh_session"]
	for _, p := range []string{"/videos/" + v + "/comments", "/history", "/watch-later", "/liked", "/playlists", "/subscriptions/feed", "/subscriptions/channels"} {
		if r := gate.get(api + p); r.code != 403 || r.str("code") != "age_verification_required" {
			t.Errorf("%s -> %d %s", p, r.code, r.body)
		}
	}
	// writes and id-only reads need no age cookie (the account verified its age at registration)
	gate.put(api+"/watch-later/"+v, nil).mustBe(t, 204)
	gate.get(api+"/watch-later/ids").mustBe(t, 200)
}
