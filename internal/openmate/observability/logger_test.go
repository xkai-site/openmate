package observability

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestNewLoggerJSON(t *testing.T) {
	var buffer bytes.Buffer
	logger, err := NewLogger(Config{
		Level:  "info",
		Format: "json",
		Writer: &buffer,
	})
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	logger.Info("hello", slog.String("component", "test"))
	if buffer.Len() == 0 {
		t.Fatalf("expected logger output")
	}
}

func TestNewLoggerRejectsBadLevel(t *testing.T) {
	var buffer bytes.Buffer
	_, err := NewLogger(Config{
		Level:  "bad",
		Format: "json",
		Writer: &buffer,
	})
	if err == nil {
		t.Fatalf("NewLogger() error = nil, want error")
	}
}

func TestWithFieldsUsesContextLogger(t *testing.T) {
	var buffer bytes.Buffer
	logger, err := NewLogger(Config{
		Level:  "debug",
		Format: "json",
		Writer: &buffer,
	})
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	ctx := WithLogger(context.Background(), logger)
	ctx = WithFields(ctx, slog.String(FieldRequestID, "req-1"))
	LoggerFromContext(ctx, nil).Info("test")
	if !bytes.Contains(buffer.Bytes(), []byte(`"request_id":"req-1"`)) {
		t.Fatalf("expected request_id field in output: %s", buffer.String())
	}
}
