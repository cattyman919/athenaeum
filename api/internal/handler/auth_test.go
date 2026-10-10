package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/config"
	"github.com/cattyman919/athenaeum/api/internal/handler"
	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/server"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/testutil/memauth"
)

type stubGoogle struct {
	profile auth.GoogleProfile
	err     error
}

func (g stubGoogle) AuthURL(_ context.Context, state string) (string, error) {
	return "https://accounts.example/o/oauth2/v2/auth?state=" + url.QueryEscape(state), nil
}

func (g stubGoogle) Exchange(context.Context, string) (auth.GoogleProfile, error) {
	return g.profile, g.err
}

type app struct {
	router http.Handler
	users  *memauth.Users
}

const testOrigin = "http://localhost:3000"

func newApp(t *testing.T, g auth.Google) *app {
	t.Helper()
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", 15*time.Minute)
	users, tokens := memauth.NewUsers(), memauth.NewTokens()
	svc := auth.New(users, tokens, jm, g, 168*time.Hour, logging.Discard())
	cfg := config.Config{WebOrigin: testOrigin, JWTRefreshSecret: "refresh-secret-refresh-secret-1234"}
	r := server.NewRouter(server.Deps{Config: cfg, Log: logging.Discard(), JWT: jm, Auth: svc})
	return &app{router: r, users: users}
}

func (a *app) do(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

func cookieMap(rec *httptest.ResponseRecorder) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		out[c.Name] = c
	}
	return out
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	s, _ := b["error"].(string)
	return s
}

const registerBody = `{"name":"Dana Reader","email":"dana@athenaeum.dev","password":"correct horse battery"}`

func TestRegisterSetsCookiesAndEnvelope(t *testing.T) {
	a := newApp(t, nil)
	rec := a.do("POST", "/api/auth/register", registerBody)
	if rec.Code != 201 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		User map[string]any `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.User["email"] != "dana@athenaeum.dev" || body.User["role"] != "user" || body.User["id"] == "" {
		t.Fatalf("user %v", body.User)
	}
	if _, leaked := body.User["password_hash"]; leaked {
		t.Fatal("hash leaked")
	}
	cs := cookieMap(rec)
	for _, name := range []string{middleware.AccessCookie, handler.RefreshCookie} {
		c := cs[name]
		if c == nil || !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.Value == "" {
			t.Fatalf("cookie %s: %+v", name, c)
		}
	}
	if rec := a.do("POST", "/api/auth/register", registerBody); rec.Code != 409 || errCode(t, rec) != "EMAIL_TAKEN" {
		t.Fatalf("dup: %d %s", rec.Code, rec.Body)
	}
}

func TestRegisterValidation(t *testing.T) {
	a := newApp(t, nil)
	for _, body := range []string{
		`{`, `{"name":"A","email":"a@b.co","password":"short"}`, `{"name":"A","email":"bad","password":"long enough pass"}`,
	} {
		rec := a.do("POST", "/api/auth/register", body)
		if rec.Code != 400 || errCode(t, rec) != "VALIDATION_FAILED" {
			t.Errorf("%s → %d %s", body, rec.Code, rec.Body)
		}
	}
}

func TestLoginMeLogout(t *testing.T) {
	a := newApp(t, nil)
	a.do("POST", "/api/auth/register", registerBody)

	bad := a.do("POST", "/api/auth/login", `{"email":"dana@athenaeum.dev","password":"wrong-wrong-wrong"}`)
	unknown := a.do("POST", "/api/auth/login", `{"email":"ghost@athenaeum.dev","password":"wrong-wrong-wrong"}`)
	if bad.Code != 401 || errCode(t, bad) != "INVALID_CREDENTIALS" || bad.Body.String() != unknown.Body.String() {
		t.Fatalf("credential errors must be identical: %s vs %s", bad.Body, unknown.Body)
	}

	rec := a.do("POST", "/api/auth/login", `{"email":"DANA@athenaeum.dev","password":"correct horse battery"}`)
	if rec.Code != 200 {
		t.Fatalf("login %d %s", rec.Code, rec.Body)
	}
	cs := cookieMap(rec)
	at, rt := cs[middleware.AccessCookie], cs[handler.RefreshCookie]

	if rec := a.do("GET", "/api/auth/me", ""); rec.Code != 401 || errCode(t, rec) != "UNAUTHENTICATED" {
		t.Fatalf("me anon: %d %s", rec.Code, rec.Body)
	}
	if rec := a.do("GET", "/api/auth/me", "", &http.Cookie{Name: middleware.AccessCookie, Value: "garbage"}); rec.Code != 401 {
		t.Fatalf("me garbage: %d", rec.Code)
	}
	me := a.do("GET", "/api/auth/me", "", &http.Cookie{Name: middleware.AccessCookie, Value: at.Value})
	if me.Code != 200 || !strings.Contains(me.Body.String(), "dana@athenaeum.dev") {
		t.Fatalf("me %d %s", me.Code, me.Body)
	}

	out := a.do("POST", "/api/auth/logout", "", &http.Cookie{Name: middleware.AccessCookie, Value: at.Value})
	if out.Code != 204 {
		t.Fatalf("logout %d", out.Code)
	}
	for _, c := range out.Result().Cookies() {
		if c.MaxAge >= 0 || c.Value != "" {
			t.Fatalf("cookie %s not cleared: %+v", c.Name, c)
		}
	}
	if rec := a.do("POST", "/api/auth/refresh", "", &http.Cookie{Name: handler.RefreshCookie, Value: rt.Value}); rec.Code != 401 {
		t.Fatalf("refresh after logout: %d %s", rec.Code, rec.Body)
	}
	if rec := a.do("POST", "/api/auth/logout", ""); rec.Code != 401 {
		t.Fatalf("logout anon: %d", rec.Code)
	}
}

func TestRefreshRotationAndReplayOverHTTP(t *testing.T) {
	a := newApp(t, nil)
	reg := a.do("POST", "/api/auth/register", registerBody)
	rt1 := cookieMap(reg)[handler.RefreshCookie]

	r2 := a.do("POST", "/api/auth/refresh", "", rt1)
	if r2.Code != 200 {
		t.Fatalf("rotate %d %s", r2.Code, r2.Body)
	}
	rt2 := cookieMap(r2)[handler.RefreshCookie]
	if rt2.Value == rt1.Value {
		t.Fatal("rt not rotated")
	}

	replay := a.do("POST", "/api/auth/refresh", "", rt1)
	if replay.Code != 401 || errCode(t, replay) != "REUSED_TOKEN" {
		t.Fatalf("replay %d %s", replay.Code, replay.Body)
	}
	if rec := a.do("POST", "/api/auth/refresh", "", rt2); rec.Code != 401 {
		t.Fatalf("family should be dead: %d", rec.Code)
	}
	if rec := a.do("POST", "/api/auth/refresh", ""); rec.Code != 401 || errCode(t, rec) != "UNAUTHENTICATED" {
		t.Fatalf("no cookie: %d %s", rec.Code, rec.Body)
	}
}

func TestRoleGuards(t *testing.T) {
	jm, _ := jwt.NewManager("0123456789abcdef0123456789abcdef", time.Minute)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Identify(jm))
	r.GET("/user", middleware.RequireAuth(handler.RespondError), func(c *gin.Context) { c.Status(200) })
	r.GET("/admin", middleware.RequireAuth(handler.RespondError), middleware.RequireAdmin(handler.RespondError),
		func(c *gin.Context) { c.Status(200) })

	tok := func(role string) *http.Cookie {
		s, _, _ := jm.Issue(jwt.Claims{Subject: "0f51a1b0-0000-4000-8000-000000000001", Role: role})
		return &http.Cookie{Name: middleware.AccessCookie, Value: s}
	}
	tests := []struct {
		path   string
		cookie *http.Cookie
		want   int
		code   string
	}{
		{"/user", nil, 401, "UNAUTHENTICATED"},
		{"/user", tok(model.RoleUser), 200, ""},
		{"/admin", nil, 401, "UNAUTHENTICATED"},
		{"/admin", tok(model.RoleUser), 403, "FORBIDDEN"},
		{"/admin", tok(model.RoleAdmin), 200, ""},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		if tt.cookie != nil {
			req.AddCookie(tt.cookie)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != tt.want || (tt.code != "" && errCode(t, rec) != tt.code) {
			t.Errorf("%s: %d %s", tt.path, rec.Code, rec.Body)
		}
	}
}

func TestGoogleSSOFlow(t *testing.T) {
	a := newApp(t, stubGoogle{profile: auth.GoogleProfile{Sub: "g-1", Email: "sso@athenaeum.dev", Name: "SSO User"}})

	start := a.do("GET", "/api/auth/google?next=%2Fbooks%2F42", "")
	if start.Code != 302 || !strings.HasPrefix(start.Header().Get("Location"), "https://accounts.example/") {
		t.Fatalf("start %d %v", start.Code, start.Header())
	}
	st := cookieMap(start)["athenaeum_oauth_state"]
	if st == nil || !st.HttpOnly || st.MaxAge != 600 {
		t.Fatalf("state cookie %+v", st)
	}
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")

	cb := a.do("GET", "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), "", st)
	if cb.Code != 302 || cb.Header().Get("Location") != testOrigin+"/books/42" {
		t.Fatalf("callback %d %v", cb.Code, cb.Header())
	}
	if cookieMap(cb)[middleware.AccessCookie] == nil {
		t.Fatal("session cookie missing")
	}

	failures := map[string]string{
		"state mismatch": "/api/auth/google/callback?code=abc&state=other",
		"no code":        "/api/auth/google/callback?state=" + url.QueryEscape(state),
	}
	for name, path := range failures {
		rec := a.do("GET", path, "", st)
		if rec.Code != 302 || rec.Header().Get("Location") != testOrigin+"/login?error=sso" {
			t.Errorf("%s: %d %v", name, rec.Code, rec.Header())
		}
	}
	// Missing / tampered cookie.
	if rec := a.do("GET", "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), ""); rec.Header().Get("Location") != testOrigin+"/login?error=sso" {
		t.Errorf("no cookie: %v", rec.Header())
	}
	tampered := *st
	tampered.Value = strings.Replace(st.Value, ".", "x.", 1)
	if rec := a.do("GET", "/api/auth/google/callback?code=abc&state="+url.QueryEscape(state), "", &tampered); rec.Header().Get("Location") != testOrigin+"/login?error=sso" {
		t.Errorf("tampered: %v", rec.Header())
	}
}

func TestGoogleOpenRedirectGuardAndFailure(t *testing.T) {
	a := newApp(t, stubGoogle{profile: auth.GoogleProfile{Sub: "g-1", Email: "sso@athenaeum.dev"}})
	for _, next := range []string{"//evil.example", "https://evil.example", "%2F%2Fevil.example"} {
		start := a.do("GET", "/api/auth/google?next="+next, "")
		st := cookieMap(start)["athenaeum_oauth_state"]
		loc, _ := url.Parse(start.Header().Get("Location"))
		cb := a.do("GET", "/api/auth/google/callback?code=c&state="+url.QueryEscape(loc.Query().Get("state")), "", st)
		if got := cb.Header().Get("Location"); got != testOrigin+"/catalog" {
			t.Errorf("next=%s redirected to %s", next, got)
		}
	}

	bad := newApp(t, stubGoogle{err: context.DeadlineExceeded})
	start := bad.do("GET", "/api/auth/google", "")
	st := cookieMap(start)["athenaeum_oauth_state"]
	loc, _ := url.Parse(start.Header().Get("Location"))
	cb := bad.do("GET", "/api/auth/google/callback?code=c&state="+url.QueryEscape(loc.Query().Get("state")), "", st)
	if cb.Header().Get("Location") != testOrigin+"/login?error=sso" {
		t.Errorf("exchange failure: %v", cb.Header())
	}

	none := newApp(t, nil)
	if rec := none.do("GET", "/api/auth/google", ""); rec.Header().Get("Location") != testOrigin+"/login?error=sso" {
		t.Errorf("unconfigured: %v", rec.Header())
	}
}
