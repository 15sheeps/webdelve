package logger

import (
	"github.com/15sheeps/webdelve/internal/config"
	"log/slog"
	"os"
)

func New(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	var handler slog.Handler

	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handlerOpts := &slog.HandlerOptions{Level: level}

	switch cfg.Handler {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	default:
		handler = slog.NewTextHandler(os.Stdout, handlerOpts)
	}

	return slog.New(handler)
}
