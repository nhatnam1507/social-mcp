// Package youtube implements the YouTube Data API v3 provider.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/provider"
)

const (
	scopes        = "https://www.googleapis.com/auth/youtube.upload https://www.googleapis.com/auth/youtube.readonly"
	maxFileBytes  = 256 << 30
	defaultChunk  = 8 << 20
	chunkMultiple = 256 << 10
)

// Provider uploads to YouTube.
type Provider struct {
	ClientID     string
	ClientSecret string
	AuthBase     string
	TokenURL     string
	APIBase      string
	UploadBase   string
	ChunkSize    int64
	HTTP         *http.Client
}

var _ provider.Provider = (*Provider)(nil)

// New returns a provider for a Google OAuth desktop client.
func New(clientID, clientSecret string) *Provider {
	return &Provider{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthBase:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		APIBase:      "https://www.googleapis.com/youtube/v3",
		UploadBase:   "https://www.googleapis.com/upload/youtube/v3",
		ChunkSize:    defaultChunk,
		HTTP:         &http.Client{Timeout: 10 * time.Minute},
	}
}

func (p *Provider) Platform() string     { return "youtube" }
func (p *Provider) RedirectHost() string { return "127.0.0.1" }
func (p *Provider) Refreshable() bool    { return true }

func (p *Provider) AuthURL(state, verifier, redirectURI string) string {
	q := url.Values{
		"client_id":             {p.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {scopes},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {state},
		"code_challenge":        {oauth.ChallengeS256Base64URL(verifier)},
		"code_challenge_method": {"S256"},
	}
	return p.AuthBase + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func (p *Provider) Exchange(ctx context.Context, code, verifier, redirectURI string) (provider.Token, error) {
	return p.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
}

func (p *Provider) Refresh(ctx context.Context, tok provider.Token) (provider.Token, error) {
	if tok.RefreshToken == "" {
		return provider.Token{}, provider.ErrReauthRequired
	}
	fresh, err := p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}})
	if err != nil {
		return fresh, err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	return fresh, nil
}

func (p *Provider) token(ctx context.Context, form url.Values) (provider.Token, error) {
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
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
		return provider.Token{}, fmt.Errorf("token endpoint returned %s", resp.Status)
	}
	if tr.Error == "invalid_grant" {
		return provider.Token{}, fmt.Errorf("%w: %s", provider.ErrReauthRequired, tr.Description)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return provider.Token{}, &provider.RateLimitError{RetryAfter: provider.RetryAfter(resp.Header, time.Minute), Msg: tr.Description}
	}
	if resp.StatusCode >= 500 {
		return provider.Token{}, fmt.Errorf("Google token endpoint returned %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		return provider.Token{}, provider.Permanent("Google token endpoint: %s %s %s", resp.Status, tr.Error, tr.Description)
	}
	t := provider.Token{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken}
	if tr.ExpiresIn > 0 {
		t.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return t, nil
}

func (p *Provider) Identities(ctx context.Context, tok provider.Token) (string, []provider.Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.APIBase+"/channels?part=snippet&mine=true&maxResults=50", nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return "", nil, apiError(resp, body)
	}
	var out struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", nil, err
	}
	if len(out.Items) == 0 {
		return "", nil, errors.New("this Google account has no YouTube channel; create one at youtube.com first")
	}
	var ids []provider.Identity
	var channelIDs []string
	for _, it := range out.Items {
		ids = append(ids, provider.Identity{ExternalID: it.ID, DisplayName: it.Snippet.Title})
		channelIDs = append(channelIDs, it.ID)
	}
	sort.Strings(channelIDs)
	return strings.Join(channelIDs, ","), ids, nil
}

func (p *Provider) Capabilities(map[string]string) provider.Capabilities {
	return provider.Capabilities{
		PublishMode:   provider.PublishDirect,
		PrivacyLevels: []string{provider.PrivacyPrivate, provider.PrivacyUnlisted, provider.PrivacyPublic},
		Restrictions:  []string{"quota_100_uploads_per_day_per_project"},
		MaxFileBytes:  maxFileBytes,
		Formats:       []string{provider.FormatMP4, provider.FormatMOV, provider.FormatWebM, provider.FormatMKV},
	}
}

func (p *Provider) Options() []provider.OptionSpec {
	return []provider.OptionSpec{
		{Name: "category_id", Kind: "string", Description: "YouTube video category ID (default 22, People & Blogs)"},
		{Name: "made_for_kids", Kind: "bool", Description: "whether the video is made for kids (default false)"},
	}
}

func (p *Provider) ValidateMeta(m provider.VideoMeta) error {
	switch {
	case strings.TrimSpace(m.Title) == "":
		return errors.New("title is required")
	case utf8.RuneCountInString(m.Title) > 100:
		return errors.New("title must be at most 100 characters")
	case strings.ContainsAny(m.Title, "<>"):
		return errors.New("title must not contain < or >")
	case len(m.Description) > 5000:
		return errors.New("description must be at most 5000 bytes")
	}
	return nil
}

// apiError maps a Google API error response.
func apiError(resp *http.Response, body []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	json.Unmarshal(body, &e)
	reason := ""
	if len(e.Error.Errors) > 0 {
		reason = e.Error.Errors[0].Reason
	}
	msg := strings.TrimSpace(reason + ": " + e.Error.Message)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", provider.ErrUnauthorized, msg)
	case resp.StatusCode == http.StatusTooManyRequests || reason == "rateLimitExceeded" || reason == "userRateLimitExceeded":
		return &provider.RateLimitError{RetryAfter: provider.RetryAfter(resp.Header, time.Minute), Msg: msg}
	case resp.StatusCode >= 500:
		return fmt.Errorf("YouTube returned %s %s", resp.Status, msg)
	default:
		return provider.Permanent("YouTube rejected the request (%s): %s", resp.Status, msg)
	}
}
