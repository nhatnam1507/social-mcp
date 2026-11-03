// Package refresher keeps refreshable credentials fresh in the background.
package refresher

import (
	"context"
	"log/slog"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/store"
	"github.com/nhatnam1507/social-mcp/internal/tokens"
)

// Refresher periodically refreshes credentials that are about to expire.
type Refresher struct {
	Store    *store.Store
	Tokens   *tokens.Manager
	Interval time.Duration // default 5m
	Within   time.Duration // default 15m
	Now      func() time.Time
	Log      *slog.Logger
}

// Run refreshes until ctx is cancelled.
func (r *Refresher) Run(ctx context.Context) {
	interval := r.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	r.RunOnce(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce refreshes refreshable credentials expiring soon and marks expired
// non-refreshable credentials as needing re-authorization.
func (r *Refresher) RunOnce(ctx context.Context) {
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	within := r.Within
	if within == 0 {
		within = 15 * time.Minute
	}
	log := r.Log
	if log == nil {
		log = slog.Default()
	}
	creds, err := r.Store.ListCredentials()
	if err != nil {
		log.Error("list credentials", "err", err)
		return
	}
	for _, c := range creds {
		if c.Status == store.StatusNeedsReauth || c.ExpiresAt.IsZero() {
			continue
		}
		if !c.Refreshable {
			if !now.Before(c.ExpiresAt) {
				if err := r.Store.MarkCredentialStatus(c.ID, c.Version, store.StatusNeedsReauth); err != nil {
					log.Error("mark credential needs_reauth", "credential", c.ID, "err", err)
				}
			}
			continue
		}
		if c.ExpiresAt.Sub(now) > within {
			continue
		}
		lease := tokens.Lease{CredentialID: c.ID, Version: c.Version, Refreshable: true}
		if _, err := r.Tokens.Refresh(ctx, lease); err != nil {
			log.Warn("refresh credential", "credential", c.ID, "platform", c.Platform, "err", err)
		}
	}
}
