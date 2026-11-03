package provider

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrReauthRequired means the token cannot be used or refreshed; the user must log in again.
	ErrReauthRequired = errors.New("re-authorization required")
	// ErrUnauthorized means the platform rejected the access token; one refresh may fix it.
	ErrUnauthorized = errors.New("access token rejected")
	// ErrOutcomeUnknown means a publish step may or may not have taken effect.
	ErrOutcomeUnknown = errors.New("upload outcome unknown")
)

// PermanentError is a failure that retrying cannot fix (quota, policy, validation).
type PermanentError struct {
	Msg string
	Err error // optional cause
}

func (e *PermanentError) Error() string { return e.Msg }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent returns a PermanentError.
func Permanent(format string, args ...any) error {
	return &PermanentError{Msg: fmt.Sprintf(format, args...)}
}

// RateLimitError asks the runner to retry after a delay without consuming an attempt.
type RateLimitError struct {
	RetryAfter time.Duration
	Msg        string
}

func (e *RateLimitError) Error() string { return "rate limited: " + e.Msg }

// IsPermanent reports whether err is a PermanentError.
func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// OutcomeUnknown wraps detail as ErrOutcomeUnknown.
func OutcomeUnknown(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrOutcomeUnknown, fmt.Sprintf(format, args...))
}
