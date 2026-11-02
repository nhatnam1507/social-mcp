package daemon

import (
	"log/slog"

	"github.com/nhatnam1507/social-mcp/internal/config"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/youtube"
)

// buildProviders registers every platform configured in cfg. This is the only place that
// imports platform packages.
func buildProviders(cfg config.Config, log *slog.Logger) provider.Registry {
	reg := provider.Registry{}
	if cfg.YouTubeEnabled() {
		reg["youtube"] = youtube.New(cfg.YouTube.ClientID, cfg.YouTube.ClientSecret)
	}
	return reg
}
