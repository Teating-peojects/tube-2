package handler_test

import (
	"fmt"
	"strings"
	"testing"
)

const api = "/api/v1"

// ---------- reactions ----------

func TestReactions(t *testing.T) {
	e := setupAuth(t)
	_, cid_ := e.channel()
	v := e.addVideo(cid_, "reaction target", vo{})
	viewer, _, _ := e.verifiedUser()
	unverified, _, _ := e.newUser()
	anon := e.viewer()

	anon.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "like"}).wantErr(t, 401, "unauthenticated")
	unverified.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "like"}).wantErr(t, 403, "verification_required")

	viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "like"}).mustBe(t, 204)
	d := viewer.get(api + "/videos/" + v)
	if d.m["likes"] != float64(1) || d.m["dislikes"] != float64(0) || d.m["reaction"] != "like" {
		t.Fatalf("viewer detail: %s", d.body)
	}
	if a := anon.get(api + "/videos/" + v); a.m["likes"] != float64(1) || a.m["reaction"] != nil {
		t.Fatalf("anonymous must see counts but no personal reaction: %s", a.body)
	}
	viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "like"}).mustBe(t, 204) // idempotent
	if l, _, _, _ := e.videoCounts(v); l != 1 {
		t.Fatalf("repeated PUT must not double count: %d", l)
	}

	viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "dislike"}).mustBe(t, 204)
	if l, dl, _, _ := e.videoCounts(v); l != 0 || dl != 1 {
		t.Fatalf("switch like->dislike: %d/%d", l, dl)
	}
	viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": nil}).mustBe(t, 204)
	if l, dl, _, _ := e.videoCounts(v); l != 0 || dl != 0 {
		t.Fatalf("cleared: %d/%d", l, dl)
	}
	if d := viewer.get(api + "/videos/" + v); d.m["reaction"] != nil {
		t.Fatalf("reaction not cleared: %s", d.body)
	}

	for _, bad := range []any{"love", "LIKE", "", 5} {
		if r := viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": bad}); r.code != 422 && r.code != 400 {
			t.Errorf("reaction %v accepted: %d", bad, r.code)
		}
	}
	viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "like", "admin": true}).wantErr(t, 400, "invalid_json")

	viewer.put(api+"/videos/00000000-0000-4000-8000-000000000000/reaction", map[string]any{"reaction": "like"}).wantErr(t, 404, "video_not_found")
	viewer.put(api+"/videos/not-a-uuid/reaction", map[string]any{"reaction": "like"}).wantErr(t, 404, "video_not_found")

	// liked list follows the reaction
	viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "like"}).mustBe(t, 204)
	if ids := viewer.get(api + "/liked").itemIDs(); len(ids) != 1 || ids[0] != v {
		t.Fatalf("liked list: %v", ids)
	}
	other, _, _ := e.verifiedUser()
	if ids := other.get(api + "/liked").itemIDs(); len(ids) != 0 {
		t.Fatal("liked list leaked between users")
	}
	viewer.put(api+"/videos/"+v+"/reaction", map[string]any{"reaction": "dislike"}).mustBe(t, 204)
	if ids := viewer.get(api + "/liked").itemIDs(); len(ids) != 0 {
		t.Fatalf("disliked video must leave the liked list: %v", ids)
	}
	viewer.del(api+"/liked").wantErr(t, 404, "not_found")
}

func TestReactionsRespectVisibilityAndRateLimit(t *testing.T) {
	e := setupAuth(t)
	owner, oid := e.channel()
	private := e.addVideo(oid, "private", vo{vis: "PRIVATE"})
	draft := e.addVideo(oid, "draft", vo{status: "PROCESSING"})
	removed := e.addVideo(oid, "removed", vo{deleted: true})
	pub := e.addVideo(oid, "public", vo{})
	viewer, _, _ := e.verifiedUser()
	for _, id := range []string{private, draft, removed} {
		viewer.put(api+"/videos/"+id+"/reaction", map[string]any{"reaction": "like"}).wantErr(t, 404, "video_not_found")
	}
	owner.put(api+"/videos/"+private+"/reaction", map[string]any{"reaction": "like"}).mustBe(t, 204) // owners can react to their own private video

	var last *resp
	for i := 0; i < 61; i++ {
		last = viewer.put(api+"/videos/"+pub+"/reaction", map[string]any{"reaction": []string{"like", "dislike"}[i%2]})
	}
	last.wantErr(t, 429, "rate_limited")
	if last.hdr.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

// ---------- subscriptions ----------

func TestSubscriptions(t *testing.T) {
	e := setupAuth(t)
	creator, chID := e.channel()
	vid1 := e.addVideo(chID, "channel video", vo{})
	sub, _, _ := e.verifiedUser()
	anon := e.viewer()
	unverified, _, _ := e.newUser()

	anon.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).wantErr(t, 401, "unauthenticated")
	unverified.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).wantErr(t, 403, "verification_required")
	sub.put(api+"/channels/"+chID+"/subscription", map[string]any{}).wantErr(t, 422, "validation_failed")

	sub.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204)
	sub.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204) // idempotent

	ch := sub.get(api + "/channels/" + chID)
	if ch.m["isSubscribed"] != true || ch.m["subscribers"] != float64(1) {
		t.Fatalf("channel (subscriber): %s", ch.body)
	}
	if a := anon.get(api + "/channels/" + chID); a.m["isSubscribed"] != false || a.m["subscribers"] != float64(1) {
		t.Fatalf("channel (anon): %s", a.body)
	}
	if d := sub.get(api + "/videos/" + vid1); d.m["creator"].(map[string]any)["isSubscribed"] != true {
		t.Fatalf("video detail creator.isSubscribed: %s", d.body)
	}
	var popular []map[string]any
	_ = jsonInto(sub.get(api+"/channels/popular").body, &popular)
	if len(popular) != 1 || popular[0]["isSubscribed"] != true {
		t.Fatalf("popular: %v", popular)
	}
	sr := sub.get(api + "/search?q=" + "a&type=channels")
	_ = sr

	if list := sub.get(api + "/subscriptions/channels"); !strings.Contains(string(list.body), chID) || !strings.Contains(string(list.body), `"isSubscribed":true`) {
		t.Fatalf("subscription list: %s", list.body)
	}

	sub.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": false}).mustBe(t, 204)
	sub.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": false}).mustBe(t, 204)
	if ch := sub.get(api + "/channels/" + chID); ch.m["isSubscribed"] != false || ch.m["subscribers"] != float64(0) {
		t.Fatalf("after unsubscribe: %s", ch.body)
	}

	creator.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).wantErr(t, 422, "cannot_subscribe_to_self")
	sub.put(api+"/channels/00000000-0000-4000-8000-000000000000/subscription", map[string]any{"subscribed": true}).wantErr(t, 404, "channel_not_found")
	sub.put(api+"/channels/xyz/subscription", map[string]any{"subscribed": true}).wantErr(t, 404, "channel_not_found")
	e.exec(`UPDATE users SET status = 'SUSPENDED' WHERE id = $1::uuid`, chID)
	sub.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).wantErr(t, 404, "channel_not_found")
}

func TestSubscriptionFeed(t *testing.T) {
	e := setupAuth(t)
	_, chA := e.channel()
	_, chB := e.channel()
	for i := 0; i < 13; i++ {
		e.addVideo(chA, fmt.Sprintf("a%d", i), vo{published: fmt.Sprintf("now() - interval '%d hours'", i+1)})
	}
	e.addVideo(chA, "a private", vo{vis: "PRIVATE"})
	e.addVideo(chA, "a short", vo{kind: "SHORT", dur: 20})
	other := e.addVideo(chB, "b video", vo{})
	sub, _, _ := e.verifiedUser()

	if ids := sub.get(api + "/subscriptions/feed").itemIDs(); len(ids) != 0 {
		t.Fatalf("empty feed before subscribing: %v", ids)
	}
	sub.put(api+"/channels/"+chA+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204)
	first := sub.get(api + "/subscriptions/feed")
	if len(first.items()) != 12 || first.next() == "" {
		t.Fatalf("first page: %d items next=%q", len(first.items()), first.next())
	}
	all := sub.walkAll(api + "/subscriptions/feed")
	if len(all) != 13 {
		t.Fatalf("feed must hold exactly the 13 public videos (no private, no shorts): %d", len(all))
	}
	seen := map[string]bool{}
	for _, id := range all {
		if seen[id] {
			t.Fatalf("duplicate %s", id)
		}
		seen[id] = true
	}
	if has(all, other) {
		t.Fatal("feed leaked a channel the user does not subscribe to")
	}
	e.client().get(api+"/subscriptions/feed").wantErr(t, 403, "age_verification_required")
	e.viewer().get(api+"/subscriptions/feed").wantErr(t, 401, "unauthenticated")
}

func TestSubscriberNotifications(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	a, _, _ := e.verifiedUser()
	b, _, _ := e.verifiedUser()
	a.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204)
	a.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": false}).mustBe(t, 204)
	a.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204)
	if n := e.notificationCount(chID, "NEW_SUBSCRIBER"); n != 1 {
		t.Fatalf("toggling must not spam the creator: %d notifications", n)
	}
	e.exec(`UPDATE user_settings SET subscriber_notifications = false WHERE user_id = $1::uuid`, chID)
	b.put(api+"/channels/"+chID+"/subscription", map[string]any{"subscribed": true}).mustBe(t, 204)
	if n := e.notificationCount(chID, "NEW_SUBSCRIBER"); n != 1 {
		t.Fatalf("a creator who turned the setting off must not be notified: %d", n)
	}
}

// ---------- comments ----------

func TestCommentsFlow(t *testing.T) {
	e := setupAuth(t)
	creator, chID := e.channel()
	v := e.addVideo(chID, "comment target", vo{})
	a, aEmail, _ := e.verifiedUser()
	b, bEmail, _ := e.verifiedUser()
	aID, bID := e.userID(aEmail), e.userID(bEmail)
	unverified, _, _ := e.newUser()
	anon := e.viewer()
	cm := api + "/videos/" + v + "/comments"

	anon.post(cm, map[string]any{"text": "hi"}).wantErr(t, 401, "unauthenticated")
	unverified.post(cm, map[string]any{"text": "hi"}).wantErr(t, 403, "verification_required")

	r := a.post(cm, map[string]any{"text": "Great video!"})
	r.mustBe(t, 201)
	author := r.m["author"].(map[string]any)
	if r.str("text") != "Great video!" || author["id"] != aID || author["isCreator"] != false || r.m["likes"] != float64(0) || r.m["liked"] != false || r.m["replyCount"] != float64(0) || r.m["pinned"] != false {
		t.Fatalf("created comment: %s", r.body)
	}
	aComment := r.str("id")
	time_sleep()
	cr := creator.post(cm, map[string]any{"text": "Thanks for watching"})
	cr.mustBe(t, 201)
	if cr.m["author"].(map[string]any)["isCreator"] != true {
		t.Fatalf("the video's creator must carry the creator badge: %s", cr.body)
	}
	creatorComment := cr.str("id")
	if _, _, n, _ := e.videoCounts(v); n != 2 {
		t.Fatalf("comment_count = %d", n)
	}
	if d := anon.get(api + "/videos/" + v); d.m["commentCount"] != float64(2) {
		t.Fatalf("detail commentCount: %s", d.body)
	}

	// sorting
	nw := anon.get(cm + "?sort=new")
	if ids := nw.itemIDs(); len(ids) != 2 || ids[0] != creatorComment || ids[1] != aComment {
		t.Fatalf("sort=new must be newest first: %v", ids)
	}
	b.put(api+"/comments/"+creatorComment+"/like", map[string]any{"liked": true}).mustBe(t, 204)
	a.put(api+"/comments/"+creatorComment+"/like", map[string]any{"liked": true}).mustBe(t, 204)
	b.put(api+"/comments/"+aComment+"/like", map[string]any{"liked": true}).mustBe(t, 204)
	top := b.get(cm + "?sort=top")
	if ids := top.itemIDs(); ids[0] != creatorComment || ids[1] != aComment {
		t.Fatalf("sort=top must rank by likes: %v", ids)
	}
	if top.items()[0]["liked"] != true || top.items()[0]["likes"] != float64(2) {
		t.Fatalf("liked flag/count for b: %v", top.items()[0])
	}
	if anon.get(cm).items()[0]["liked"] != false {
		t.Fatal("anonymous viewers never see liked=true")
	}
	if r := anon.get(cm + "?sort=weird"); r.code != 422 {
		t.Fatalf("bad sort accepted: %d", r.code)
	}

	// like is idempotent; unlike works; needs a verified account
	b.put(api+"/comments/"+aComment+"/like", map[string]any{"liked": true}).mustBe(t, 204)
	if n := e.countRows(`SELECT like_count FROM comments WHERE id = $1::uuid`, aComment); n != 1 {
		t.Fatalf("repeated like double counted: %d", n)
	}
	b.put(api+"/comments/"+aComment+"/like", map[string]any{"liked": false}).mustBe(t, 204)
	if n := e.countRows(`SELECT like_count FROM comments WHERE id = $1::uuid`, aComment); n != 0 {
		t.Fatalf("unlike: %d", n)
	}
	unverified.put(api+"/comments/"+aComment+"/like", map[string]any{"liked": true}).wantErr(t, 403, "verification_required")
	b.put(api+"/comments/"+aComment+"/like", map[string]any{}).wantErr(t, 422, "validation_failed")
	b.put(api+"/comments/"+"00000000-0000-4000-8000-000000000000"+"/like", map[string]any{"liked": true}).wantErr(t, 404, "comment_not_found")

	// replies, including a reply to a reply (attached to the top-level comment)
	rep := b.post(cm, map[string]any{"text": "I agree", "parentId": aComment})
	rep.mustBe(t, 201)
	reply1 := rep.str("id")
	rep2 := a.post(cm, map[string]any{"text": "thanks!", "parentId": reply1})
	rep2.mustBe(t, 201)
	reply2 := rep2.str("id")
	if n := e.countRows(`SELECT reply_count FROM comments WHERE id = $1::uuid`, aComment); n != 2 {
		t.Fatalf("top-level reply_count = %d (reply-to-reply must flatten onto it)", n)
	}
	if n := e.countRows(`SELECT reply_count FROM comments WHERE id = $1::uuid`, reply1); n != 0 {
		t.Fatalf("replies cannot have replies: %d", n)
	}
	if _, _, n, _ := e.videoCounts(v); n != 4 {
		t.Fatalf("comment_count counts replies too: %d", n)
	}
	rr := anon.get(api + "/comments/" + aComment + "/replies")
	if ids := rr.itemIDs(); len(ids) != 2 || ids[0] != reply1 || ids[1] != reply2 {
		t.Fatalf("replies must be oldest first: %v", ids)
	}
	if got := anon.get(cm + "?sort=new").itemIDs(); has(got, reply1) || has(got, reply2) {
		t.Fatal("replies must not appear among top-level comments")
	}
	// bad parents
	other := e.addVideo(chID, "other video", vo{})
	a.post(api+"/videos/"+other+"/comments", map[string]any{"text": "wrong video", "parentId": aComment}).wantErr(t, 404, "comment_not_found")
	a.post(cm, map[string]any{"text": "bad parent", "parentId": "nope"}).wantErr(t, 422, "validation_failed")

	// notifications go to the right people, never to yourself
	if n := e.notificationCount(chID, "NEW_COMMENT"); n != 3 { // a's top-level, b's reply, a's reply-to-reply (creator's own comment excluded)
		t.Fatalf("creator NEW_COMMENT notifications = %d", n)
	}
	if n := e.notificationCount(aID, "COMMENT_REPLY"); n != 1 { // b replied to a; a's own reply-to-reply is not notified to a
		t.Fatalf("a COMMENT_REPLY notifications = %d", n)
	}
	if n := e.notificationCount(bID, "COMMENT_REPLY"); n != 1 { // a replied to b's reply
		t.Fatalf("b COMMENT_REPLY notifications = %d", n)
	}
	if n := e.notificationCount(chID, "COMMENT_REPLY"); n != 0 {
		t.Fatalf("creator must not get COMMENT_REPLY for others' threads: %d", n)
	}
}

func time_sleep() { sleepMs(15) }

func TestCommentValidationAndAbuse(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v := e.addVideo(chID, "target", vo{})
	u, _, _ := e.verifiedUser()
	cm := api + "/videos/" + v + "/comments"

	for name, text := range map[string]string{"empty": "", "spaces": "   \n\t ", "too long": strings.Repeat("x", 2001), "only control": "\x00\x07\u202e"} {
		u.post(cm, map[string]any{"text": text}).wantErr(t, 422, "validation_failed")
		_ = name
	}
	exact := strings.Repeat("é", 2000)
	u.post(cm, map[string]any{"text": exact}).mustBe(t, 201)

	// stored as plain text, never altered into HTML: the frontend renders it as text
	xss := `<script>alert(1)</script> <img src=x onerror=alert(1)>`
	r := u.post(cm, map[string]any{"text": xss})
	r.mustBe(t, 201)
	if r.str("text") != xss {
		t.Fatalf("text must round-trip unchanged: %q", r.str("text"))
	}
	if got := u.post(cm, map[string]any{"text": "a\x00b\u202ec\n\n\n\n\nd"}); got.str("text") != "abc\n\nd" {
		t.Fatalf("control/bidi chars stripped and blank lines collapsed: %q", got.str("text"))
	}
	if r := u.post(cm, map[string]any{"text": "ok", "role": "ADMIN"}); r.code != 400 {
		t.Fatalf("unknown fields: %d", r.code)
	}
	// duplicates: same words (any case/spacing of case) within 5 minutes
	u.post(cm, map[string]any{"text": "Buy my stuff"}).mustBe(t, 201)
	u.post(cm, map[string]any{"text": "buy MY stuff"}).wantErr(t, 409, "duplicate_comment")
}

func TestCommentRateLimit(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v := e.addVideo(chID, "target", vo{})
	u, _, _ := e.verifiedUser()
	var last *resp
	for i := 0; i < 11; i++ {
		last = u.post(api+"/videos/"+v+"/comments", map[string]any{"text": fmt.Sprintf("distinct comment number %d", i)})
	}
	last.wantErr(t, 429, "rate_limited")
	if last.hdr.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestCommentDeletion(t *testing.T) {
	e := setupAuth(t)
	creator, chID := e.channel()
	v := e.addVideo(chID, "target", vo{})
	a, aEmail, _ := e.verifiedUser()
	b, _, _ := e.verifiedUser()
	mod, modEmail, _ := e.verifiedUser()
	e.exec(`UPDATE users SET role = 'MODERATOR' WHERE email = $1`, modEmail)
	cm := api + "/videos/" + v + "/comments"

	top := a.post(cm, map[string]any{"text": "top comment"}).str("id")
	b.post(cm, map[string]any{"text": "reply one", "parentId": top}).mustBe(t, 201)
	reply2 := a.post(cm, map[string]any{"text": "reply two", "parentId": top}).str("id")
	if _, _, n, _ := e.videoCounts(v); n != 3 {
		t.Fatalf("count before: %d", n)
	}

	b.del(api+"/comments/"+top).wantErr(t, 403, "forbidden") // not the author
	e.client().del(api+"/comments/"+top).wantErr(t, 401, "unauthenticated")
	// deleting one reply fixes the parent's reply_count and the video count
	a.del(api+"/comments/"+reply2).mustBe(t, 204)
	if n := e.countRows(`SELECT reply_count FROM comments WHERE id = $1::uuid`, top); n != 1 {
		t.Fatalf("reply_count after deleting a reply: %d", n)
	}
	if _, _, n, _ := e.videoCounts(v); n != 2 {
		t.Fatalf("count after reply delete: %d", n)
	}
	// deleting the top-level comment takes its remaining replies with it
	a.del(api+"/comments/"+top).mustBe(t, 204)
	if _, _, n, _ := e.videoCounts(v); n != 0 {
		t.Fatalf("count after thread delete: %d", n)
	}
	if ids := e.client().get(cm).itemIDs(); len(ids) != 0 {
		t.Fatal("deleted comments must disappear")
	}
	a.del(api+"/comments/"+top).wantErr(t, 404, "comment_not_found")
	e.viewer().get(api+"/comments/"+top+"/replies").wantErr(t, 404, "comment_not_found")
	if e.countRows(`SELECT count(*) FROM audit_logs WHERE action = 'comment.delete'`) != 0 {
		t.Fatal("authors deleting their own comments is not a moderation act")
	}

	// the video's creator and moderators can remove others' comments, and it is audited
	c1 := a.post(cm, map[string]any{"text": "remove me (creator)"}).str("id")
	creator.del(api+"/comments/"+c1).mustBe(t, 204)
	c2 := a.post(cm, map[string]any{"text": "remove me (moderator)"}).str("id")
	mod.del(api+"/comments/"+c2).mustBe(t, 204)
	if n := e.countRows(`SELECT count(*) FROM audit_logs WHERE action = 'comment.delete' AND metadata ? 'request_id'`); n != 2 {
		t.Fatalf("moderation deletions must be audited with the actor: %d", n)
	}
	_ = aEmail
}

func TestPinnedComment(t *testing.T) {
	e := setupAuth(t)
	creator, chID := e.channel()
	v := e.addVideo(chID, "target", vo{})
	a, _, _ := e.verifiedUser()
	b, _, _ := e.verifiedUser()
	cm := api + "/videos/" + v + "/comments"
	c1 := a.post(cm, map[string]any{"text": "first"}).str("id")
	sleepMs(15)
	c2 := b.post(cm, map[string]any{"text": "second"}).str("id")
	sleepMs(15)
	reply := a.post(cm, map[string]any{"text": "a reply", "parentId": c1}).str("id")

	a.put(api+"/comments/"+c1+"/pin", map[string]any{"pinned": true}).wantErr(t, 403, "forbidden")
	e.client().put(api+"/comments/"+c1+"/pin", map[string]any{"pinned": true}).wantErr(t, 401, "unauthenticated")
	creator.put(api+"/comments/"+c1+"/pin", map[string]any{}).wantErr(t, 422, "validation_failed")
	creator.put(api+"/comments/"+reply+"/pin", map[string]any{"pinned": true}).wantErr(t, 422, "not_pinnable")

	creator.put(api+"/comments/"+c1+"/pin", map[string]any{"pinned": true}).mustBe(t, 204)
	for _, sort := range []string{"new", "top"} {
		got := e.viewer().get(cm + "?sort=" + sort)
		if ids := got.itemIDs(); len(ids) != 2 || ids[0] != c1 || got.items()[0]["pinned"] != true {
			t.Fatalf("sort=%s: pinned comment must lead: %v", sort, ids)
		}
	}
	// only one pinned comment per video: pinning another moves the pin
	creator.put(api+"/comments/"+c2+"/pin", map[string]any{"pinned": true}).mustBe(t, 204)
	if ids := e.viewer().get(cm + "?sort=new").itemIDs(); ids[0] != c2 || len(ids) != 2 {
		t.Fatalf("pin should move: %v", ids)
	}
	if n := e.countRows(`SELECT count(*) FROM comments WHERE video_id = $1::uuid AND is_pinned`, v); n != 1 {
		t.Fatalf("pinned comments: %d", n)
	}
	creator.put(api+"/comments/"+c2+"/pin", map[string]any{"pinned": false}).mustBe(t, 204)
	if got := e.viewer().get(cm + "?sort=new"); got.items()[0]["pinned"] != false {
		t.Fatal("unpin failed")
	}
	// deleting the pinned comment clears the pin
	creator.put(api+"/comments/"+c1+"/pin", map[string]any{"pinned": true}).mustBe(t, 204)
	a.del(api+"/comments/"+c1).mustBe(t, 204)
	if n := e.countRows(`SELECT count(*) FROM comments WHERE video_id = $1::uuid AND is_pinned AND deleted_at IS NULL`, v); n != 0 {
		t.Fatalf("pin must disappear with the comment: %d", n)
	}
}

func TestCommentPaginationWithTies(t *testing.T) {
	e := setupAuth(t)
	_, chID := e.channel()
	v := e.addVideo(chID, "target", vo{})
	u, uEmail, _ := e.verifiedUser()
	uid := e.userID(uEmail)
	// 45 comments with IDENTICAL created_at and like counts: only the id breaks ties
	e.exec(`INSERT INTO comments (id, video_id, video_creator_id, user_id, content, created_at, like_count)
SELECT gen_random_uuid(), $1::uuid, (SELECT creator_id FROM videos WHERE id = $1::uuid), $2::uuid, 'c' || g, TIMESTAMPTZ '2026-03-01 10:00:00+00', CASE WHEN g % 2 = 0 THEN 5 ELSE 0 END FROM generate_series(1, 45) g`, v, uid)
	for _, sort := range []string{"new", "top"} {
		first := u.get(api + "/videos/" + v + "/comments?sort=" + sort)
		if len(first.items()) != 20 || first.next() == "" {
			t.Fatalf("sort=%s first page: %d next=%q", sort, len(first.items()), first.next())
		}
		all := u.walkAll(api + "/videos/" + v + "/comments?sort=" + sort)
		seen := map[string]bool{}
		for _, id := range all {
			if seen[id] {
				t.Fatalf("sort=%s duplicate %s", sort, id)
			}
			seen[id] = true
		}
		if len(all) != 45 {
			t.Fatalf("sort=%s returned %d of 45 (rows skipped)", sort, len(all))
		}
	}
	// cursors cannot cross listings
	cur := u.get(api + "/videos/" + v + "/comments?sort=new").next()
	u.get(api+"/videos/"+v+"/comments?sort=top&cursor="+cur).wantErr(t, 400, "invalid_cursor")
	u.get(api+"/videos/"+v+"/comments?cursor=garbage").wantErr(t, 400, "invalid_cursor")
}

func TestCommentModes(t *testing.T) {
	e := setupAuth(t)
	creator, chID := e.channel()
	off := e.addVideo(chID, "comments off", vo{mode: "OFF"})
	held := e.addVideo(chID, "comments held", vo{mode: "HELD"})
	u, _, _ := e.verifiedUser()

	u.post(api+"/videos/"+off+"/comments", map[string]any{"text": "hello"}).wantErr(t, 403, "comments_disabled")
	if r := u.get(api + "/videos/" + off + "/comments"); r.code != 200 || len(r.items()) != 0 {
		t.Fatalf("comments off must list nothing: %s", r.body)
	}

	// held: other people's comments wait for approval; the creator's own are published
	r := u.post(api+"/videos/"+held+"/comments", map[string]any{"text": "needs approval"})
	r.mustBe(t, 201)
	if n := e.countRows(`SELECT count(*) FROM comments WHERE video_id = $1::uuid AND status = 'UNDER_REVIEW'`, held); n != 1 {
		t.Fatal("comment should be held")
	}
	if got := e.viewer().get(api + "/videos/" + held + "/comments").items(); len(got) != 0 {
		t.Fatalf("held comments must not be public: %v", got)
	}
	if _, _, n, _ := e.videoCounts(held); n != 0 {
		t.Fatalf("held comments are not counted: %d", n)
	}
	if n := e.notificationCount(chID, "NEW_COMMENT"); n != 0 {
		t.Fatalf("held comments must not notify yet: %d", n)
	}
	creator.post(api+"/videos/"+held+"/comments", map[string]any{"text": "creator speaking"}).mustBe(t, 201)
	if got := e.viewer().get(api + "/videos/" + held + "/comments").items(); len(got) != 1 {
		t.Fatalf("creator's own comment should be public: %v", got)
	}
}

func TestCommentsOnHiddenVideos(t *testing.T) {
	e := setupAuth(t)
	owner, oid := e.channel()
	private := e.addVideo(oid, "private", vo{vis: "PRIVATE"})
	removed := e.addVideo(oid, "removed", vo{deleted: true})
	pub := e.addVideo(oid, "public", vo{})
	u, _, _ := e.verifiedUser()
	cid1 := owner.post(api+"/videos/"+pub+"/comments", map[string]any{"text": "visible"}).str("id")

	for _, id := range []string{private, removed} {
		u.get(api+"/videos/"+id+"/comments").wantErr(t, 404, "video_not_found")
		u.post(api+"/videos/"+id+"/comments", map[string]any{"text": "sneaky"}).wantErr(t, 404, "video_not_found")
	}
	owner.get(api+"/videos/"+private+"/comments").mustBe(t, 200) // owners can use their own private video
	// once the video is hidden, its comments cannot be liked or read through the comment id
	e.exec(`UPDATE videos SET visibility = 'PRIVATE' WHERE id = $1::uuid`, pub)
	u.put(api+"/comments/"+cid1+"/like", map[string]any{"liked": true}).wantErr(t, 404, "comment_not_found")
	u.get(api+"/comments/"+cid1+"/replies").wantErr(t, 404, "comment_not_found")
	// the age gate covers comment reads
	e.client().get(api+"/videos/"+private+"/comments").wantErr(t, 403, "age_verification_required")
}
