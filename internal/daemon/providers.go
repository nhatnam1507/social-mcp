package daemon

import (
	"github.com/nhatnam1507/social-mcp/internal/appauth"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/facebook"
	"github.com/nhatnam1507/social-mcp/internal/provider/instagram"
	"github.com/nhatnam1507/social-mcp/internal/provider/meta"
	"github.com/nhatnam1507/social-mcp/internal/provider/tiktok"
	"github.com/nhatnam1507/social-mcp/internal/provider/youtube"
)

// buildProviders registers every platform that has an app identity. This is the only place that
// imports platform packages.
func buildProviders(apps appauth.Apps) provider.Registry {
	reg := provider.Registry{}
	if a := apps.YouTube; a != nil {
		reg["youtube"] = youtube.New(a.ClientID, a.TokenEndpoint(youtube.TokenURL))
	}
	if a := apps.TikTok; a != nil {
		reg["tiktok"] = tiktok.New(a.ClientID, a.TokenEndpoint(tiktok.TokenURL))
	}
	if a := apps.Meta; a != nil {
		c := meta.NewClient(meta.Config{
			AppID: a.ClientID, LoginConfigID: a.LoginConfigID, AppMode: a.AppMode, Tokens: a.TokenEndpoint(meta.TokenURL),
		})
		reg["facebook"] = facebook.New(c)
		reg["instagram"] = instagram.New(c)
	}
	return reg
}
