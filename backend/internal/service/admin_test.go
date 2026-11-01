package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/repository"
)

type fakeAdmin struct {
	AdminStore
	roles             map[string]string
	applied           *repository.ActionInput
	rejected, blocked bool
	roleSet           string
}

func (f *fakeAdmin) UserRole(_ context.Context, id string) (string, string, error) {
	return f.roles[id], "ACTIVE", nil
}
func (f *fakeAdmin) Apply(_ context.Context, in repository.ActionInput) error {
	f.applied = &in
	return nil
}
func (f *fakeAdmin) RejectReport(context.Context, string, string, repository.AuditEntry) error {
	f.rejected = true
	return nil
}
func (f *fakeAdmin) ReportTarget(context.Context, string) (string, string, error) {
	return "VIDEO", vidID, nil
}
func (f *fakeAdmin) SetRole(_ context.Context, _, role string, _ repository.AuditEntry) error {
	f.roleSet = role
	return nil
}
func (f *fakeAdmin) BlockHash(context.Context, []byte, string, string, repository.AuditEntry) (int64, error) {
	f.blocked = true
	return 2, nil
}

const targetUser = "44444444-4444-7444-8444-444444444444"

func adminSvc(f *fakeAdmin) *Admin {
	return NewAdmin(f, &okLimiter{}, []byte("k"), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func staff(role auth.Role) *auth.Principal {
	return &auth.Principal{UserID: "55555555-5555-7555-8555-555555555555", Role: role, EmailVerified: true}
}

func TestActionTypeMapping(t *testing.T) {
	ok := map[string]string{"VIDEO:remove": "CONTENT_REMOVED", "VIDEO:restrict": "VIDEO_RESTRICTED", "COMMENT:remove": "COMMENT_REMOVED",
		"USER:suspend": "USER_SUSPENDED", "USER:ban": "USER_BANNED", "USER:warn": "WARNING", "VIDEO:warn": "WARNING"}
	for k, want := range ok {
		p := strings.Split(k, ":")
		if got := actionType(p[0], p[1]); got != want {
			t.Errorf("%s -> %q", k, got)
		}
	}
	for _, k := range []string{"COMMENT:restrict", "USER:remove", "VIDEO:suspend", "VIDEO:ban", "USER:delete"} {
		p := strings.Split(k, ":")
		if actionType(p[0], p[1]) != "" {
			t.Errorf("%s must be rejected", k)
		}
	}
}

func TestActRankRules(t *testing.T) {
	f := &fakeAdmin{roles: map[string]string{targetUser: "USER", "mod": "MODERATOR"}}
	f.roles["66666666-6666-7666-8666-666666666666"] = "MODERATOR"
	f.roles["77777777-7777-7777-8777-777777777777"] = "ADMIN"
	svc := adminSvc(f)
	ctx := context.Background()
	in := ActionInput{Action: "suspend", Reason: "spam"}

	// moderator suspends a normal user: default 7 days, applied
	if err := svc.Act(ctx, staff(auth.RoleModerator), testCI, nil, "user", targetUser, in); err != nil {
		t.Fatal(err)
	}
	if f.applied == nil || f.applied.Action != "USER_SUSPENDED" || f.applied.ExpiresAt == nil {
		t.Fatalf("applied %+v", f.applied)
	}
	// moderator cannot ban
	f.applied = nil
	if err := svc.Act(ctx, staff(auth.RoleModerator), testCI, nil, "user", targetUser, ActionInput{Action: "ban", Reason: "x"}); ecode(t, err) != "forbidden" || f.applied != nil {
		t.Errorf("mod ban: %v", err)
	}
	// nobody acts on an equal/higher role, or on themselves
	for _, tgt := range []string{"66666666-6666-7666-8666-666666666666", "77777777-7777-7777-8777-777777777777", staff(auth.RoleModerator).UserID} {
		if err := svc.Act(ctx, staff(auth.RoleModerator), testCI, nil, "user", tgt, in); ecode(t, err) != "forbidden" || f.applied != nil {
			t.Errorf("target %s: %v", tgt, err)
		}
	}
	// validation
	for name, bad := range map[string]ActionInput{
		"no reason":     {Action: "warn", Reason: "  "},
		"long reason":   {Action: "warn", Reason: strings.Repeat("x", 501)},
		"days":          {Action: "suspend", Reason: "x", Days: ptrInt(400)},
		"wrong verb":    {Action: "restrict", Reason: "x"},
		"dismiss alone": {Action: "dismiss", Reason: "x"},
	} {
		if err := svc.Act(ctx, staff(auth.RoleModerator), testCI, nil, "user", targetUser, bad); ecode(t, err) != "validation_failed" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func ptrInt(i int) *int { return &i }

func TestResolveReportDismissAndRemove(t *testing.T) {
	f := &fakeAdmin{}
	svc := adminSvc(f)
	rid := "88888888-8888-7888-8888-888888888888"
	if err := svc.ResolveReport(context.Background(), staff(auth.RoleModerator), testCI, rid, ActionInput{Action: "dismiss", Reason: "not a violation"}); err != nil || !f.rejected {
		t.Fatalf("dismiss: %v %v", err, f.rejected)
	}
	if err := svc.ResolveReport(context.Background(), staff(auth.RoleModerator), testCI, rid, ActionInput{Action: "remove", Reason: "illegal"}); err != nil {
		t.Fatal(err)
	}
	if f.applied.Action != "CONTENT_REMOVED" || f.applied.ReportID == nil || *f.applied.ReportID != rid || f.applied.TargetID != vidID {
		t.Errorf("applied %+v", f.applied)
	}
	if err := svc.ResolveReport(context.Background(), staff(auth.RoleModerator), testCI, "nope", ActionInput{}); ecode(t, err) != "report_not_found" {
		t.Error("bad id")
	}
}

func TestHashAndRoleValidation(t *testing.T) {
	f := &fakeAdmin{}
	svc := adminSvc(f)
	ctx := context.Background()
	for _, h := range []string{"", "zz", strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		if _, err := svc.BlockHash(ctx, staff(auth.RoleAdmin), testCI, h, "csam"); ecode(t, err) != "validation_failed" || f.blocked {
			t.Errorf("hash %q: %v", h, err)
		}
	}
	out, err := svc.BlockHash(ctx, staff(auth.RoleAdmin), testCI, strings.ToUpper(strings.Repeat("ab", 32)), "takedown")
	if err != nil || out.VideosRemoved != 2 {
		t.Errorf("block: %+v %v", out, err)
	}
	if err := svc.SetRole(ctx, staff(auth.RoleSuperAdmin), testCI, targetUser, "super_admin"); ecode(t, err) != "validation_failed" {
		t.Error("super_admin must not be assignable via the API")
	}
	if err := svc.SetRole(ctx, staff(auth.RoleSuperAdmin), testCI, staff(auth.RoleSuperAdmin).UserID, "user"); ecode(t, err) != "forbidden" {
		t.Error("self role change")
	}
	if err := svc.SetRole(ctx, staff(auth.RoleSuperAdmin), testCI, targetUser, "moderator"); err != nil || f.roleSet != "MODERATOR" {
		t.Errorf("set role: %v %q", err, f.roleSet)
	}
}

func TestListFiltersRejectWildcards(t *testing.T) {
	svc := adminSvc(&fakeAdmin{})
	if _, err := svc.Users(context.Background(), "a%", "", ""); ecode(t, err) != "validation_failed" {
		t.Error("LIKE wildcard accepted in q")
	}
	if _, err := svc.Audit(context.Background(), "x_", "", ""); ecode(t, err) != "validation_failed" {
		t.Error("LIKE wildcard accepted in action")
	}
	if _, err := svc.Reports(context.Background(), "weird", ""); ecode(t, err) != "validation_failed" {
		t.Error("bad status")
	}
	if _, err := svc.Reports(context.Background(), "open", "garbage"); ecode(t, err) != "invalid_cursor" {
		t.Error("bad cursor")
	}
}
