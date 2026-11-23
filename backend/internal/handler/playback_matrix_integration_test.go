package handler_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/repository"
)

// B7.2 / B7.4: one table decides, for every combination of viewer, video state and owner state, who may be handed a
// stream (the catalog's VideoDetail, which is where the signed stream URL comes from) and whether the gateway may keep
// serving segments of an already issued token (VideoServable). The expectations below are written from the rules, not
// copied from the SQL.

type matrixOwner struct {
	name    string
	status  string
	deleted bool // the account is DELETED (users.deleted_at is set with it)
}

type matrixVideo struct {
	status, visibility, moderation string
	deleted                        bool
	published                      bool // published_at is set; the database allows it exactly for status PUBLISHED (a CHECK), so the watch rule's status and published_at tests cannot be told apart by data
}

// ownerActive: only an ACTIVE, not deleted account's videos are watchable at all.
func (o matrixOwner) active() bool { return o.status == "ACTIVE" && !o.deleted }

// mayOpen: may this viewer open the video detail and so receive a stream URL?
func mayOpen(v matrixVideo, o matrixOwner, viewerIsOwner bool) bool {
	if v.status != "PUBLISHED" || !v.published || v.deleted || v.moderation == "REMOVED" || !o.active() {
		return false
	}
	if (v.visibility == "PUBLIC" || v.visibility == "UNLISTED") && v.moderation == "OK" {
		return true
	}
	return viewerIsOwner // PRIVATE or restricted videos: the owner only
}

// mayKeepServing: may the gateway keep serving segments for a token that was issued earlier? It has no viewer, so it
// can only enforce what holds for everybody: finished, not removed, not deleted, owner in good standing.
//
// KNOWN GAP, pinned here on purpose (B7.4): a video that becomes PRIVATE or RESTRICTED after a token was issued keeps
// streaming for that token (until it expires, 6 h). The gateway cannot tell the owner's own private playback from a
// stranger's, because the token does not say who it was issued to. Closing it needs the token model to change
// (decision D4): the day it does, this expectation changes with it.
func mayKeepServing(v matrixVideo, o matrixOwner) bool {
	return (v.status == "READY" || v.status == "PUBLISHED") && !v.deleted && v.moderation != "REMOVED" && o.active()
}

// matrixVideo inserts one fixture video; a combination the database forbids (a CHECK constraint) returns the error.
func (e *env) matrixVideo(id, creator string, v matrixVideo) error {
	e.t.Helper()
	ready := v.status == "READY" || v.status == "PUBLISHED"
	_, err := e.pool.Exec(context.Background(), `INSERT INTO videos (id, creator_id, title, video_type, status, visibility, moderation_status, comments_mode, category_id,
  duration_seconds, published_at, hls_master_key, deleted_at)
VALUES ($1::uuid, $2::uuid, 'matrix', 'LONG', $3::video_status, $4::video_visibility, $5::moderation_status, 'ON',
  (SELECT id FROM categories WHERE slug = 'music'), 300,
  CASE WHEN $8 THEN now() - interval '1 day' END,
  CASE WHEN $6 THEN 'videos/'||$1::text||'/hls/master.m3u8' END,
  CASE WHEN $7 THEN now() END)`, id, creator, v.status, v.visibility, v.moderation, ready, v.deleted, v.published)
	return err
}

func TestPlaybackAuthorizationMatrix(t *testing.T) {
	e := setup(t)
	repo := repository.NewCatalog(e.app)
	ctx := context.Background()

	owners := []matrixOwner{
		{"active owner", "ACTIVE", false},
		{"suspended owner", "SUSPENDED", false},
		{"banned owner", "BANNED", false},
		{"deleted owner", "DELETED", true},
		{"pending owner", "PENDING", false},
	}
	for i, o := range owners {
		e.creator(i+1, fmt.Sprintf("Owner %d", i+1), o.status, false, 0) // a DELETED account gets deleted_at from the helper
	}
	// other viewers: a plain user, a moderator and an admin (none of them owns a matrix video)
	e.creator(11, "Stranger", "ACTIVE", false, 0)
	e.creator(12, "Moderator", "ACTIVE", false, 0)
	e.creator(13, "Admin", "ACTIVE", false, 0)
	e.exec(`UPDATE users SET role = 'MODERATOR' WHERE id = $1::uuid`, cid(12))
	e.exec(`UPDATE users SET role = 'ADMIN' WHERE id = $1::uuid`, cid(13))
	viewers := []struct {
		name string
		id   *string
	}{{"anonymous", nil}, {"stranger", ptr(cid(11))}, {"moderator", ptr(cid(12))}, {"admin", ptr(cid(13))}}

	statuses := []string{"UPLOADING", "PROCESSING", "READY", "PUBLISHED", "FAILED"}
	visibilities := []string{"PUBLIC", "UNLISTED", "PRIVATE"}
	moderations := []string{"OK", "RESTRICTED", "REMOVED"}
	var checks, allowedOpen, allowedServe, mismatches, skipped int
	report := func(format string, args ...any) {
		mismatches++
		if mismatches <= 25 {
			t.Errorf(format, args...)
		}
	}
	for oi, o := range owners {
		for _, st := range statuses {
			for _, vis := range visibilities {
				for _, mod := range moderations {
					for _, del := range []bool{false, true} {
						for _, pub := range []bool{st == "PUBLISHED"} { // the database ties published_at to status PUBLISHED (a CHECK), see below
							v := matrixVideo{st, vis, mod, del, pub}
							id := auth.NewUUIDv7()
							if err := e.matrixVideo(id, cid(oi+1), v); err != nil {
								skipped++
								continue
							}
							label := fmt.Sprintf("[%s | %s %s %s deleted=%v publishedAt=%v]", o.name, st, vis, mod, del, pub)

							asOwner := cid(oi + 1)
							all := append([]struct {
								name string
								id   *string
							}{{"owner", &asOwner}}, viewers...)
							for _, vw := range all {
								d, err := repo.VideoDetail(ctx, id, vw.id)
								if err != nil {
									t.Fatalf("%s %s: %v", label, vw.name, err)
								}
								got, want := d != nil, mayOpen(v, o, vw.name == "owner")
								checks++
								if want {
									allowedOpen++
								}
								if got != want {
									report("%s %s opens the video: got %v, want %v", label, vw.name, got, want)
								}
								if got && d.HLSKey == nil {
									report("%s %s: a watchable video must carry its stream key", label, vw.name)
								}
							}
							ok, err := repo.VideoServable(ctx, id)
							if err != nil {
								t.Fatalf("%s: %v", label, err)
							}
							checks++
							if want := mayKeepServing(v, o); ok != want {
								report("%s gateway serves: got %v, want %v", label, ok, want)
							} else if want {
								allowedServe++
							}
						}
					}
				}
			}
		}
	}
	if mismatches > 25 {
		t.Errorf("... and %d more mismatches", mismatches-25)
	}
	if d, err := repo.VideoDetail(ctx, auth.NewUUIDv7(), nil); err != nil || d != nil {
		t.Errorf("an unknown video must not open: %v %v", d, err)
	}
	if ok, err := repo.VideoServable(ctx, auth.NewUUIDv7()); err != nil || ok {
		t.Errorf("an unknown video must not be served: %v %v", ok, err)
	}
	if skipped != 0 {
		t.Fatalf("%d fixture combinations were refused by the database", skipped)
	}
	// a vacuous table must not pass: both outcomes have to occur in volume
	if checks < 2000 || allowedOpen < 10 || allowedOpen > checks/2 || allowedServe < 10 {
		t.Fatalf("the matrix is not exercising what it should: %d checks, %d allowed opens, %d allowed gateway answers", checks, allowedOpen, allowedServe)
	}
	t.Logf("%d checks, %d combinations allowed to open, %d allowed to keep streaming, %d combinations skipped", checks, allowedOpen, allowedServe, skipped)
}

func ptr(s string) *string { return &s }
