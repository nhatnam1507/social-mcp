// Package wsl detects Windows Subsystem for Linux and bridges paths and environment between the
// Linux side, where social-mcp runs, and Windows apps such as Claude Desktop.
package wsl

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// Detected reports whether this process runs inside WSL.
func Detected() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// Distro returns the name of the WSL distribution, or "" when unknown (wsl.exe then uses the default).
func Distro() string { return os.Getenv("WSL_DISTRO_NAME") }

var windowsPath = regexp.MustCompile(`^([A-Za-z]:[\\/]|\\\\)`)

// IsWindowsPath reports whether p looks like a Windows path: a drive path such as `C:\Videos\a.mp4`
// or a UNC path such as `\\wsl.localhost\Ubuntu\home\me\a.mp4`.
func IsWindowsPath(p string) bool { return windowsPath.MatchString(p) }

// LinuxPath converts a Windows path to the path WSL mounts it at, so an agent running on Windows can
// name a file the way it sees it. Other paths, and every path outside WSL, are returned unchanged.
func LinuxPath(p string) string {
	if !IsWindowsPath(p) || !Detected() {
		return p
	}
	out, err := exec.Command("wslpath", "-u", p).Output()
	if err != nil {
		return p
	}
	if s := strings.TrimRight(string(out), "\r\n"); s != "" {
		return s
	}
	return p
}

// WindowsEnvDir returns the Linux path of a Windows environment variable that names a folder, such
// as LOCALAPPDATA or APPDATA, by asking cmd.exe through WSL interop.
func WindowsEnvDir(name string) (string, error) {
	cmd := exec.Command("cmd.exe", "/d", "/c", "echo %"+name+"%")
	if fi, err := os.Stat("/mnt/c"); err == nil && fi.IsDir() {
		cmd.Dir = "/mnt/c" // cmd.exe warns when started in a Linux (UNC) directory
	}
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("cannot run cmd.exe; is WSL interop turned on?")
	}
	win := strings.TrimSpace(string(out))
	if win == "" || win == "%"+name+"%" {
		return "", errors.New("Windows has no " + name + " variable")
	}
	lin, err := exec.Command("wslpath", "-u", win).Output()
	if err != nil {
		return "", errors.New("cannot convert " + win + " with wslpath")
	}
	return strings.TrimRight(string(lin), "\r\n"), nil
}
