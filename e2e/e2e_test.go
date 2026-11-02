//go:build e2e

// Package e2e drives the built social-mcp binary the way a user and an MCP client would.
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nhatnam1507/social-mcp/internal/enginetest"
)

type env struct {
	t    *testing.T
	bin  string
	home string
	port int
}

func setup(t *testing.T) *env {
	t.Helper()
	dir, err := os.MkdirTemp("", "sme")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	bin := filepath.Join(dir, "social-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "../cmd/social-mcp")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := fmt.Sprintf("[server]\nlisten_port = %d\n", port)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, bin: bin, home: home, port: port}
	t.Cleanup(func() { e.run("daemon", "stop") })
	return e
}

func (e *env) environ() []string {
	return append(os.Environ(), "SOCIAL_MCP_HOME="+e.home, "SOCIAL_MCP_KEYRING=off")
}

func (e *env) run(args ...string) (string, int) {
	cmd := exec.Command(e.bin, args...)
	cmd.Env = e.environ()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &ee):
		return out.String(), ee.ExitCode()
	}
	e.t.Fatalf("run %v: %v", args, err)
	return "", -1
}

func (e *env) token() string {
	b, err := os.ReadFile(filepath.Join(e.home, "mcp-token"))
	if err != nil {
		e.t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func callText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestEndToEnd(t *testing.T) {
	e := setup(t)

	// The daemon outlives the `daemon start` process and answers fresh CLI processes.
	if out, code := e.run("daemon", "start"); code != 0 || !strings.Contains(out, "daemon started") {
		t.Fatalf("daemon start (%d): %s", code, out)
	}
	time.Sleep(200 * time.Millisecond)
	if out, code := e.run("daemon", "status"); code != 0 || !strings.Contains(out, "running") {
		t.Fatalf("daemon status (%d): %s", code, out)
	}
	if out, code := e.run("daemon", "start"); code != 0 || !strings.Contains(out, "already running") {
		t.Fatalf("second start (%d): %s", code, out)
	}

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", e.port)
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: bearer{e.token()}}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e"}, nil).Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect over HTTP: %v", err)
	}
	if out, isErr := callText(t, cs, "list_accounts", nil); isErr || !strings.Contains(out, `"accounts":[]`) {
		t.Fatalf("list_accounts: %s", out)
	}
	dir := filepath.Dir(e.home)
	notVideo := filepath.Join(dir, "notes.txt")
	os.WriteFile(notVideo, []byte(strings.Repeat("plain text, not a video. ", 10)), 0o600)
	if out, isErr := callText(t, cs, "upload_video", map[string]any{"file_path": notVideo, "account_ids": []string{"acc_x"}, "title": "T"}); !isErr || !strings.Contains(out, "not a recognized video") {
		t.Fatalf("text file upload: %v %s", isErr, out)
	}
	video := enginetest.WriteMP4(t, filepath.Join(dir, "clip.mp4"), 4096)
	if out, isErr := callText(t, cs, "upload_video", map[string]any{"file_path": video, "account_ids": []string{"acc_missing"}, "title": "T"}); !isErr || !strings.Contains(out, "unknown account") {
		t.Fatalf("unknown account upload: %v %s", isErr, out)
	}
	cs.Close()

	resp, err := http.Post(endpoint, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{}`))
	req.Host = fmt.Sprintf("evil.example:%d", e.port)
	req.Header.Set("Authorization", "Bearer "+e.token())
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host: %v %v", resp, err)
	}

	// stdio mode backed by the daemon.
	cmd := exec.Command(e.bin, "mcp")
	cmd.Env = e.environ()
	stdio, err := mcp.NewClient(&mcp.Implementation{Name: "e2e-stdio"}, nil).Connect(context.Background(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect over stdio: %v", err)
	}
	tools, err := stdio.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("stdio tools: %v %v", tools, err)
	}
	stdio.Close()

	if out, code := e.run("daemon", "stop"); code != 0 || !strings.Contains(out, "stopped") {
		t.Fatalf("daemon stop (%d): %s", code, out)
	}
	if out, code := e.run("daemon", "status"); code != 3 {
		t.Fatalf("status after stop (%d): %s", code, out)
	}
	if out, code := e.run("daemon", "start"); code != 0 {
		t.Fatalf("restart (%d): %s", code, out)
	}
	if out, code := e.run("daemon", "stop"); code != 0 {
		t.Fatalf("final stop (%d): %s", code, out)
	}
}
