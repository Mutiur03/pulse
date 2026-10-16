// Package logging sets up structured JSON logs for the services.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Setup makes slog write JSON lines to stderr at the given level (debug,
// info, warn or error; default info), each tagged with the service name.
// Output from the standard log package goes through the same handler.
func Setup(service, level string) *slog.Logger {
	l := New(os.Stderr, service, level)
	slog.SetDefault(l)
	return l
}

// New builds the logger Setup installs, writing to w.
func New(w io.Writer, service, level string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: ParseLevel(level)})
	return slog.New(h).With("service", service)
}

// ParseLevel maps a LOG_LEVEL value to a slog level.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
