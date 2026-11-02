// Package facebook uploads videos to Facebook Pages with Page access tokens.
package facebook

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"unicode/utf8"

	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/meta"
)

// Provider publishes to Facebook Pages.
type Provider struct {
	Meta *meta.Client
}

var _ provider.Provider = (*Provider)(nil)

// New returns a Facebook Pages provider.
func New(c *meta.Client) *Provider { return &Provider{Meta: c} }

func (p *Provider) Platform() string     { return "facebook" }
func (p *Provider) RedirectHost() string { return "localhost" }
func (p *Provider) Refreshable() bool    { return false }

func (p *Provider) AuthURL(state, _ string, redirectURI string) string {
	return p.Meta.AuthURL(state, redirectURI)
}

func (p *Provider) Exchange(ctx context.Context, code, _ string, redirectURI string) (provider.Token, error) {
	return p.Meta.Exchange(ctx, code, redirectURI)
}

// Refresh always requires a new login: Meta issues no refresh tokens.
func (p *Provider) Refresh(context.Context, provider.Token) (provider.Token, error) {
	return provider.Token{}, provider.ErrReauthRequired
}

type page struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	AccessToken string   `json:"access_token"`
	Tasks       []string `json:"tasks"`
}

// Identities returns the Pages where the user can create content. Each Page carries its own
// Page token, which does not expire when derived from a long-lived user token.
func (p *Provider) Identities(ctx context.Context, tok provider.Token) (string, []provider.Identity, error) {
	subject, err := p.Meta.Me(ctx, tok)
	if err != nil {
		return "", nil, err
	}
	pages, err := meta.GetAll[page](ctx, p.Meta, "/me/accounts", url.Values{"fields": {"id,name,access_token,tasks"}, "limit": {"100"}}, tok.AccessToken)
	if err != nil {
		return "", nil, err
	}
	var ids []provider.Identity
	for _, pg := range pages {
		if pg.AccessToken == "" || !slices.Contains(pg.Tasks, "CREATE_CONTENT") {
			continue
		}
		ids = append(ids, provider.Identity{ExternalID: pg.ID, DisplayName: pg.Name, AccountToken: &provider.Token{AccessToken: pg.AccessToken}})
	}
	if len(ids) == 0 {
		return "", nil, errors.New("no Facebook Pages where you can create content were granted to the app")
	}
	return subject, ids, nil
}

func (p *Provider) Capabilities(map[string]string) provider.Capabilities {
	return provider.Capabilities{
		PublishMode:   provider.PublishDirect,
		PrivacyLevels: []string{provider.PrivacyPrivate, provider.PrivacyPublic},
		Restrictions:  p.Meta.Restrictions(),
		Formats:       []string{provider.FormatMP4, provider.FormatMOV},
	}
}

func (p *Provider) Options() []provider.OptionSpec { return nil }

func (p *Provider) ValidateMeta(m provider.VideoMeta) error {
	if utf8.RuneCountInString(m.Title) > 255 {
		return errors.New("title must be at most 255 characters")
	}
	return nil
}
