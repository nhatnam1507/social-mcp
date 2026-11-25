package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsReleaseAndNewer(t *testing.T) {
	release := map[string]bool{
		"v1.2.3":                       true,
		"v0.1.0-rc.2":                  true,
		"dev":                          false,
		"":                             false,
		"1.2.3":                        false,
		"v0.1.0-rc.1-1-gfd2b9a9-dirty": false, // git describe on a development checkout
		"v0.1.0-3-gabc1234":            false,
		"v0.1.0-dirty":                 false,
		"fd2b9a9":                      false, // git describe --always without tags
		"0.1.0-next+fd2b9a9":           false, // GoReleaser snapshot
	}
	for v, want := range release {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
	newer := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0-rc.2", "v0.1.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.2.0", "v0.1.0", false},
		{"dev", "v9.9.9", false},
		{"v0.1.0", "garbage", false},
	}
	for _, c := range newer {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("windows", "arm64"); got != "social-mcp-windows-arm64.exe" {
		t.Fatal(got)
	}
	if got := AssetName("darwin", "amd64"); got != "social-mcp-darwin-amd64" {
		t.Fatal(got)
	}
}

// release is a fake release host serving one tag.
type release struct {
	latest    string // redirect target path for /releases/latest; "" answers 404
	tag       string
	binary    []byte
	sums      string // SHA256SUMS body; computed from binary when empty
	noAsset   bool
	userAgent string
}

func (r *release) server(t *testing.T) *httptest.Server {
	t.Helper()
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.userAgent = req.UserAgent()
		switch req.URL.Path {
		case "/releases/latest":
			if r.latest == "" {
				http.NotFound(w, req)
				return
			}
			http.Redirect(w, req, r.latest, http.StatusFound)
		case "/releases/download/" + r.tag + "/SHA256SUMS":
			sums := r.sums
			if sums == "" {
				h := sha256.Sum256(r.binary)
				sums = fmt.Sprintf("%s  social-mcp-other\n%s  %s\n", strings.Repeat("0", 64), hex.EncodeToString(h[:]), asset)
			}
			fmt.Fprint(w, sums)
		case "/releases/download/" + r.tag + "/" + asset:
			if r.noAsset {
				http.NotFound(w, req)
				return
			}
			w.Write(r.binary)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testClient(srv *httptest.Server, check func(ctx context.Context, path, want string) error) *Client {
	c := NewClient("v0.1.0")
	c.BaseURL = srv.URL
	c.SelfCheck = check
	return c
}

func acceptAll(context.Context, string, string) error { return nil }

func TestLatest(t *testing.T) {
	cases := []struct {
		name    string
		latest  string
		want    string
		wantErr error
	}{
		{"tag", "/releases/tag/v1.2.3", "v1.2.3", nil},
		{"no stable release", "/releases", "", ErrNoRelease},
		{"not found", "", "", ErrNoRelease},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &release{latest: c.latest}
			got, err := testClient(r.server(t), acceptAll).Latest(context.Background())
			if !errors.Is(err, c.wantErr) || got != c.want {
				t.Fatalf("Latest() = %q, %v; want %q, %v", got, err, c.want, c.wantErr)
			}
			if r.userAgent != "social-mcp/v0.1.0" {
				t.Fatalf("User-Agent = %q", r.userAgent)
			}
		})
	}
}

func TestLatestRejectsNonReleaseTag(t *testing.T) {
	r := &release{latest: "/releases/tag/nightly"}
	if _, err := testClient(r.server(t), acceptAll).Latest(context.Background()); err == nil || errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v, want an unexpected-tag error", err)
	}
}

func TestDownloadVerifiesAndChecksVersion(t *testing.T) {
	r := &release{tag: "v1.2.3", binary: []byte("new binary")}
	dir := t.TempDir()
	var checked string
	c := testClient(r.server(t), func(_ context.Context, path, want string) error {
		checked = want
		return nil
	})
	path, err := c.Download(context.Background(), "v1.2.3", dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("downloaded to %s, want a file in %s", path, dir)
	}
	if b, _ := os.ReadFile(path); string(b) != "new binary" {
		t.Fatalf("content = %q", b)
	}
	if checked != "v1.2.3" {
		t.Fatalf("self-check asked for %q", checked)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm()&0o100 == 0 {
			t.Fatalf("mode %v is not executable", fi.Mode())
		}
	}
}

func TestDownloadFailuresLeaveNothing(t *testing.T) {
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	cases := map[string]struct {
		r     release
		check func(context.Context, string, string) error
		want  string
	}{
		"checksum mismatch": {release{tag: "v1.2.3", binary: []byte("x"), sums: strings.Repeat("a", 64) + "  " + asset + "\n"}, acceptAll, "checksum mismatch"},
		"no sums entry":     {release{tag: "v1.2.3", binary: []byte("x"), sums: strings.Repeat("a", 64) + "  other\n"}, acceptAll, "no entry for " + asset},
		"missing asset":     {release{tag: "v1.2.3", binary: []byte("x"), noAsset: true}, acceptAll, "HTTP 404"},
		"wrong version": {release{tag: "v1.2.3", binary: []byte("x")}, func(context.Context, string, string) error {
			return errors.New("reports version v1.0.0")
		}, "reports version"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := testClient(c.r.server(t), c.check).Download(context.Background(), "v1.2.3", dir)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("left %d files behind", len(entries))
			}
		})
	}
}

func TestReplaceSwapsTheBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "social-mcp")
	tmp := filepath.Join(dir, "new")
	os.WriteFile(target, []byte("old"), 0o755)
	os.WriteFile(tmp, []byte("new"), 0o755)
	if err := Replace(tmp, target); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatalf("target = %q", b)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("tmp file still exists")
	}
	CleanupOld(target)
	if _, err := os.Stat(target + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(".old file still exists after CleanupOld")
	}
}

func TestReplaceKeepsTargetWhenTmpIsMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "social-mcp")
	os.WriteFile(target, []byte("old"), 0o755)
	if err := Replace(filepath.Join(dir, "missing"), target); err == nil {
		t.Fatal("Replace succeeded without a new binary")
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "old" {
		t.Fatalf("target = %q, %v; want the old binary restored", b, err)
	}
}

func TestRunVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake binary")
	}
	bin := filepath.Join(t.TempDir(), "fake")
	os.WriteFile(bin, []byte("#!/bin/sh\necho v1.2.3\n"), 0o755)
	if err := runVersion(context.Background(), bin, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if err := runVersion(context.Background(), bin, "v2.0.0"); err == nil || !strings.Contains(err.Error(), `reports version "v1.2.3"`) {
		t.Fatalf("err = %v", err)
	}
}
