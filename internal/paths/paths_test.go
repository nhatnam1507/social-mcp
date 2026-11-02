package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveHonorsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvHome, dir)
	got, err := ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("ResolveHome() = %q, want %q", got, dir)
	}
}

func TestResolveRejectsRelativeEnv(t *testing.T) {
	t.Setenv(EnvHome, "relative/home")
	if _, err := ResolveHome(); err == nil {
		t.Fatal("expected error for relative SOCIAL_MCP_HOME")
	}
}

func TestEnsureCreatesPrivateDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if _, err := Ensure(home); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want 0700", fi.Mode().Perm())
	}
}

func TestEnsureTightensLooseDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	warn, err := Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	if warn == "" {
		t.Fatal("expected a warning when tightening permissions")
	}
	fi, _ := os.Stat(home)
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want 0700", fi.Mode().Perm())
	}
}

func TestEnsureRefusesSymlinkHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ")
	}
	base := t.TempDir()
	target := filepath.Join(base, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(link); err == nil {
		t.Fatal("expected error for symlinked home")
	}
}

func TestSocketPathFallsBackWhenTooLong(t *testing.T) {
	short := filepath.Join(os.TempDir(), "sm")
	if got := SocketPath(short); got != filepath.Join(short, "daemon.sock") {
		t.Fatalf("short home socket = %q", got)
	}
	long := filepath.Join(os.TempDir(), strings.Repeat("x", 120))
	got := SocketPath(long)
	if filepath.Dir(got) != filepath.Clean(os.TempDir()) {
		t.Fatalf("long home socket %q not in temp dir", got)
	}
	if !strings.HasPrefix(filepath.Base(got), "social-mcp-") || got == SocketPath(long+"y") {
		t.Fatalf("unexpected fallback socket name %q", got)
	}
}
