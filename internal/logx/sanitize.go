// Package logx removes secrets from text that is logged, stored or shown to agents.
package logx

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"regexp"
)

var secretPatterns = []*regexp.Regexp{
	// key=value in query strings and form bodies
	regexp.MustCompile(`(?i)((?:access_token|refresh_token|client_secret|fb_exchange_token|code_verifier|code|input_token)=)[^&\s"']+`),
	// "key":"value" in JSON bodies
	regexp.MustCompile(`(?i)("(?:access_token|refresh_token|client_secret|id_token)"\s*:\s*")[^"]*`),
	// Authorization header values
	regexp.MustCompile(`(?i)(\b(?:bearer|oauth)\s+)[A-Za-z0-9._~+/=-]{6,}`),
}

// Sanitize masks token, secret and code values.
func Sanitize(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "${1}[redacted]")
	}
	return s
}

// Error renders err for storage or display: *url.Error is reduced to its operation and host
// (dropping the query string) and the rest is sanitized.
func Error(err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		host := ue.URL
		if u, perr := url.Parse(ue.URL); perr == nil {
			host = u.Host
		}
		return Sanitize(ue.Op + " " + host + ": " + ue.Err.Error())
	}
	return Sanitize(err.Error())
}

// Handler wraps a slog.Handler and sanitizes the message and string-like attributes.
type Handler struct{ next slog.Handler }

// NewHandler returns a sanitizing handler.
func NewHandler(next slog.Handler) *Handler { return &Handler{next: next} }

func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, Sanitize(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(sanitizeAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = sanitizeAttr(a)
	}
	return &Handler{next: h.next.WithAttrs(clean)}
}

func (h *Handler) WithGroup(name string) slog.Handler { return &Handler{next: h.next.WithGroup(name)} }

func sanitizeAttr(a slog.Attr) slog.Attr {
	switch v := a.Value.Any().(type) {
	case string:
		return slog.String(a.Key, Sanitize(v))
	case error:
		return slog.String(a.Key, Error(v))
	}
	return a
}
