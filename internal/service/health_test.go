package service

import (
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/store"
)

func TestHealth(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		name string
		acc  store.Account
		cred store.Credential
		want string
	}{
		{"page token survives expired user token",
			store.Account{SealedAccountToken: []byte("x"), Status: store.StatusOK},
			store.Credential{ExpiresAt: now.Add(-day), Status: store.StatusNeedsReauth}, HealthOK},
		{"page token own reauth flag",
			store.Account{SealedAccountToken: []byte("x"), Status: store.StatusNeedsReauth},
			store.Credential{Status: store.StatusOK}, HealthNeedsReauth},
		{"non-refreshable credential within 7 days",
			store.Account{Status: store.StatusOK},
			store.Credential{ExpiresAt: now.Add(3 * day), Status: store.StatusOK}, HealthExpiring},
		{"non-refreshable credential expired",
			store.Account{Status: store.StatusOK},
			store.Credential{ExpiresAt: now.Add(-time.Minute), Status: store.StatusOK}, HealthNeedsReauth},
		{"refreshable ignores short access token expiry",
			store.Account{Status: store.StatusOK},
			store.Credential{Refreshable: true, ExpiresAt: now.Add(time.Hour), Status: store.StatusOK}, HealthOK},
		{"refreshable near refresh expiry",
			store.Account{Status: store.StatusOK},
			store.Credential{Refreshable: true, ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(2 * day), Status: store.StatusOK}, HealthExpiring},
		{"credential flagged",
			store.Account{Status: store.StatusOK},
			store.Credential{Refreshable: true, Status: store.StatusNeedsReauth}, HealthNeedsReauth},
		{"unknown deadline", store.Account{Status: store.StatusOK}, store.Credential{Status: store.StatusOK}, HealthOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := Health(tc.acc, tc.cred, now); got != tc.want {
				t.Fatalf("Health = %s, want %s", got, tc.want)
			}
		})
	}
}
