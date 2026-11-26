package meta

import (
	"context"
	"errors"
	"net/url"
	"strings"
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
	if err := c.token(ctx, url.Values{"redirect_uri": {redirectURI}, "code": {code}}, &short); err != nil {
		return provider.Token{}, err
	}
	var long tokenResponse
	if err := c.token(ctx, url.Values{"grant_type": {"fb_exchange_token"}, "fb_exchange_token": {short.AccessToken}}, &long); err != nil {
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

// token calls the OAuth token endpoint, which adds the app's credentials.
func (c *Client) token(ctx context.Context, params url.Values, out *tokenResponse) error {
	resp, err := c.Tokens.Do(ctx, params)
	if err != nil {
		return err
	}
	err = decode(resp, out)
	var ge *GraphError
	// 101: invalid application ID; 1 with "client secret": the app secret was rejected.
	if errors.As(err, &ge) && (ge.Code == 101 || (ge.Code == 1 && strings.Contains(strings.ToLower(ge.Message), "client secret"))) {
		return provider.AppCredentials("Meta", ge.Message)
	}
	return err
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
