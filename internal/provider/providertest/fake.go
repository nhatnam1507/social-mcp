// Package providertest provides a scriptable provider for tests of the core engine.
package providertest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

// Fake is a scriptable provider. UploadFunc decides each Upload call; RefreshFunc each refresh.
type Fake struct {
	Name         string
	Host         string
	CanRefresh   bool
	Caps         provider.Capabilities
	OptionSpecs  []provider.OptionSpec
	Subject      string
	IDs          []provider.Identity
	ExchangeErr  error
	ValidateFunc func(provider.VideoMeta) error
	RefreshFunc  func(provider.Token) (provider.Token, error)
	UploadFunc   func(ctx context.Context, call int, req provider.UploadRequest, progress provider.Progress) (provider.Result, error)

	mu           sync.Mutex
	UploadCalls  []provider.UploadRequest
	RefreshCalls int
}

// New returns a direct-publish fake supporting all privacy levels and formats.
func New(name string) *Fake {
	return &Fake{
		Name:       name,
		Host:       "127.0.0.1",
		CanRefresh: true,
		Subject:    "subject-1",
		IDs:        []provider.Identity{{ExternalID: "ext-1", DisplayName: "Account One"}},
		Caps: provider.Capabilities{
			PublishMode:   provider.PublishDirect,
			PrivacyLevels: []string{provider.PrivacyPrivate, provider.PrivacyUnlisted, provider.PrivacyPublic},
			Formats:       []string{provider.FormatMP4, provider.FormatMOV, provider.FormatWebM, provider.FormatMKV},
		},
		OptionSpecs: []provider.OptionSpec{{Name: "category_id", Kind: "string"}, {Name: "made_for_kids", Kind: "bool"}},
	}
}

func (f *Fake) Platform() string     { return f.Name }
func (f *Fake) RedirectHost() string { return f.Host }
func (f *Fake) Refreshable() bool    { return f.CanRefresh }

func (f *Fake) AuthURL(state, verifier, redirectURI string) string {
	return "https://auth.example/authorize?state=" + state + "&redirect_uri=" + redirectURI
}

func (f *Fake) Exchange(ctx context.Context, code, verifier, redirectURI string) (provider.Token, error) {
	if f.ExchangeErr != nil {
		return provider.Token{}, f.ExchangeErr
	}
	return provider.Token{AccessToken: "access-" + code, RefreshToken: "refresh-" + code}, nil
}

func (f *Fake) Refresh(ctx context.Context, tok provider.Token) (provider.Token, error) {
	f.mu.Lock()
	f.RefreshCalls++
	f.mu.Unlock()
	if f.RefreshFunc != nil {
		return f.RefreshFunc(tok)
	}
	if !f.CanRefresh {
		return provider.Token{}, provider.ErrReauthRequired
	}
	return provider.Token{AccessToken: tok.AccessToken + "+r", RefreshToken: tok.RefreshToken}, nil
}

func (f *Fake) Identities(ctx context.Context, tok provider.Token) (string, []provider.Identity, error) {
	return f.Subject, f.IDs, nil
}

func (f *Fake) Capabilities(map[string]string) provider.Capabilities { return f.Caps }
func (f *Fake) Options() []provider.OptionSpec                       { return f.OptionSpecs }

func (f *Fake) ValidateMeta(m provider.VideoMeta) error {
	if f.ValidateFunc != nil {
		return f.ValidateFunc(m)
	}
	if m.Title == "" {
		return errors.New("title is required")
	}
	return nil
}

func (f *Fake) Upload(ctx context.Context, req provider.UploadRequest, progress provider.Progress) (provider.Result, error) {
	f.mu.Lock()
	f.UploadCalls = append(f.UploadCalls, req)
	call := len(f.UploadCalls)
	f.mu.Unlock()
	if f.UploadFunc != nil {
		return f.UploadFunc(ctx, call, req, progress)
	}
	if err := progress(provider.ProgressUpdate{BytesSent: req.FileSize, Phase: provider.PhaseCommitting, Resume: json.RawMessage(`{}`)}); err != nil {
		return provider.Result{}, err
	}
	return provider.Result{RemoteID: "remote-1", URL: "https://example/v/remote-1", Outcome: provider.OutcomePublished}, nil
}

// Calls returns a copy of the recorded upload requests.
func (f *Fake) Calls() []provider.UploadRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.UploadRequest(nil), f.UploadCalls...)
}

// Refreshes returns how many times Refresh was called.
func (f *Fake) Refreshes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.RefreshCalls
}
