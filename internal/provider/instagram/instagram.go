// Package instagram publishes Reels to Instagram professional accounts linked to Facebook
// Pages, using Facebook Login for Business and resumable uploads of local files.
package instagram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/provider/meta"
)

const (
	maxCaption  = 2200
	maxHashtags = 30
)

// Provider publishes Instagram Reels.
type Provider struct {
	Meta         *meta.Client
	PollInterval time.Duration
	PollTimeout  time.Duration
	HTTP         *http.Client
}

var _ provider.Provider = (*Provider)(nil)

// New returns an Instagram Reels provider.
func New(c *meta.Client) *Provider {
	return &Provider{Meta: c, PollInterval: 10 * time.Second, PollTimeout: 15 * time.Minute, HTTP: &http.Client{Timeout: 2 * time.Hour}}
}

func (p *Provider) Platform() string     { return "instagram" }
func (p *Provider) RedirectHost() string { return "localhost" }
func (p *Provider) Refreshable() bool    { return false }

func (p *Provider) AuthURL(state, _ string, redirectURI string) string {
	return p.Meta.AuthURL(state, redirectURI)
}

func (p *Provider) Exchange(ctx context.Context, code, _ string, redirectURI string) (provider.Token, error) {
	return p.Meta.Exchange(ctx, code, redirectURI)
}

// Refresh always requires a new login: Meta issues no refresh tokens, so Instagram accounts
// need re-authorization about every 60 days.
func (p *Provider) Refresh(context.Context, provider.Token) (provider.Token, error) {
	return provider.Token{}, provider.ErrReauthRequired
}

type pageWithIG struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	IG   *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"instagram_business_account"`
}

// Identities returns the Instagram professional accounts linked to the user's Pages. They
// use the user token, so they have no token of their own.
func (p *Provider) Identities(ctx context.Context, tok provider.Token) (string, []provider.Identity, error) {
	subject, err := p.Meta.Me(ctx, tok)
	if err != nil {
		return "", nil, err
	}
	pages, err := meta.GetAll[pageWithIG](ctx, p.Meta, "/me/accounts", url.Values{"fields": {"id,name,instagram_business_account{id,username}"}, "limit": {"100"}}, tok.AccessToken)
	if err != nil {
		return "", nil, err
	}
	var ids []provider.Identity
	for _, pg := range pages {
		if pg.IG == nil || pg.IG.ID == "" {
			continue
		}
		name := "@" + pg.IG.Username
		if pg.IG.Username == "" {
			name = pg.Name + " (Instagram)"
		}
		ids = append(ids, provider.Identity{ExternalID: pg.IG.ID, DisplayName: name, Meta: map[string]string{"page_id": pg.ID}})
	}
	if len(ids) == 0 {
		return "", nil, errors.New("no Instagram professional account is linked to the Facebook Pages granted to the app")
	}
	return subject, ids, nil
}

func (p *Provider) Capabilities(map[string]string) provider.Capabilities {
	return provider.Capabilities{
		PublishMode:   provider.PublishDirect,
		PrivacyLevels: []string{provider.PrivacyPublic},
		Restrictions:  append([]string{provider.RestrictionNoPrivateOption}, p.Meta.Restrictions()...),
		Formats:       []string{provider.FormatMP4, provider.FormatMOV},
	}
}

func (p *Provider) Options() []provider.OptionSpec {
	return []provider.OptionSpec{{Name: "share_to_feed", Kind: "bool", Description: "also show the Reel in the profile feed (default true)"}}
}

func (p *Provider) ValidateMeta(m provider.VideoMeta) error {
	c := caption(m)
	if utf8.RuneCountInString(c) > maxCaption {
		return fmt.Errorf("caption (title, description and tags) must be at most %d characters", maxCaption)
	}
	if n := countHashtags(c); n > maxHashtags {
		return fmt.Errorf("caption has %d hashtags; Instagram allows at most %d", n, maxHashtags)
	}
	return nil
}

// caption joins title, description and tags into an Instagram caption.
func caption(m provider.VideoMeta) string {
	var parts []string
	if t := strings.TrimSpace(m.Title); t != "" {
		parts = append(parts, t)
	}
	if d := strings.TrimSpace(m.Description); d != "" {
		parts = append(parts, d)
	}
	var tags []string
	for _, t := range m.Tags {
		if t = strings.TrimPrefix(strings.TrimSpace(t), "#"); t != "" {
			tags = append(tags, "#"+strings.ReplaceAll(t, " ", ""))
		}
	}
	if len(tags) > 0 {
		parts = append(parts, strings.Join(tags, " "))
	}
	return strings.Join(parts, "\n\n")
}

func countHashtags(s string) int {
	n := 0
	for _, w := range strings.Fields(s) {
		if strings.HasPrefix(w, "#") && len(w) > 1 {
			n++
		}
	}
	return n
}
