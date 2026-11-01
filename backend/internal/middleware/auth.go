// Package middleware holds the security middleware: session authentication,
// role checks, the age gate and CSRF origin checking. Authorization is always
// decided here from database state, never from anything the client sends.
package middleware

import (
	"net/http"
	"net/url"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/service"
)

// Cookies describes how cookies are written. Domain lets redxtube.com and
// api.redxtube.com share them (e.g. ".redxtube.com").
type Cookies struct {
	Session string // session cookie name (httpOnly)
	Age     string // signed age cookie name (httpOnly)
	AgeMark string // readable marker for the Next.js middleware (UI-level only)
	Domain  string
	Secure  bool
}

func (c Cookies) base(name, value string, expires time.Time, httpOnly bool) *http.Cookie {
	ck := &http.Cookie{Name: name, Value: value, Path: "/", Domain: c.Domain, Secure: c.Secure, HttpOnly: httpOnly, SameSite: http.SameSiteLaxMode}
	if value == "" {
		ck.MaxAge = -1
		ck.Expires = time.Unix(0, 0)
	} else {
		ck.Expires = expires
		ck.MaxAge = int(time.Until(expires).Seconds())
	}
	return ck
}

func (c Cookies) SetSession(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, c.base(c.Session, token, expires, true))
}

func (c Cookies) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, c.base(c.Session, "", time.Time{}, true))
}

// SetAge writes the signed cookie plus the plain marker. Only the signed cookie
// is trusted by the API; the marker just lets the frontend skip a redirect.
func (c Cookies) SetAge(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, c.base(c.Age, token, expires, true))
	http.SetCookie(w, c.base(c.AgeMark, "1", expires, false))
}

func (c Cookies) ClearAge(w http.ResponseWriter) {
	http.SetCookie(w, c.base(c.Age, "", time.Time{}, true))
	http.SetCookie(w, c.base(c.AgeMark, "", time.Time{}, false))
}

// Authenticate attaches the principal (if any) to the request context. It
// never rejects: use RequireAuth on routes that need a signed-in user.
func Authenticate(svc *service.Auth, ck Cookies) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(ck.Session)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			p, renewed, err := svc.Authenticate(r.Context(), c.Value)
			if err != nil {
				httpx.Error(w, r, apperr.Internal(err))
				return
			}
			if p == nil {
				ck.ClearSession(w) // stale, revoked or expired cookie
				next.ServeHTTP(w, r)
				return
			}
			if renewed != nil {
				ck.SetSession(w, c.Value, *renewed) // sliding expiry: keep the browser cookie in step
			}
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}
}

// RequireAuth answers 401 for anonymous callers.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r.Context()) == nil {
			httpx.Error(w, r, apperr.New(http.StatusUnauthorized, "unauthenticated", "Not signed in"))
			return
		}
		next(w, r)
	}
}

// RequireVerified additionally needs a verified email (403 verification_required).
func RequireVerified(next http.HandlerFunc) http.HandlerFunc {
	return RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !auth.FromContext(r.Context()).EmailVerified {
			httpx.Error(w, r, apperr.Forbidden("verification_required", "Verify your email to continue"))
			return
		}
		next(w, r)
	})
}

// RequireRole needs a verified user whose role is at least min.
func RequireRole(min auth.Role) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return RequireVerified(func(w http.ResponseWriter, r *http.Request) {
			if !auth.FromContext(r.Context()).Role.AtLeast(min) {
				httpx.Error(w, r, apperr.Forbidden("forbidden", "You don't have permission to do that"))
				return
			}
			next(w, r)
		})
	}
}

// RequireAge enforces the 18+ gate on the server. The check is a signature and
// expiry verification of the age cookie (no database call per request); a
// plain "xh_age_ok=1" cookie set by the browser is worthless here.
func RequireAge(signer auth.AgeSigner, cookieName string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(cookieName)
			if err != nil {
				httpx.Error(w, r, apperr.Forbidden("age_verification_required", "Age verification required"))
				return
			}
			if _, ok := signer.Verify(c.Value); !ok {
				httpx.Error(w, r, apperr.Forbidden("age_verification_required", "Age verification required"))
				return
			}
			next(w, r)
		}
	}
}

// CSRFOrigin blocks cross-site state-changing requests (defence in depth on
// top of SameSite=Lax cookies and strict CORS). Unsafe methods must come from
// an allowed Origin; requests without an Origin header (curl, mobile apps,
// server-to-server) are allowed unless the browser says they are cross-site.
func CSRFOrigin(allowed []string) httpx.Middleware {
	set := map[string]bool{}
	for _, o := range allowed {
		set[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
					httpx.Error(w, r, apperr.Forbidden("csrf_blocked", "Cross-site request blocked"))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if u, err := url.Parse(origin); err != nil || !set[u.Scheme+"://"+u.Host] {
				httpx.Error(w, r, apperr.Forbidden("csrf_blocked", "Cross-site request blocked"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
