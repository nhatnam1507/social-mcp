# social-mcp

`social-mcp` lets AI agents upload videos to your social media accounts. It is one Go binary that runs as a
background daemon, keeps OAuth tokens for several accounts per platform on your machine, and exposes MCP
tools so an agent can see which accounts are connected and upload a local video to the ones you choose.

| Platform | What an upload does |
|---|---|
| YouTube | Uploads the video (private, unlisted or public) |
| TikTok | Sends the video to your TikTok inbox as a draft; you add the caption and post in the app |
| Facebook Pages | Uploads to a Page you manage (published, or unpublished for `private`) |
| Instagram Reels | Publishes a Reel on a professional account linked to a Page (always public) |

Tokens never leave your machine except to the platform they belong to. You register your own developer app
on each platform; see [docs/platform-setup.md](docs/platform-setup.md).

## Install

Download a binary for your OS from the CI build artifacts, or build from source with Go 1.26+:

```sh
go install github.com/nhatnam1507/social-mcp/cmd/social-mcp@latest
```

Supported: Linux, macOS and Windows 10 1803+ on amd64 and arm64. Build every target with `scripts/build-all.sh`.

## Quick start

```sh
social-mcp config init            # writes config.toml; `social-mcp config path` shows where
# edit config.toml: add the credentials of the platforms you use (docs/platform-setup.md)
social-mcp daemon start
social-mcp account add youtube    # opens the browser to sign in; repeat for each account
social-mcp config mcp             # prints the MCP endpoint, token and client setup commands
```

Then add the server to your MCP client. For Claude Code, run the `claude mcp add --transport http ...`
command that `config mcp` prints. Clients that launch stdio servers can run `social-mcp mcp` instead.

Now ask your agent something like *"upload /home/me/Videos/launch.mp4 to my YouTube channel as unlisted"*.
The agent calls `list_accounts`, then `upload_video`, then polls `get_upload_status` until it finishes.

## CLI

| Command | Purpose |
|---|---|
| `daemon start` / `stop` | Start in the background / stop (running uploads resume on the next start) |
| `daemon status [--json]` | Show status; exit code 3 when stopped |
| `daemon logs [-n N] [-f]` | Show or follow the daemon log |
| `daemon run` | Run in the foreground (for systemd, launchd or other service managers) |
| `account add <youtube\|tiktok\|facebook\|instagram> [--no-browser]` | Connect accounts through browser sign-in |
| `account list [--platform P] [--json]` | List accounts with health and restrictions |
| `account reauth <id>` | Sign in again when an account shows `needs_reauth` or `expiring` |
| `account remove <id> [--yes]` | Delete an account and its stored tokens |
| `config init [--force]` / `config path` | Write the starter config / show file locations |
| `config mcp [--rotate]` | Show the MCP endpoint and token, or generate a new token (then restart the daemon) |
| `mcp` | Serve MCP over stdio, backed by the running daemon |
| `version` | Print the version |

Environment variables: `SOCIAL_MCP_HOME` overrides the home directory (default: your user config directory
plus `social-mcp`). `SOCIAL_MCP_KEYRING=off` makes a new home keep its encryption key in a file instead of the
OS keyring (useful on headless machines).

## MCP tools

| Tool | Description |
|---|---|
| `list_accounts` | Connected accounts with platform, health, privacy levels, restrictions and accepted options |
| `upload_video` | Upload one local file to one or more accounts; returns a job per account and any rejections |
| `get_upload_status` | Status, progress, outcome and URL of a job |
| `list_uploads` | Recent jobs, filterable by status and account |
| `cancel_upload` | Cancel a job that has not started publishing |

Uploads run in the background and survive daemon restarts. Job status is one of `queued`, `uploading`,
`succeeded`, `failed`, `cancelled` or `needs_review`. `needs_review` means the platform may or may not have
published the video, so check it before uploading again. `social-mcp` never repeats a step that could
publish twice.

## Security model

- Tokens are encrypted with AES-256-GCM in a local SQLite database. The key is in the OS keyring, or in a
  `0600` key file when no keyring is available. Windows relies on your user profile's permissions.
- The MCP endpoint listens only on `127.0.0.1`/`[::1]`. It requires the bearer token and rejects foreign
  `Host` and `Origin` headers, which blocks DNS rebinding and web pages.
- Agents cannot connect or remove accounts; only the CLI can.
- `upload_video` is marked as a destructive, open-world tool, so MCP clients ask before running it. That
  permission prompt, which shows the file path, accounts and privacy, is your approval step. If you set the
  tool to always-allowed, the agent can publish without asking.
- Privacy defaults to `private`. An account that cannot honor the requested privacy is rejected rather than
  published more widely; for example, Instagram Reels require an explicit `public`.
- Only real video files (MP4, MOV, WebM, MKV) are accepted, and a file that changes between submission and
  upload is refused. Set `uploads.allowed_dirs` to restrict which folders agents may upload from.
- `account remove` deletes local tokens only. To revoke the app's access completely, also remove it in the
  platform's connected-apps settings.

## Platform limits

These were verified against platform documentation on 2026-11-02:

- **YouTube:** the API allows 100 uploads per day per Google Cloud project. While the OAuth consent screen is
  in "Testing" status, logins expire after 7 days, so set it to "In production".
- **TikTok:** apps for personal use can't get public Direct Post, so uploads go to the inbox as drafts. TikTok
  allows at most 5 pending drafts per 24 hours and videos up to 10 minutes.
- **Facebook:** posts to Pages only, not personal profiles. Posts made while the Meta app is in Development
  mode are visible only to people with a role on the app (shown as `visible_to_app_roles_only`).
- **Instagram:** Reels are always public and limited per account per 24 hours. Instagram uses the user token,
  so you need to run `account reauth` about every 60 days.

## Documentation

- [Platform setup](docs/platform-setup.md): register the developer apps and fill in `config.toml`
- [Architecture](docs/architecture.md): components, storage and the upload lifecycle

## Development

```sh
go test ./...                       # unit and contract tests
go test -tags e2e ./e2e/...         # builds the binary and drives it end to end
bash scripts/build-all.sh           # six static binaries plus SHA256SUMS
```
