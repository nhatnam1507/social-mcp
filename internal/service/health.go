package service

import (
	"fmt"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/store"
)

const expiringWindow = 7 * 24 * time.Hour

// Health derives an account's health. An account with its own token (e.g. a Facebook Page)
// depends only on that token; otherwise it follows its credential.
func Health(a store.Account, c store.Credential, now time.Time) (health, detail string) {
	ownToken := len(a.SealedAccountToken) > 0
	if a.Status == store.StatusNeedsReauth || (!ownToken && c.Status == store.StatusNeedsReauth) {
		return HealthNeedsReauth, "re-authorization required: run `social-mcp account reauth " + a.ID + "`"
	}
	var deadline time.Time
	switch {
	case ownToken:
		deadline = a.AccountTokenExpiresAt
	case c.Refreshable:
		deadline = c.RefreshExpiresAt
	default:
		deadline = c.ExpiresAt
	}
	switch {
	case deadline.IsZero():
		return HealthOK, ""
	case !now.Before(deadline):
		return HealthNeedsReauth, fmt.Sprintf("authorization expired on %s: run `social-mcp account reauth %s`", deadline.UTC().Format(time.DateOnly), a.ID)
	case deadline.Sub(now) < expiringWindow:
		return HealthExpiring, fmt.Sprintf("authorization expires on %s: run `social-mcp account reauth %s`", deadline.UTC().Format(time.DateOnly), a.ID)
	}
	return HealthOK, ""
}
