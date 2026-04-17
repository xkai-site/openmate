package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

const (
	FieldComponent    = "component"
	FieldOperation    = "op"
	FieldTraceID      = "trace_id"
	FieldRequestID    = "request_id"
	FieldTopicID      = "topic_id"
	FieldNodeID       = "node_id"
	FieldSessionID    = "session_id"
	FieldEventID      = "event_id"
	FieldInvocationID = "invocation_id"
	FieldAttemptID    = "attempt_id"
	FieldDurationMS   = "duration_ms"
)

type Config struct {
	Level     string
	Format    string
	Writer    io.Writer
	AddSource bool
}

type loggerContextKey struct{}

func NewLogger(config Config) (*slog.Logger, error) {
	writer := config.Writer
	if writer == nil {
		return nil, fmt.Errorf("log writer is required")
	}
	level, err := parseLevel(config.Level)
	if err != nil {
		return nil, err
	}
	format := strings.ToLower(strings.TrimSpace(config.Format))
	if format == "" {
		format = "json"
	}
	options := &slog.HandlerOptions{
		Level:     level,
		AddSource: config.AddSource,
	}
	switch format {
	case "json":
		return slog.New(slog.NewJSONHandler(writer, options)), nil
	case "text":
		return slog.New(slog.NewTextHandler(writer, options)), nil
	default:
		return nil, fmt.Errorf("unsupported log format: %s", format)
	}
}

func NormalizeLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, loggerContextKey{}, NormalizeLogger(logger))
}

func LoggerFromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if ctx != nil {
		if value := ctx.Value(loggerContextKey{}); value != nil {
			if logger, ok := value.(*slog.Logger); ok && logger != nil {
				return logger
			}
		}
	}
	return NormalizeLogger(fallback)
}

func WithFields(ctx context.Context, attrs ...slog.Attr) context.Context {
	logger := LoggerFromContext(ctx, nil)
	if len(attrs) == 0 {
		return WithLogger(ctx, logger)
	}
	args := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	return WithLogger(ctx, logger.With(args...))
}

func parseLevel(raw string) (slog.Level, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = "info"
	}
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level: %s", value)
	}
}
