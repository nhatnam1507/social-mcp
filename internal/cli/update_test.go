package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhatnam1507/social-mcp/internal/update"
)

// fakeReleases serves one latest tag and writes a fake binary on Download.
type fakeReleases struct {
	latest      string
	latestErr   error
	downloadErr error
	downloaded  string
}

func (f *fakeReleases) Latest(context.Context) (string, error) { return f.latest, f.latestErr }

func (f *fakeReleases) Download(_ context.Context, tag, dir string) (string, error) {
	if f.downloadErr != nil {
		return "", f.downloadErr
	}
	p := filepath.Join(dir, ".new-"+tag)
	if err := os.WriteFile(p, []byte("binary "+tag), 0o755); err != nil {
		return "", err
	}
	f.downloaded = tag
	return p, nil
}

// updateEnv fakes everything outside the command: the installed binary, the daemon and the release host.
type updateEnv struct {
	exe       string
	running   bool
	stopped   bool
	startedBy string
	startErr  error
	src       *fakeReleases
}

func newUpdateEnv(t *testing.T, src *fakeReleases) *updateEnv {
	t.Helper()
	e := &updateEnv{exe: filepath.Join(t.TempDir(), "social-mcp"), src: src}
	os.WriteFile(e.exe, []byte("binary old"), 0o755)
	h := t.TempDir()
	resolveHome = func() (string, error) { return h, nil }
	daemonRunning = func(string) bool { return e.running }
	newReleaseSource = func(string) releaseSource { return src }
	installedPath = func() (string, error) { return e.exe, nil }
	replaceBinary = update.Replace
	stopDaemon = func(string) (bool, error) { e.stopped = true; e.running = false; return false, nil }
	startDaemonWith = func(exe, _ string, out io.Writer) error {
		e.startedBy = exe
		fmt.Fprintln(out, "daemon started")
		return e.startErr
	}
	return e
}

func (e *updateEnv) run(t *testing.T, version string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot(version, &out, &out)
	root.SetArgs(append([]string{"update"}, args...))
	err := root.Execute()
	return out.String(), err
}

func (e *updateEnv) installed(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateInstallsLatestAndRestartsRunningDaemon(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{latest: "v0.2.0"})
	e.running = true
	out, err := e.run(t, "v0.1.0")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := e.installed(t); got != "binary v0.2.0" {
		t.Fatalf("installed %q", got)
	}
	if !e.stopped || e.startedBy != e.exe {
		t.Fatalf("stopped=%v startedBy=%q, want the daemon restarted through %s", e.stopped, e.startedBy, e.exe)
	}
	if !strings.Contains(out, "updated social-mcp v0.1.0 -> v0.2.0") || !strings.Contains(out, "daemon started") {
		t.Fatalf("output: %s", out)
	}
}

func TestUpdateLeavesStoppedDaemonStopped(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{latest: "v0.2.0"})
	if _, err := e.run(t, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	if e.stopped || e.startedBy != "" {
		t.Fatalf("stopped=%v startedBy=%q, want the daemon left alone", e.stopped, e.startedBy)
	}
}

func TestUpdateAlreadyCurrent(t *testing.T) {
	src := &fakeReleases{latest: "v0.2.0"}
	e := newUpdateEnv(t, src)
	out, err := e.run(t, "v0.2.0")
	if err != nil || !strings.Contains(out, "already up to date") || src.downloaded != "" {
		t.Fatalf("out=%q err=%v downloaded=%q", out, err, src.downloaded)
	}
}

func TestUpdateNoStableRelease(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{latestErr: update.ErrNoRelease})
	out, err := e.run(t, "v0.1.0")
	if err != nil || !strings.Contains(out, "no stable release") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestUpdateToChosenPrerelease(t *testing.T) {
	src := &fakeReleases{latest: "v0.2.0"}
	e := newUpdateEnv(t, src)
	if _, err := e.run(t, "v0.1.0", "--version", "v0.3.0-rc.1"); err != nil {
		t.Fatal(err)
	}
	if got := e.installed(t); got != "binary v0.3.0-rc.1" {
		t.Fatalf("installed %q", got)
	}
}

func TestUpdateRejectsNonReleaseVersionFlag(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{})
	if _, err := e.run(t, "v0.1.0", "--version", "main"); err == nil || !strings.Contains(err.Error(), "release tag") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateFromDevelopmentBuild(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{latest: "v0.2.0"})
	out, err := e.run(t, "dev")
	if err != nil || !strings.Contains(out, "development build") || e.installed(t) != "binary v0.2.0" {
		t.Fatalf("out=%q err=%v installed=%q", out, err, e.installed(t))
	}
}

func TestUpdateDownloadFailureChangesNothing(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{latest: "v0.2.0", downloadErr: errors.New("checksum mismatch")})
	e.running = true
	if _, err := e.run(t, "v0.1.0"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if e.installed(t) != "binary old" || e.stopped {
		t.Fatalf("installed=%q stopped=%v, want nothing touched", e.installed(t), e.stopped)
	}
}

func TestUpdateUnwritableFolderHint(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{latest: "v0.2.0", downloadErr: fmt.Errorf("create a file: %w", fs.ErrPermission)})
	_, err := e.run(t, "v0.1.0")
	if err == nil || !strings.Contains(err.Error(), "is not writable") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateReportsDaemonStartFailure(t *testing.T) {
	e := newUpdateEnv(t, &fakeReleases{latest: "v0.2.0"})
	e.running = true
	e.startErr = errors.New("port in use")
	_, err := e.run(t, "v0.1.0")
	if err == nil || !strings.Contains(err.Error(), "daemon start") || e.installed(t) != "binary v0.2.0" {
		t.Fatalf("err=%v installed=%q", err, e.installed(t))
	}
}
