package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/enginetest"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/providertest"
	"github.com/nhatnam1507/social-mcp/internal/secrets"
	"github.com/nhatnam1507/social-mcp/internal/store"
	"github.com/nhatnam1507/social-mcp/internal/tokens"
	"github.com/nhatnam1507/social-mcp/internal/videofile"
)

type memKeyring struct {
	mu   sync.Mutex
	data map[string]string
}

func (m *memKeyring) Get(s, u string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[s+u]
	if !ok {
		return "", secrets.ErrKeyringNotFound
	}
	return v, nil
}

func (m *memKeyring) Set(s, u, v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[s+u] = v
	return nil
}

type testDaemon struct {
	home string
	kr   *memKeyring
	reg  provider.Registry
}

func newTestDaemon(t *testing.T) *testDaemon {
	home, err := os.MkdirTemp("", "smd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	return &testDaemon{home: home, kr: &memKeyring{data: map[string]string{}}, reg: provider.Registry{"youtube": providertest.New("youtube")}}
}

// start runs the daemon in the background and waits until it serves.
func (d *testDaemon) start(t *testing.T) (Info, <-chan error) {
	t.Helper()
	ready := make(chan Info, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{
			Home: d.home, Version: "test", Ephemeral: true, Keyring: d.kr, Registry: d.reg,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Ready: func(i Info) { ready <- i },
		})
	}()
	select {
	case info := <-ready:
		return info, done
	case err := <-done:
		t.Fatalf("daemon exited: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not start")
	}
	return Info{}, done
}

func (d *testDaemon) stop(t *testing.T, done <-chan error) {
	t.Helper()
	if err := control.NewClient(paths.SocketPath(d.home)).Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestRunServesAndStops(t *testing.T) {
	d := newTestDaemon(t)
	info, done := d.start(t)
	c := control.NewClient(info.Socket)
	st, err := c.Status()
	if err != nil || st.KeyBackend != secrets.BackendKeyring || st.Platforms[0] != "youtube" {
		t.Fatalf("status %+v %v", st, err)
	}
	if _, err := os.Stat(paths.MCPToken(d.home)); err != nil {
		t.Fatalf("mcp token not created: %v", err)
	}
	if !Running(d.home) {
		t.Fatal("lock not held while running")
	}
	d.stop(t, done)
	if Running(d.home) {
		t.Fatal("lock still held after stop")
	}
	if _, err := os.Stat(info.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("socket not removed")
	}
}

func TestConcurrentRunsOnlyOneServes(t *testing.T) {
	d := newTestDaemon(t)
	results := make(chan error, 2)
	ready := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- Run(context.Background(), Options{Home: d.home, Ephemeral: true, Keyring: d.kr, Registry: d.reg,
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Ready: func(Info) { ready <- struct{}{} }})
		}()
	}
	select {
	case err := <-results:
		if !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("loser returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no daemon reported already running")
	}
	<-ready
	d.stop(t, results)
}

func TestStaleSocketReplaced(t *testing.T) {
	d := newTestDaemon(t)
	if err := os.WriteFile(paths.SocketPath(d.home), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, done := d.start(t)
	d.stop(t, done)
}

// seedJob creates an account and a job for a daemon home, sealing with the home's key.
func seedJob(t *testing.T, home string, kr secrets.Keyring, status string) string {
	t.Helper()
	st, err := store.Open(paths.DB(home))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key, err := secrets.LoadKey(home, kr, st)
	if err != nil {
		t.Fatal(err)
	}
	sealer, _ := secrets.NewSealer(key.Bytes)
	tm := tokens.NewManager(st, sealer, nil, slog.Default())
	sealed, _ := tm.Seal(provider.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)})
	_, accs, err := st.UpsertLogin(store.Credential{Platform: "youtube", Subject: "s", SealedToken: sealed, Refreshable: true},
		[]store.Account{{Platform: "youtube", ExternalID: "ch1", DisplayName: "Ch"}})
	if err != nil {
		t.Fatal(err)
	}
	file := enginetest.WriteMP4(t, filepath.Join(home, "v.mp4"), 2048)
	info, err := videofile.Check(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.InsertUpload(store.Upload{AccountID: accs[0].ID, Platform: "youtube", FilePath: info.Path, FileSize: info.Size, FileMtime: info.ModTime,
		FileFingerprint: info.Fingerprint, Container: info.Container, Request: []byte(`{"title":"T","privacy":"private"}`), Status: store.StatusQueued, Phase: store.PhasePending})
	if err != nil {
		t.Fatal(err)
	}
	if status != store.StatusQueued {
		st.TransitionUpload(u.ID, store.StatusQueued, status, store.UploadFields{})
	}
	return u.ID
}

func TestShutdownLeavesUploadResumable(t *testing.T) {
	d := newTestDaemon(t)
	_, done := d.start(t) // initialise the home and its key
	d.stop(t, done)
	jobID := seedJob(t, d.home, d.kr, store.StatusQueued)

	started := make(chan struct{}, 1)
	fake := d.reg["youtube"].(*providertest.Fake)
	fake.UploadFunc = func(ctx context.Context, call int, req provider.UploadRequest, p provider.Progress) (provider.Result, error) {
		p(provider.ProgressUpdate{BytesSent: 100, Phase: provider.PhaseTransferring})
		started <- struct{}{}
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	}
	info, done := d.start(t)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("upload never started")
	}
	d.stop(t, done)
	st, _ := store.Open(paths.DB(d.home))
	defer st.Close()
	u, _ := st.GetUpload(jobID)
	if u.Status != store.StatusUploading || u.Phase != store.PhaseTransferring || u.Attempts != 0 {
		t.Fatalf("after shutdown: status=%s phase=%s attempts=%d (socket %s)", u.Status, u.Phase, u.Attempts, info.Socket)
	}
}

func TestPortInUseFailsWithoutTouchingJobs(t *testing.T) {
	d := newTestDaemon(t)
	jobID := seedJob(t, d.home, d.kr, store.StatusUploading)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	os.WriteFile(paths.Config(d.home), []byte(fmt.Sprintf("[server]\nlisten_port = %d\n", port)), 0o600)

	err = Run(context.Background(), Options{Home: d.home, Keyring: d.kr, Registry: d.reg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(port)) {
		t.Fatalf("Run with busy port = %v", err)
	}
	st, _ := store.Open(paths.DB(d.home))
	defer st.Close()
	if u, _ := st.GetUpload(jobID); u.Status != store.StatusUploading {
		t.Fatalf("failed start changed job status to %s", u.Status)
	}
	if Running(d.home) {
		t.Fatal("lock left held after failed start")
	}
}

func TestCallbackHandler(t *testing.T) {
	env := enginetest.New(t, "youtube")
	svc := env.Local(nil, nil)
	h := callbackHandler(svc)
	get := func(q string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/oauth/callback?"+q, nil))
		return rec
	}
	if rec := get("code=x"); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing state: %d", rec.Code)
	}
	if rec := get("state=unknown&code=x"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("unknown state: %d %s", rec.Code, rec.Body.String())
	}

	start, _ := svc.BeginAuth("youtube", "")
	sess, _ := env.Sessions.Get(start.SessionID)
	rec := get("state=" + sess.State + "&error=access_denied&error_description=user+said+no")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "access_denied") {
		t.Fatalf("denied: %d %s", rec.Code, rec.Body.String())
	}
	if v, _ := svc.AuthSession(start.SessionID); v.State != "failed" {
		t.Fatalf("session state %s", v.State)
	}

	env.Fake("youtube").IDs = []provider.Identity{{ExternalID: "ch1", DisplayName: "<b>Chan</b>"}}
	start, _ = svc.BeginAuth("youtube", "")
	sess, _ = env.Sessions.Get(start.SessionID)
	rec = get("state=" + sess.State + "&code=abc")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "&lt;b&gt;Chan&lt;/b&gt;") {
		t.Fatalf("success: %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("state=" + sess.State + "&code=abc"); rec.Code != http.StatusBadRequest {
		t.Fatalf("state reuse: %d", rec.Code)
	}

	env.Fake("youtube").ExchangeErr = errors.New("bad request client_secret=LEAKME")
	start, _ = svc.BeginAuth("youtube", "")
	sess, _ = env.Sessions.Get(start.SessionID)
	if rec := get("state=" + sess.State + "&code=abc"); strings.Contains(rec.Body.String(), "LEAKME") {
		t.Fatalf("secret on callback page: %s", rec.Body.String())
	}
}
