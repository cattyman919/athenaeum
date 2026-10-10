// Package config loads the typed runtime configuration from the environment
// (docs/01-architecture.md §7). It is the only place that reads env vars.
package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

const minSecretBytes = 32

// Config mirrors the environment variable table in docs/01 §7.
type Config struct {
	AppEnv    string `env:"APP_ENV" envDefault:"dev"`
	APIPort   int    `env:"API_PORT" envDefault:"8080"`
	WebOrigin string `env:"WEB_ORIGIN" envDefault:"http://localhost:3000"`

	DatabaseURL   string `env:"DATABASE_URL" envDefault:"postgres://athenaeum:athenaeum@localhost:5432/athenaeum?sslmode=disable"`
	RedisAddr     string `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	RedisPassword string `env:"REDIS_PASSWORD"`

	JWTAccessSecret  string        `env:"JWT_ACCESS_SECRET"`
	JWTRefreshSecret string        `env:"JWT_REFRESH_SECRET"`
	AccessTTL        time.Duration `env:"ACCESS_TTL" envDefault:"15m"`
	RefreshTTL       time.Duration `env:"REFRESH_TTL" envDefault:"168h"`

	GoogleClientID     string `env:"GOOGLE_OAUTH_CLIENT_ID"`
	GoogleClientSecret string `env:"GOOGLE_OAUTH_CLIENT_SECRET"`
	GoogleIssuer       string `env:"GOOGLE_OAUTH_ISSUER" envDefault:"https://accounts.google.com"`

	OpenRouterAPIKey  string `env:"OPENROUTER_API_KEY"`
	OpenRouterBaseURL string `env:"OPENROUTER_BASE_URL" envDefault:"https://openrouter.ai/api/v1"`
	OpenRouterModel   string `env:"OPENROUTER_MODEL" envDefault:"anthropic/claude-3.5-sonnet"`

	OllamaBaseURL    string `env:"OLLAMA_BASE_URL" envDefault:"http://localhost:11434"`
	OllamaEmbedModel string `env:"OLLAMA_EMBED_MODEL" envDefault:"nomic-embed-text"`
	OllamaEmbedDim   int    `env:"OLLAMA_EMBED_DIM" envDefault:"768"`

	WhisperBaseURL string `env:"WHISPER_BASE_URL" envDefault:"http://localhost:9000"`
	WhisperModel   string `env:"WHISPER_MODEL" envDefault:"small"`

	OpenLibraryBaseURL string `env:"OPENLIBRARY_BASE_URL" envDefault:"https://openlibrary.org"`
	OfflineMode        bool   `env:"OFFLINE_MODE" envDefault:"false"`

	SeedAdminEmail    string `env:"SEED_ADMIN_EMAIL" envDefault:"admin@athenaeum.dev"`
	SeedAdminPassword string `env:"SEED_ADMIN_PASSWORD" envDefault:"athenaeum-demo"`
	SeedUserEmail     string `env:"SEED_USER_EMAIL" envDefault:"demo@athenaeum.dev"`
	SeedUserPassword  string `env:"SEED_USER_PASSWORD" envDefault:"athenaeum-demo"`
}

// IsProd reports whether the service runs with APP_ENV=prod.
func (c Config) IsProd() bool { return c.AppEnv == "prod" }

// Load parses the process environment.
func Load() (Config, error) {
	return LoadFrom(nil)
}

// LoadFrom parses the given environment map; a nil map means os.Environ.
func LoadFrom(environ map[string]string) (Config, error) {
	var cfg Config
	opts := env.Options{}
	if environ != nil {
		opts.Environment = environ
	}
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return Config{}, fmt.Errorf("config: parse env: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch c.AppEnv {
	case "dev", "prod":
	default:
		return fmt.Errorf("config: APP_ENV must be dev or prod, got %q", c.AppEnv)
	}
	if c.OllamaEmbedDim <= 0 {
		return errors.New("config: OLLAMA_EMBED_DIM must be positive")
	}
	if c.IsProd() {
		if len(c.JWTAccessSecret) < minSecretBytes {
			return fmt.Errorf("config: JWT_ACCESS_SECRET must be at least %d bytes in prod", minSecretBytes)
		}
		if len(c.JWTRefreshSecret) < minSecretBytes {
			return fmt.Errorf("config: JWT_REFRESH_SECRET must be at least %d bytes in prod", minSecretBytes)
		}
	}
	return nil
}
