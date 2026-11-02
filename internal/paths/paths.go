// Package paths resolves the social-mcp home directory and the files inside it.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// EnvHome overrides the home directory.
const EnvHome = "SOCIAL_MCP_HOME"

// maxSocketPath keeps AF_UNIX paths under the smallest platform limit (104 bytes on macOS).
const maxSocketPath = 100

// ResolveHome returns $SOCIAL_MCP_HOME or <user config dir>/social-mcp.
func ResolveHome() (string, error) {
	if h := os.Getenv(EnvHome); h != "" {
		if !filepath.IsAbs(h) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", EnvHome, h)
		}
		return filepath.Clean(h), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, "social-mcp"), nil
}

// Ensure creates home with mode 0700 and checks that it is a private directory owned by
// the current user. It returns a non-empty warning when it had to tighten permissions.
func Ensure(home string) (warning string, err error) {
	if err := os.MkdirAll(filepath.Dir(home), 0o755); err != nil {
		return "", err
	}
	if err := os.Mkdir(home, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	return checkHome(home)
}

// Config returns the path of config.toml.
func Config(home string) string { return filepath.Join(home, "config.toml") }

// DB returns the path of the SQLite database.
func DB(home string) string { return filepath.Join(home, "social-mcp.db") }

// MCPToken returns the path of the MCP bearer token file.
func MCPToken(home string) string { return filepath.Join(home, "mcp-token") }

// Lock returns the path of the daemon lock file.
func Lock(home string) string { return filepath.Join(home, "daemon.lock") }

// PID returns the path of the daemon pidfile.
func PID(home string) string { return filepath.Join(home, "daemon.pid") }

// Log returns the path of the daemon log file.
func Log(home string) string { return filepath.Join(home, "daemon.log") }

// SocketPath returns the control socket path: home/daemon.sock, or, when that would exceed
// AF_UNIX path limits, a short path in a directory private to the current user
// ($XDG_RUNTIME_DIR on Linux, otherwise a per-user directory under the system temp dir).
func SocketPath(home string) string {
	p := filepath.Join(home, "daemon.sock")
	if len(p) <= maxSocketPath {
		return p
	}
	sum := sha256.Sum256([]byte(home))
	return filepath.Join(socketDir(), "social-mcp-"+hex.EncodeToString(sum[:])[:12]+".sock")
}

func socketDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); runtime.GOOS == "linux" && filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(os.TempDir(), privateDirName())
}

// EnsureSocketDir creates the socket's directory when it is outside home and verifies it is
// a private directory owned by the current user.
func EnsureSocketDir(sock string) error {
	dir := filepath.Dir(sock)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_, err := checkHome(dir)
	return err
}
