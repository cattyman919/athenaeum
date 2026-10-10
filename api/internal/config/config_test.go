package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := LoadFrom(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIPort != 8080 || cfg.AppEnv != "dev" || cfg.AccessTTL != 15*time.Minute ||
		cfg.RefreshTTL != 168*time.Hour || cfg.OllamaEmbedDim != 768 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadFromValidation(t *testing.T) {
	long := strings.Repeat("x", 32)
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"dev without secrets ok", map[string]string{"APP_ENV": "dev"}, ""},
		{"bad env", map[string]string{"APP_ENV": "staging"}, "APP_ENV"},
		{"prod missing access secret", map[string]string{"APP_ENV": "prod", "JWT_REFRESH_SECRET": long}, "JWT_ACCESS_SECRET"},
		{"prod short refresh secret", map[string]string{"APP_ENV": "prod", "JWT_ACCESS_SECRET": long, "JWT_REFRESH_SECRET": "short"}, "JWT_REFRESH_SECRET"},
		{"prod ok", map[string]string{"APP_ENV": "prod", "JWT_ACCESS_SECRET": long, "JWT_REFRESH_SECRET": long}, ""},
		{"bad dim", map[string]string{"OLLAMA_EMBED_DIM": "0"}, "OLLAMA_EMBED_DIM"},
		{"bad port", map[string]string{"API_PORT": "abc"}, "APIPort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFrom(tt.env)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
