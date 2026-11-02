package browser

import (
	"errors"
	"testing"
)

func TestOpenAllowlist(t *testing.T) {
	var launched []string
	run = func(name string, args ...string) error { launched = append(launched, name); return nil }
	lookPath = func(string) (string, error) { return "/usr/bin/x", nil }
	wsl = func() bool { return false }
	allowed := []string{"accounts.google.com"}

	if !Open("https://accounts.google.com/o/oauth2/v2/auth?x=1", allowed) {
		t.Fatal("allowlisted https URL not opened")
	}
	for _, bad := range []string{"http://accounts.google.com/x", "https://evil.example/x", `C:\Users\Public\x.exe`, "file:///etc/passwd"} {
		if Open(bad, allowed) {
			t.Fatalf("opened %q", bad)
		}
	}
	if len(launched) != 1 {
		t.Fatalf("launched %v", launched)
	}
}

func TestCommandSelection(t *testing.T) {
	wsl = func() bool { return false }
	lookPath = func(string) (string, error) { return "/usr/bin/x", nil }
	if n, _ := command("darwin", "u"); n != "open" {
		t.Fatalf("darwin: %s", n)
	}
	if n, a := command("windows", "u"); n != "rundll32" || a[0] != "url.dll,FileProtocolHandler" {
		t.Fatalf("windows: %s %v", n, a)
	}
	if n, _ := command("linux", "u"); n != "xdg-open" {
		t.Fatalf("linux: %s", n)
	}
	wsl = func() bool { return true }
	lookPath = func(name string) (string, error) { return "", errors.New("missing") }
	if n, _ := command("linux", "u"); n != "explorer.exe" {
		t.Fatalf("wsl without wslview: %s", n)
	}
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	if n, _ := command("linux", "u"); n != "wslview" {
		t.Fatalf("wsl with wslview: %s", n)
	}
	wsl = func() bool { return false }
	lookPath = func(string) (string, error) { return "", errors.New("missing") }
	if n, _ := command("linux", "u"); n != "" {
		t.Fatalf("headless linux: %s", n)
	}
}
