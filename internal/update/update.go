// Package update finds social-mcp releases on GitHub and installs a release binary over the running
// executable. It uses the same public download routes as scripts/install.sh, so it needs no GitHub API
// access, token or rate-limit budget.
package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Repo is the GitHub repository that publishes releases.
const Repo = "nhatnam1507/social-mcp"

// ErrNoRelease means the repository has no stable release yet.
var ErrNoRelease = errors.New("no stable release has been published yet")

// releaseTag matches the tags GoReleaser stamps into release builds: vX.Y.Z with an optional single
// prerelease segment (v1.2.3-rc.1). `git describe` output (v1.2.3-4-gabc1234) and "dev" do not match.
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// IsRelease reports whether v is the version of a published release build rather than a development build.
func IsRelease(v string) bool {
	return releaseTag.MatchString(v) && semver.IsValid(v) && !strings.HasSuffix(v, "-dirty")
}

// Newer reports whether latest is a newer release than current. Development builds are never behind.
func Newer(current, latest string) bool {
	return IsRelease(current) && IsRelease(latest) && semver.Compare(latest, current) > 0
}

// Notice is the one-line message telling the user that latest is available.
func Notice(current, latest string) string {
	return fmt.Sprintf("social-mcp %s is available (you have %s). Run \"social-mcp update\" to install it.", latest, current)
}

// AssetName is the release asset for an OS and CPU, matching .goreleaser.yaml and the installers.
func AssetName(goos, goarch string) string {
	name := "social-mcp-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Client talks to the release host.
type Client struct {
	// BaseURL is the repository URL, for example https://github.com/nhatnam1507/social-mcp.
	BaseURL   string
	UserAgent string
	HTTP      *http.Client
	// SelfCheck runs a downloaded binary and confirms it reports version want.
	SelfCheck func(ctx context.Context, path, want string) error
}

// NewClient returns a client for the public GitHub repository. version identifies the caller in the
// User-Agent header.
func NewClient(version string) *Client {
	return &Client{
		BaseURL:   "https://github.com/" + Repo,
		UserAgent: "social-mcp/" + version,
		HTTP:      &http.Client{Timeout: 10 * time.Minute},
		SelfCheck: runVersion,
	}
}

// Latest returns the tag of the newest stable release. It reads the redirect target of
// <repo>/releases/latest instead of calling the API; GitHub redirects to <repo>/releases/tag/<tag>, or to
// <repo>/releases when no stable release exists.
func (c *Client) Latest(ctx context.Context) (string, error) {
	req, err := c.newRequest(ctx, c.BaseURL+"/releases/latest")
	if err != nil {
		return "", err
	}
	noFollow := *c.HTTP
	noFollow.Timeout = 30 * time.Second
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req)
	if err != nil {
		return "", fmt.Errorf("look up the latest release: %w", err)
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNoRelease
	case resp.StatusCode < 300 || resp.StatusCode > 399:
		return "", fmt.Errorf("look up the latest release: unexpected HTTP %d", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("look up the latest release: %w", err)
	}
	_, tag, found := strings.Cut(loc.Path, "/releases/tag/")
	if !found || tag == "" {
		return "", ErrNoRelease
	}
	tag, err = url.PathUnescape(tag)
	if err != nil || !IsRelease(tag) {
		return "", fmt.Errorf("look up the latest release: unexpected tag %q", tag)
	}
	return tag, nil
}

// Download fetches the release binary for this OS and CPU, verifies it against the release's SHA256SUMS
// and confirms it reports tag as its version. The verified binary is written to a new file in dir (the
// install folder, so the final rename stays on one file system) and its path is returned. On any error
// nothing is left behind.
func (c *Client) Download(ctx context.Context, tag, dir string) (path string, err error) {
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	base := c.BaseURL + "/releases/download/" + url.PathEscape(tag) + "/"

	sums, err := c.fetch(ctx, base+"SHA256SUMS", 1<<20)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS for %s: %w", tag, err)
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return "", fmt.Errorf("%s: %w", tag, err)
	}

	f, err := os.CreateTemp(dir, ".social-mcp-update-*")
	if err != nil {
		return "", fmt.Errorf("create a file in %s: %w", dir, err)
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	req, err := c.newRequest(ctx, base+asset)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", asset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s from %s: HTTP %d", asset, tag, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", fmt.Errorf("download %s: %w", asset, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("checksum mismatch for %s (expected %s, got %s); nothing was installed", asset, want, got)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		return "", err
	}
	if err := c.SelfCheck(ctx, f.Name(), tag); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// Replace moves the verified binary at tmp over target. On Windows the running executable cannot be
// overwritten or deleted but can be renamed, so target is first renamed to target.old; CleanupOld removes
// that file later.
func Replace(tmp, target string) error {
	return replace(tmp, target)
}

// CleanupOld removes a target.old left by an earlier Replace on Windows. It is best effort: the file stays
// while a process started from it is still running.
func CleanupOld(target string) {
	os.Remove(target + ".old")
}

// InstalledPath returns the real path of the running executable, following symlinks.
func InstalledPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func (c *Client) newRequest(ctx context.Context, u string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	return req, nil
}

func (c *Client) fetch(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := c.newRequest(ctx, u)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// checksumFor finds asset in SHA256SUMS ("<hex>  <name>" lines, optionally "*<name>" in binary mode).
func checksumFor(sums []byte, asset string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS has no entry for %s", asset)
}

// runVersion runs `<path> version` and checks that it prints want.
func runVersion(ctx context.Context, path, want string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return fmt.Errorf("run the downloaded binary: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		return fmt.Errorf("the downloaded binary reports version %q, expected %q; nothing was installed", got, want)
	}
	return nil
}
