package mcpserver

import (
	"crypto/subtle"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Protect wraps the MCP handler with loopback Host and Origin checks (against DNS
// rebinding and browser pages) followed by bearer-token authentication.
func Protect(next http.Handler, port int, token string) http.Handler {
	p := strconv.Itoa(port)
	hosts := []string{"127.0.0.1:" + p, "localhost:" + p, "[::1]:" + p}
	origins := []string{"http://127.0.0.1:" + p, "http://localhost:" + p, "http://[::1]:" + p}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(hosts, strings.ToLower(r.Host)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !slices.Contains(origins, strings.ToLower(o)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || got == "" || len(want) == 0 || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="social-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
