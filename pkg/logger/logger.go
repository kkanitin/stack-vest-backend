package logger

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kanitin/stackvest/backend/pkg/requestid"
)

// New creates a *zap.Logger with the given level and format ("json" or "text").
// Call zap.ReplaceGlobals with the result to make it the process-wide default.
func New(level, format string) (*zap.Logger, error) {
	var lvl zapcore.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = zapcore.DebugLevel
	case "warn", "warning":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	default:
		lvl = zapcore.InfoLevel
	}

	encoding := "json"
	if strings.ToLower(format) == "text" {
		encoding = "console"
	}

	cfg := zap.Config{
		Level:             zap.NewAtomicLevelAt(lvl),
		Development:       false,
		DisableCaller:     true,
		DisableStacktrace: true,
		Encoding:          encoding,
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "time",
			LevelKey:       "level",
			MessageKey:     "msg",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.CapitalLevelEncoder,
			EncodeTime:     zapcore.RFC3339NanoTimeEncoder,
			EncodeDuration: zapcore.NanosDurationEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	return cfg.Build()
}

// RequestID returns a zap.Field for the request ID carried in ctx, or a
// no-op field if ctx carries none (e.g. call sites with no context.Context).
func RequestID(ctx context.Context) zap.Field {
	if id := requestid.FromContext(ctx); id != "" {
		return zap.String("request_id", id)
	}
	return zap.Skip()
}
