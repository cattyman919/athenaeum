package oauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/cattyman919/athenaeum/api/internal/oauth"
)

// fakeOIDC is an httptest OpenID provider: discovery, JWKS and token endpoint.
type fakeOIDC struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	clientID string
	claims   func(iss string) map[string]any
}

func newFakeOIDC(t *testing.T, clientID string) *fakeOIDC {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOIDC{key: key, clientID: clientID}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/auth",
			"token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "good-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		raw, err := jwt.Signed(signer).Claims(f.claims(f.srv.URL)).Serialize()
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.claims = func(iss string) map[string]any {
		return map[string]any{
			"iss": iss, "aud": clientID, "sub": "google-sub-1", "email": "reader@gmail.com",
			"email_verified": true, "name": "Reader", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		}
	}
	return f
}

func TestGoogleAuthURLAndExchange(t *testing.T) {
	f := newFakeOIDC(t, "client-1")
	g := oauth.NewGoogle(f.srv.URL, "client-1", "secret", "http://localhost:3000/api/auth/google/callback")
	ctx := context.Background()

	u, err := g.AuthURL(ctx, "state-xyz")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	if parsed.Path != "/auth" || q.Get("state") != "state-xyz" || q.Get("client_id") != "client-1" ||
		q.Get("redirect_uri") != "http://localhost:3000/api/auth/google/callback" || q.Get("response_type") != "code" {
		t.Fatalf("auth url %s", u)
	}

	p, err := g.Exchange(ctx, "good-code")
	if err != nil || p.Sub != "google-sub-1" || p.Email != "reader@gmail.com" || p.Name != "Reader" {
		t.Fatalf("exchange %+v %v", p, err)
	}
	if _, err := g.Exchange(ctx, "bad-code"); err == nil {
		t.Fatal("bad code must fail")
	}
}

func TestGoogleRejectsBadIDTokens(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(m map[string]any){
		"wrong audience":   func(m map[string]any) { m["aud"] = "someone-else" },
		"wrong issuer":     func(m map[string]any) { m["iss"] = "https://evil.example" },
		"expired":          func(m map[string]any) { m["exp"] = time.Now().Add(-time.Hour).Unix() },
		"unverified email": func(m map[string]any) { m["email_verified"] = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeOIDC(t, "client-1")
			base := f.claims
			f.claims = func(iss string) map[string]any { m := base(iss); mutate(m); return m }
			g := oauth.NewGoogle(f.srv.URL, "client-1", "secret", "http://localhost/cb")
			if _, err := g.Exchange(ctx, "good-code"); err == nil {
				t.Fatal("must be rejected")
			}
		})
	}
}

func TestGoogleDiscoveryFailure(t *testing.T) {
	g := oauth.NewGoogle("http://127.0.0.1:1", "c", "s", "http://localhost/cb")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := g.AuthURL(ctx, "s"); err == nil {
		t.Fatal("unreachable issuer must error")
	}
}
