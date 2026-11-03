package daemon

import (
	"log/slog"

	"github.com/nhatnam1507/social-mcp/internal/config"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/facebook"
	"github.com/nhatnam1507/social-mcp/internal/provider/instagram"
	"github.com/nhatnam1507/social-mcp/internal/provider/meta"
	"github.com/nhatnam1507/social-mcp/internal/provider/tiktok"
	"github.com/nhatnam1507/social-mcp/internal/provider/youtube"
)

// buildProviders registers every platform configured in cfg. This is the only place that
// imports platform packages.
func buildProviders(cfg config.Config, log *slog.Logger) provider.Registry {
	reg := provider.Registry{}
	if cfg.YouTubeEnabled() {
		reg["youtube"] = youtube.New(cfg.YouTube.ClientID, cfg.YouTube.ClientSecret)
	}
	if cfg.TikTokEnabled() {
		reg["tiktok"] = tiktok.New(cfg.TikTok.ClientKey, cfg.TikTok.ClientSecret)
	}
	if cfg.MetaEnabled() {
		c := meta.NewClient(meta.Config{
			AppID: cfg.Meta.AppID, AppSecret: cfg.Meta.AppSecret, LoginConfigID: cfg.Meta.LoginConfigID,
			GraphVersion: cfg.Meta.GraphVersion, AppMode: cfg.Meta.AppMode,
		})
		reg["facebook"] = facebook.New(c)
		reg["instagram"] = instagram.New(c)
	}
	return reg
}
