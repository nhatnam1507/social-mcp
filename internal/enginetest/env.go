// Package enginetest builds a real store, sealer and token manager with fake providers for
// tests of the core engine.
package enginetest

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/providertest"
	"github.com/nhatnam1507/social-mcp/internal/secrets"
	"github.com/nhatnam1507/social-mcp/internal/service"
	"github.com/nhatnam1507/social-mcp/internal/store"
	"github.com/nhatnam1507/social-mcp/internal/tokens"
)

// Env is a test environment.
type Env struct {
	T        testing.TB
	Dir      string
	Store    *store.Store
	Sealer   *secrets.Sealer
	Tokens   *tokens.Manager
	Registry provider.Registry
	Sessions *oauth.Sessions
	Log      *slog.Logger
}

// New returns an environment with one fake provider per platform name.
func New(t testing.TB, platforms ...string) *Env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sealer, err := secrets.NewSealer(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.Registry{}
	for _, p := range platforms {
		reg[p] = providertest.New(p)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Env{T: t, Dir: dir, Store: st, Sealer: sealer, Tokens: tokens.NewManager(st, sealer, reg, log), Registry: reg, Sessions: oauth.NewSessions(), Log: log}
}

// Fake returns the fake provider for platform.
func (e *Env) Fake(platform string) *providertest.Fake {
	return e.Registry[platform].(*providertest.Fake)
}

// AccountSpec describes an account to seed.
type AccountSpec struct {
	Platform     string
	ExternalID   string
	Token        provider.Token
	Refreshable  bool
	AccountToken *provider.Token
	CredExpiry   time.Time
}

// AddAccount seeds a credential and one account.
func (e *Env) AddAccount(s AccountSpec) (store.Credential, store.Account) {
	e.T.Helper()
	if s.Token.AccessToken == "" {
		s.Token = provider.Token{AccessToken: "access", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}
	}
	sealed, err := e.Tokens.Seal(s.Token)
	if err != nil {
		e.T.Fatal(err)
	}
	cred := store.Credential{Platform: s.Platform, Subject: "subject-" + s.ExternalID, SealedToken: sealed, ExpiresAt: s.Token.Expiry, Refreshable: s.Refreshable}
	if !s.CredExpiry.IsZero() {
		cred.ExpiresAt = s.CredExpiry
	}
	acc := store.Account{Platform: s.Platform, ExternalID: s.ExternalID, DisplayName: "Account " + s.ExternalID}
	if s.AccountToken != nil {
		if acc.SealedAccountToken, err = e.Tokens.Seal(*s.AccountToken); err != nil {
			e.T.Fatal(err)
		}
		acc.AccountTokenExpiresAt = s.AccountToken.Expiry
	}
	c, accs, err := e.Store.UpsertLogin(cred, []store.Account{acc})
	if err != nil {
		e.T.Fatal(err)
	}
	return c, accs[0]
}

// Local returns a service over the environment.
func (e *Env) Local(jobs service.JobControl, allowedDirs []string) *service.Local {
	return service.NewLocal(service.Options{
		Store: e.Store, Tokens: e.Tokens, Registry: e.Registry, Sessions: e.Sessions, Jobs: jobs,
		AllowedDirs: allowedDirs, ListenPort: 8765, Version: "test", KeyBackend: "file", StartedAt: time.Now(),
	})
}

// VideoFile writes an MP4-looking file of the given size and returns its path.
func (e *Env) VideoFile(name string, size int) string {
	e.T.Helper()
	return WriteMP4(e.T, filepath.Join(e.Dir, name), size)
}

// WriteMP4 writes a file starting with an ISO-BMFF ftyp box.
func WriteMP4(t testing.TB, path string, size int) string {
	t.Helper()
	head := binary.BigEndian.AppendUint32(nil, 16)
	head = append(head, "ftypisom\x00\x00\x00\x00"...)
	data := make([]byte, max(size, len(head)+8))
	copy(data, head)
	for i := len(head); i < len(data); i++ {
		data[i] = byte(i)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
