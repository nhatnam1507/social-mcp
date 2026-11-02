// Package tiktok implements TikTok Login Kit for Desktop and the Content Posting API
// "upload to inbox" flow: videos arrive as drafts the creator finishes in the TikTok app.
package tiktok

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/provider"
)

const (
	scopes       = "user.info.basic,video.upload"
	maxFileBytes = 4_000_000_000
	draftNote    = "Draft sent to the TikTok inbox. Open the TikTok app notification to add the caption, choose privacy and post."
)

// Provider uploads drafts to TikTok.
type Provider struct {
	ClientKey    string
	ClientSecret string
	AuthBase     string
	APIBase      string
	PollInterval time.Duration
	PollTimeout  time.Duration
	HTTP         *http.Client
}

var _ provider.Provider = (*Provider)(nil)

// New returns a provider for a TikTok developer app.
func New(clientKey, clientSecret string) *Provider {
	return &Provider{
		ClientKey:    clientKey,
		ClientSecret: clientSecret,
		AuthBase:     "https://www.tiktok.com/v2/auth/authorize/",
		APIBase:      "https://open.tiktokapis.com",
		PollInterval: 5 * time.Second,
		PollTimeout:  10 * time.Minute,
		HTTP:         &http.Client{Timeout: 10 * time.Minute},
	}
}

func (p *Provider) Platform() string     { return "tiktok" }
func (p *Provider) RedirectHost() string { return "127.0.0.1" }
func (p *Provider) Refreshable() bool    { return true }

// AuthURL uses the desktop PKCE variant, whose challenge is hex-encoded SHA-256.
func (p *Provider) AuthURL(state, verifier, redirectURI string) string {
	q := url.Values{
		"client_key":            {p.ClientKey},
		"scope":                 {scopes},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {oauth.ChallengeS256Hex(verifier)},
		"code_challenge_method": {"S256"},
	}
	return p.AuthBase + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
	OpenID           string `json:"open_id"`
	Error            string `json:"error"`
	Description      string `json:"error_description"`
}

func (p *Provider) Exchange(ctx context.Context, code, verifier, redirectURI string) (provider.Token, error) {
	return p.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
}

// Refresh exchanges the refresh token. TikTok rotates refresh tokens, so the returned token
// must replace the stored one.
func (p *Provider) Refresh(ctx context.Context, tok provider.Token) (provider.Token, error) {
	if tok.RefreshToken == "" {
		return provider.Token{}, provider.ErrReauthRequired
	}
	return p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}})
}

func (p *Provider) token(ctx context.Context, form url.Values) (provider.Token, error) {
	form.Set("client_key", p.ClientKey)
	form.Set("client_secret", p.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.APIBase+"/v2/oauth/token/", strings.NewReader(form.Encode()))
	if err != nil {
		return provider.Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return provider.Token{}, err
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil {
		return provider.Token{}, fmt.Errorf("TikTok token endpoint returned %s", resp.Status)
	}
	// A refresh rejected by TikTok (expired, revoked or rotated-away token) needs a new login.
	if tr.Error == "invalid_grant" || (form.Get("grant_type") == "refresh_token" && tr.AccessToken == "" && resp.StatusCode < 500) {
		return provider.Token{}, fmt.Errorf("%w: %s %s", provider.ErrReauthRequired, tr.Error, tr.Description)
	}
	if tr.AccessToken == "" {
		if resp.StatusCode >= 500 {
			return provider.Token{}, fmt.Errorf("TikTok token endpoint returned %s", resp.Status)
		}
		return provider.Token{}, provider.Permanent("TikTok token endpoint: %s %s", tr.Error, tr.Description)
	}
	now := time.Now()
	t := provider.Token{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, Extra: map[string]string{"open_id": tr.OpenID}}
	if tr.ExpiresIn > 0 {
		t.Expiry = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	if tr.RefreshExpiresIn > 0 {
		t.RefreshExpiry = now.Add(time.Duration(tr.RefreshExpiresIn) * time.Second)
	}
	return t, nil
}

func (p *Provider) Identities(ctx context.Context, tok provider.Token) (string, []provider.Identity, error) {
	var out struct {
		User struct {
			OpenID      string `json:"open_id"`
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	if err := p.api(ctx, http.MethodGet, "/v2/user/info/?fields=open_id,display_name,avatar_url", tok, nil, &out); err != nil {
		return "", nil, err
	}
	if out.User.OpenID == "" {
		return "", nil, errors.New("TikTok returned no user")
	}
	name := out.User.DisplayName
	if name == "" {
		name = out.User.OpenID
	}
	return out.User.OpenID, []provider.Identity{{ExternalID: out.User.OpenID, DisplayName: name}}, nil
}

func (p *Provider) Capabilities(map[string]string) provider.Capabilities {
	return provider.Capabilities{
		PublishMode:    provider.PublishDraft,
		PrivacyLevels:  []string{provider.PrivacyPrivate, provider.PrivacyUnlisted, provider.PrivacyPublic},
		Restrictions:   []string{provider.RestrictionDraftOnly, provider.RestrictionPrivacySetInApp, "max_5_pending_drafts_per_24h"},
		MaxFileBytes:   maxFileBytes,
		MaxDurationSec: 600,
		Formats:        []string{provider.FormatMP4, provider.FormatMOV, provider.FormatWebM},
	}
}

func (p *Provider) Options() []provider.OptionSpec { return nil }

// ValidateMeta accepts any metadata: the inbox API takes no caption, so the creator writes it in the app.
func (p *Provider) ValidateMeta(provider.VideoMeta) error { return nil }

// api calls a TikTok API endpoint and decodes the "data" field of the envelope.
func (p *Provider) api(ctx context.Context, method, path string, tok provider.Token, body, data any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.APIBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(raw, &env)
	if resp.StatusCode == http.StatusOK && (env.Error.Code == "" || env.Error.Code == "ok") {
		if data != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, data)
		}
		return nil
	}
	return apiError(resp, env.Error.Code, env.Error.Message)
}

func apiError(resp *http.Response, code, msg string) error {
	switch {
	case code == "access_token_invalid" || (resp.StatusCode == http.StatusUnauthorized && code != "scope_not_authorized"):
		return fmt.Errorf("%w: %s", provider.ErrUnauthorized, msg)
	case code == "scope_not_authorized":
		return provider.Permanent("TikTok: the login did not grant video.upload; run account reauth and approve all permissions")
	case code == "spam_risk_too_many_pending_share":
		return provider.Permanent("TikTok: too many pending drafts (at most 5 in 24 hours); post or delete drafts in the TikTok app first")
	case code == "spam_risk_user_banned_from_posting":
		return provider.Permanent("TikTok: this account is banned from posting")
	case code == "rate_limit_exceeded" || resp.StatusCode == http.StatusTooManyRequests:
		return &provider.RateLimitError{RetryAfter: provider.RetryAfter(resp.Header, time.Minute), Msg: msg}
	case resp.StatusCode >= 500:
		return fmt.Errorf("TikTok returned %s %s %s", resp.Status, code, msg)
	}
	return provider.Permanent("TikTok rejected the request (%s): %s %s", resp.Status, code, msg)
}
