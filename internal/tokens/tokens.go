// Package tokens loads, refreshes and persists OAuth tokens without losing rotated tokens.
package tokens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/secrets"
	"github.com/nhatnam1507/social-mcp/internal/store"
)

// Lease is a token ready to use for one account, with enough context to refresh it.
type Lease struct {
	Token        provider.Token
	AccountOwned bool // token belongs to the account (e.g. a Page token), not the credential
	CredentialID string
	Version      int64 // credential version the token was read at
	Refreshable  bool
}

// Manager serializes refreshes per credential.
type Manager struct {
	store  *store.Store
	sealer *secrets.Sealer
	reg    provider.Registry
	log    *slog.Logger

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewManager returns a Manager.
func NewManager(st *store.Store, sealer *secrets.Sealer, reg provider.Registry, log *slog.Logger) *Manager {
	return &Manager{store: st, sealer: sealer, reg: reg, log: log, locks: map[string]*sync.Mutex{}}
}

// Seal encodes and encrypts a token.
func (m *Manager) Seal(t provider.Token) ([]byte, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return m.sealer.Seal(b)
}

// Open decrypts and decodes a token.
func (m *Manager) Open(sealed []byte) (provider.Token, error) {
	var t provider.Token
	b, err := m.sealer.Open(sealed)
	if err != nil {
		return t, err
	}
	return t, json.Unmarshal(b, &t)
}

// ForAccount returns the token to use for an account.
func (m *Manager) ForAccount(a store.Account) (Lease, error) {
	if len(a.SealedAccountToken) > 0 {
		t, err := m.Open(a.SealedAccountToken)
		if err != nil {
			return Lease{}, fmt.Errorf("decrypt account token: %w", err)
		}
		return Lease{Token: t, AccountOwned: true, CredentialID: a.CredentialID}, nil
	}
	c, err := m.store.GetCredential(a.CredentialID)
	if err != nil {
		return Lease{}, err
	}
	t, err := m.Open(c.SealedToken)
	if err != nil {
		return Lease{}, fmt.Errorf("decrypt credential token: %w", err)
	}
	return Lease{Token: t, CredentialID: c.ID, Version: c.Version, Refreshable: c.Refreshable}, nil
}

// EnsureFresh refreshes a credential-owned, refreshable lease that expires within `within`.
func (m *Manager) EnsureFresh(ctx context.Context, l Lease, within time.Duration) (Lease, error) {
	if l.AccountOwned || !l.Refreshable || l.Token.Expiry.IsZero() || time.Until(l.Token.Expiry) > within {
		return l, nil
	}
	return m.Refresh(ctx, l)
}

// Refresh refreshes the credential behind l. If the stored credential is newer than the
// lease (another refresh or a re-login happened), the stored token is returned instead of
// calling the platform with a possibly rotated-away refresh token.
func (m *Manager) Refresh(ctx context.Context, l Lease) (Lease, error) {
	lock := m.lock(l.CredentialID)
	lock.Lock()
	defer lock.Unlock()

	c, err := m.store.GetCredential(l.CredentialID)
	if err != nil {
		return l, err
	}
	stored, err := m.Open(c.SealedToken)
	if err != nil {
		return l, err
	}
	if c.Version != l.Version {
		return Lease{Token: stored, CredentialID: c.ID, Version: c.Version, Refreshable: c.Refreshable}, nil
	}
	p, ok := m.reg.Get(c.Platform)
	if !ok {
		return l, provider.Permanent("platform %s is not configured", c.Platform)
	}
	fresh, err := p.Refresh(ctx, stored)
	if err != nil {
		if errors.Is(err, provider.ErrReauthRequired) {
			if serr := m.store.SetCredentialStatus(c.ID, store.StatusNeedsReauth); serr != nil {
				m.log.Error("mark credential needs_reauth", "credential", c.ID, "err", serr)
			}
		}
		return l, err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = stored.RefreshToken
	}
	if fresh.RefreshExpiry.IsZero() {
		fresh.RefreshExpiry = stored.RefreshExpiry
	}
	sealed, err := m.Seal(fresh)
	if err != nil {
		return l, err
	}
	version := c.Version
	var applied bool
	for attempt := 0; attempt < 5; attempt++ {
		applied, err = m.store.UpdateCredentialToken(c.ID, c.Version, sealed, fresh.Expiry, fresh.RefreshExpiry)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	switch {
	case err != nil:
		// The platform may already have rotated the refresh token; keep using the new token
		// for this attempt and make the failure loud.
		m.log.Error("persist refreshed token failed; the account may need re-authorization", "credential", c.ID, "err", err)
	case !applied:
		// A concurrent re-login wrote a newer token; prefer it.
		if latest, lerr := m.store.GetCredential(c.ID); lerr == nil {
			if t, oerr := m.Open(latest.SealedToken); oerr == nil {
				return Lease{Token: t, CredentialID: c.ID, Version: latest.Version, Refreshable: latest.Refreshable}, nil
			}
		}
	default:
		version++
	}
	return Lease{Token: fresh, CredentialID: c.ID, Version: version, Refreshable: c.Refreshable}, nil
}

func (m *Manager) lock(credID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[credID]
	if !ok {
		l = &sync.Mutex{}
		m.locks[credID] = l
	}
	return l
}
