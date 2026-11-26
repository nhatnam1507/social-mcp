// Package config loads and validates config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultListenPort is the loopback port for the MCP endpoint and OAuth callbacks. It is fixed
// because the redirect URIs registered on social-mcp's OAuth apps name it.
const DefaultListenPort = 8765

// ListenPortEnv moves the listener for tests and development only. Meta sign-in only works on
// DefaultListenPort, since Meta matches redirect URIs exactly.
const ListenPortEnv = "SOCIAL_MCP_LISTEN_PORT"

// ListenPort returns the loopback port: DefaultListenPort unless ListenPortEnv sets another.
func ListenPort(getenv func(string) string) (int, error) {
	v := strings.TrimSpace(getenv(ListenPortEnv))
	if v == "" {
		return DefaultListenPort, nil
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("%s must be a port number 1-65535, got %q", ListenPortEnv, v)
	}
	return p, nil
}

// Config is the on-disk configuration.
type Config struct {
	Uploads Uploads `toml:"uploads"`
	Updates Updates `toml:"updates"`

	// Retired lists settings from older releases that are now ignored, such as platform app
	// credentials and server.listen_port.
	Retired []string `toml:"-"`
	// LegacyClientIDs holds the public client IDs of the user's own apps from retired settings,
	// keyed by app name ("youtube", "tiktok", "meta"). Secrets are never kept.
	LegacyClientIDs map[string]string `toml:"-"`
	// LegacyListenPort is the retired server.listen_port, or 0.
	LegacyListenPort int `toml:"-"`
}

// Uploads configures the upload runner and file access.
type Uploads struct {
	// AllowedDirs optionally restricts uploads to these directories. Empty means any path.
	AllowedDirs   []string `toml:"allowed_dirs"`
	MaxConcurrent int      `toml:"max_concurrent"`
}

// Updates configures the daily check for a new social-mcp release.
type Updates struct {
	// Check lets the daemon look up the latest release once a day so the CLI and the agent can say
	// that an update is available. `social-mcp update` works either way.
	Check bool `toml:"check"`
}

// retiredTables are sections older releases read; their keys are accepted and ignored.
var retiredTables = []string{"server", "youtube", "tiktok", "meta"}

// file is the decoded shape: current settings plus the retired sections.
type file struct {
	Uploads Uploads        `toml:"uploads"`
	Updates Updates        `toml:"updates"`
	Server  map[string]any `toml:"server"`
	YouTube map[string]any `toml:"youtube"`
	TikTok  map[string]any `toml:"tiktok"`
	Meta    map[string]any `toml:"meta"`
}

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		Uploads: Uploads{MaxConcurrent: 2},
		Updates: Updates{Check: true},
	}
}

// Load reads path over the defaults. A missing file yields the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	f := file{Uploads: cfg.Uploads, Updates: cfg.Updates}
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return cfg, fmt.Errorf("%s: unknown settings %s (check for typos)", path, strings.Join(keys, ", "))
	}
	cfg.Uploads, cfg.Updates = f.Uploads, f.Updates
	for _, k := range md.Keys() {
		if len(k) > 1 && slices.Contains(retiredTables, k[0]) {
			cfg.Retired = append(cfg.Retired, k.String())
		}
	}
	cfg.LegacyClientIDs = map[string]string{}
	for app, v := range map[string]any{"youtube": f.YouTube["client_id"], "tiktok": f.TikTok["client_key"], "meta": f.Meta["app_id"]} {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			cfg.LegacyClientIDs[app] = strings.TrimSpace(s)
		}
	}
	if p, ok := f.Server["listen_port"].(int64); ok {
		cfg.LegacyListenPort = int(p)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// RetiredWarning describes ignored settings for the user, or "" when there are none.
func (c Config) RetiredWarning() string {
	if len(c.Retired) == 0 {
		return ""
	}
	msg := "config.toml settings no longer used and ignored: " + strings.Join(c.Retired, ", ") +
		". social-mcp now ships its own app for each platform; you can delete these lines."
	if c.LegacyListenPort != 0 && c.LegacyListenPort != DefaultListenPort {
		msg += fmt.Sprintf(" The listener now always uses port %d, not %d: run `social-mcp config mcp` and update your MCP client.",
			DefaultListenPort, c.LegacyListenPort)
	}
	return msg
}

// Validate reports the first invalid setting.
func (c Config) Validate() error {
	if c.Uploads.MaxConcurrent < 1 || c.Uploads.MaxConcurrent > 16 {
		return fmt.Errorf("uploads.max_concurrent must be 1-16, got %d", c.Uploads.MaxConcurrent)
	}
	for _, d := range c.Uploads.AllowedDirs {
		if !filepath.IsAbs(d) {
			return fmt.Errorf("uploads.allowed_dirs entries must be absolute, got %q", d)
		}
	}
	return nil
}
