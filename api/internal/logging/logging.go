// Package logging builds the slog logger (docs/09 §7).
package logging

import (
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
)

// New returns a tint (colorized, debug) logger for dev and a JSON (info)
// logger for prod.
func New(prod bool) *slog.Logger {
	if prod {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelDebug, TimeFormat: time.Kitchen}))
}

// Discard returns a logger that drops everything (tests).
func Discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
