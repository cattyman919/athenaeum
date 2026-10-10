// Package auth implements registration, login, refresh rotation, logout and
// Google SSO (docs/04-auth.md). Repository interfaces are declared here
// (consumer side, docs/01 §2).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/hash"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
)

// MinPasswordLen is the registration password policy (docs/03 §4.2).
const MinPasswordLen = 10

// Users is the user persistence the service needs.
type Users interface {
	// Create inserts a user; a duplicate email yields model.CodeEmailTaken.
	Create(ctx context.Context, u model.User) error
	// ByEmail / ByID / ByGoogleSub return model.ErrNotFound when absent.
	ByEmail(ctx context.Context, email string) (model.User, error)
	ByID(ctx context.Context, id uuid.UUID) (model.User, error)
	ByGoogleSub(ctx context.Context, sub string) (model.User, error)
	LinkGoogle(ctx context.Context, id uuid.UUID, sub string) error
}

// Tokens is the refresh-token persistence the service needs.
type Tokens interface {
	Insert(ctx context.Context, t model.RefreshToken) error
	// Consume atomically revokes the token identified by hash if it is live
	// and reports the outcome; userID is set when the token exists.
	Consume(ctx context.Context, hash string, now time.Time) (userID uuid.UUID, res model.ConsumeResult, err error)
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
}

// GoogleProfile is the verified identity returned by the OAuth adapter.
type GoogleProfile struct {
	Sub   string
	Email string
	Name  string
}

// Google abstracts the OAuth provider (ACL; implemented in internal/oauth).
type Google interface {
	AuthURL(ctx context.Context, state string) (string, error)
	Exchange(ctx context.Context, code string) (GoogleProfile, error)
}

// Session is a freshly minted token pair.
type Session struct {
	AccessToken    string
	AccessExpires  time.Time
	RefreshToken   string
	RefreshExpires time.Time
}

// Service is the auth use-case layer.
type Service struct {
	users      Users
	tokens     Tokens
	jwt        *jwt.Manager
	google     Google // may be nil when SSO is not configured
	refreshTTL time.Duration
	now        func() time.Time
	log        *slog.Logger
}

// New builds the service; google may be nil.
func New(users Users, tokens Tokens, jm *jwt.Manager, google Google, refreshTTL time.Duration, log *slog.Logger) *Service {
	return &Service{users: users, tokens: tokens, jwt: jm, google: google, refreshTTL: refreshTTL, now: time.Now, log: log}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	c := *s
	c.now = now
	return &c
}

// Register creates an account and logs it in (docs/03 §4.2).
func (s *Service) Register(ctx context.Context, name, email, password string) (model.User, Session, error) {
	name = strings.TrimSpace(name)
	norm, err := normalizeEmail(email)
	if err != nil {
		return model.User{}, Session{}, err
	}
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return model.User{}, Session{}, model.New(model.CodeValidationFailed,
			fmt.Sprintf("Password must be at least %d characters.", MinPasswordLen))
	}
	if name == "" {
		return model.User{}, Session{}, model.New(model.CodeValidationFailed, "Name is required.")
	}
	ph, err := hash.Hash(password)
	if err != nil {
		return model.User{}, Session{}, err
	}
	u := model.User{ID: uuid.New(), Email: norm, Name: name, Role: model.RoleUser, PasswordHash: &ph}
	if err := s.users.Create(ctx, u); err != nil {
		return model.User{}, Session{}, err
	}
	sess, err := s.newSession(ctx, u)
	return u, sess, err
}

// Login verifies credentials; identical error for unknown email, wrong
// password and SSO-only accounts, with equalized timing (docs/04 §2/§6).
func (s *Service) Login(ctx context.Context, email, password string) (model.User, Session, error) {
	norm, err := normalizeEmail(email)
	if err != nil {
		hash.DummyVerify(password)
		return model.User{}, Session{}, model.ErrInvalidCredentials
	}
	u, err := s.users.ByEmail(ctx, norm)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			hash.DummyVerify(password)
			return model.User{}, Session{}, model.ErrInvalidCredentials
		}
		return model.User{}, Session{}, err
	}
	if u.PasswordHash == nil {
		hash.DummyVerify(password)
		return model.User{}, Session{}, model.ErrInvalidCredentials
	}
	ok, verr := hash.Verify(password, *u.PasswordHash)
	if verr != nil || !ok {
		return model.User{}, Session{}, model.ErrInvalidCredentials
	}
	sess, err := s.newSession(ctx, u)
	return u, sess, err
}

// Refresh rotates the presented refresh token (docs/04 §5). A replayed token
// revokes every session of the user and yields REUSED_TOKEN.
func (s *Service) Refresh(ctx context.Context, raw string) (model.User, Session, error) {
	if raw == "" {
		return model.User{}, Session{}, model.ErrUnauthenticated
	}
	uid, res, err := s.tokens.Consume(ctx, HashToken(raw), s.now())
	if err != nil {
		return model.User{}, Session{}, err
	}
	switch res {
	case model.ConsumeOK:
	case model.ConsumeReplayed:
		if err := s.tokens.RevokeAllForUser(ctx, uid); err != nil {
			s.log.Error("revoke family failed", "err", err)
		}
		return model.User{}, Session{}, model.New(model.CodeReusedToken, "Session invalidated. Please sign in again.")
	default:
		return model.User{}, Session{}, model.ErrUnauthenticated
	}
	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.User{}, Session{}, model.ErrUnauthenticated
		}
		return model.User{}, Session{}, err
	}
	sess, err := s.newSession(ctx, u)
	return u, sess, err
}

// Logout revokes every refresh token of the user.
func (s *Service) Logout(ctx context.Context, userID uuid.UUID) error {
	return s.tokens.RevokeAllForUser(ctx, userID)
}

// Me returns the current user.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (model.User, error) {
	u, err := s.users.ByID(ctx, userID)
	if errors.Is(err, model.ErrNotFound) {
		return model.User{}, model.ErrUnauthenticated
	}
	return u, err
}

// GoogleEnabled reports whether SSO is configured.
func (s *Service) GoogleEnabled() bool { return s.google != nil }

// GoogleAuthURL builds the provider redirect for the given state nonce.
func (s *Service) GoogleAuthURL(ctx context.Context, state string) (string, error) {
	if s.google == nil {
		return "", model.New(model.CodeNotFound, "Google sign-in is not configured.")
	}
	return s.google.AuthURL(ctx, state)
}

// GoogleLogin exchanges the code and upserts the user by google_sub, linking
// an existing same-email account silently (docs/04 §3).
func (s *Service) GoogleLogin(ctx context.Context, code string) (model.User, Session, error) {
	if s.google == nil {
		return model.User{}, Session{}, model.New(model.CodeNotFound, "Google sign-in is not configured.")
	}
	p, err := s.google.Exchange(ctx, code)
	if err != nil {
		return model.User{}, Session{}, model.Wrap(model.CodeUnauthenticated, "Google sign-in failed.", err)
	}
	email, err := normalizeEmail(p.Email)
	if err != nil || p.Sub == "" {
		return model.User{}, Session{}, model.New(model.CodeUnauthenticated, "Google sign-in failed.")
	}
	u, err := s.users.ByGoogleSub(ctx, p.Sub)
	switch {
	case err == nil:
	case errors.Is(err, model.ErrNotFound):
		u, err = s.users.ByEmail(ctx, email)
		switch {
		case err == nil:
			if err := s.users.LinkGoogle(ctx, u.ID, p.Sub); err != nil {
				return model.User{}, Session{}, err
			}
			s.log.Info("google account linked", "user_id", u.ID, "linked", true)
		case errors.Is(err, model.ErrNotFound):
			name := p.Name
			if strings.TrimSpace(name) == "" {
				name = email
			}
			sub := p.Sub
			u = model.User{ID: uuid.New(), Email: email, Name: name, Role: model.RoleUser, GoogleSub: &sub}
			if err := s.users.Create(ctx, u); err != nil {
				return model.User{}, Session{}, err
			}
		default:
			return model.User{}, Session{}, err
		}
	default:
		return model.User{}, Session{}, err
	}
	sess, err := s.newSession(ctx, u)
	return u, sess, err
}

func (s *Service) newSession(ctx context.Context, u model.User) (Session, error) {
	at, atExp, err := s.jwt.Issue(jwt.Claims{Subject: u.ID.String(), Email: u.Email, Role: u.Role})
	if err != nil {
		return Session{}, err
	}
	raw, err := newRefreshToken()
	if err != nil {
		return Session{}, err
	}
	rtExp := s.now().Add(s.refreshTTL)
	if err := s.tokens.Insert(ctx, model.RefreshToken{ID: uuid.New(), UserID: u.ID, Hash: HashToken(raw), ExpiresAt: rtExp}); err != nil {
		return Session{}, err
	}
	return Session{AccessToken: at, AccessExpires: atExp, RefreshToken: raw, RefreshExpires: rtExp}, nil
}

// HashToken is the sha256 hex digest stored in refresh_tokens.token_hash.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func newRefreshToken() (string, error) {
	b := make([]byte, 32) // 256-bit
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func normalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || !strings.Contains(e[strings.LastIndex(e, "@"):], ".") {
		return "", model.New(model.CodeValidationFailed, "A valid email is required.")
	}
	return e, nil
}
