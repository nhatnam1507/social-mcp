package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

var connectNow = time.Date(2026, 11, 26, 16, 0, 0, 0, time.UTC)

func TestAddServerKeepsOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	orig := `{
  "preferences": {"theme": "dark"},
  "mcpServers": {
    "other": {"command": "other-mcp"},
    "social-mcp": {"command": "npx", "args": ["-y", "mcp-remote", "http://127.0.0.1:8765/mcp"]}
  },
  "zeta": 1
}`
	os.WriteFile(path, []byte(orig), 0o600)
	backup, replaced, err := addServer(path, stdioServer{Command: "/bin/social-mcp", Args: []string{"relay"}}, connectNow)
	if err != nil || !replaced {
		t.Fatalf("addServer: replaced=%v err=%v", replaced, err)
	}
	if b, _ := os.ReadFile(backup); string(b) != orig {
		t.Fatalf("backup %s holds %q", backup, b)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !(strings.Index(s, `"preferences"`) < strings.Index(s, `"mcpServers"`) && strings.Index(s, `"mcpServers"`) < strings.Index(s, `"zeta"`)) ||
		strings.Index(s, `"other"`) > strings.Index(s, `"social-mcp"`) {
		t.Fatalf("key order changed:\n%s", s)
	}
	var cfg struct {
		Preferences map[string]string
		MCPServers  map[string]stdioServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Preferences["theme"] != "dark" || cfg.MCPServers["other"].Command != "other-mcp" {
		t.Fatalf("other settings lost: %+v", cfg)
	}
	if sm := cfg.MCPServers["social-mcp"]; sm.Command != "/bin/social-mcp" || len(sm.Args) != 1 || sm.Args[0] != "relay" {
		t.Fatalf("social-mcp entry = %+v", sm)
	}
}

func TestAddServerCreatesFile(t *testing.T) {
	for name, content := range map[string]*string{"missing": nil, "empty": new(string)} {
		path := filepath.Join(t.TempDir(), "Claude", "claude_desktop_config.json")
		if content != nil {
			os.MkdirAll(filepath.Dir(path), 0o700)
			os.WriteFile(path, []byte(*content), 0o600)
		}
		backup, replaced, err := addServer(path, stdioServer{Command: "x", Args: []string{"relay"}}, connectNow)
		if err != nil || replaced || backup != "" {
			t.Fatalf("%s: backup=%q replaced=%v err=%v", name, backup, replaced, err)
		}
		got, _ := os.ReadFile(path)
		if !strings.Contains(string(got), `"social-mcp": {`) {
			t.Fatalf("%s: wrote %s", name, got)
		}
	}
}

func TestAddServerRefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")
	os.WriteFile(path, []byte(`{"mcpServers": {`), 0o600)
	if _, _, err := addServer(path, stdioServer{Command: "x"}, connectNow); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"mcpServers": {` {
		t.Fatalf("file changed to %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left extra files: %v", entries)
	}
}

func TestPickWindowsConfig(t *testing.T) {
	notFound := errors.New("not found")
	const name = "claude_desktop_config.json"
	local, roaming := t.TempDir(), t.TempDir()
	if _, err := pickWindowsConfig(local, roaming, name, notFound); err != notFound {
		t.Fatalf("no Claude folders: %v", err)
	}
	classic := filepath.Join(roaming, "Claude")
	os.MkdirAll(classic, 0o700)
	if p, err := pickWindowsConfig(local, roaming, name, notFound); err != nil || p != filepath.Join(classic, name) {
		t.Fatalf("classic folder only: %q %v", p, err)
	}
	data := filepath.Join(local, "Claude-Data", "claude_pkg")
	os.MkdirAll(data, 0o700)
	os.WriteFile(filepath.Join(local, "Claude-Data", "README.txt"), []byte("x"), 0o600)
	if p, err := pickWindowsConfig(local, roaming, name, notFound); err != nil || p != filepath.Join(data, name) {
		t.Fatalf("per-package data folder preferred when no file exists: %q %v", p, err)
	}
	os.WriteFile(filepath.Join(data, name), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(classic, name), []byte("{}"), 0o600)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(data, name), old, old)
	if p, err := pickWindowsConfig(local, roaming, name, notFound); err != nil || p != filepath.Join(classic, name) {
		t.Fatalf("newest file wins: %q %v", p, err)
	}
}

func TestServerEntries(t *testing.T) {
	t.Setenv("SOCIAL_MCP_HOME", "/data/sm")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if s := localServer("/bin/social-mcp"); s.Command != "/bin/social-mcp" || s.Env["SOCIAL_MCP_HOME"] != "/data/sm" {
		t.Fatalf("local = %+v", s)
	}
	s := windowsServer("/bin/social-mcp")
	want := []string{"-d", "Ubuntu", "--", "env", "SOCIAL_MCP_HOME=/data/sm", "/bin/social-mcp", "relay"}
	if s.Command != "wsl.exe" || strings.Join(s.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("windows = %+v", s)
	}
}

func TestConnectUnknownApp(t *testing.T) {
	if _, err := run(t, &stubService{}, "connect", "notepad"); err == nil || !strings.Contains(err.Error(), "social-mcp connect") {
		t.Fatalf("unknown app: %v", err)
	}
}

func TestSetCodexServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	orig := `# my settings
model = "gpt-5"

[mcp_servers.social-mcp]
command = "old"
args = ["mcp"]

[mcp_servers.social-mcp.env]
X = "1"

[mcp_servers.other]
command = "other-mcp" # keep me
`
	os.WriteFile(path, []byte(orig), 0o600)
	server := stdioServer{Command: `C:\bin\social-mcp "x".exe`, Args: []string{"relay"}, Env: map[string]string{"SOCIAL_MCP_HOME": "/data"}}
	backup, replaced, err := setCodexServer(path, server, connectNow)
	if err != nil || !replaced {
		t.Fatalf("replaced=%v err=%v", replaced, err)
	}
	if b, _ := os.ReadFile(backup); string(b) != orig {
		t.Fatalf("backup = %q", b)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	for _, want := range []string{"# my settings", `command = "other-mcp" # keep me`, "[mcp_servers.social-mcp]\n"} {
		if !strings.Contains(s, want) {
			t.Fatalf("lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"old"`) || strings.Contains(s, "social-mcp.env") {
		t.Fatalf("old social-mcp table kept:\n%s", s)
	}
	var cfg struct {
		Model      string
		MCPServers map[string]stdioServer `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(s, &cfg); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, s)
	}
	sm := cfg.MCPServers["social-mcp"]
	if cfg.Model != "gpt-5" || cfg.MCPServers["other"].Command != "other-mcp" || sm.Command != server.Command || sm.Env["SOCIAL_MCP_HOME"] != "/data" {
		t.Fatalf("decoded %+v", cfg)
	}

	fresh := filepath.Join(t.TempDir(), "config.toml")
	if backup, replaced, err := setCodexServer(fresh, server, connectNow); err != nil || replaced || backup != "" {
		t.Fatalf("new file: %q %v %v", backup, replaced, err)
	}

	broken := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(broken, []byte("model = \n"), 0o600)
	if _, _, err := setCodexServer(broken, server, connectNow); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}

func TestFindAntigravity(t *testing.T) {
	h := t.TempDir()
	if _, err := antigravityConfig(h); err == nil {
		t.Fatal("found Antigravity without its folders")
	}
	// Only the folder for tokens and caches exists: the current settings path is used.
	os.MkdirAll(filepath.Join(h, ".gemini", "antigravity"), 0o700)
	if p, err := antigravityConfig(h); err != nil || p != filepath.Join(h, ".gemini", "config", "mcp_config.json") {
		t.Fatalf("got %q %v", p, err)
	}
	// An early version's settings file is the only one: keep using it.
	legacy := filepath.Join(h, ".gemini", "antigravity", "mcp_config.json")
	os.WriteFile(legacy, []byte("{}"), 0o600)
	if p, err := antigravityConfig(h); err != nil || p != legacy {
		t.Fatalf("got %q %v", p, err)
	}
}

func TestFindCodexHonorsCodexHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	if p, err := findCodex(); err != nil || p != filepath.Join(dir, "config.toml") {
		t.Fatalf("got %q %v", p, err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(dir, "missing"))
	if _, err := findCodex(); err == nil {
		t.Fatal("missing CODEX_HOME accepted")
	}
}
