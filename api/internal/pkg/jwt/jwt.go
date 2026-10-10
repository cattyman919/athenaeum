// Package jwt issues and verifies the HS256 access tokens of docs/04 §5.
package jwt

import (
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const (
	// Issuer is the "iss" claim.
	Issuer = "athenaeum"
	// Audience is the "aud" claim.
	Audience = "athenaeum-web"
)

// ErrInvalidToken wraps every verification failure (bad signature, expired, wrong iss/aud…).
var ErrInvalidToken = errors.New("jwt: invalid token")

// Claims is the application view of the token payload.
type Claims struct {
	Subject string // user id
	Email   string
	Role    string
}

// Manager signs and verifies access tokens.
type Manager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewManager builds a Manager; secret must be non-empty.
func NewManager(secret string, ttl time.Duration) (*Manager, error) {
	if secret == "" {
		return nil, errors.New("jwt: empty secret")
	}
	return &Manager{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

// WithClock returns a copy using the given clock (tests).
func (m *Manager) WithClock(now func() time.Time) *Manager {
	c := *m
	c.now = now
	return &c
}

// Issue returns a signed access token and its expiry.
func (m *Manager) Issue(c Claims) (string, time.Time, error) {
	now := m.now()
	exp := now.Add(m.ttl)
	tok := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.MapClaims{
		"sub":   c.Subject,
		"email": c.Email,
		"role":  c.Role,
		"iss":   Issuer,
		"aud":   Audience,
		"iat":   now.Unix(),
		"exp":   exp.Unix(),
	})
	s, err := tok.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwt: sign: %w", err)
	}
	return s, exp, nil
}

// Parse verifies signature (HS256 only), expiry, issuer and audience.
func (m *Manager) Parse(token string) (Claims, error) {
	parsed, err := gojwt.Parse(token, func(t *gojwt.Token) (any, error) { return m.secret, nil },
		gojwt.WithValidMethods([]string{"HS256"}),
		gojwt.WithIssuer(Issuer),
		gojwt.WithAudience(Audience),
		gojwt.WithExpirationRequired(),
		gojwt.WithTimeFunc(m.now),
	)
	if err != nil || !parsed.Valid {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	mc, ok := parsed.Claims.(gojwt.MapClaims)
	if !ok {
		return Claims{}, ErrInvalidToken
	}
	sub, _ := mc["sub"].(string)
	email, _ := mc["email"].(string)
	role, _ := mc["role"].(string)
	if sub == "" {
		return Claims{}, fmt.Errorf("%w: missing sub", ErrInvalidToken)
	}
	return Claims{Subject: sub, Email: email, Role: role}, nil
}
