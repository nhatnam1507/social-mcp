package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nhatnam1507/social-mcp/internal/enginetest"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/service"
	"github.com/nhatnam1507/social-mcp/internal/store"
)

type fakeJobs struct {
	woke      int
	cancelErr error
}

func (f *fakeJobs) Wake()                     { f.woke++ }
func (f *fakeJobs) Cancel(jobID string) error { return f.cancelErr }

func TestSubmitUploadDefaultsToPrivate(t *testing.T) {
	env := enginetest.New(t, "youtube")
	_, acc := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	jobs := &fakeJobs{}
	svc := env.Local(jobs, nil)
	sub, err := svc.SubmitUpload(service.UploadInput{FilePath: env.VideoFile("v.mp4", 100), AccountIDs: []string{acc.ID}, Title: "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Jobs) != 1 || jobs.woke != 1 {
		t.Fatalf("jobs = %+v woke = %d", sub.Jobs, jobs.woke)
	}
	job, _ := svc.GetUpload(sub.Jobs[0].JobID)
	if job.Privacy != provider.PrivacyPrivate || job.Status != store.StatusQueued {
		t.Fatalf("job = %+v", job)
	}
}

func TestSubmitUploadRejections(t *testing.T) {
	env := enginetest.New(t, "youtube", "instagram")
	env.Fake("instagram").Caps.PrivacyLevels = []string{provider.PrivacyPublic}
	_, yt := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	_, ig := env.AddAccount(enginetest.AccountSpec{Platform: "instagram", ExternalID: "ig1"})
	svc := env.Local(&fakeJobs{}, nil)
	file := env.VideoFile("v.mp4", 100)

	sub, err := svc.SubmitUpload(service.UploadInput{FilePath: file, AccountIDs: []string{yt.ID, ig.ID, "acc_unknown"}, Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Jobs) != 1 || sub.Jobs[0].AccountID != yt.ID {
		t.Fatalf("jobs = %+v", sub.Jobs)
	}
	if len(sub.Rejected) != 2 || !strings.Contains(sub.Rejected[0].Reason, "privacy") || !strings.Contains(sub.Rejected[1].Reason, "unknown") {
		t.Fatalf("rejected = %+v", sub.Rejected)
	}

	_, err = svc.SubmitUpload(service.UploadInput{FilePath: file, AccountIDs: []string{ig.ID}, Title: "T"})
	if !service.IsValidation(err) {
		t.Fatalf("all-rejected should be a validation error, got %v", err)
	}
}

func TestSubmitUploadOptionChecks(t *testing.T) {
	env := enginetest.New(t, "youtube", "tiktok")
	_, yt := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	svc := env.Local(&fakeJobs{}, nil)
	file := env.VideoFile("v.mp4", 100)
	cases := []struct {
		name string
		opts map[string]map[string]any
	}{
		{"unknown key", map[string]map[string]any{"youtube": {"privacyStatus": "public"}}},
		{"wrong kind", map[string]map[string]any{"youtube": {"made_for_kids": "yes"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SubmitUpload(service.UploadInput{FilePath: file, AccountIDs: []string{yt.ID}, Title: "T", PlatformOptions: tc.opts})
			if !service.IsValidation(err) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
	_, err := svc.SubmitUpload(service.UploadInput{FilePath: file, AccountIDs: []string{yt.ID}, Title: "T",
		PlatformOptions: map[string]map[string]any{"tiktok": {}}})
	if !service.IsValidation(err) || !strings.Contains(err.Error(), "tiktok") {
		t.Fatalf("options for unselected platform: %v", err)
	}
	sub, err := svc.SubmitUpload(service.UploadInput{FilePath: file, AccountIDs: []string{yt.ID}, Title: "T",
		PlatformOptions: map[string]map[string]any{"youtube": {"made_for_kids": true, "category_id": "10"}}})
	if err != nil || len(sub.Jobs) != 1 {
		t.Fatalf("valid options rejected: %v", err)
	}
}

func TestSubmitUploadFileChecks(t *testing.T) {
	env := enginetest.New(t, "youtube")
	_, yt := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	svc := env.Local(&fakeJobs{}, []string{t.TempDir()})
	_, err := svc.SubmitUpload(service.UploadInput{FilePath: env.VideoFile("v.mp4", 100), AccountIDs: []string{yt.ID}, Title: "T"})
	if !service.IsValidation(err) || !strings.Contains(err.Error(), "allowed_dirs") {
		t.Fatalf("file outside allowed dirs: %v", err)
	}
}

func TestSubmitUploadRejectsNeedsReauth(t *testing.T) {
	env := enginetest.New(t, "youtube")
	cred, yt := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	env.Store.SetCredentialStatus(cred.ID, store.StatusNeedsReauth)
	svc := env.Local(&fakeJobs{}, nil)
	_, err := svc.SubmitUpload(service.UploadInput{FilePath: env.VideoFile("v.mp4", 100), AccountIDs: []string{yt.ID}, Title: "T"})
	if !service.IsValidation(err) || !strings.Contains(err.Error(), "reauth") {
		t.Fatalf("needs_reauth account accepted: %v", err)
	}
}

func TestRemoveAccountRefusedWithActiveJobs(t *testing.T) {
	env := enginetest.New(t, "youtube")
	_, yt := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	svc := env.Local(&fakeJobs{}, nil)
	if _, err := svc.SubmitUpload(service.UploadInput{FilePath: env.VideoFile("v.mp4", 100), AccountIDs: []string{yt.ID}, Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveAccount(yt.ID); !service.IsValidation(err) {
		t.Fatalf("remove with active job: %v", err)
	}
	if err := svc.RemoveAccount("acc_missing"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("remove unknown: %v", err)
	}
}

func TestCancelUpload(t *testing.T) {
	env := enginetest.New(t, "youtube")
	_, yt := env.AddAccount(enginetest.AccountSpec{Platform: "youtube", ExternalID: "ch1", Refreshable: true})
	jobs := &fakeJobs{cancelErr: service.ErrJobCommitting}
	svc := env.Local(jobs, nil)
	sub, _ := svc.SubmitUpload(service.UploadInput{FilePath: env.VideoFile("v.mp4", 100), AccountIDs: []string{yt.ID}, Title: "T"})
	id := sub.Jobs[0].JobID

	got, err := svc.CancelUpload(id)
	if err != nil || got.Status != store.StatusCancelled {
		t.Fatalf("cancel queued: %+v %v", got, err)
	}
	if _, err := svc.CancelUpload(id); !service.IsValidation(err) {
		t.Fatalf("cancel finished job: %v", err)
	}

	sub, _ = svc.SubmitUpload(service.UploadInput{FilePath: env.VideoFile("w.mp4", 100), AccountIDs: []string{yt.ID}, Title: "T"})
	env.Store.TransitionUpload(sub.Jobs[0].JobID, store.StatusQueued, store.StatusUploading, store.UploadFields{})
	if _, err := svc.CancelUpload(sub.Jobs[0].JobID); !service.IsValidation(err) || !strings.Contains(err.Error(), "published") {
		t.Fatalf("cancel during commit: %v", err)
	}
}

func TestAuthFlow(t *testing.T) {
	env := enginetest.New(t, "youtube")
	fake := env.Fake("youtube")
	fake.IDs = []provider.Identity{{ExternalID: "ch1", DisplayName: "Channel 1"}, {ExternalID: "ch2", DisplayName: "Channel 2"}}
	svc := env.Local(&fakeJobs{}, nil)

	start, err := svc.BeginAuth("youtube", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(start.AuthURL, "127.0.0.1%3A8765") && !strings.Contains(start.AuthURL, "127.0.0.1:8765") {
		t.Fatalf("auth url lacks redirect: %s", start.AuthURL)
	}
	sess, _ := env.Sessions.Get(start.SessionID)
	view, err := svc.CompleteAuth(context.Background(), sess.State, "code1", "")
	if err != nil || view.State != "succeeded" || len(view.Accounts) != 2 {
		t.Fatalf("complete: %+v %v", view, err)
	}
	accs, _ := svc.ListAccounts("youtube")
	if len(accs) != 2 || accs[0].Health != service.HealthOK {
		t.Fatalf("accounts = %+v", accs)
	}

	// Re-authorizing ch1 with a login that no longer includes it must fail without writing.
	start, _ = svc.BeginAuth("youtube", accs[0].ID)
	sess, _ = env.Sessions.Get(start.SessionID)
	fake.IDs = []provider.Identity{{ExternalID: "other", DisplayName: "Other"}}
	view, err = svc.CompleteAuth(context.Background(), sess.State, "code2", "")
	if err == nil || view.State != "failed" {
		t.Fatalf("reauth with wrong identity: %+v %v", view, err)
	}
	if accs2, _ := svc.ListAccounts("youtube"); len(accs2) != 2 {
		t.Fatalf("wrong-identity reauth wrote accounts: %d", len(accs2))
	}

	// Provider-reported errors are recorded and sanitized.
	start, _ = svc.BeginAuth("youtube", "")
	sess, _ = env.Sessions.Get(start.SessionID)
	fake.ExchangeErr = errors.New("token endpoint said client_secret=TOPSECRET is bad")
	view, _ = svc.CompleteAuth(context.Background(), sess.State, "code3", "")
	if view.State != "failed" || strings.Contains(view.Error, "TOPSECRET") {
		t.Fatalf("exchange failure view = %+v", view)
	}
	if _, err := svc.CompleteAuth(context.Background(), sess.State, "code3", ""); err == nil {
		t.Fatal("state reuse accepted")
	}
}
