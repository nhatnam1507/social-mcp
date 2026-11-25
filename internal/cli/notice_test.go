package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/service"
	"github.com/nhatnam1507/social-mcp/internal/update"
)

// noticeHome prepares a home whose daemon has recorded latest as the newest release.
func noticeHome(t *testing.T, latest string) string {
	t.Helper()
	h := t.TempDir()
	if err := update.SaveState(paths.UpdateCheck(h), update.State{CheckedAt: time.Now(), Latest: latest}); err != nil {
		t.Fatal(err)
	}
	resolveHome = func() (string, error) { return h, nil }
	dialService = func(string) service.Service { return &stubService{} }
	daemonRunning = func(string) bool { return false }
	return h
}

func runSplit(t *testing.T, version string, args ...string) (stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(version, &out, &errOut)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out.String(), errOut.String()
}

func TestUpdateNoticeShownOncePerReleaseOnStderr(t *testing.T) {
	h := noticeHome(t, "v0.2.0")
	stdout, stderr := runSplit(t, "v0.1.0", "account", "list", "--json")
	if !strings.Contains(stderr, "social-mcp v0.2.0 is available (you have v0.1.0)") || strings.Contains(stdout, "available") {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	if _, stderr = runSplit(t, "v0.1.0", "account", "list"); stderr != "" {
		t.Fatalf("second command repeated the notice: %q", stderr)
	}
	// A newer release is announced again.
	update.SaveState(paths.UpdateCheck(h), update.State{CheckedAt: time.Now(), Latest: "v0.3.0"})
	if _, stderr = runSplit(t, "v0.1.0", "account", "list"); !strings.Contains(stderr, "v0.3.0") {
		t.Fatalf("newer release not announced: %q", stderr)
	}
}

func TestUpdateNoticeSkippedCommands(t *testing.T) {
	noticeHome(t, "v0.2.0")
	for _, args := range [][]string{{"version"}, {"config", "path"}} {
		_, stderr := runSplit(t, "v0.1.0", args...)
		if args[0] == "version" && stderr != "" {
			t.Fatalf("version printed the notice: %q", stderr)
		}
		if args[0] == "config" && !strings.Contains(stderr, "v0.2.0") {
			t.Fatalf("config path did not print the notice: %q", stderr)
		}
	}
	for _, path := range []string{"social-mcp mcp", "social-mcp daemon run", "social-mcp update"} {
		if !noNoticeCommands[path] {
			t.Fatalf("%s must not print the notice", path)
		}
	}
}

func TestUpdateNoticeQuietWhenCurrentOrDisabled(t *testing.T) {
	cases := map[string]func(t *testing.T, h string) string{
		"up to date":        func(*testing.T, string) string { return "v0.2.0" },
		"development build": func(*testing.T, string) string { return "dev" },
		"env opt-out": func(t *testing.T, _ string) string {
			t.Setenv(update.EnvNoCheck, "1")
			return "v0.1.0"
		},
		"config opt-out": func(t *testing.T, h string) string {
			os.WriteFile(paths.Config(h), []byte("[updates]\ncheck = false\n"), 0o600)
			return "v0.1.0"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			h := noticeHome(t, "v0.2.0")
			version := setup(t, h)
			if _, stderr := runSplit(t, version, "account", "list"); stderr != "" {
				t.Fatalf("stderr = %q, want no notice", stderr)
			}
		})
	}
}
