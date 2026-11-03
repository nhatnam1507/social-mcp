// Package browser opens login URLs in the user's browser.
package browser

import (
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
)

// run starts a command; replaced in tests.
var run = func(name string, args ...string) error { return exec.Command(name, args...).Start() }

// wsl reports whether we run under Windows Subsystem for Linux; replaced in tests.
var wsl = func() bool {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

var lookPath = exec.LookPath

// Open opens rawURL when it is an https URL on one of allowedHosts. It reports whether a
// browser was launched; callers always print the URL as well.
func Open(rawURL string, allowedHosts []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !slices.Contains(allowedHosts, u.Hostname()) {
		return false
	}
	name, args := command(runtime.GOOS, u.String())
	if name == "" {
		return false
	}
	return run(name, args...) == nil
}

func command(goos, u string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{u}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", u}
	}
	if wsl() {
		if _, err := lookPath("wslview"); err == nil {
			return "wslview", []string{u}
		}
		return "explorer.exe", []string{u}
	}
	if _, err := lookPath("xdg-open"); err == nil {
		return "xdg-open", []string{u}
	}
	return "", nil
}
