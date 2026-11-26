// Package appauth holds the OAuth app identity social-mcp signs users in with, and is the only
// package that uses app secrets.
//
// Release builds carry the maintainer's app identity, injected at build time with
// -ldflags "-X github.com/nhatnam1507/social-mcp/internal/appauth.<var>=<value>". Source builds
// carry none. Either way, SOCIAL_MCP_* environment variables replace a platform's identity with
// the user's own developer app.
package appauth

import (
	"fmt"
	"log/slog"
	"strings"
)

// Set at build time; empty in source builds. See the package comment.
var (
	youtubeClientID     string
	youtubeClientSecret string
	tiktokClientKey     string
	tiktokClientSecret  string
	metaAppID           string
	metaAppSecret       string
	metaLoginConfigID   string
	metaAppMode         string
)

// Platform app names. Facebook and Instagram share the Meta app.
const (
	YouTube = "youtube"
	TikTok  = "tiktok"
	Meta    = "meta"
)

// Meta app modes.
const (
	AppModeDevelopment = "development"
	AppModeLive        = "live"
)

// Source says where an app identity came from.
type Source string

const (
	SourceEmbedded Source = "embedded"
	SourceEnv      Source = "env"
)

// App is one platform's OAuth app. The secret is never exposed; only TokenEndpoint uses it.
type App struct {
	Platform      string
	ClientID      string // public: client_id, TikTok client_key or Meta app_id
	LoginConfigID string // Meta only
	AppMode       string // Meta only
	Source        Source
	secret        string
}

// NewApp returns an app identity for platform.
func NewApp(platform, clientID, secret string, source Source) *App {
	return &App{Platform: platform, ClientID: clientID, Source: source, secret: secret}
}

// String and LogValue keep the secret out of logs and error messages.
func (a *App) String() string {
	return fmt.Sprintf("%s app %s (%s)", a.Platform, a.ClientID, a.Source)
}

func (a *App) LogValue() slog.Value {
	return slog.GroupValue(slog.String("client_id", a.ClientID), slog.String("source", string(a.Source)))
}

// Apps are the app identities this process can use. A nil field means the platform is unavailable.
type Apps struct {
	YouTube, TikTok, Meta *App
}

// Get returns the app for an app name (YouTube, TikTok or Meta), or nil.
func (a Apps) Get(name string) *App {
	switch name {
	case YouTube:
		return a.YouTube
	case TikTok:
		return a.TikTok
	case Meta:
		return a.Meta
	}
	return nil
}

// AppFor maps a provider platform to its app name: facebook and instagram use the Meta app.
func AppFor(platform string) string {
	if platform == "facebook" || platform == "instagram" {
		return Meta
	}
	return platform
}

// EnvVars lists the override variables per app; the last Meta entry (app mode) is optional.
var EnvVars = map[string][]string{
	YouTube: {"SOCIAL_MCP_YOUTUBE_CLIENT_ID", "SOCIAL_MCP_YOUTUBE_CLIENT_SECRET"},
	TikTok:  {"SOCIAL_MCP_TIKTOK_CLIENT_KEY", "SOCIAL_MCP_TIKTOK_CLIENT_SECRET"},
	Meta:    {"SOCIAL_MCP_META_APP_ID", "SOCIAL_MCP_META_APP_SECRET", "SOCIAL_MCP_META_LOGIN_CONFIG_ID", "SOCIAL_MCP_META_APP_MODE"},
}

// Resolve picks each platform's identity: a complete set of SOCIAL_MCP_* variables wins, a
// partial set is an error, otherwise the build's embedded identity is used when it has one.
func Resolve(getenv func(string) string) (Apps, error) {
	var apps Apps
	var err error
	if apps.YouTube, err = resolve(getenv, YouTube, youtubeClientID, youtubeClientSecret); err != nil {
		return Apps{}, err
	}
	if apps.TikTok, err = resolve(getenv, TikTok, tiktokClientKey, tiktokClientSecret); err != nil {
		return Apps{}, err
	}
	if apps.Meta, err = resolve(getenv, Meta, metaAppID, metaAppSecret, metaLoginConfigID, metaAppMode); err != nil {
		return Apps{}, err
	}
	return apps, nil
}

// resolve reads one platform. embedded holds the build values in EnvVars order.
func resolve(getenv func(string) string, platform string, embedded ...string) (*App, error) {
	names := EnvVars[platform]
	required := names
	if platform == Meta {
		required = names[:3] // app mode is optional
	}
	vals := make([]string, len(names))
	var set, missing []string
	for i, n := range names {
		vals[i] = strings.TrimSpace(getenv(n))
		if i >= len(required) {
			continue
		}
		if vals[i] != "" {
			set = append(set, n)
		} else {
			missing = append(missing, n)
		}
	}
	source := SourceEnv
	switch {
	case len(set) > 0 && len(missing) > 0:
		return nil, fmt.Errorf("incomplete %s app override: %s set but %s missing", platform, strings.Join(set, ", "), strings.Join(missing, ", "))
	case len(set) == 0:
		for i := range required {
			if embedded[i] == "" {
				return nil, nil
			}
		}
		copy(vals, embedded)
		source = SourceEmbedded
	}
	a := NewApp(platform, vals[0], vals[1], source)
	if platform == Meta {
		a.LoginConfigID, a.AppMode = vals[2], vals[3]
		if a.AppMode == "" {
			a.AppMode = AppModeDevelopment
		}
		if a.AppMode != AppModeDevelopment && a.AppMode != AppModeLive {
			return nil, fmt.Errorf("Meta app mode must be %q or %q, got %q", AppModeDevelopment, AppModeLive, a.AppMode)
		}
	}
	return a, nil
}
