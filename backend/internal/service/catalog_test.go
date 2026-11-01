package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/pagination"
	"github.com/redxtube/api/internal/repository"
)

// fakeStore records the queries the service builds and returns canned rows.
type fakeStore struct {
	listQ    repository.VideoListQuery
	listRows []repository.VideoRow
	searchQ  repository.SearchVideosQuery
	searchN  int
	chRows   []repository.ChannelRow
	plRows   []repository.PlaylistRow
	titles   []string
	names    []string
	detail   *repository.VideoDetailRow
	detailN  int
	trendOff int
	trendLim int
	trendRow []repository.VideoRow
	err      error
}

func (f *fakeStore) ListVideos(_ context.Context, q repository.VideoListQuery) ([]repository.VideoRow, error) {
	f.listQ = q
	return f.listRows, f.err
}
func (f *fakeStore) Trending(_ context.Context, _ string, _ *string, off, lim int) ([]repository.VideoRow, error) {
	f.trendOff, f.trendLim = off, lim
	return f.trendRow, f.err
}
func (f *fakeStore) VideoDetail(context.Context, string, *string) (*repository.VideoDetailRow, error) {
	f.detailN++
	return f.detail, f.err
}
func (f *fakeStore) Sitemap(context.Context, int) ([]repository.SitemapRow, error) { return nil, f.err }
func (f *fakeStore) PopularChannels(context.Context, *string, int) ([]repository.ChannelRow, error) {
	return f.chRows, f.err
}
func (f *fakeStore) Channel(context.Context, string) (*repository.ChannelDetailRow, error) {
	return nil, f.err
}
func (f *fakeStore) ChannelPlaylists(context.Context, string, int) ([]repository.PlaylistRow, error) {
	return f.plRows, f.err
}
func (f *fakeStore) SearchVideos(_ context.Context, q repository.SearchVideosQuery) ([]repository.VideoRow, int, error) {
	f.searchQ = q
	return f.listRows, f.searchN, f.err
}
func (f *fakeStore) SearchChannels(context.Context, *string, string, int) ([]repository.ChannelRow, error) {
	return f.chRows, f.err
}
func (f *fakeStore) SearchPlaylists(context.Context, string, int) ([]repository.PlaylistRow, error) {
	return f.plRows, f.err
}
func (f *fakeStore) SuggestTitles(context.Context, string, int) ([]string, error) {
	return f.titles, f.err
}
func (f *fakeStore) SuggestChannels(context.Context, string, int) ([]string, error) {
	return f.names, f.err
}

const id1 = "11111111-1111-1111-1111-111111111111"

func rows(n int) []repository.VideoRow {
	out := make([]repository.VideoRow, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = repository.VideoRow{
			ID: fmt.Sprintf("%08d-0000-0000-0000-000000000000", i+1), Title: fmt.Sprintf("v%d", i),
			PublishedAt: base.Add(-time.Duration(i) * time.Hour), Views: int64(1000 - i), CreatorID: id1, CreatorName: "C",
		}
	}
	return out
}

func newSvc(f *fakeStore) *Catalog { return NewCatalog(f, media.New("https://cdn.test")) }

func code(err error) string { return apperr.From(err).Code }

func TestVideosCategoryRouting(t *testing.T) {
	cases := []struct {
		category string
		check    func(repository.VideoListQuery) bool
	}{
		{"", func(q repository.VideoListQuery) bool { return q.Kind == "LONG" && q.Order == repository.OrderLatest }},
		{"All", func(q repository.VideoListQuery) bool { return q.Kind == "LONG" && q.CategorySlug == "" }},
		{"new", func(q repository.VideoListQuery) bool { return q.Order == repository.OrderLatest }},
		{"popular", func(q repository.VideoListQuery) bool { return q.Order == repository.OrderPopular }},
		{"shorts", func(q repository.VideoListQuery) bool { return q.Kind == "SHORT" }},
		{"live", func(q repository.VideoListQuery) bool { return q.Live != nil && *q.Live }},
		{"music", func(q repository.VideoListQuery) bool { return q.CategorySlug == "music" && q.Kind == "LONG" }},
		{"  GAMING ", func(q repository.VideoListQuery) bool { return q.CategorySlug == "gaming" }},
	}
	for _, c := range cases {
		f := &fakeStore{}
		if _, err := newSvc(f).Videos(context.Background(), c.category, ""); err != nil {
			t.Fatalf("%q: %v", c.category, err)
		}
		if !c.check(f.listQ) {
			t.Errorf("%q routed wrongly: %+v", c.category, f.listQ)
		}
	}
	for _, bad := range []string{"Bad Slug!", "a/b", "x'; drop", strings.Repeat("a", 41)} {
		if _, err := newSvc(&fakeStore{}).Videos(context.Background(), bad, ""); code(err) != "validation_failed" {
			t.Errorf("%q should be rejected, got %v", bad, err)
		}
	}
}

func TestListPagination(t *testing.T) {
	f := &fakeStore{listRows: rows(FeedPageSize + 1)}
	s := newSvc(f)
	page, err := s.Videos(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if f.listQ.Limit != FeedPageSize+1 {
		t.Fatalf("must fetch one extra row to detect the next page, limit=%d", f.listQ.Limit)
	}
	if len(page.Items) != FeedPageSize || page.NextCursor == nil {
		t.Fatalf("items=%d next=%v", len(page.Items), page.NextCursor)
	}

	// the next request resumes after the last returned row
	if _, err := s.Videos(context.Background(), "", *page.NextCursor); err != nil {
		t.Fatal(err)
	}
	last := f.listRows[FeedPageSize-1]
	if f.listQ.After == nil || f.listQ.After.ID != last.ID || !f.listQ.After.Time.Equal(last.PublishedAt) {
		t.Fatalf("cursor not applied: %+v", f.listQ.After)
	}

	// last page has no cursor
	f.listRows = rows(3)
	page, _ = s.Videos(context.Background(), "", "")
	if page.NextCursor != nil || len(page.Items) != 3 {
		t.Fatalf("last page wrong: %+v", page)
	}
	// empty results serialise as [] not null
	f.listRows = nil
	page, _ = s.Videos(context.Background(), "", "")
	if page.Items == nil {
		t.Fatal("items must be an empty slice, not nil")
	}
}

func TestCursorIsBoundToItsListing(t *testing.T) {
	f := &fakeStore{listRows: rows(FeedPageSize + 1)}
	s := newSvc(f)
	latest, _ := s.Videos(context.Background(), "", "")
	if _, err := s.Videos(context.Background(), "popular", *latest.NextCursor); code(err) != "invalid_cursor" {
		t.Fatalf("a latest cursor must not work for popular, got %v", err)
	}
	for _, bad := range []string{"garbage", "e30", ""} {
		if bad == "" {
			continue
		}
		if _, err := s.Videos(context.Background(), "", bad); code(err) != "invalid_cursor" {
			t.Errorf("cursor %q accepted", bad)
		}
	}
	tampered, _ := pagination.Encode(keyCursor{K: "latest", T: 1, I: "not-a-uuid"})
	if _, err := s.Videos(context.Background(), "", tampered); code(err) != "invalid_cursor" {
		t.Error("non-uuid cursor id accepted")
	}
}

func TestStoreErrorsAreInternal(t *testing.T) {
	f := &fakeStore{err: errors.New("pq: connection refused to 10.0.0.1")}
	_, err := newSvc(f).Videos(context.Background(), "", "")
	ae := apperr.From(err)
	if ae.Status != 500 || strings.Contains(ae.Message, "10.0.0.1") {
		t.Fatalf("internal error leaked: %+v", ae)
	}
}

func TestTrending(t *testing.T) {
	f := &fakeStore{trendRow: rows(FeedPageSize + 1)}
	s := newSvc(f)
	if _, err := s.Trending(context.Background(), "bogus", ""); code(err) != "validation_failed" {
		t.Fatalf("bogus tab accepted: %v", err)
	}
	page, err := s.Trending(context.Background(), "", "") // default tab
	if err != nil || page.NextCursor == nil || f.trendOff != 0 {
		t.Fatalf("first page: %v %+v off=%d", err, page, f.trendOff)
	}
	// walking to the end yields exactly TrendingMax items and then stops,
	// even if the store over-returns rows
	total := len(page.Items)
	cur := page.NextCursor
	for pages := 0; cur != nil; pages++ {
		if pages > 20 {
			t.Fatal("pagination did not terminate")
		}
		next, err := s.Trending(context.Background(), "now", *cur)
		if err != nil {
			t.Fatal(err)
		}
		if f.trendOff+f.trendLim > TrendingMax {
			t.Fatalf("window %d+%d exceeds cap %d", f.trendOff, f.trendLim, TrendingMax)
		}
		total += len(next.Items)
		cur = next.NextCursor
	}
	if total != TrendingMax {
		t.Fatalf("walked %d items, want %d", total, TrendingMax)
	}
	// cursor for another tab, or an out-of-range offset, is rejected
	other, _ := pagination.Encode(offsetCursor{K: "trending:recent", O: 12})
	if _, err := s.Trending(context.Background(), "now", other); code(err) != "invalid_cursor" {
		t.Error("cross-tab cursor accepted")
	}
	huge, _ := pagination.Encode(offsetCursor{K: "trending:now", O: 5000})
	if _, err := s.Trending(context.Background(), "now", huge); code(err) != "invalid_cursor" {
		t.Error("huge offset accepted")
	}
}

func TestVideoDetail(t *testing.T) {
	f := &fakeStore{}
	s := newSvc(f)
	if _, err := s.VideoDetail(context.Background(), "../etc/passwd", nil); code(err) != "video_not_found" || f.detailN != 0 {
		t.Fatalf("invalid id must 404 without touching the database (calls=%d)", f.detailN)
	}
	if _, err := s.VideoDetail(context.Background(), id1, nil); code(err) != "video_not_found" {
		t.Fatalf("missing video: %v", err)
	}
	f.detail = &repository.VideoDetailRow{VideoRow: rows(1)[0], Description: "d", Subscribers: 7, Likes: 3}
	d, err := s.VideoDetail(context.Background(), id1, nil)
	if err != nil || d.StreamURL != nil || d.Reaction != nil || d.Creator.Subscribers != 7 || d.Likes != 3 {
		t.Fatalf("detail: %v %+v", err, d)
	}
	if d.Captions == nil {
		t.Fatal("captions must be [] not null")
	}
	key := "videos/" + id1 + "/hls/master.m3u8"
	f.detail.HLSKey = &key
	d, _ = s.VideoDetail(context.Background(), id1, nil)
	if d.StreamURL == nil || *d.StreamURL != "https://cdn.test/"+key {
		t.Fatalf("stream url: %v", d.StreamURL)
	}
}

func TestSearchValidation(t *testing.T) {
	s := newSvc(&fakeStore{})
	for name, p := range map[string]SearchParams{
		"sort": {Q: "x", Sort: "evil"}, "type": {Q: "x", Type: "all; drop"}, "duration": {Q: "x", Duration: "huge"}, "date": {Q: "x", Date: "decade"},
	} {
		if _, err := s.Search(context.Background(), p, ""); code(err) != "validation_failed" {
			t.Errorf("%s accepted", name)
		}
	}
	page, err := s.Search(context.Background(), SearchParams{Q: "   "}, "")
	if err != nil || len(page.Items) != 0 || page.Items == nil || page.Total != 0 {
		t.Fatalf("blank query: %v %+v", err, page)
	}
}

func TestSearchCombinesChannelsPlaylistsAndVideos(t *testing.T) {
	f := &fakeStore{
		chRows:   []repository.ChannelRow{{ID: id1, Name: "Neon"}, {ID: id1, Name: "Neon 2"}},
		plRows:   []repository.PlaylistRow{{ID: id1, Title: "Neon list"}},
		listRows: rows(7), searchN: 30,
	}
	s := newSvc(f)
	page, err := s.Search(context.Background(), SearchParams{Q: "neon"}, "")
	if err != nil {
		t.Fatal(err)
	}
	kinds := ""
	for _, it := range page.Items {
		kinds += it.Kind[:1]
	}
	if kinds != "ccpvvvvvvv" || len(page.Items) != SearchPageSize {
		t.Fatalf("first page order/size wrong: %s (%d)", kinds, len(page.Items))
	}
	if f.searchQ.Limit != 7 || f.searchQ.Offset != 0 {
		t.Fatalf("videos should fill the rest of the page: %+v", f.searchQ)
	}
	if page.Total != 33 || page.NextCursor == nil {
		t.Fatalf("total=%d next=%v", page.Total, page.NextCursor)
	}

	// page 2: all non-video results are behind us, video offset accounts for the 3 above
	f.listRows = rows(10)
	page2, err := s.Search(context.Background(), SearchParams{Q: "neon"}, *page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if f.searchQ.Offset != 7 || f.searchQ.Limit != SearchPageSize || len(page2.Items) != 10 {
		t.Fatalf("page 2 window wrong: %+v items=%d", f.searchQ, len(page2.Items))
	}
	for _, it := range page2.Items {
		if it.Kind != "video" {
			t.Fatalf("unexpected %s on page 2", it.Kind)
		}
	}
}

func TestSearchFilters(t *testing.T) {
	f := &fakeStore{}
	s := newSvc(f)
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }

	_, _ = s.Search(context.Background(), SearchParams{Q: "x", Duration: "short", Date: "week", Sort: "views"}, "")
	if f.searchQ.MaxDur == nil || *f.searchQ.MaxDur != 239 || f.searchQ.MinDur != nil || f.searchQ.Sort != "views" {
		t.Fatalf("short: %+v", f.searchQ)
	}
	if f.searchQ.Since == nil || !f.searchQ.Since.Equal(fixed.Add(-7*24*time.Hour)) {
		t.Fatalf("week: %v", f.searchQ.Since)
	}
	_, _ = s.Search(context.Background(), SearchParams{Q: "x", Duration: "medium"}, "")
	if *f.searchQ.MinDur != 240 || *f.searchQ.MaxDur != 1200 {
		t.Fatalf("medium: %+v", f.searchQ)
	}
	_, _ = s.Search(context.Background(), SearchParams{Q: "x", Duration: "long", Type: "live"}, "")
	if *f.searchQ.MinDur != 1201 || f.searchQ.MaxDur != nil || !f.searchQ.LiveOnly {
		t.Fatalf("long/live: %+v", f.searchQ)
	}
}

func TestSearchRejectsDeepPagingAndOverlongQuery(t *testing.T) {
	f := &fakeStore{}
	s := newSvc(f)
	deep, _ := pagination.Encode(offsetCursor{K: "search", O: SearchMaxOffset + 10})
	if _, err := s.Search(context.Background(), SearchParams{Q: "x"}, deep); code(err) != "invalid_cursor" {
		t.Error("deep offset accepted")
	}
	_, _ = s.Search(context.Background(), SearchParams{Q: strings.Repeat("é", 500)}, "")
	if n := len([]rune(f.searchQ.Q)); n != MaxQueryRunes {
		t.Fatalf("query not truncated to %d runes: %d", MaxQueryRunes, n)
	}
}

func TestSuggest(t *testing.T) {
	f := &fakeStore{titles: []string{"Neon City Drive", "neon studio", "Late Neon Night"}, names: []string{"Neon Studio"}}
	got, err := newSvc(f).Suggest(context.Background(), "neon")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "Neon Studio|Neon City Drive|Late Neon Night" {
		t.Fatalf("dedupe/order wrong: %v", got)
	}
	for _, q := range []string{"", " ", "a"} {
		got, _ := newSvc(f).Suggest(context.Background(), q)
		if got == nil || len(got) != 0 {
			t.Errorf("%q should give an empty list, got %v", q, got)
		}
	}
	many := &fakeStore{}
	for i := 0; i < 20; i++ {
		many.titles = append(many.titles, fmt.Sprintf("neon %d", i))
	}
	if got, _ := newSvc(many).Suggest(context.Background(), "neon"); len(got) != 8 {
		t.Fatalf("must cap at 8, got %d", len(got))
	}
}

func TestBuildLinksOnlyExposesSafeURLs(t *testing.T) {
	site := "javascript:alert(1)"
	if got := buildLinks(&site, nil); len(got) != 0 {
		t.Fatalf("javascript: URL exposed: %v", got)
	}
	ok := "https://example.com/me"
	social := []byte(`[{"label":"Twitter","url":"https://x.example/a"},{"label":"Evil","url":"javascript:alert(1)"},
	  {"label":"Data","url":"data:text/html,x"},{"label":"Ftp","url":"ftp://x.example"},{"label":"","url":"https://blank.example"},
	  {"label":"Insta","url":"https://i.example/b"}]`)
	got := buildLinks(&ok, social)
	var labels []string
	for _, l := range got {
		labels = append(labels, l.Label)
	}
	if strings.Join(labels, ",") != "Website,Twitter,Insta" {
		t.Fatalf("labels (website first, safe only, stored order): %v", labels)
	}
	for _, bad := range []string{`not json`, `{"twitter":"https://x.example"}`, `[1,2]`, ``} {
		if got := buildLinks(nil, []byte(bad)); len(got) != 0 {
			t.Fatalf("%q must yield no links: %v", bad, got)
		}
	}
	big := `[`
	for i := 0; i < 20; i++ {
		big += fmt.Sprintf(`{"label":"k%02d","url":"https://e.example/%d"},`, i, i)
	}
	big = strings.TrimSuffix(big, ",") + `]`
	if got := buildLinks(nil, []byte(big)); len(got) != 8 {
		t.Fatalf("link count must be capped at 8, got %d", len(got))
	}
}

func TestChannelVideosValidation(t *testing.T) {
	f := &fakeStore{}
	s := newSvc(f)
	if _, err := s.ChannelVideos(context.Background(), "nope", "videos", "latest", ""); code(err) != "channel_not_found" {
		t.Error("invalid channel id must be 404")
	}
	if _, err := s.ChannelVideos(context.Background(), id1, "evil", "worst", ""); code(err) != "validation_failed" {
		t.Error("bad kind/sort accepted")
	}
	_, _ = s.ChannelVideos(context.Background(), id1, "shorts", "popular", "")
	if f.listQ.Kind != "SHORT" || f.listQ.Order != repository.OrderPopular || f.listQ.CreatorID != id1 {
		t.Fatalf("shorts/popular: %+v", f.listQ)
	}
	_, _ = s.ChannelVideos(context.Background(), id1, "", "", "")
	if f.listQ.Kind != "LONG" || f.listQ.Live == nil || *f.listQ.Live {
		t.Fatalf("default tab must exclude live: %+v", f.listQ)
	}
}

func TestReportReasonsMatchDatabaseEnum(t *testing.T) {
	want := map[string]bool{"non_consensual": true, "underage": true, "illegal": true, "copyright": true, "harassment": true, "spam": true, "other": true}
	for _, r := range newSvc(&fakeStore{}).ReportReasons() {
		if !want[r.ID] || r.Label == "" {
			t.Errorf("unexpected reason %+v", r)
		}
		delete(want, r.ID)
	}
	if len(want) != 0 {
		t.Errorf("missing reasons: %v", want)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := repository.EscapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("got %s", got)
	}
}

func TestNormalizeQuery(t *testing.T) {
	cases := map[string]string{
		"  neon   city \t drive ": "neon city drive",
		"a\x00b":                  "a b",
		"bad\xffbytes":            "badbytes",
		"line\nbreak\r":           "line break",
		"":                        "",
	}
	for in, want := range cases {
		if got := normalizeQuery(in); got != want {
			t.Errorf("normalizeQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
