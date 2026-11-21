package observability

import (
	"io"
	"log/slog"
	"strings"
)

// NewLogger returns a JSON slog logger that stamps every line with the process name,
// environment and version (docs/OBSERVABILITY.md). trace_id is added in M3.
func NewLogger(w io.Writer, service, env, version, level string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(h).With(
		slog.String("service", service),
		slog.String("env", env),
		slog.String("version", version),
	)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
