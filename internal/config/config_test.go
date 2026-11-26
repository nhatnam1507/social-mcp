package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(Template()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(template): %v", err)
	}
	if cfg.Uploads.MaxConcurrent != 2 || !cfg.Updates.Check {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.Uploads.AllowedDirs) != 0 {
		t.Fatalf("template must not restrict directories, got %v", cfg.Uploads.AllowedDirs)
	}
	if len(cfg.Retired) != 0 || cfg.RetiredWarning() != "" {
		t.Fatalf("template contains retired settings: %v", cfg.Retired)
	}
}

func TestMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Updates.Check {
		t.Fatal("the update check must default to on")
	}
}

func TestValidate(t *testing.T) {
	abs, _ := filepath.Abs(t.TempDir())
	cases := []struct {
		name    string
		edit    func(*Config)
		wantErr string
	}{
		{"defaults ok", func(*Config) {}, ""},
		{"relative dir", func(c *Config) { c.Uploads.AllowedDirs = []string{"videos"} }, "allowed_dirs"},
		{"absolute dir ok", func(c *Config) { c.Uploads.AllowedDirs = []string{abs} }, ""},
		{"zero concurrency", func(c *Config) { c.Uploads.MaxConcurrent = 0 }, "max_concurrent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.edit(&cfg)
			err := cfg.Validate()
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v, want mention of %q", err, tc.wantErr)
			}
		})
	}
}

func TestRetiredSettingsLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	old := `[server]
listen_port = 9000

[uploads]
max_concurrent = 3

[youtube]
client_id = "my-yt.apps.googleusercontent.com"
client_secret = "OLD-YT-SECRET"

[tiktok]
client_key = ""
client_secret = ""

[meta]
app_id = "my-meta-app"
app_secret = "OLD-META-SECRET"
login_config_id = "cfg"
graph_version = "v26.0"
app_mode = "development"
`
	os.WriteFile(path, []byte(old), 0o600)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("an old config must still load: %v", err)
	}
	if cfg.Uploads.MaxConcurrent != 3 {
		t.Fatalf("current settings lost: %+v", cfg.Uploads)
	}
	if len(cfg.Retired) != 10 || cfg.LegacyListenPort != 9000 {
		t.Fatalf("retired %v port %d", cfg.Retired, cfg.LegacyListenPort)
	}
	want := map[string]string{"youtube": "my-yt.apps.googleusercontent.com", "meta": "my-meta-app"}
	if len(cfg.LegacyClientIDs) != 2 || cfg.LegacyClientIDs["youtube"] != want["youtube"] || cfg.LegacyClientIDs["meta"] != want["meta"] {
		t.Fatalf("legacy client ids %v", cfg.LegacyClientIDs)
	}
	w := cfg.RetiredWarning()
	if !strings.Contains(w, "youtube.client_secret") || !strings.Contains(w, "9000") || strings.Contains(w, "OLD-") {
		t.Fatalf("warning %q", w)
	}
}

func TestListenPort(t *testing.T) {
	env := func(v string) func(string) string { return func(string) string { return v } }
	if p, err := ListenPort(env("")); err != nil || p != DefaultListenPort {
		t.Fatalf("default %d %v", p, err)
	}
	if p, err := ListenPort(env("18765")); err != nil || p != 18765 {
		t.Fatalf("override %d %v", p, err)
	}
	if _, err := ListenPort(env("abc")); err == nil {
		t.Fatal("bad port accepted")
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[uploads]\nmax-concurrent = 2\n"), 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "max-concurrent") {
		t.Fatalf("typo accepted: %v", err)
	}
}

func TestTemplateHasNoUpdatesKeys(t *testing.T) {
	// Older releases reject unknown settings; a config written by `config init` must stay loadable by
	// them, so the opt-out is only shown commented out.
	for _, line := range strings.Split(Template(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[updates]") {
			t.Fatalf("template enables the [updates] section: %q", line)
		}
	}
}

func TestUpdatesOptOutLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[updates]\ncheck = false\n"), 0o600)
	cfg, err := Load(path)
	if err != nil || cfg.Updates.Check {
		t.Fatalf("cfg.Updates.Check = %v, err = %v", cfg.Updates.Check, err)
	}
}
