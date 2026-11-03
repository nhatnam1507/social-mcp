package meta

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

// AuthURL builds the Facebook Login for Business dialog URL. The configuration ID selects
// the permissions, so no scope is sent.
func (c *Client) AuthURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {c.AppID},
		"redirect_uri":  {redirectURI},
		"state":         {state},
		"config_id":     {c.LoginConfigID},
		"response_type": {"code"},
	}
	return c.DialogBase + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// Exchange trades the code for a short-lived user token, then for a long-lived one (~60 days).
func (c *Client) Exchange(ctx context.Context, code, redirectURI string) (provider.Token, error) {
	var short tokenResponse
	if err := c.Get(ctx, c.GraphBase, "/oauth/access_token", url.Values{
		"client_id": {c.AppID}, "client_secret": {c.AppSecret}, "redirect_uri": {redirectURI}, "code": {code},
	}, "", &short); err != nil {
		return provider.Token{}, err
	}
	var long tokenResponse
	if err := c.Get(ctx, c.GraphBase, "/oauth/access_token", url.Values{
		"grant_type": {"fb_exchange_token"}, "client_id": {c.AppID}, "client_secret": {c.AppSecret}, "fb_exchange_token": {short.AccessToken},
	}, "", &long); err != nil {
		return provider.Token{}, err
	}
	if long.AccessToken == "" {
		return provider.Token{}, errors.New("Meta returned no long-lived token")
	}
	t := provider.Token{AccessToken: long.AccessToken}
	if long.ExpiresIn > 0 {
		t.Expiry = time.Now().Add(time.Duration(long.ExpiresIn) * time.Second)
	}
	return t, nil
}

// Me returns the Facebook user ID behind a user token.
func (c *Client) Me(ctx context.Context, tok provider.Token) (string, error) {
	var me struct {
		ID string `json:"id"`
	}
	if err := c.Get(ctx, c.GraphBase, "/me", url.Values{"fields": {"id"}}, tok.AccessToken, &me); err != nil {
		return "", err
	}
	if me.ID == "" {
		return "", errors.New("Meta returned no user id")
	}
	return me.ID, nil
}
