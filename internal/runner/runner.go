// Package runner executes upload jobs from the store with retries, resume and cancellation.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/logx"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/service"
	"github.com/nhatnam1507/social-mcp/internal/store"
	"github.com/nhatnam1507/social-mcp/internal/tokens"
	"github.com/nhatnam1507/social-mcp/internal/videofile"
)

var (
	errShutdown   = errors.New("daemon shutting down")
	errUserCancel = errors.New("cancelled by user")
)

const (
	maxAttempts      = 3
	defaultRateDelay = 60 * time.Second
)

// Options configures a Runner.
type Options struct {
	Store       *store.Store
	Tokens      *tokens.Manager
	Registry    provider.Registry
	AllowedDirs []string
	Workers     int
	// Serialized platforms run at most one job per account at a time.
	Serialized []string
	Backoff    []time.Duration
	// ProgressEvery throttles byte-only progress writes.
	ProgressEvery time.Duration
	// IdlePoll is how often idle workers look for jobs whose not_before has passed.
	IdlePoll time.Duration
	Log      *slog.Logger
}

// Runner runs upload jobs.
type Runner struct {
	o      Options
	wake   chan struct{}
	stop   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	jobs   map[string]*jobCtl
	once   sync.Once
	cancel context.CancelFunc
}

type jobCtl struct {
	cancel     context.CancelCauseFunc
	committing bool
}

// New returns a Runner with defaults applied.
func New(o Options) *Runner {
	if o.Workers <= 0 {
		o.Workers = 2
	}
	if o.Backoff == nil {
		o.Backoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}
	}
	if o.ProgressEvery == 0 {
		o.ProgressEvery = 2 * time.Second
	}
	if o.IdlePoll == 0 {
		o.IdlePoll = time.Second
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Runner{o: o, wake: make(chan struct{}, 1), stop: make(chan struct{}), jobs: map[string]*jobCtl{}}
}

// Start re-queues jobs interrupted by a previous process and starts the workers.
func (r *Runner) Start() error {
	n, err := r.o.Store.ResetInterrupted()
	if err != nil {
		return err
	}
	if n > 0 {
		r.o.Log.Info("resuming interrupted uploads", "count", n)
	}
	base, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	for i := 0; i < r.o.Workers; i++ {
		r.wg.Add(1)
		go r.worker(base)
	}
	return nil
}

// Stop stops claiming jobs, interrupts running jobs without changing their status, and waits
// for workers until ctx expires.
func (r *Runner) Stop(ctx context.Context) {
	r.once.Do(func() {
		close(r.stop)
		r.mu.Lock()
		for _, j := range r.jobs {
			j.cancel(errShutdown)
		}
		r.mu.Unlock()
	})
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		r.o.Log.Warn("workers did not stop in time")
	}
	if r.cancel != nil {
		r.cancel()
	}
}

// Wake makes idle workers look for jobs now.
func (r *Runner) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Cancel interrupts a running job unless it has started publishing.
func (r *Runner) Cancel(jobID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[jobID]
	if !ok {
		return service.ErrJobNotRunning
	}
	if j.committing {
		return service.ErrJobCommitting
	}
	j.cancel(errUserCancel)
	return nil
}

func (r *Runner) worker(base context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.o.IdlePoll)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		job, err := r.o.Store.ClaimNextUpload(time.Now(), r.o.Serialized)
		if err != nil {
			r.o.Log.Error("claim upload", "err", err)
		}
		if job != nil {
			r.run(base, *job)
			continue
		}
		select {
		case <-r.stop:
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

// attemptState tracks the latest persisted phase and resume data across retries.
type attemptState struct {
	mu        sync.Mutex
	phase     string
	resume    json.RawMessage
	bytes     int64
	lastWrite time.Time
}

func (r *Runner) run(base context.Context, job store.Upload) {
	ctx, cancel := context.WithCancelCause(base)
	defer cancel(nil)
	ctl := &jobCtl{cancel: cancel, committing: provider.PhaseAtLeast(job.Phase, provider.PhaseCommitting)}
	r.mu.Lock()
	r.jobs[job.ID] = ctl
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.jobs, job.ID)
		r.mu.Unlock()
	}()

	st := &attemptState{phase: job.Phase, resume: job.ResumeState, bytes: job.BytesSent}
	res, owner, err := r.attempt(ctx, job, ctl, st)
	r.finish(ctx, job, st, res, owner, err)
}

// tokenOwner records which row a reauth failure should mark.
type tokenOwner struct {
	accountID    string
	credentialID string
	accountOwned bool
}

func (r *Runner) attempt(ctx context.Context, job store.Upload, ctl *jobCtl, st *attemptState) (provider.Result, tokenOwner, error) {
	var owner tokenOwner
	acc, err := r.o.Store.GetAccount(job.AccountID)
	if err != nil {
		return provider.Result{}, owner, provider.Permanent("account %s no longer exists", job.AccountID)
	}
	owner.accountID, owner.credentialID = acc.ID, acc.CredentialID
	p, ok := r.o.Registry.Get(acc.Platform)
	if !ok {
		return provider.Result{}, owner, provider.Permanent("platform %s is not configured", acc.Platform)
	}
	var meta provider.VideoMeta
	if err := json.Unmarshal(job.Request, &meta); err != nil {
		return provider.Result{}, owner, provider.Permanent("stored request is invalid: %v", err)
	}
	f, err := videofile.Open(videofile.Info{Path: job.FilePath, Size: job.FileSize, ModTime: job.FileMtime, Fingerprint: job.FileFingerprint, Container: job.Container}, r.o.AllowedDirs)
	if err != nil {
		return provider.Result{}, owner, err
	}
	defer f.Close()

	lease, err := r.o.Tokens.ForAccount(acc)
	if err != nil {
		return provider.Result{}, owner, err
	}
	owner.accountOwned = lease.AccountOwned
	if lease, err = r.o.Tokens.EnsureFresh(ctx, lease, 5*time.Minute); err != nil {
		return provider.Result{}, owner, err
	}

	progress := func(u provider.ProgressUpdate) error { return r.progress(job.ID, ctl, st, u) }
	build := func(tok provider.Token) provider.UploadRequest {
		st.mu.Lock()
		defer st.mu.Unlock()
		return provider.UploadRequest{
			AccountExternalID: acc.ExternalID, AccountMeta: acc.Meta, Token: tok, File: f, FileSize: job.FileSize,
			Container: job.Container, Meta: meta, Phase: st.phase, Resume: st.resume,
		}
	}
	res, err := p.Upload(ctx, build(lease.Token), progress)
	if errors.Is(err, provider.ErrUnauthorized) && ctx.Err() == nil {
		if lease.AccountOwned || !lease.Refreshable {
			return res, owner, fmt.Errorf("%w: %v", provider.ErrReauthRequired, err)
		}
		if lease, err = r.o.Tokens.Refresh(ctx, lease); err != nil {
			return res, owner, err
		}
		res, err = p.Upload(ctx, build(lease.Token), progress)
		if errors.Is(err, provider.ErrUnauthorized) {
			err = fmt.Errorf("%w: %v", provider.ErrReauthRequired, err)
		}
	}
	return res, owner, err
}

// progress persists phase changes synchronously and byte counts at most every ProgressEvery.
func (r *Runner) progress(jobID string, ctl *jobCtl, st *attemptState, u provider.ProgressUpdate) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	phaseChanged := u.Phase != "" && u.Phase != st.phase
	if phaseChanged && provider.PhaseAtLeast(u.Phase, provider.PhaseCommitting) {
		r.mu.Lock()
		ctl.committing = true
		r.mu.Unlock()
	}
	if u.Phase != "" {
		st.phase = u.Phase
	}
	if u.Resume != nil {
		st.resume = u.Resume
	}
	st.bytes = u.BytesSent
	if !phaseChanged && time.Since(st.lastWrite) < r.o.ProgressEvery {
		return nil
	}
	phase, resume, bytes := st.phase, st.resume, st.bytes
	f := store.UploadFields{Phase: &phase, BytesSent: &bytes}
	if resume != nil {
		f.ResumeState = &resume
	}
	var err error
	for i := 0; i < 5; i++ {
		if _, err = r.o.Store.TransitionUpload(jobID, store.StatusUploading, store.StatusUploading, f); err == nil {
			st.lastWrite = time.Now()
			return nil
		}
		time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
	}
	if phaseChanged {
		return fmt.Errorf("persist upload phase: %w", err)
	}
	r.o.Log.Warn("persist upload progress", "job", jobID, "err", err)
	return nil
}

func (r *Runner) finish(ctx context.Context, job store.Upload, st *attemptState, res provider.Result, owner tokenOwner, err error) {
	st.mu.Lock()
	phase, resume, bytes := st.phase, st.resume, st.bytes
	st.mu.Unlock()
	f := store.UploadFields{Phase: &phase, BytesSent: &bytes}
	if resume != nil {
		f.ResumeState = &resume
	}
	to := store.StatusFailed
	log := r.o.Log.With("job", job.ID, "platform", job.Platform)

	if err != nil && ctx.Err() != nil {
		switch cause := context.Cause(ctx); {
		case errors.Is(cause, errShutdown):
			// Leave the job uploading; the next start resumes it from the persisted phase.
			r.write(job.ID, store.StatusUploading, f)
			return
		case errors.Is(cause, errUserCancel):
			note := "cancelled by user"
			f.Note = &note
			r.write(job.ID, store.StatusCancelled, f)
			return
		}
	}

	var rl *provider.RateLimitError
	switch {
	case err == nil:
		to, phase, bytes = store.StatusSucceeded, provider.PhaseDone, job.FileSize
		f.RemoteID, f.RemoteURL, f.Outcome, f.Note = &res.RemoteID, &res.URL, &res.Outcome, &res.Note
		empty := ""
		f.Error = &empty
		log.Info("upload finished", "outcome", res.Outcome, "remote_id", res.RemoteID)
	case errors.Is(err, provider.ErrReauthRequired):
		r.markReauth(owner)
		msg := logx.Error(err) + "; run `social-mcp account reauth " + job.AccountID + "` and upload again"
		f.Error = &msg
	case errors.Is(err, provider.ErrOutcomeUnknown):
		to = store.StatusNeedsReview
		msg := logx.Error(err)
		note := "The upload may or may not have been published. Check the platform before uploading again."
		f.Error, f.Note = &msg, &note
	case errors.As(err, &rl):
		to = store.StatusQueued
		delay := rl.RetryAfter
		if delay <= 0 {
			delay = defaultRateDelay
		}
		nb := time.Now().Add(delay)
		msg := logx.Error(err)
		f.NotBefore, f.Error = &nb, &msg
	case provider.IsPermanent(err):
		msg := logx.Error(err)
		f.Error = &msg
	default:
		attempts := job.Attempts + 1
		msg := logx.Error(err)
		f.Attempts, f.Error = &attempts, &msg
		if attempts < maxAttempts {
			to = store.StatusQueued
			nb := time.Now().Add(r.o.Backoff[min(attempts-1, len(r.o.Backoff)-1)])
			f.NotBefore = &nb
		}
	}
	if err != nil {
		log.Warn("upload attempt failed", "next_status", to, "err", err)
	}
	r.write(job.ID, to, f)
	if to == store.StatusQueued {
		r.Wake()
	}
}

func (r *Runner) write(jobID, to string, f store.UploadFields) {
	for i := 0; i < 5; i++ {
		ok, err := r.o.Store.TransitionUpload(jobID, store.StatusUploading, to, f)
		if err == nil {
			if !ok {
				r.o.Log.Warn("upload changed state concurrently", "job", jobID)
			}
			return
		}
		r.o.Log.Error("record upload result", "job", jobID, "err", err)
		time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
	}
}

func (r *Runner) markReauth(o tokenOwner) {
	var err error
	if o.accountOwned {
		err = r.o.Store.SetAccountStatus(o.accountID, store.StatusNeedsReauth)
	} else if o.credentialID != "" {
		err = r.o.Store.SetCredentialStatus(o.credentialID, store.StatusNeedsReauth)
	}
	if err != nil {
		r.o.Log.Error("mark needs_reauth", "account", o.accountID, "err", err)
	}
}
