# Maintainer: OAuth apps and release secrets

This page is for the maintainer of social-mcp. Users don't register anything: release binaries carry the
maintainer's OAuth app identity, and users only run `social-mcp account add <platform>`.

## How it fits together

- `internal/appauth` is the only code that uses app secrets. It resolves each platform's identity and performs
  the token calls that need the secret (code exchange, refresh, Meta's long-lived token swap) through the
  `appauth.TokenEndpoint` interface. Providers only see the public client ID.
- Release builds get the identity at build time: `.goreleaser.yaml` passes `-ldflags -X` values from
  environment variables that `.github/workflows/release.yml` fills from repository secrets. Nothing is
  committed to the repository.
- Source builds (`go install`, `make build`) carry no identity. Developers and power users supply one at
  runtime with the `SOCIAL_MCP_*` variables below; `make run` loads them from `./.env`.
- User tokens stay on the user's machine. They only travel between the daemon and the platform.

| Platform | App status | Variables |
|---|---|---|
| YouTube | Registered; required for every release | `SOCIAL_MCP_YOUTUBE_CLIENT_ID`, `SOCIAL_MCP_YOUTUBE_CLIENT_SECRET` |
| TikTok | Not registered yet; optional | `SOCIAL_MCP_TIKTOK_CLIENT_KEY`, `SOCIAL_MCP_TIKTOK_CLIENT_SECRET` |
| Meta (Facebook, Instagram) | Not registered yet; optional | `SOCIAL_MCP_META_APP_ID`, `SOCIAL_MCP_META_APP_SECRET`, `SOCIAL_MCP_META_LOGIN_CONFIG_ID`, `SOCIAL_MCP_META_APP_MODE` (`development` or `live`, default `development`) |

A platform is available only when all of its required values are set. A release without an optional
platform's secrets simply doesn't offer that platform; `account add` then explains how to use your own app.

### Accepted risk

The secrets ship inside every release binary and can be extracted from it. Google treats Desktop-app client
secrets as public, so this is expected for YouTube. Meta and TikTok ask apps to keep their secrets
confidential, so embedding theirs goes against their terms; this was accepted while the user base is small.
Before Meta App Review or a wider launch, move TikTok and Meta to a hosted broker: implement
`appauth.TokenEndpoint` with an HTTPS call to a service that holds the secrets, use it for those platforms in
`internal/daemon/providers.go`, then rotate their secrets so the leaked ones stop working.

## Add the secrets to GitHub Actions

1. Open the repository on GitHub, then **Settings > Secrets and variables > Actions**.
2. On the **Secrets** tab, choose **New repository secret**.
3. Add each value with exactly these names (for now only the YouTube pair is required):
   - `SOCIAL_MCP_YOUTUBE_CLIENT_ID`: the OAuth client ID, ending in `.apps.googleusercontent.com`
   - `SOCIAL_MCP_YOUTUBE_CLIENT_SECRET`: the client secret, starting with `GOCSPX-`
4. Later, add the TikTok and Meta names from the table above once those apps exist, and add the required
   ones to the "check the app identity secrets" step in `.github/workflows/release.yml`.

With the GitHub CLI, `gh secret set SOCIAL_MCP_YOUTUBE_CLIENT_SECRET` prompts for the value, so it stays out of
your shell history.

Every tag release first checks the required secrets and fails before building if any is missing. GitHub masks
secret values in logs; the check prints only names.

## Develop locally with your app

```sh
cp .env.example .env    # git-ignored; fill in SOCIAL_MCP_YOUTUBE_CLIENT_ID and _SECRET
make run                # daemon in the foreground against ./.dev, signing in through your app
make run ARGS="account add youtube"
```

`make run` sources `./.env` into the daemon's environment. The startup log shows each platform's identity
source (`embedded`, `env`, or unavailable) and the client ID, never the secret. To run the dev daemon next to
an installed one, also set `SOCIAL_MCP_LISTEN_PORT` in `.env` (development only; Meta sign-in needs 8765).

## Register the apps

Every app uses the fixed loopback port 8765.

### YouTube

1. In the Google Cloud console, create a project and enable **YouTube Data API v3**.
2. Configure the **OAuth consent screen** with the scopes `youtube.upload` and `youtube.readonly`.
3. Set its publishing status to **In production**. In "Testing" status, logins expire after 7 days, and only
   listed test users can sign in. An unverified app shows a warning screen and is capped at 100 users;
   Google verification of the `youtube.upload` scope removes both.
4. Create an **OAuth client ID** of type **Desktop app**. Desktop clients accept any loopback redirect such as
   `http://127.0.0.1:8765/oauth/callback` without registering it.
5. Store the client ID and secret as described above.

All users share the project's YouTube Data API quota, which allows about 100 uploads per day. Request a quota
increase before many people use the release.

### TikTok

1. On developers.tiktok.com, create an app and add **Login Kit** and the **Content Posting API**.
2. Under Login Kit, choose the **Desktop** platform and add the redirect URI
   `http://127.0.0.1:8765/oauth/callback`.
3. Request the scopes `user.info.basic` and `video.upload`.
4. Until TikTok audits the app, use the sandbox; it allows up to 10 target users.

Uploads go to the creator's TikTok inbox as drafts; TikTok allows at most 5 pending drafts per 24 hours.
TikTok user IDs are per app, so accounts users connected through their own TikTok app must be connected again
(`account add tiktok`, then `account remove` the old one).

### Meta (Facebook Pages and Instagram Reels)

1. On developers.facebook.com, create a **Business** app and add **Facebook Login for Business**.
2. Create a **Login configuration** with a user access token granting `pages_show_list`,
   `pages_read_engagement`, `pages_manage_posts`, `instagram_basic` and `instagram_content_publish`. Its
   configuration ID is `SOCIAL_MCP_META_LOGIN_CONFIG_ID`.
3. Add `http://localhost:8765/oauth/callback` to **Valid OAuth Redirect URIs**. Meta matches it exactly.
4. Instagram accounts must be professional accounts linked to a Facebook Page.

In Development mode only people with a role on the app can see its posts (`visible_to_app_roles_only`). Going
Live needs App Review and Business Verification. Whether Meta accepts the `http://localhost` redirect in Live
mode has **not been verified**: test it before setting `SOCIAL_MCP_META_APP_MODE=live`. If it is rejected, a
hosted HTTPS redirect page is needed.

Facebook Page tokens don't expire. Instagram uses the user token, which expires after about 60 days
(`account reauth`).

## Rotate a secret

1. Create the new secret in the platform console (Google: add a secret to the client, then delete the old one).
2. Update the GitHub secret and cut a release.
3. Old binaries now fail token calls with "the platform rejected this social-mcp build's app credentials; run
   `social-mcp update`". The daemon's update notice also tells users and their agents about the new release.

Rotating while the secret still ships in the binary only helps until someone extracts the new one.

## Accounts from users' own apps

Before this change, users signed in through their own apps. Each login now records the client ID that issued
it. At startup the daemon marks logins from any other app as `needs_reauth`, because the current app can't
refresh them. Logins made before client IDs were recorded take the client ID from the user's old
`config.toml` when it is still there, so power users who set the same app through `SOCIAL_MCP_*` keep working.
