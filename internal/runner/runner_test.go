package runner_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/enginetest"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/runner"
	"github.com/nhatnam1507/social-mcp/internal/service"
	"github.com/nhatnam1507/social-mcp/internal/store"
)

type harness struct {
	files atomic.Int32
	env   *enginetest.Env
	svc   *service.Local
	run   *runner.Runner
}

func newHarness(t *testing.T, platforms ...string) *harness {
	env := enginetest.New(t, platforms...)
	h := &harness{env: env}
	h.run = h.newRunner()
	h.svc = env.Local(h.run, nil)
	return h
}

func (h *harness) newRunner() *runner.Runner {
	return runner.New(runner.Options{
		Store: h.env.Store, Tokens: h.env.Tokens, Registry: h.env.Registry, Workers: 2,
		Serialized: []string{"tiktok", "instagram"}, Backoff: []time.Duration{10 * time.Millisecond, 10 * time.Millisecond},
		ProgressEvery: time.Millisecond, IdlePoll: 10 * time.Millisecond, Log: h.env.Log,
	})
}

func (h *harness) start(t *testing.T) {
	t.Helper()
	if err := h.run.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h.run.Stop(ctx)
	})
}

func (h *harness) submit(t *testing.T, accountIDs ...string) []string {
	t.Helper()
	sub, err := h.svc.SubmitUpload(service.UploadInput{FilePath: h.env.VideoFile(fmt.Sprintf("v%d.mp4", h.files.Add(1)), 4096), AccountIDs: accountIDs, Title: "T", Privacy: provider.PrivacyPublic})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, j := range sub.Jobs {
		out = append(out, j.JobID)
	}
	return out
}

func waitStatus(t *testing.T, st *store.Store, id string, want ...string) store.Upload {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		u, err := st.GetUpload(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if u.Status == w {
				return u
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	u, _ := st.GetUpload(id)
	t.Fatalf("job %s status = %s (error %q), want %v", id, u.Status, u.Error, want)
	return u
}

func TestSuccessPersistsResult(t *testing.T) {
	h := newHarness(t, "youtube")
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusSucceeded)
	if u.RemoteID != "remote-1" || u.Outcome != provider.OutcomePublished || u.Phase != store.PhaseDone || u.BytesSent != u.FileSize {
		t.Fatalf("result not persisted: %+v", u)
	}
}

func TestTransientRetriedThenSucceeds(t *testing.T) {
	h := newHarness(t, "youtube")
	h.env.Fake("youtube").UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if call == 1 {
			return provider.Result{}, errors.New("connection reset")
		}
		return provider.Result{RemoteID: "r2", Outcome: provider.OutcomePublished}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusSucceeded)
	if u.Attempts != 1 || u.RemoteID != "r2" {
		t.Fatalf("attempts=%d remote=%s", u.Attempts, u.RemoteID)
	}
}

func TestTransientExhaustsAttempts(t *testing.T) {
	h := newHarness(t, "youtube")
	h.env.Fake("youtube").UploadFunc = func(context.Context, int, provider.UploadRequest, provider.Progress) (provider.Result, error) {
		return provider.Result{}, errors.New("503 from server")
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusFailed)
	if u.Attempts != 3 || len(h.env.Fake("youtube").Calls()) != 3 {
		t.Fatalf("attempts=%d calls=%d", u.Attempts, len(h.env.Fake("youtube").Calls()))
	}
}

func TestRateLimitDoesNotConsumeAttempts(t *testing.T) {
	h := newHarness(t, "youtube")
	h.env.Fake("youtube").UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if call <= 3 {
			return provider.Result{}, &provider.RateLimitError{RetryAfter: 5 * time.Millisecond, Msg: "slow down"}
		}
		return provider.Result{RemoteID: "ok", Outcome: provider.OutcomePublished}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusSucceeded)
	if u.Attempts != 0 {
		t.Fatalf("rate limits consumed attempts: %d", u.Attempts)
	}
}

func TestPermanentNotRetried(t *testing.T) {
	h := newHarness(t, "youtube")
	h.env.Fake("youtube").UploadFunc = func(context.Context, int, provider.UploadRequest, provider.Progress) (provider.Result, error) {
		return provider.Result{}, provider.Permanent("quotaExceeded")
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusFailed)
	if len(h.env.Fake("youtube").Calls()) != 1 || u.Error != "quotaExceeded" {
		t.Fatalf("calls=%d error=%q", len(h.env.Fake("youtube").Calls()), u.Error)
	}
}

func TestUnauthorizedRefreshesOnce(t *testing.T) {
	h := newHarness(t, "youtube")
	fake := h.env.Fake("youtube")
	fake.UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if req.Token.AccessToken == "access" {
			return provider.Result{}, provider.ErrUnauthorized
		}
		return provider.Result{RemoteID: "after-refresh", Outcome: provider.OutcomePublished}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusSucceeded)
	if fake.Refreshes() != 1 || u.RemoteID != "after-refresh" {
		t.Fatalf("refreshes=%d remote=%s", fake.Refreshes(), u.RemoteID)
	}
}

func TestReauthWithAccountTokenMarksOnlyAccount(t *testing.T) {
	h := newHarness(t, "facebook")
	fake := h.env.Fake("facebook")
	fake.CanRefresh = false
	fake.UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if req.Token.AccessToken == "page-a" {
			return provider.Result{}, provider.ErrUnauthorized
		}
		return provider.Result{RemoteID: "ok", Outcome: provider.OutcomePublished}, nil
	}
	cred, a := h.env.AddAccount(enginetest.AccountSpec{Platform: "facebook", ExternalID: "pageA", AccountToken: &provider.Token{AccessToken: "page-a"}})
	_, b := h.env.AddAccount(enginetest.AccountSpec{Platform: "facebook", ExternalID: "pageB", AccountToken: &provider.Token{AccessToken: "page-b"}})
	h.start(t)
	jobs := h.submit(t, a.ID, b.ID)
	waitStatus(t, h.env.Store, jobs[0], store.StatusFailed)
	waitStatus(t, h.env.Store, jobs[1], store.StatusSucceeded)
	ga, _ := h.env.Store.GetAccount(a.ID)
	gb, _ := h.env.Store.GetAccount(b.ID)
	gc, _ := h.env.Store.GetCredential(cred.ID)
	if ga.Status != store.StatusNeedsReauth || gb.Status != store.StatusOK || gc.Status != store.StatusOK || fake.Refreshes() != 0 {
		t.Fatalf("a=%s b=%s cred=%s refreshes=%d", ga.Status, gb.Status, gc.Status, fake.Refreshes())
	}
}

func TestOutcomeUnknownNeedsReview(t *testing.T) {
	h := newHarness(t, "instagram")
	h.env.Fake("instagram").UploadFunc = func(context.Context, int, provider.UploadRequest, provider.Progress) (provider.Result, error) {
		return provider.Result{}, provider.OutcomeUnknown("publish response lost")
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "instagram", ExternalID: "ig1"})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusNeedsReview)
	if len(h.env.Fake("instagram").Calls()) != 1 || u.Note == "" {
		t.Fatalf("calls=%d note=%q", len(h.env.Fake("instagram").Calls()), u.Note)
	}
}

func TestRetryAfterCommitResumesInCommittingPhase(t *testing.T) {
	h := newHarness(t, "youtube")
	fake := h.env.Fake("youtube")
	fake.UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if call == 1 {
			if err := p(provider.ProgressUpdate{BytesSent: 10, Phase: provider.PhaseCommitting, Resume: json.RawMessage(`{"session":"s1"}`)}); err != nil {
				return provider.Result{}, err
			}
			return provider.Result{}, errors.New("timeout after final chunk")
		}
		if req.Phase != provider.PhaseCommitting || string(req.Resume) != `{"session":"s1"}` {
			return provider.Result{}, provider.Permanent("retry did not receive committing state: %s %s", req.Phase, req.Resume)
		}
		return provider.Result{RemoteID: "reconciled", Outcome: provider.OutcomePublished}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusSucceeded, store.StatusFailed)
	if u.Status != store.StatusSucceeded || u.RemoteID != "reconciled" {
		t.Fatalf("status=%s error=%q", u.Status, u.Error)
	}
}

func TestUserCancelAndShutdown(t *testing.T) {
	h := newHarness(t, "youtube")
	fake := h.env.Fake("youtube")
	started := make(chan string, 4)
	fake.UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if err := p(provider.ProgressUpdate{BytesSent: 1, Phase: provider.PhaseTransferring, Resume: json.RawMessage(`{"offset":1}`)}); err != nil {
			return provider.Result{}, err
		}
		started <- req.AccountExternalID
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	}
	_, a := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	_, b := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch2", Refreshable: true})
	h.start(t)
	jobs := h.submit(t, a.ID, b.ID)
	<-started
	<-started

	got, err := h.svc.CancelUpload(jobs[0])
	if err != nil || got.Status != store.StatusCancelled {
		t.Fatalf("cancel: %+v %v", got, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.run.Stop(ctx)
	u, _ := h.env.Store.GetUpload(jobs[1])
	if u.Status != store.StatusUploading || u.Attempts != 0 || u.Phase != store.PhaseTransferring {
		t.Fatalf("shutdown changed job: status=%s attempts=%d phase=%s", u.Status, u.Attempts, u.Phase)
	}

	// A new runner resumes the interrupted job with its stored phase and resume state.
	var resumed atomic.Value
	fake.UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		resumed.Store(req.Phase + " " + string(req.Resume))
		return provider.Result{RemoteID: "resumed", Outcome: provider.OutcomePublished}, nil
	}
	h.run = h.newRunner()
	h.start(t)
	waitStatus(t, h.env.Store, jobs[1], store.StatusSucceeded)
	if resumed.Load() != `transferring {"offset":1}` {
		t.Fatalf("resume got %v", resumed.Load())
	}
}

func TestCancelRefusedWhileCommitting(t *testing.T) {
	h := newHarness(t, "youtube")
	committing := make(chan struct{})
	release := make(chan struct{})
	h.env.Fake("youtube").UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		p(provider.ProgressUpdate{Phase: provider.PhaseCommitting})
		close(committing)
		<-release
		return provider.Result{RemoteID: "x", Outcome: provider.OutcomePublished}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	id := h.submit(t, acc.ID)[0]
	<-committing
	if _, err := h.svc.CancelUpload(id); !service.IsValidation(err) {
		t.Fatalf("cancel during commit: %v", err)
	}
	close(release)
	waitStatus(t, h.env.Store, id, store.StatusSucceeded)
}

func TestSerializedPlatformRunsOneJobPerAccount(t *testing.T) {
	h := newHarness(t, "tiktok")
	var running, peak atomic.Int32
	h.env.Fake("tiktok").UploadFunc = func(context.Context, int, provider.UploadRequest, provider.Progress) (provider.Result, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		return provider.Result{Outcome: provider.OutcomeSentToDrafts}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "tiktok", ExternalID: "u1", Refreshable: true})
	ids := append(h.submit(t, acc.ID), h.submit(t, acc.ID)...)
	h.start(t)
	for _, id := range ids {
		waitStatus(t, h.env.Store, id, store.StatusSucceeded)
	}
	if peak.Load() != 1 {
		t.Fatalf("peak concurrency for one TikTok account = %d", peak.Load())
	}
}

// commitThen returns an upload func that reaches the publish step, then fails with err.
func commitThen(err error) func(context.Context, int, provider.UploadRequest, provider.Progress) (provider.Result, error) {
	return func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if call == 1 {
			if perr := p(provider.ProgressUpdate{Phase: provider.PhaseCommitting, Resume: json.RawMessage(`{"s":1}`)}); perr != nil {
				return provider.Result{}, perr
			}
		}
		return provider.Result{}, err
	}
}

func TestCommittedJobNeverEndsFailed(t *testing.T) {
	cases := map[string]error{
		"transient retries exhausted": errors.New("connection reset"),
		"permanent":                   provider.Permanent("quota"),
		"reauth":                      provider.ErrReauthRequired,
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "youtube")
			h.env.Fake("youtube").UploadFunc = commitThen(failure)
			_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
			h.start(t)
			u := waitStatus(t, h.env.Store, h.submit(t, acc.ID)[0], store.StatusNeedsReview, store.StatusFailed)
			if u.Status != store.StatusNeedsReview || strings.Contains(u.Error, "upload again") {
				t.Fatalf("status=%s error=%q", u.Status, u.Error)
			}
		})
	}
}

func TestCommittedJobReconcilesWithoutSourceFile(t *testing.T) {
	h := newHarness(t, "youtube")
	fake := h.env.Fake("youtube")
	var path string
	fake.UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if call == 1 {
			p(provider.ProgressUpdate{Phase: provider.PhaseCommitting})
			// The file changes after the final chunk was sent. (Rewriting instead of deleting,
			// since Windows refuses to delete a file the runner still has open.)
			if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
				f.WriteAt([]byte{0xFF, 0xFE}, 100)
				f.Close()
			}
			return provider.Result{}, errors.New("timeout")
		}
		if req.File != nil {
			return provider.Result{}, provider.Permanent("expected no file on reconcile")
		}
		return provider.Result{RemoteID: "found", Outcome: provider.OutcomePublished}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	path = h.env.VideoFile("changed.mp4", 4096)
	sub, err := h.svc.SubmitUpload(service.UploadInput{FilePath: path, AccountIDs: []string{acc.ID}, Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	u := waitStatus(t, h.env.Store, sub.Jobs[0].JobID, store.StatusSucceeded, store.StatusFailed, store.StatusNeedsReview)
	if u.Status != store.StatusSucceeded || u.RemoteID != "found" {
		t.Fatalf("status=%s error=%q", u.Status, u.Error)
	}
}

func TestCancelRefusedForRequeuedCommittingJob(t *testing.T) {
	h := newHarness(t, "youtube")
	h.env.Fake("youtube").UploadFunc = commitThen(&provider.RateLimitError{RetryAfter: time.Hour, Msg: "slow"})
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	h.start(t)
	id := h.submit(t, acc.ID)[0]
	u := waitStatus(t, h.env.Store, id, store.StatusQueued)
	for u.Phase != store.PhaseCommitting {
		time.Sleep(5 * time.Millisecond)
		u, _ = h.env.Store.GetUpload(id)
	}
	if _, err := h.svc.CancelUpload(id); !service.IsValidation(err) {
		t.Fatalf("cancel of requeued committing job: %v", err)
	}
	if u, _ := h.env.Store.GetUpload(id); u.Status != store.StatusQueued {
		t.Fatalf("status %s", u.Status)
	}
}

func TestRateLimitCapCountsFromFirstLimit(t *testing.T) {
	h := newHarness(t, "youtube")
	h.env.Fake("youtube").UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		if call == 1 {
			return provider.Result{}, &provider.RateLimitError{RetryAfter: time.Millisecond, Msg: "slow"}
		}
		return provider.Result{RemoteID: "ok", Outcome: provider.OutcomePublished}, nil
	}
	_, acc := h.env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	id := h.submit(t, acc.ID)[0]
	// The job has been queued for days, but this is its first rate limit: it must be retried.
	if _, err := h.env.Store.TransitionUpload(id, store.StatusQueued, store.StatusQueued, store.UploadFields{}); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if u := waitStatus(t, h.env.Store, id, store.StatusSucceeded, store.StatusFailed); u.Status != store.StatusSucceeded {
		t.Fatalf("status=%s error=%q", u.Status, u.Error)
	}
}
