---
title: "Phase 1: App identity and token endpoint seam"
status: done
priority: P1
effort: 5h
dependencies: []
---

# Phase 1: App identity and token endpoint seam

## Goal

Move every use of an app secret out of the providers into a new `internal/appauth` package that resolves the
app identity (embedded or env override) and performs secret-bearing token calls behind one interface.

## Key Insights

- Secrets are used in exactly three places today:
  - `internal/provider/youtube/youtube.go` `token()`: POST form, `client_id` + `client_secret` (exchange, refresh).
  - `internal/provider/tiktok/tiktok.go` `token()`: POST form, `client_key` + `client_secret` (exchange, refresh).
  - `internal/provider/meta/oauth.go` `Exchange()`: two GETs to `/oauth/access_token` with `client_id` +
    `client_secret` (code exchange, then `fb_exchange_token`).
- Response parsing and error mapping are platform-specific and already well tested. Keep them in the
  providers; the seam only adds credentials and returns the raw platform response. A future remote broker
  returns the same raw response, so providers never change again.
- Authorize URLs need only the public client ID and stay in the providers.

## Architecture

```go
package appauth

// Set at release build time with -ldflags "-X .../internal/appauth.youtubeClientID=...". Empty in source builds.
var (
    youtubeClientID, youtubeClientSecret                       string
    tiktokClientKey, tiktokClientSecret                        string
    metaAppID, metaAppSecret, metaLoginConfigID, metaAppMode    string
)

type Source string // "embedded" or "env"

// App is one platform's resolved identity. The secret is unexported and only used by TokenEndpoint.
type App struct {
    ClientID      string // client_id / client_key / app_id: public
    LoginConfigID string // Meta only
    AppMode       string // Meta only: "development" | "live"
    Source        Source
    secret        string
}

// Apps holds the identities available in this build; a missing platform is not usable.
type Apps struct{ YouTube, TikTok, Meta *App }

// Resolve applies, per platform: all env vars set → env; some set → error naming the missing ones;
// none set → embedded values when present; otherwise nil.
func Resolve(getenv func(string) string) (Apps, error)

// TokenEndpoint performs a token call that needs the app secret. Callers pass only grant parameters
// (code, verifier, redirect_uri, refresh_token, fb_exchange_token, ...); the endpoint adds the app
// credentials and returns the platform's raw response for the provider to parse.
type TokenEndpoint interface {
    Do(ctx context.Context, params url.Values) (*http.Response, error)
}

func (a *App) YouTubeTokenEndpoint(tokenURL string, hc *http.Client) TokenEndpoint // POST form, client_id
func (a *App) TikTokTokenEndpoint(tokenURL string, hc *http.Client) TokenEndpoint  // POST form, client_key
func (a *App) MetaTokenEndpoint(tokenURL string, hc *http.Client) TokenEndpoint    // GET query, client_id
```

Env override variables (all-or-nothing per platform):

| Platform | Variables |
|---|---|
| YouTube | `SOCIAL_MCP_YOUTUBE_CLIENT_ID`, `SOCIAL_MCP_YOUTUBE_CLIENT_SECRET` |
| TikTok | `SOCIAL_MCP_TIKTOK_CLIENT_KEY`, `SOCIAL_MCP_TIKTOK_CLIENT_SECRET` |
| Meta | `SOCIAL_MCP_META_APP_ID`, `SOCIAL_MCP_META_APP_SECRET`, `SOCIAL_MCP_META_LOGIN_CONFIG_ID`, optional `SOCIAL_MCP_META_APP_MODE` (default `development`) |

`metaAppMode` defaults to `development` when empty; any value other than `development`/`live` is a resolve
error. The Meta Graph version becomes a constant (`v26.0`) in `internal/provider/meta`.

## Files to Create / Modify

- Create: `internal/appauth/appauth.go`: build vars, `App`, `Apps`, `Resolve`.
- Create: `internal/appauth/endpoint.go`: the three embedded `TokenEndpoint` implementations.
- Create: `internal/appauth/appauth_test.go`
- Modify: `internal/provider/youtube/youtube.go`: drop `ClientSecret`; `New(clientID string, tokens appauth.TokenEndpoint)`; `token()` calls `tokens.Do` and keeps its parsing.
- Modify: `internal/provider/tiktok/tiktok.go`: same pattern with `client_key`.
- Modify: `internal/provider/meta/client.go`, `internal/provider/meta/oauth.go`: drop `AppSecret` from `Config`; add `Tokens appauth.TokenEndpoint`; split the Graph response decode out of `Get` so `Exchange` can parse a `TokenEndpoint` response with the same error mapping; `GraphVersion` constant.
- Modify: `internal/provider/*/…_test.go`: construct providers with a test endpoint built from `appauth` against `httptest` servers; keep the assertions that the secret reaches the platform form.
- Modify: `internal/daemon/providers.go`: `buildProviders(apps appauth.Apps, log)`; register only platforms whose `App` is non-nil.
- Modify: `internal/daemon/daemon.go`: call `appauth.Resolve(os.Getenv)` at startup; fail startup on a partial override with the missing variable names; log each platform's source (`embedded`/`env`/`unavailable`), never values.

## Tasks & Steps

1. Write `appauth_test.go` first: resolve precedence (env full, env partial → error, embedded, none), Meta app mode validation, and that each endpoint sends the right method, URL, credential parameter names and grant params.
2. Implement `appauth` to pass the tests. Embedded endpoints set `Content-Type` and use the supplied `*http.Client`.
3. Refactor YouTube and TikTok `token()` to build `url.Values` of grant params and call `p.Tokens.Do`. Keep the response decode, rate-limit, `invalid_grant` and 5xx handling unchanged.
4. Refactor Meta: extract `decodeGraph(resp, out)` from `Client.Get`; `Exchange` uses `c.Tokens.Do` for both calls and `decodeGraph` for both responses.
5. Rename the "rejected the app credentials" messages (TikTok `invalid_client`, Google `invalid_client`, Meta OAuthException code 1 / 101 on the token call) to one shared `provider.ErrAppCredentials` wrapped as Permanent, with the text: "the platform rejected this social-mcp build's app credentials; run `social-mcp update` (or check your SOCIAL_MCP_* override)".
6. Wire `buildProviders` and daemon startup.
7. Add a test that a token-call error string never contains the secret (extend the existing `TestTransportErrorDoesNotLeakSecret` pattern to YouTube and TikTok).

## Verification

- `go test ./internal/appauth/... ./internal/provider/... ./internal/daemon/...`
- `grep -rn "ClientSecret\|AppSecret\|client_secret\|app_secret" internal/provider` returns only test fixtures that assert the endpoint adds the secret.
- `make lint`

## Success Criteria

- Providers hold only public client IDs and a `TokenEndpoint`.
- Swapping the embedded endpoint for another `TokenEndpoint` implementation needs no provider change.

## Risk Assessment

- **Meta error mapping drift** when splitting `Get`: covered by keeping `meta_test.go` cases green.
- **Partial override foot-gun:** a user sets only a client ID. Mitigated by the startup error listing missing vars.

## Security Considerations

- The secret field is unexported; `App` has a `String`/`LogValue` that prints only the platform, client ID and source.
- No secret, code, verifier or token is logged; endpoints never log request or response bodies.

## Next Steps

Phase 2 removes the config sections now unused; phase 3 records which client ID each login used.
