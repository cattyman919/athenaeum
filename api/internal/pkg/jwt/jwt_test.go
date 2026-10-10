package jwt

import (
	"errors"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

func mgr(t *testing.T, now time.Time) *Manager {
	t.Helper()
	m, err := NewManager("0123456789abcdef0123456789abcdef", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return m.WithClock(func() time.Time { return now })
}

func TestIssueParseRoundTrip(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	m := mgr(t, now)
	tok, exp, err := m.Issue(Claims{Subject: "u1", Email: "a@b.c", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("exp %v", exp)
	}
	c, err := m.Parse(tok)
	if err != nil || c != (Claims{Subject: "u1", Email: "a@b.c", Role: "admin"}) {
		t.Fatalf("claims %+v err %v", c, err)
	}
}

func TestParseRejects(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	m := mgr(t, now)
	good, _, _ := m.Issue(Claims{Subject: "u1", Role: "user"})

	other, _ := NewManager("ffffffffffffffffffffffffffffffff", time.Minute)
	badSig, _, _ := other.WithClock(func() time.Time { return now }).Issue(Claims{Subject: "u1"})

	signRaw := func(method gojwt.SigningMethod, key any, claims gojwt.MapClaims) string {
		s, err := gojwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	base := func() gojwt.MapClaims {
		return gojwt.MapClaims{"sub": "u1", "iss": Issuer, "aud": Audience, "exp": now.Add(time.Minute).Unix()}
	}
	wrongIss := base()
	wrongIss["iss"] = "evil"
	wrongAud := base()
	wrongAud["aud"] = "other"
	noExp := base()
	delete(noExp, "exp")
	noSub := base()
	delete(noSub, "sub")

	tests := map[string]string{
		"garbage":    "not.a.jwt",
		"bad sig":    badSig,
		"wrong iss":  signRaw(gojwt.SigningMethodHS256, secret, wrongIss),
		"wrong aud":  signRaw(gojwt.SigningMethodHS256, secret, wrongAud),
		"no exp":     signRaw(gojwt.SigningMethodHS256, secret, noExp),
		"no sub":     signRaw(gojwt.SigningMethodHS256, secret, noSub),
		"hs512":      signRaw(gojwt.SigningMethodHS512, secret, base()),
		"alg none":   signRaw(gojwt.SigningMethodNone, gojwt.UnsafeAllowNoneSignatureType, base()),
		"empty":      "",
		"good(ctrl)": "",
	}
	delete(tests, "good(ctrl)")
	for name, tok := range tests {
		if _, err := m.Parse(tok); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%s: want ErrInvalidToken, got %v", name, err)
		}
	}
	// Expired: parse the good token 16 minutes later.
	later := mgr(t, now.Add(16*time.Minute))
	if _, err := later.Parse(good); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired token accepted: %v", err)
	}
}

func TestNewManagerEmptySecret(t *testing.T) {
	if _, err := NewManager("", time.Minute); err == nil {
		t.Fatal("empty secret must fail")
	}
}
