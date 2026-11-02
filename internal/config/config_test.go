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
	if cfg.Server.ListenPort != 8765 || cfg.Uploads.MaxConcurrent != 2 || cfg.Meta.GraphVersion != "v26.0" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.Uploads.AllowedDirs) != 0 {
		t.Fatalf("template must not restrict directories, got %v", cfg.Uploads.AllowedDirs)
	}
	if cfg.YouTubeEnabled() || cfg.TikTokEnabled() || cfg.MetaEnabled() {
		t.Fatal("template must not enable platforms")
	}
}

func TestMissingFileYieldsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenPort != 8765 {
		t.Fatalf("listen_port = %d", cfg.Server.ListenPort)
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
		{"port too low", func(c *Config) { c.Server.ListenPort = 0 }, "listen_port"},
		{"port too high", func(c *Config) { c.Server.ListenPort = 70000 }, "listen_port"},
		{"relative dir", func(c *Config) { c.Uploads.AllowedDirs = []string{"videos"} }, "allowed_dirs"},
		{"absolute dir ok", func(c *Config) { c.Uploads.AllowedDirs = []string{abs} }, ""},
		{"zero concurrency", func(c *Config) { c.Uploads.MaxConcurrent = 0 }, "max_concurrent"},
		{"bad app mode", func(c *Config) { c.Meta.AppMode = "prod" }, "app_mode"},
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

func TestPlatformAvailability(t *testing.T) {
	cfg := Default()
	cfg.YouTube.ClientID, cfg.YouTube.ClientSecret = "id", "secret"
	cfg.TikTok.ClientKey = "key" // secret missing
	cfg.Meta.AppID, cfg.Meta.AppSecret, cfg.Meta.LoginConfigID = "app", "secret", "cfg"
	if !cfg.YouTubeEnabled() || cfg.TikTokEnabled() || !cfg.MetaEnabled() {
		t.Fatalf("availability wrong: yt=%v tt=%v meta=%v", cfg.YouTubeEnabled(), cfg.TikTokEnabled(), cfg.MetaEnabled())
	}
}
