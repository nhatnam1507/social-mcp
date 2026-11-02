package control

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/service"
)

type fakeService struct{ lastInput service.UploadInput }

func (f *fakeService) Status() (service.StatusView, error) {
	return service.StatusView{Version: "v1", PID: 42}, nil
}
func (f *fakeService) ListAccounts(platform string) ([]service.AccountView, error) {
	return []service.AccountView{{ID: "acc_1", Platform: platform}}, nil
}
func (f *fakeService) RemoveAccount(id string) error {
	if id == "missing" {
		return service.ErrNotFound
	}
	return nil
}
func (f *fakeService) BeginAuth(platform, reauth string) (service.AuthStart, error) {
	if platform == "nope" {
		return service.AuthStart{}, service.Invalid("platform %q is not configured", platform)
	}
	return service.AuthStart{SessionID: "auth_1", AuthURL: "https://x/" + platform + "/" + reauth}, nil
}
func (f *fakeService) AuthSession(id string) (service.AuthSessionView, error) {
	return service.AuthSessionView{ID: id, State: "pending"}, nil
}
func (f *fakeService) SubmitUpload(in service.UploadInput) (service.UploadSubmission, error) {
	f.lastInput = in
	return service.UploadSubmission{Jobs: []service.JobRef{{JobID: "job_1"}}, Rejected: []service.Rejection{}}, nil
}
func (f *fakeService) GetUpload(id string) (service.JobView, error) {
	if id == "boom" {
		return service.JobView{}, errors.New("db failed: access_token=SECRETX")
	}
	return service.JobView{ID: id}, nil
}
func (f *fakeService) ListUploads(fl service.UploadFilter) ([]service.JobView, error) {
	return []service.JobView{{ID: fl.Status + "|" + fl.AccountID}}, nil
}
func (f *fakeService) CancelUpload(id string) (service.JobView, error) {
	return service.JobView{ID: id, Status: "cancelled"}, nil
}

func TestRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "sm")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "c.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeService{}
	shut := make(chan struct{})
	srv := &http.Server{Handler: NewHandler(fake, func() { close(shut) })}
	go srv.Serve(ln)
	defer srv.Close()
	c := NewClient(sock)

	if st, err := c.Status(); err != nil || st.PID != 42 {
		t.Fatalf("status %+v %v", st, err)
	}
	if accs, err := c.ListAccounts("youtube"); err != nil || accs[0].Platform != "youtube" {
		t.Fatalf("accounts %+v %v", accs, err)
	}
	if err := c.RemoveAccount("missing"); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("remove missing: %v", err)
	}
	if _, err := c.BeginAuth("nope", ""); !service.IsValidation(err) {
		t.Fatalf("validation not mapped: %v", err)
	}
	if s, err := c.BeginAuth("tiktok", "acc_1"); err != nil || s.AuthURL != "https://x/tiktok/acc_1" {
		t.Fatalf("begin auth %+v %v", s, err)
	}
	if v, err := c.AuthSession("auth_1"); err != nil || v.State != "pending" {
		t.Fatalf("session %+v %v", v, err)
	}
	in := service.UploadInput{FilePath: "/v.mp4", AccountIDs: []string{"a"}, PlatformOptions: map[string]map[string]any{"youtube": {"made_for_kids": true}}}
	if sub, err := c.SubmitUpload(in); err != nil || sub.Jobs[0].JobID != "job_1" || fake.lastInput.PlatformOptions["youtube"]["made_for_kids"] != true {
		t.Fatalf("submit %+v %v %+v", sub, err, fake.lastInput)
	}
	if _, err := c.GetUpload("boom"); err == nil || strings.Contains(err.Error(), "SECRETX") {
		t.Fatalf("internal error leaked or missing: %v", err)
	}
	if jobs, err := c.ListUploads(service.UploadFilter{Status: "queued", AccountID: "a", Limit: 5}); err != nil || jobs[0].ID != "queued|a" {
		t.Fatalf("list %+v %v", jobs, err)
	}
	if j, err := c.CancelUpload("job_1"); err != nil || j.Status != "cancelled" {
		t.Fatalf("cancel %+v %v", j, err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-shut:
	case <-time.After(time.Second):
		t.Fatal("shutdown not called")
	}
}

func TestClientReportsDaemonNotRunning(t *testing.T) {
	c := NewClient(filepath.Join(t.TempDir(), "none.sock"))
	if _, err := c.Status(); !errors.Is(err, ErrDaemonNotRunning) {
		t.Fatalf("err = %v", err)
	}
}
