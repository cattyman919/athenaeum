package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/logging"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
	"github.com/cattyman919/athenaeum/api/internal/testutil/memauth"
)

type fakeGoogle struct {
	profile auth.GoogleProfile
	err     error
}

func (f fakeGoogle) AuthURL(_ context.Context, state string) (string, error) {
	return "https://accounts.example/auth?state=" + state, nil
}

func (f fakeGoogle) Exchange(context.Context, string) (auth.GoogleProfile, error) {
	return f.profile, f.err
}

type env struct {
	svc    *auth.Service
	users  *memauth.Users
	tokens *memauth.Tokens
	clock  *time.Time
}

func newEnv(t *testing.T, g auth.Google) *env {
	t.Helper()
	jm, err := jwt.NewManager("0123456789abcdef0123456789abcdef", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	users, tokens := memauth.NewUsers(), memauth.NewTokens()
	now := time.Now()
	e := &env{users: users, tokens: tokens, clock: &now}
	e.svc = auth.New(users, tokens, jm, g, 168*time.Hour, logging.Discard()).
		WithClock(func() time.Time { return *e.clock })
	return e
}

func code(err error) model.Code {
	if de, ok := model.AsError(err); ok {
		return de.Code
	}
	return ""
}

func TestRegister(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	u, s, err := e.svc.Register(ctx, " Dana ", "Dana@Athenaeum.dev", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "dana@athenaeum.dev" || u.Name != "Dana" || u.Role != model.RoleUser {
		t.Fatalf("user %+v", u)
	}
	if s.AccessToken == "" || s.RefreshToken == "" {
		t.Fatal("empty session")
	}
	if u.PasswordHash == nil || *u.PasswordHash == "correct horse battery" {
		t.Fatal("password must be hashed")
	}

	tests := []struct {
		name, nm, email, pw string
		want                model.Code
	}{
		{"duplicate case-insensitive", "X", "DANA@athenaeum.dev", "correct horse battery", model.CodeEmailTaken},
		{"short password", "X", "x@athenaeum.dev", "short", model.CodeValidationFailed},
		{"bad email", "X", "not-an-email", "correct horse battery", model.CodeValidationFailed},
		{"missing tld", "X", "x@localhost", "correct horse battery", model.CodeValidationFailed},
		{"empty name", " ", "y@athenaeum.dev", "correct horse battery", model.CodeValidationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := e.svc.Register(ctx, tt.nm, tt.email, tt.pw)
			if code(err) != tt.want {
				t.Fatalf("got %v want %s", err, tt.want)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	if _, _, err := e.svc.Register(ctx, "Demo", "demo@athenaeum.dev", "athenaeum-demo"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.Login(ctx, "DEMO@athenaeum.dev", "athenaeum-demo"); err != nil {
		t.Fatalf("good login: %v", err)
	}
	for name, c := range map[string][2]string{
		"wrong password": {"demo@athenaeum.dev", "nope-nope-nope"},
		"unknown email":  {"ghost@athenaeum.dev", "athenaeum-demo"},
		"malformed":      {"???", "x"},
	} {
		_, _, err := e.svc.Login(ctx, c[0], c[1])
		if !errors.Is(err, model.ErrInvalidCredentials) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// SSO-only account: identical error.
	sub := "g-1"
	_ = e.users.Create(ctx, model.User{ID: mustID(), Email: "sso@athenaeum.dev", Name: "S", Role: "user", GoogleSub: &sub})
	if _, _, err := e.svc.Login(ctx, "sso@athenaeum.dev", "whatever-password"); !errors.Is(err, model.ErrInvalidCredentials) {
		t.Fatalf("sso-only: %v", err)
	}
}

func TestRefreshRotationAndReplay(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	u, s1, _ := e.svc.Register(ctx, "Demo", "demo@athenaeum.dev", "athenaeum-demo")

	_, s2, err := e.svc.Refresh(ctx, s1.RefreshToken)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if s2.RefreshToken == s1.RefreshToken {
		t.Fatal("refresh token must rotate")
	}
	if got := e.tokens.Live(u.ID); got != 1 {
		t.Fatalf("live tokens %d, want 1", got)
	}

	// Replaying the old token kills the whole session family.
	_, _, err = e.svc.Refresh(ctx, s1.RefreshToken)
	if code(err) != model.CodeReusedToken {
		t.Fatalf("replay: %v", err)
	}
	if got := e.tokens.Live(u.ID); got != 0 {
		t.Fatalf("family not revoked, live=%d", got)
	}
	if _, _, err := e.svc.Refresh(ctx, s2.RefreshToken); code(err) != model.CodeReusedToken {
		t.Fatalf("revoked sibling must be rejected: %v", err)
	}
}

func TestRefreshInvalid(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	if _, _, err := e.svc.Refresh(ctx, ""); code(err) != model.CodeUnauthenticated {
		t.Fatalf("empty: %v", err)
	}
	if _, _, err := e.svc.Refresh(ctx, "unknown"); code(err) != model.CodeUnauthenticated {
		t.Fatalf("unknown: %v", err)
	}
	_, s, _ := e.svc.Register(ctx, "Demo", "demo@athenaeum.dev", "athenaeum-demo")
	*e.clock = e.clock.Add(169 * time.Hour)
	if _, _, err := e.svc.Refresh(ctx, s.RefreshToken); code(err) != model.CodeUnauthenticated {
		t.Fatalf("expired: %v", err)
	}
}

func TestLogoutRevokesAll(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	u, s, _ := e.svc.Register(ctx, "Demo", "demo@athenaeum.dev", "athenaeum-demo")
	_, _, _ = e.svc.Login(ctx, "demo@athenaeum.dev", "athenaeum-demo")
	if err := e.svc.Logout(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if e.tokens.Live(u.ID) != 0 {
		t.Fatal("tokens still live")
	}
	if _, _, err := e.svc.Refresh(ctx, s.RefreshToken); err == nil {
		t.Fatal("refresh after logout must fail")
	}
}

func TestGoogleLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("not configured", func(t *testing.T) {
		e := newEnv(t, nil)
		if _, _, err := e.svc.GoogleLogin(ctx, "c"); code(err) != model.CodeNotFound {
			t.Fatalf("%v", err)
		}
	})
	t.Run("creates new user with role user and no password", func(t *testing.T) {
		e := newEnv(t, fakeGoogle{profile: auth.GoogleProfile{Sub: "s1", Email: "New@Gmail.com", Name: "New"}})
		u, s, err := e.svc.GoogleLogin(ctx, "c")
		if err != nil {
			t.Fatal(err)
		}
		if u.Role != model.RoleUser || u.PasswordHash != nil || u.Email != "new@gmail.com" || s.AccessToken == "" {
			t.Fatalf("%+v", u)
		}
		// Second login reuses the same account.
		u2, _, err := e.svc.GoogleLogin(ctx, "c")
		if err != nil || u2.ID != u.ID {
			t.Fatalf("second login: %v %v", u2.ID, err)
		}
	})
	t.Run("links existing password account silently", func(t *testing.T) {
		e := newEnv(t, fakeGoogle{profile: auth.GoogleProfile{Sub: "s2", Email: "demo@athenaeum.dev", Name: "Demo G"}})
		orig, _, _ := e.svc.Register(ctx, "Demo", "demo@athenaeum.dev", "athenaeum-demo")
		u, _, err := e.svc.GoogleLogin(ctx, "c")
		if err != nil || u.ID != orig.ID {
			t.Fatalf("link: %v id=%v want=%v", err, u.ID, orig.ID)
		}
		if _, _, err := e.svc.Login(ctx, "demo@athenaeum.dev", "athenaeum-demo"); err != nil {
			t.Fatalf("password login must still work: %v", err)
		}
		if got, _ := e.users.ByGoogleSub(ctx, "s2"); got.ID != orig.ID {
			t.Fatal("google_sub not linked")
		}
	})
	t.Run("exchange failure → UNAUTHENTICATED", func(t *testing.T) {
		e := newEnv(t, fakeGoogle{err: errors.New("bad code")})
		if _, _, err := e.svc.GoogleLogin(ctx, "c"); code(err) != model.CodeUnauthenticated {
			t.Fatalf("%v", err)
		}
	})
}

func mustID() uuid.UUID { return uuid.New() }
