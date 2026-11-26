package appauth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenEndpoint performs one token call that needs the app secret: a code exchange, a refresh
// or Meta's long-lived token swap. Callers pass only the grant parameters; the endpoint adds the
// app's credentials and returns the platform's raw response for the caller to parse.
//
// The embedded endpoint calls the platform directly. A hosted broker that keeps the secrets off
// users' machines can implement the same interface without any change to the providers.
type TokenEndpoint interface {
	Do(ctx context.Context, params url.Values) (*http.Response, error)
}

var tokenHTTP = &http.Client{Timeout: time.Minute}

type directEndpoint struct {
	url     string
	idParam string
	app     *App
}

// TokenEndpoint returns an endpoint that POSTs to tokenURL with this app's credentials in the form
// body, never in the URL, so they cannot appear in transport errors or proxy logs.
func (a *App) TokenEndpoint(tokenURL string) TokenEndpoint {
	idParam := "client_id"
	if a.Platform == TikTok {
		idParam = "client_key"
	}
	return &directEndpoint{url: tokenURL, idParam: idParam, app: a}
}

func (e *directEndpoint) Do(ctx context.Context, params url.Values) (*http.Response, error) {
	form := url.Values{}
	for k, v := range params {
		form[k] = v
	}
	form.Set(e.idParam, e.app.ClientID)
	form.Set("client_secret", e.app.secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return tokenHTTP.Do(req)
}
