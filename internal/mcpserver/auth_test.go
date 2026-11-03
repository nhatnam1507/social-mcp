package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtect(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h := Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }), 8765, token)
	cases := []struct {
		name, host, origin, auth string
		want                     int
	}{
		{"dns rebinding host without token", "evil.example:8765", "", "", http.StatusForbidden},
		{"dns rebinding host with token", "evil.example:8765", "", "Bearer " + token, http.StatusForbidden},
		{"foreign origin", "127.0.0.1:8765", "https://evil.example", "Bearer " + token, http.StatusForbidden},
		{"missing token", "127.0.0.1:8765", "", "", http.StatusUnauthorized},
		{"empty bearer", "127.0.0.1:8765", "", "Bearer ", http.StatusUnauthorized},
		{"wrong token", "localhost:8765", "", "Bearer nope", http.StatusUnauthorized},
		{"ok ipv4", "127.0.0.1:8765", "", "Bearer " + token, http.StatusTeapot},
		{"lowercase scheme", "127.0.0.1:8765", "", "bearer " + token, http.StatusTeapot},
		{"ok ipv6 with local origin", "[::1]:8765", "http://[::1]:8765", "Bearer " + token, http.StatusTeapot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
	empty := Protect(http.NotFoundHandler(), 8765, "")
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.Host = "127.0.0.1:8765"
	r.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	empty.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty configured token accepted an empty bearer: %d", rec.Code)
	}
}
