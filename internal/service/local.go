package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/logx"
	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/store"
	"github.com/nhatnam1507/social-mcp/internal/tokens"
	"github.com/nhatnam1507/social-mcp/internal/videofile"
)

var (
	// ErrJobCommitting means the job is publishing and can no longer be cancelled.
	ErrJobCommitting = errors.New("upload is already being published and cannot be cancelled")
	// ErrJobNotRunning means the runner holds no in-flight job with that ID.
	ErrJobNotRunning = errors.New("upload is not running")
)

// JobControl is the runner surface the service needs.
type JobControl interface {
	Wake()
	Cancel(jobID string) error
}

// Options configures Local.
type Options struct {
	Store       *store.Store
	Tokens      *tokens.Manager
	Registry    provider.Registry
	Sessions    *oauth.Sessions
	Jobs        JobControl
	AllowedDirs []string
	ListenPort  int
	Version     string
	KeyBackend  string
	StartedAt   time.Time
	Now         func() time.Time
}

// Local implements Service inside the daemon.
type Local struct{ o Options }

// NewLocal returns a Local service.
func NewLocal(o Options) *Local {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Local{o: o}
}

var _ Service = (*Local)(nil)

// Status summarizes the daemon.
func (l *Local) Status() (StatusView, error) {
	accounts, err := l.ListAccounts("")
	if err != nil {
		return StatusView{}, err
	}
	byPlatform := map[string]map[string]int{}
	for _, a := range accounts {
		if byPlatform[a.Platform] == nil {
			byPlatform[a.Platform] = map[string]int{}
		}
		byPlatform[a.Platform][a.Health]++
	}
	active, err := l.o.Store.CountActiveUploads()
	if err != nil {
		return StatusView{}, err
	}
	return StatusView{
		Version:       l.o.Version,
		PID:           os.Getpid(),
		StartedAt:     l.o.StartedAt,
		ListenURL:     fmt.Sprintf("http://127.0.0.1:%d/mcp", l.o.ListenPort),
		KeyBackend:    l.o.KeyBackend,
		Platforms:     l.o.Registry.Platforms(),
		Accounts:      byPlatform,
		ActiveUploads: active,
	}, nil
}

// ListAccounts returns accounts with health and capabilities.
func (l *Local) ListAccounts(platform string) ([]AccountView, error) {
	accs, err := l.o.Store.ListAccounts(platform)
	if err != nil {
		return nil, err
	}
	creds := map[string]store.Credential{}
	out := make([]AccountView, 0, len(accs))
	for _, a := range accs {
		c, ok := creds[a.CredentialID]
		if !ok {
			if c, err = l.o.Store.GetCredential(a.CredentialID); err != nil {
				return nil, err
			}
			creds[a.CredentialID] = c
		}
		out = append(out, l.accountView(a, c))
	}
	return out, nil
}

func (l *Local) accountView(a store.Account, c store.Credential) AccountView {
	v := AccountView{ID: a.ID, Platform: a.Platform, DisplayName: a.DisplayName, ExternalID: a.ExternalID, Options: []provider.OptionSpec{}}
	v.Health, v.HealthDetail = Health(a, c, l.o.Now())
	p, ok := l.o.Registry.Get(a.Platform)
	if !ok {
		v.HealthDetail = "platform " + a.Platform + " is not configured in config.toml"
		v.Capabilities = provider.Capabilities{PrivacyLevels: []string{}, Restrictions: []string{}, Formats: []string{}}
		return v
	}
	v.Capabilities = p.Capabilities(a.Meta)
	if specs := p.Options(); specs != nil {
		v.Options = specs
	}
	return v
}

// RemoveAccount deletes an account unless it has active uploads.
func (l *Local) RemoveAccount(id string) error {
	if _, err := l.o.Store.GetAccount(id); err != nil {
		return mapErr(err)
	}
	active, err := l.o.Store.HasActiveUploads(id)
	if err != nil {
		return err
	}
	if active {
		return Invalid("account %s has queued or running uploads; wait for them or cancel them first", id)
	}
	return mapErr(l.o.Store.DeleteAccount(id))
}

// BeginAuth starts a browser login.
func (l *Local) BeginAuth(platform, reauthAccountID string) (AuthStart, error) {
	p, ok := l.o.Registry.Get(platform)
	if !ok {
		return AuthStart{}, Invalid("platform %q is not configured; add its credentials to config.toml (configured: %v)", platform, l.o.Registry.Platforms())
	}
	reauthExternal := ""
	if reauthAccountID != "" {
		a, err := l.o.Store.GetAccount(reauthAccountID)
		if err != nil {
			return AuthStart{}, mapErr(err)
		}
		if a.Platform != platform {
			return AuthStart{}, Invalid("account %s belongs to %s, not %s", reauthAccountID, a.Platform, platform)
		}
		reauthExternal = a.ExternalID
	}
	redirect := fmt.Sprintf("http://%s:%d/oauth/callback", p.RedirectHost(), l.o.ListenPort)
	sess := l.o.Sessions.Begin(platform, redirect, reauthExternal)
	return AuthStart{SessionID: sess.ID, AuthURL: p.AuthURL(sess.State, sess.Verifier, redirect)}, nil
}

// AuthSession reports a login's progress.
func (l *Local) AuthSession(id string) (AuthSessionView, error) {
	sess, ok := l.o.Sessions.Get(id)
	if !ok {
		return AuthSessionView{}, ErrNotFound
	}
	v := AuthSessionView{ID: sess.ID, Platform: sess.Platform, State: sess.Status, Error: sess.Error}
	for _, accID := range sess.AccountIDs {
		a, err := l.o.Store.GetAccount(accID)
		if err != nil {
			continue
		}
		c, err := l.o.Store.GetCredential(a.CredentialID)
		if err != nil {
			continue
		}
		v.Accounts = append(v.Accounts, l.accountView(a, c))
	}
	return v, nil
}

// CompleteAuth handles the OAuth redirect: exchange the code, discover identities and store them.
func (l *Local) CompleteAuth(ctx context.Context, state, code, oauthErr string) (AuthSessionView, error) {
	sess, err := l.o.Sessions.Take(state)
	if err != nil {
		return AuthSessionView{}, err
	}
	accountIDs, err := l.completeAuth(ctx, sess, code, oauthErr)
	if err != nil {
		err = errors.New(logx.Error(err))
	}
	l.o.Sessions.Complete(sess.ID, accountIDs, err)
	view, verr := l.AuthSession(sess.ID)
	if verr != nil {
		return view, verr
	}
	return view, err
}

func (l *Local) completeAuth(ctx context.Context, sess oauth.Session, code, oauthErr string) ([]string, error) {
	if oauthErr != "" {
		return nil, fmt.Errorf("the platform reported: %s", oauthErr)
	}
	if code == "" {
		return nil, errors.New("the platform did not return an authorization code")
	}
	p, ok := l.o.Registry.Get(sess.Platform)
	if !ok {
		return nil, fmt.Errorf("platform %s is not configured", sess.Platform)
	}
	tok, err := p.Exchange(ctx, code, sess.Verifier, sess.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}
	subject, idents, err := p.Identities(ctx, tok)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	if len(idents) == 0 {
		return nil, errors.New("this login has no accounts that can publish videos")
	}
	if sess.ReauthExternalID != "" && !slices.ContainsFunc(idents, func(i provider.Identity) bool { return i.ExternalID == sess.ReauthExternalID }) {
		return nil, fmt.Errorf("the signed-in account does not include the account being re-authorized (%s); sign in with the same account", sess.ReauthExternalID)
	}
	sealed, err := l.o.Tokens.Seal(tok)
	if err != nil {
		return nil, err
	}
	cred := store.Credential{
		Platform:         sess.Platform,
		Subject:          subject,
		SealedToken:      sealed,
		ExpiresAt:        tok.Expiry,
		RefreshExpiresAt: tok.RefreshExpiry,
		Refreshable:      p.Refreshable() && tok.RefreshToken != "",
	}
	accs := make([]store.Account, 0, len(idents))
	for _, id := range idents {
		a := store.Account{Platform: sess.Platform, ExternalID: id.ExternalID, DisplayName: id.DisplayName, Meta: id.Meta}
		if id.AccountToken != nil {
			if a.SealedAccountToken, err = l.o.Tokens.Seal(*id.AccountToken); err != nil {
				return nil, err
			}
			a.AccountTokenExpiresAt = id.AccountToken.Expiry
		}
		accs = append(accs, a)
	}
	_, stored, err := l.o.Store.UpsertLogin(cred, accs)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(stored))
	for i, a := range stored {
		out[i] = a.ID
	}
	return out, nil
}

// SubmitUpload validates the request and creates one job per accepted account.
func (l *Local) SubmitUpload(in UploadInput) (UploadSubmission, error) {
	if len(in.AccountIDs) == 0 {
		return UploadSubmission{}, Invalid("account_ids must list at least one account (see list_accounts)")
	}
	info, err := videofile.Check(in.FilePath, l.o.AllowedDirs)
	if err != nil {
		return UploadSubmission{}, Invalid("%v", err)
	}
	privacy := in.Privacy
	if privacy == "" {
		privacy = provider.PrivacyPrivate
	}

	sub := UploadSubmission{Jobs: []JobRef{}, Rejected: []Rejection{}}
	type accepted struct {
		acc  store.Account
		meta provider.VideoMeta
	}
	var ok []accepted
	selectedPlatforms := map[string]bool{}
	seen := map[string]bool{}
	for _, id := range in.AccountIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		reject := func(format string, args ...any) {
			sub.Rejected = append(sub.Rejected, Rejection{AccountID: id, Reason: fmt.Sprintf(format, args...)})
		}
		a, err := l.o.Store.GetAccount(id)
		if errors.Is(err, store.ErrNotFound) {
			reject("unknown account (see list_accounts)")
			continue
		} else if err != nil {
			return UploadSubmission{}, err
		}
		selectedPlatforms[a.Platform] = true
		c, err := l.o.Store.GetCredential(a.CredentialID)
		if err != nil {
			return UploadSubmission{}, err
		}
		p, found := l.o.Registry.Get(a.Platform)
		if !found {
			reject("platform %s is not configured", a.Platform)
			continue
		}
		if h, detail := Health(a, c, l.o.Now()); h == HealthNeedsReauth {
			reject("%s", detail)
			continue
		}
		caps := p.Capabilities(a.Meta)
		if !slices.Contains(caps.PrivacyLevels, privacy) {
			reject("%s does not support privacy %q (supported: %v)", a.Platform, privacy, caps.PrivacyLevels)
			continue
		}
		if !slices.Contains(caps.Formats, info.Container) {
			reject("%s does not accept %s files (accepted: %v)", a.Platform, info.Container, caps.Formats)
			continue
		}
		if caps.MaxFileBytes > 0 && info.Size > caps.MaxFileBytes {
			reject("file is %d bytes; %s accepts at most %d", info.Size, a.Platform, caps.MaxFileBytes)
			continue
		}
		opts := in.PlatformOptions[a.Platform]
		if err := provider.CheckOptions(p.Options(), opts); err != nil {
			reject("%s: %v", a.Platform, err)
			continue
		}
		meta := provider.VideoMeta{Title: in.Title, Description: in.Description, Tags: in.Tags, Privacy: privacy, Options: opts}
		if err := p.ValidateMeta(meta); err != nil {
			reject("%s: %v", a.Platform, err)
			continue
		}
		ok = append(ok, accepted{acc: a, meta: meta})
	}
	for platform := range in.PlatformOptions {
		if !selectedPlatforms[platform] {
			return UploadSubmission{}, Invalid("platform_options has %q but no selected account is on that platform", platform)
		}
	}
	for _, a := range ok {
		req, err := json.Marshal(a.meta)
		if err != nil {
			return UploadSubmission{}, err
		}
		u, err := l.o.Store.InsertUpload(store.Upload{
			AccountID: a.acc.ID, Platform: a.acc.Platform, FilePath: info.Path, FileSize: info.Size, FileMtime: info.ModTime,
			FileFingerprint: info.Fingerprint, Container: info.Container, Request: req,
			Status: store.StatusQueued, Phase: store.PhasePending,
		})
		if err != nil {
			return UploadSubmission{}, err
		}
		sub.Jobs = append(sub.Jobs, JobRef{JobID: u.ID, AccountID: u.AccountID, Platform: u.Platform, Status: u.Status})
	}
	if len(sub.Jobs) == 0 {
		msg := "no upload was created:"
		for _, r := range sub.Rejected {
			msg += fmt.Sprintf(" %s: %s;", r.AccountID, r.Reason)
		}
		return sub, Invalid("%s", msg)
	}
	if l.o.Jobs != nil {
		l.o.Jobs.Wake()
	}
	return sub, nil
}

// GetUpload returns one job.
func (l *Local) GetUpload(id string) (JobView, error) {
	u, err := l.o.Store.GetUpload(id)
	if err != nil {
		return JobView{}, mapErr(err)
	}
	return jobView(u), nil
}

// ListUploads returns jobs, newest first.
func (l *Local) ListUploads(f UploadFilter) ([]JobView, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	f.Limit = min(f.Limit, 100)
	us, err := l.o.Store.ListUploads(store.UploadFilter{Status: f.Status, AccountID: f.AccountID, Limit: f.Limit})
	if err != nil {
		return nil, err
	}
	out := make([]JobView, len(us))
	for i, u := range us {
		out[i] = jobView(u)
	}
	return out, nil
}

// CancelUpload cancels a queued job, or a running job that has not started publishing.
func (l *Local) CancelUpload(id string) (JobView, error) {
	u, err := l.o.Store.GetUpload(id)
	if err != nil {
		return JobView{}, mapErr(err)
	}
	switch u.Status {
	case store.StatusQueued:
		note := "cancelled by user"
		applied, err := l.o.Store.TransitionUpload(id, store.StatusQueued, store.StatusCancelled, store.UploadFields{Note: &note})
		if err != nil {
			return JobView{}, err
		}
		if !applied { // a worker claimed it in the meantime
			return l.cancelRunning(id)
		}
	case store.StatusUploading:
		return l.cancelRunning(id)
	default:
		return JobView{}, Invalid("upload %s already finished with status %s", id, u.Status)
	}
	return l.GetUpload(id)
}

func (l *Local) cancelRunning(id string) (JobView, error) {
	if l.o.Jobs == nil {
		return JobView{}, ErrJobNotRunning
	}
	if err := l.o.Jobs.Cancel(id); err != nil {
		if errors.Is(err, ErrJobCommitting) {
			return JobView{}, Invalid("%v", err)
		}
		return JobView{}, err
	}
	// The worker records the cancellation; give it a moment so the caller sees the result.
	for i := 0; i < 50; i++ {
		u, err := l.o.Store.GetUpload(id)
		if err == nil && u.Status != store.StatusUploading {
			return jobView(u), nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return l.GetUpload(id)
}

func jobView(u store.Upload) JobView {
	v := JobView{
		ID: u.ID, AccountID: u.AccountID, Platform: u.Platform, FilePath: u.FilePath, Status: u.Status, Phase: u.Phase,
		BytesSent: u.BytesSent, FileSize: u.FileSize, Outcome: u.Outcome, RemoteID: u.RemoteID, URL: u.RemoteURL,
		Note: u.Note, Error: u.Error, Attempts: u.Attempts, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
	if u.FileSize > 0 {
		v.Percent = float64(int(float64(u.BytesSent)/float64(u.FileSize)*1000)) / 10
	}
	var meta provider.VideoMeta
	if json.Unmarshal(u.Request, &meta) == nil {
		v.Privacy = meta.Privacy
	}
	return v
}

func mapErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
