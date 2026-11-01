// Package auth holds security primitives: password hashing, one-time tokens,
// signed age cookies, roles and the authenticated principal.
package auth

import "context"

type Role string

const (
	RoleUser       Role = "USER"
	RoleCreator    Role = "CREATOR"
	RoleModerator  Role = "MODERATOR"
	RoleAdmin      Role = "ADMIN"
	RoleSuperAdmin Role = "SUPER_ADMIN"
)

var rank = map[Role]int{RoleUser: 1, RoleCreator: 2, RoleModerator: 3, RoleAdmin: 4, RoleSuperAdmin: 5}

// Valid reports whether r is a known role.
func (r Role) Valid() bool { _, ok := rank[r]; return ok }

// AtLeast reports whether r is min or higher. Unknown roles never pass.
// The role always comes from the database, never from the client.
func (r Role) AtLeast(min Role) bool {
	a, ok1 := rank[r]
	b, ok2 := rank[min]
	return ok1 && ok2 && a >= b
}

// Principal is the authenticated caller, resolved from the session cookie.
type Principal struct {
	UserID        string
	SessionID     string
	Username      string
	Role          Role
	EmailVerified bool
	IsCreator     bool // may upload (users.is_creator); staff features use Role
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal or nil for anonymous callers.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}
