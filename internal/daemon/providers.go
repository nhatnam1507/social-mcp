package daemon

import (
	"log/slog"

	"github.com/nhatnam1507/social-mcp/internal/config"
	"github.com/nhatnam1507/social-mcp/internal/provider"
)

// buildProviders registers every platform configured in cfg. This is the only place that
// imports platform packages.
func buildProviders(cfg config.Config, log *slog.Logger) provider.Registry {
	reg := provider.Registry{}
	return reg
}
