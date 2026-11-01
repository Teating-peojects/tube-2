package handler

import (
	"net/http"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/service"
)

// Auth serves registration, login, sessions, account, settings and the age gate.
type Auth struct {
	Svc        *service.Auth
	Cookies    middleware.Cookies
	TrustProxy bool
}

func (h Auth) Routes(mux *http.ServeMux) {
	const v = "/api/v1"
	mux.HandleFunc("POST "+v+"/age/verify", httpx.Handle(h.ageVerify))
	mux.HandleFunc("GET "+v+"/age/status", httpx.Handle(h.ageStatus))

	mux.HandleFunc("POST "+v+"/auth/register", httpx.Handle(h.register))
	mux.HandleFunc("POST "+v+"/auth/login", httpx.Handle(h.login))
	mux.HandleFunc("POST "+v+"/auth/logout", httpx.Handle(h.logout))
	mux.HandleFunc("GET "+v+"/auth/me", middleware.RequireAuth(httpx.Handle(h.me)))
	mux.HandleFunc("POST "+v+"/auth/verify", middleware.RequireAuth(httpx.Handle(h.verify)))
	mux.HandleFunc("POST "+v+"/auth/verify/resend", middleware.RequireAuth(httpx.Handle(h.resend)))
	mux.HandleFunc("POST "+v+"/auth/forgot-password", httpx.Handle(h.forgot))
	mux.HandleFunc("POST "+v+"/auth/reset-password", httpx.Handle(h.reset))
	mux.HandleFunc("POST "+v+"/auth/change-password", middleware.RequireAuth(httpx.Handle(h.changePassword)))
	mux.HandleFunc("POST "+v+"/auth/logout-all", middleware.RequireAuth(httpx.Handle(h.logoutAll)))

	mux.HandleFunc("PATCH "+v+"/account", middleware.RequireAuth(httpx.Handle(h.updateAccount)))
	mux.HandleFunc("POST "+v+"/account/delete", middleware.RequireAuth(httpx.Handle(h.deleteAccount)))
	mux.HandleFunc("GET "+v+"/settings", middleware.RequireAuth(httpx.Handle(h.settings)))
	mux.HandleFunc("PATCH "+v+"/settings", middleware.RequireAuth(httpx.Handle(h.patchSettings)))
}

func (h Auth) info(r *http.Request) service.ClientInfo {
	return service.ClientInfo{
		IP: auth.ClientIP(r, h.TrustProxy), UserAgent: r.UserAgent(), RequestID: httpx.RequestIDFrom(r.Context()),
		Country: r.Header.Get("CF-IPCountry"), // set by Cloudflare; ignored when absent/invalid
	}
}

func (h Auth) cookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

func principal(r *http.Request) *auth.Principal { return auth.FromContext(r.Context()) }

// ---------- age gate ----------

func (h Auth) ageVerify(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Confirmed bool `json:"confirmed"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if !in.Confirmed {
		return apperr.Validation(map[string]string{"confirmed": "You must confirm you are 18 or older"})
	}
	c, err := h.Svc.VerifyAge(r.Context(), h.cookieValue(r, h.Cookies.Age), h.info(r))
	if err != nil {
		return err
	}
	h.Cookies.SetAge(w, c.Token, c.Expires)
	httpx.NoContent(w)
	return nil
}

func (h Auth) ageStatus(w http.ResponseWriter, r *http.Request) error {
	_, ok := h.Svc.AgeSigner().Verify(h.cookieValue(r, h.Cookies.Age))
	httpx.JSON(w, http.StatusOK, map[string]bool{"verified": ok})
	return nil
}

// ---------- auth ----------

func (h Auth) writeSession(w http.ResponseWriter, res *service.SessionResult) {
	h.Cookies.SetSession(w, res.Token, res.Expires)
	if res.Age != nil {
		h.Cookies.SetAge(w, res.Age.Token, res.Age.Expires)
	}
}

func (h Auth) register(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	res, err := h.Svc.Register(r.Context(), service.RegisterInput{Email: in.Email, Username: in.Username, Password: in.Password},
		h.cookieValue(r, h.Cookies.Age), h.info(r))
	if err != nil {
		return err
	}
	h.writeSession(w, res)
	httpx.JSON(w, http.StatusCreated, res.User)
	return nil
}

func (h Auth) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	res, err := h.Svc.Login(r.Context(), in.Identifier, in.Password, h.info(r))
	if res != nil {
		h.writeSession(w, res) // also for verification_required: the verify screen needs the session
	}
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, res.User)
	return nil
}

func (h Auth) logout(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Logout(r.Context(), h.cookieValue(r, h.Cookies.Session)); err != nil {
		return err
	}
	h.Cookies.ClearSession(w)
	httpx.NoContent(w)
	return nil
}

func (h Auth) logoutAll(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.LogoutAll(r.Context(), principal(r), h.info(r)); err != nil {
		return err
	}
	h.Cookies.ClearSession(w)
	httpx.NoContent(w)
	return nil
}

func (h Auth) me(w http.ResponseWriter, r *http.Request) error {
	u, err := h.Svc.Me(r.Context(), principal(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

func (h Auth) verify(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Code string `json:"code"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	u, err := h.Svc.Verify(r.Context(), principal(r), in.Code, h.info(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

func (h Auth) resend(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.ResendCode(r.Context(), principal(r), h.info(r)); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h Auth) forgot(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email string `json:"email"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.Forgot(r.Context(), in.Email, h.info(r)); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h Auth) reset(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.Reset(r.Context(), in.Token, in.Password, h.info(r)); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h Auth) changePassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.ChangePassword(r.Context(), principal(r), in.CurrentPassword, in.NewPassword, h.info(r)); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---------- account & settings ----------

func (h Auth) updateAccount(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		DisplayName string `json:"displayName"`
		Bio         string `json:"bio"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	u, err := h.Svc.UpdateAccount(r.Context(), principal(r), in.DisplayName, in.Bio)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, u)
	return nil
}

func (h Auth) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.DeleteAccount(r.Context(), principal(r), in.Password, h.info(r)); err != nil {
		return err
	}
	h.Cookies.ClearSession(w)
	httpx.NoContent(w)
	return nil
}

func (h Auth) settings(w http.ResponseWriter, r *http.Request) error {
	s, err := h.Svc.Settings(r.Context(), principal(r))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, s)
	return nil
}

func (h Auth) patchSettings(w http.ResponseWriter, r *http.Request) error {
	var in model.SettingsPatch
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.Svc.PatchSettings(r.Context(), principal(r), in); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
