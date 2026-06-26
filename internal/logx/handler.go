package logx

import (
	"context"
	"log/slog"
)

// RedactingHandler is a slog.Handler middleware that runs every log record's
// message and string-valued attributes through [Redact] before forwarding to an
// inner handler. It is the single log-time secret boundary: wrapping the process
// logger once (in main) means no individual call site has to remember to redact.
//
// Only string-shaped values are scanned. Non-string attributes (ints, bools,
// times, durations) cannot carry a secret token and are passed through untouched,
// keeping the hot path cheap. Groups are recursed into.
type RedactingHandler struct {
	next slog.Handler
}

// NewRedactingHandler wraps next so all output is secret-redacted. If next is
// nil the returned handler is a no-op-safe wrapper (Enabled reports false).
func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

// Enabled delegates to the inner handler.
func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.next == nil {
		return false
	}
	return h.next.Enabled(ctx, level)
}

// Handle redacts the record message and attributes, then forwards it.
func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	clone := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clone.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, clone)
}

// WithAttrs redacts the pre-bound attributes too, so a secret captured via
// logger.With(...) is masked just like an inline one.
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(redacted)}
}

// WithGroup delegates; group nesting is preserved.
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

// redactAttr returns a copy of a with any string value (recursing into groups)
// passed through Redact. Non-string kinds are returned unchanged.
func redactAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, Redact(a.Value.String()))
	case slog.KindGroup:
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, g := range group {
			out[i] = redactAttr(g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindAny:
		// A value carried as Any may stringify to something secret-bearing. The
		// prime leak vector is `"error", err` where err wraps adapter stderr — slog
		// renders that via Error(), so redact it (converting to a string attr is fine
		// for log output). Then the generic String() and string cases.
		switch v := a.Value.Any().(type) {
		case error:
			return slog.String(a.Key, Redact(v.Error()))
		case string:
			return slog.String(a.Key, Redact(v))
		case interface{ String() string }:
			return slog.String(a.Key, Redact(v.String()))
		default:
			return a
		}
	default:
		return a
	}
}
