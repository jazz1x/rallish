package logx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := NewRedactingHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return slog.New(h), &buf
}

func TestRedactingHandler_MessageAndAttrs(t *testing.T) {
	logger, buf := newTestLogger()
	logger.Info("starting with sk-ant-api03-abcdef0123456789ABCDEF",
		"api_key", "ANTHROPIC_API_KEY=topsecretvalue1",
		"count", 7)

	out := buf.String()
	if strings.Contains(out, "sk-ant-api03-abcdef0123456789ABCDEF") {
		t.Errorf("message secret survived: %s", out)
	}
	if strings.Contains(out, "topsecretvalue1") {
		t.Errorf("attr secret survived: %s", out)
	}
	if !strings.Contains(out, "count=7") {
		t.Errorf("non-secret attr should be intact: %s", out)
	}
}

// The prime leak vector: `"error", err` where err wraps adapter stderr.
func TestRedactingHandler_ErrorAttr(t *testing.T) {
	logger, buf := newTestLogger()
	err := errors.New("adapter failed: GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789")
	logger.Error("turn failed", "error", err)

	out := buf.String()
	if strings.Contains(out, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("secret in error attr survived: %s", out)
	}
	if !strings.Contains(out, placeholder) {
		t.Errorf("expected placeholder in: %s", out)
	}
}

// Secrets bound via With(...) must be redacted too.
func TestRedactingHandler_WithAttrs(t *testing.T) {
	logger, buf := newTestLogger()
	logger.With("auth", "Bearer abcdef0123456789ABCDEF01").Info("ready")
	if strings.Contains(buf.String(), "abcdef0123456789ABCDEF01") {
		t.Errorf("With-bound secret survived: %s", buf.String())
	}
}

func TestRedactingHandler_NilNext(t *testing.T) {
	h := NewRedactingHandler(nil)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("nil-next handler should report disabled")
	}
}
