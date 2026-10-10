// Package oauth adapts Google's OpenID Connect provider to the auth service
// (anti-corruption layer, docs/04 §3).
package oauth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/cattyman919/athenaeum/api/internal/service/auth"
)

// DefaultIssuer is Google's OIDC issuer.
const DefaultIssuer = "https://accounts.google.com"

// Google implements auth.Google. Provider discovery is lazy so API start-up
// never needs network access.
type Google struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string

	mu       sync.Mutex
	provider *oidc.Provider
}

// NewGoogle builds the adapter; redirectURL must be the callback URL
// registered with Google (WEB_ORIGIN + /api/auth/google/callback).
func NewGoogle(issuer, clientID, clientSecret, redirectURL string) *Google {
	if issuer == "" {
		issuer = DefaultIssuer
	}
	return &Google{issuer: issuer, clientID: clientID, clientSecret: clientSecret, redirectURL: redirectURL}
}

func (g *Google) discover(ctx context.Context) (*oidc.Provider, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.provider != nil {
		return g.provider, nil
	}
	p, err := oidc.NewProvider(ctx, g.issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	g.provider = p
	return p, nil
}

func (g *Google) oauthConfig(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID: g.clientID, ClientSecret: g.clientSecret, RedirectURL: g.redirectURL,
		Endpoint: p.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
}

// AuthURL returns the consent-screen URL carrying state.
func (g *Google) AuthURL(ctx context.Context, state string) (string, error) {
	p, err := g.discover(ctx)
	if err != nil {
		return "", err
	}
	return g.oauthConfig(p).AuthCodeURL(state), nil
}

// Exchange trades the code for tokens and verifies the id_token
// (signature, iss, aud, exp).
func (g *Google) Exchange(ctx context.Context, code string) (auth.GoogleProfile, error) {
	p, err := g.discover(ctx)
	if err != nil {
		return auth.GoogleProfile{}, err
	}
	tok, err := g.oauthConfig(p).Exchange(ctx, code)
	if err != nil {
		return auth.GoogleProfile{}, fmt.Errorf("code exchange: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return auth.GoogleProfile{}, errors.New("missing id_token")
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: g.clientID}).Verify(ctx, raw)
	if err != nil {
		return auth.GoogleProfile{}, fmt.Errorf("verify id_token: %w", err)
	}
	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idt.Claims(&claims); err != nil {
		return auth.GoogleProfile{}, fmt.Errorf("id_token claims: %w", err)
	}
	if !claims.EmailVerified {
		return auth.GoogleProfile{}, errors.New("email not verified")
	}
	return auth.GoogleProfile{Sub: claims.Sub, Email: claims.Email, Name: claims.Name}, nil
}
