// Package config loads and validates config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration.
type Config struct {
	Server  Server  `toml:"server"`
	Uploads Uploads `toml:"uploads"`
	YouTube YouTube `toml:"youtube"`
	TikTok  TikTok  `toml:"tiktok"`
	Meta    Meta    `toml:"meta"`
}

// Server configures the loopback listener used for MCP and OAuth callbacks.
type Server struct {
	ListenPort int `toml:"listen_port"`
}

// Uploads configures the upload runner and file access.
type Uploads struct {
	// AllowedDirs optionally restricts uploads to these directories. Empty means any path.
	AllowedDirs   []string `toml:"allowed_dirs"`
	MaxConcurrent int      `toml:"max_concurrent"`
}

// YouTube holds the Google OAuth desktop client.
type YouTube struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
}

// TikTok holds the TikTok developer app credentials.
type TikTok struct {
	ClientKey    string `toml:"client_key"`
	ClientSecret string `toml:"client_secret"`
}

// Meta holds the Meta app used for Facebook Pages and Instagram.
type Meta struct {
	AppID         string `toml:"app_id"`
	AppSecret     string `toml:"app_secret"`
	LoginConfigID string `toml:"login_config_id"`
	GraphVersion  string `toml:"graph_version"`
	AppMode       string `toml:"app_mode"`
}

// Meta app modes.
const (
	AppModeDevelopment = "development"
	AppModeLive        = "live"
)

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		Server:  Server{ListenPort: 8765},
		Uploads: Uploads{MaxConcurrent: 2},
		Meta:    Meta{GraphVersion: "v26.0", AppMode: AppModeDevelopment},
	}
}

// Load reads path over the defaults. A missing file yields the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Validate reports the first invalid setting.
func (c Config) Validate() error {
	if c.Server.ListenPort < 1 || c.Server.ListenPort > 65535 {
		return fmt.Errorf("server.listen_port must be 1-65535, got %d", c.Server.ListenPort)
	}
	if c.Uploads.MaxConcurrent < 1 || c.Uploads.MaxConcurrent > 16 {
		return fmt.Errorf("uploads.max_concurrent must be 1-16, got %d", c.Uploads.MaxConcurrent)
	}
	for _, d := range c.Uploads.AllowedDirs {
		if !filepath.IsAbs(d) {
			return fmt.Errorf("uploads.allowed_dirs entries must be absolute, got %q", d)
		}
	}
	if c.Meta.AppMode != AppModeDevelopment && c.Meta.AppMode != AppModeLive {
		return fmt.Errorf("meta.app_mode must be %q or %q, got %q", AppModeDevelopment, AppModeLive, c.Meta.AppMode)
	}
	if c.Meta.GraphVersion == "" {
		return errors.New("meta.graph_version must not be empty")
	}
	return nil
}

// YouTubeEnabled reports whether YouTube credentials are configured.
func (c Config) YouTubeEnabled() bool {
	return c.YouTube.ClientID != "" && c.YouTube.ClientSecret != ""
}

// TikTokEnabled reports whether TikTok credentials are configured.
func (c Config) TikTokEnabled() bool {
	return c.TikTok.ClientKey != "" && c.TikTok.ClientSecret != ""
}

// MetaEnabled reports whether Meta credentials are configured.
func (c Config) MetaEnabled() bool {
	return c.Meta.AppID != "" && c.Meta.AppSecret != "" && c.Meta.LoginConfigID != ""
}
