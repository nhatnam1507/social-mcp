package provider

import (
	"net/http"
	"strconv"
	"time"
)

// RetryAfter parses a Retry-After header in seconds, falling back to def.
func RetryAfter(h http.Header, def time.Duration) time.Duration {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return def
}
