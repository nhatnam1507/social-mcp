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

Tokens never leave your machine except to the platform they belong to. There is nothing to register:
social-mcp signs you in through its own app on each platform.

## Install

Linux and macOS:

```sh
curl -fsSL https://github.com/nhatnam1507/social-mcp/releases/latest/download/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://github.com/nhatnam1507/social-mcp/releases/latest/download/install.ps1 | iex
```

The script picks the binary for your OS and CPU from the latest release, checks it against the release's
`SHA256SUMS`, and installs it to `~/.local/bin` (Windows: `%LOCALAPPDATA%\Programs\social-mcp`, added to your
user PATH). No sudo or admin rights are needed. Set `SOCIAL_MCP_VERSION` (for example `v0.1.0`) to install a
specific release, or `SOCIAL_MCP_INSTALL_DIR` to choose the folder.

To upgrade later, run `social-mcp update` (running the install command again also works). It installs the
latest release over the current binary, verified against `SHA256SUMS`, and restarts the daemon if it was
running. When a new release comes out, social-mcp tells you once in the terminal and once through your agent.

**Upgrading from a release that used your own developer apps:** the platform sections in `config.toml` are now
ignored (the daemon lists them in a warning; you can delete them), and the listener always uses port 8765.
Accounts you connected through your own apps show `needs_reauth`. Run `social-mcp account reauth <id>` for
YouTube, Facebook and Instagram. TikTok gives every app its own user IDs, so run
`social-mcp account add tiktok` and then `social-mcp account remove <old-id>`.

**Using your own app:** a platform's `SOCIAL_MCP_*` variables replace the built-in app for the daemon, for
example `SOCIAL_MCP_YOUTUBE_CLIENT_ID` and `SOCIAL_MCP_YOUTUBE_CLIENT_SECRET`. The full list and how to register
an app are in [docs/maintainer-oauth-apps.md](docs/maintainer-oauth-apps.md). The daemon reads them when it
starts, so start it with `social-mcp daemon start` from a shell where they are set: a daemon that your AI app
starts for you sees only the app's environment and uses the built-in app.

You can also download a binary from the [releases page](https://github.com/nhatnam1507/social-mcp/releases),
or build from source with Go 1.26.1 or newer:

```sh
go install github.com/nhatnam1507/social-mcp/cmd/social-mcp@latest
```

Supported: Linux, macOS and Windows 10 1803+ on amd64 and arm64.

## Quick start

The installer ends with these three steps:

```sh
social-mcp daemon start
social-mcp account add youtube          # opens the browser to sign in; repeat for each account
social-mcp connect claude-desktop       # or: claude-code, cursor, codex, antigravity
```

`connect` adds social-mcp to the app once:

| App | What `connect` changes | Afterwards |
|---|---|---|
| `claude-desktop` | `claude_desktop_config.json` (found in any of the folders Claude Desktop versions use) | Quit Claude Desktop completely and open it again |
| `claude-code` | Runs `claude mcp add --scope user` | New sessions connect; in an open one, `/mcp` then reconnect |
| `cursor` | `~/.cursor/mcp.json` | Restart Cursor and check Cursor Settings > MCP |
| `codex` | `~/.codex/config.toml` (or `$CODEX_HOME`), used by the Codex CLI and IDE extension | Start a new Codex session |
| `antigravity` | `~/.gemini/config/mcp_config.json` | Restart Antigravity, or refresh under Manage MCP Servers |

Your other servers and settings stay as they are, and the old file is saved next to it with a timestamped
`.bak` name. An earlier `social-mcp` entry, such as an HTTP or `mcp-remote` one, is replaced. If the app's
settings are not found, open the app once and run `connect` again, or pass the file with
`--config-file <path>`.

For any other app, `social-mcp connect` (with no app) prints what to paste: an `mcpServers` entry with your
binary's real path. You can also ask an agent that can run commands to run `social-mcp connect` and connect
social-mcp for you.

The entry tells the app to start `social-mcp relay`, a small relay that passes the app's messages to the daemon.
You never run it yourself. The daemon already serves MCP, but most apps reach a local server by starting a
program, and the relay also starts the daemon when it is not running (also mid-session, if the daemon was
stopped). So you connect only once and never need the daemon running before the app.

**social-mcp in WSL, apps on Windows:** run `connect` inside WSL. For the desktop apps (Claude Desktop,
Cursor, Antigravity) it edits the settings in your Windows profile and adds an entry that runs
`wsl.exe -d <distro> -- <path>/social-mcp relay`, so the Windows app reaches social-mcp in WSL without Node.js
or a token. Claude Code and Codex are terminal tools, so `connect` sets them up inside WSL, next to
social-mcp. Agents on Windows can name files the Windows way (`C:\Users\me\Videos\launch.mp4`); the daemon
reads them under `/mnt/c/...`.

`social-mcp connect` also shows the HTTP endpoint and bearer token for apps that can only connect over HTTP
(`--rotate-token` replaces the token). An HTTP connection works only when the daemon is already running as
the app starts; most apps do not retry later (in Claude Code, `/mcp` then **Reconnect**). If you connected
through HTTP or `mcp-remote` before, run `social-mcp connect <app>` (it replaces the old `social-mcp` entry)
or replace the entry with the one `social-mcp connect` prints. The older `social-mcp config mcp` still works
and prints the same as `social-mcp connect`.

Now ask your agent something like *"upload /home/me/Videos/launch.mp4 to my YouTube channel as unlisted"*.
The agent calls `list_accounts`, then `upload_video`, which returns the queued job IDs straight away. Uploads keep running in the daemon; the agent checks `get_upload_status` or `list_uploads` only when you ask about progress or results.

## CLI

| Command | Purpose |
|---|---|
| `daemon start` / `stop` | Start in the background / stop (running uploads resume on the next start) |
| `daemon status [--json]` | Show status; exit code 3 when stopped, 4 when running but its control socket does not answer |
| `daemon logs [-n N] [-f]` | Show or follow the daemon log |
| `daemon run` | Run in the foreground for systemd, launchd or other service managers; logs go to stderr, not `daemon.log` |
| `account add <youtube\|tiktok\|facebook\|instagram> [--no-browser]` | Connect accounts through browser sign-in |
| `account list [--platform P] [--json]` | List accounts with health and restrictions |
| `account reauth <id>` | Sign in again when an account shows `needs_reauth` or `expiring` |
| `account remove <id> [--yes]` | Delete an account and its stored tokens |
| `config init [--force]` / `config path` | Write the starter config / show file locations |
| `connect <claude-desktop\|claude-code\|cursor\|codex\|antigravity> [--config-file F]` | Connect social-mcp to an AI app once (inside WSL, desktop apps are set up on the Windows side) |
| `connect [--rotate-token]` | Show the settings to paste into any other AI app, plus the HTTP endpoint and token |
| `version` | Print the version |
| `update [--version TAG]` | Install the latest release (or `TAG`) in place and restart a running daemon |

Environment variables: `SOCIAL_MCP_HOME` overrides the home directory (default: your user config directory
plus `social-mcp`). `SOCIAL_MCP_KEYRING=off` makes a new home keep its encryption key in a file instead of the
OS keyring (useful on headless machines). `SOCIAL_MCP_NO_UPDATE_CHECK=1` turns off the daily update check, as
does `check = false` under `[updates]` in `config.toml`; `social-mcp update` still works.

## MCP tools

| Tool | Description |
|---|---|
| `list_accounts` | Connected accounts with platform, health, privacy levels, restrictions and accepted options |
| `upload_video` | Upload one local file to one or more accounts; returns a job per account and any rejections |
| `get_upload_status` | Status, progress, outcome and URL of a job |
| `list_uploads` | Recent jobs, filterable by status and account |
| `cancel_upload` | Cancel a job that has not started publishing |

`list_accounts` also returns a one-time `notice` when a new social-mcp release is available, so the agent can
pass it on.

Uploads run in the background and survive daemon restarts. Job status is one of `queued`, `uploading`,
`succeeded`, `failed`, `cancelled` or `needs_review`. `needs_review` means the platform may or may not have
published the video, so check it before uploading again. `social-mcp` never repeats a step that could
publish twice.

## Security model

- Tokens are encrypted with AES-256-GCM in a local SQLite database. The key is in the OS keyring, or in a
  `0600` key file when no keyring is available.
- The home directory is private to you. On Unix it must be owned by you and is kept at mode `0700`. On
  Windows it gets an access list that allows only your account, even when `SOCIAL_MCP_HOME` points outside
  your profile.
- The CLI talks to the daemon over a local socket in the home directory. When that path would be too long for
  the OS, the socket goes in a private per-user folder in the temp directory. On Unix the CLI refuses a socket
  owned by another user. On Windows the folder name is derived from your account's SID.
- On Windows, the access list is only applied when `SOCIAL_MCP_HOME` is a directory used only by social-mcp; a
  shared directory is left unchanged with a warning.
- OAuth sign-in uses PKCE and a one-time `state`, and the platform redirects only to the loopback callback on
  port 8765, so login codes land on your own machine. Release binaries embed social-mcp's app identity; the
  app secret is extractable from the binary, which gives no access to your accounts or tokens.
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

- **YouTube:** the API allows 100 uploads per day per Google Cloud project, shared by everyone using the
  built-in app.
- **TikTok:** apps for personal use can't get public Direct Post, so uploads go to the inbox as drafts. TikTok
  allows at most 5 pending drafts per 24 hours and videos up to 10 minutes.
- **Facebook:** posts to Pages only, not personal profiles. Posts made while the Meta app is in Development
  mode are visible only to people with a role on the app (shown as `visible_to_app_roles_only`).
- **Instagram:** Reels are always public and limited per account per 24 hours. Instagram uses the user token,
  so you need to run `account reauth` about every 60 days.

## Documentation

- [Maintainer: OAuth apps and release secrets](docs/maintainer-oauth-apps.md): how the built-in apps are
  registered, how releases get their secrets, and how to use your own app
- [Architecture](docs/architecture.md): components, storage and the upload lifecycle

## Development

Everything runs through `make`. You need Go (version from `go.mod`), GNU make and a POSIX shell (Git Bash or
WSL on Windows); `make lint` also needs `shellcheck`. Run `make` to list the targets:

| Target | What it does |
|---|---|
| `make test` | `go vet`, unit tests and the end-to-end test; this is what CI runs |
| `make test-unit` / `make test-e2e` | Only the unit tests (with the race detector when a C compiler is available) / only the end-to-end test |
| `make vet-cross` | Compile-check the Windows and macOS code from any machine |
| `make lint` | `shellcheck` the installer script |
| `make build` | Build `./social-mcp` for this machine |
| `make run` | Run the daemon in the foreground against a separate dev home (see below) |
| `make dist` | Build every release target into `dist/` (binaries in `dist/bin/`, plus `SHA256SUMS`) without publishing |
| `make release-check` | Validate `.goreleaser.yaml` |
| `make clean` | Remove `dist/` and `./social-mcp` |

`make dist` and `make release-check` run [GoReleaser](https://goreleaser.com) at the version pinned in the
`Makefile` through `go run`, so there is nothing to install; the first run compiles it (and downloads the newer
Go toolchain GoReleaser needs), later runs are fast.

### Running locally

`make run` builds the binary and runs `social-mcp daemon run` with `SOCIAL_MCP_HOME=./.dev` and
`SOCIAL_MCP_KEYRING=off`, so development never touches your real config, accounts or OS keyring. The first run
writes `./.dev/config.toml`. Source builds carry no app identity, so `make run` loads your development app from
`./.env`: copy `.env.example` to `.env` (git-ignored) and fill in the platforms you work on (see
[Maintainer: OAuth apps](docs/maintainer-oauth-apps.md)). Run any other command against the same dev home with
`ARGS`:

```sh
make run                              # daemon in the foreground; Ctrl+C stops it
make run ARGS="account add youtube"   # connect a test account to the dev home
make run ARGS="connect"               # agent setup, MCP endpoint and token of the dev daemon
```

The dev home listens on port 8765 like a normal install. Stop your real daemon first, or set
`SOCIAL_MCP_LISTEN_PORT` in `.env` (development only; Meta sign-in needs 8765).

### CI

- **pr-gate** runs on every pull request and must pass before merging: `make test` on Linux, `make vet-cross`,
  `make lint` and `make release-check`. A pull request that only changes Markdown, `docs/` or `plans/` skips
  these and passes.
- **ci** runs on every push to `main`: `make test` on Linux, macOS and Windows, then `make dist` only when all
  of them pass.

Both run the tests through the shared action in `.github/actions/test`.

### Releasing

Push a version tag; the `release` workflow runs ci, then GoReleaser builds every target with the tag as its
version and publishes a GitHub Release with the binaries, `SHA256SUMS`, `install.sh`, `install.ps1` and release
notes grouped from conventional commits (`feat:`, `fix:`):

```sh
git tag v0.2.0 && git push origin v0.2.0
```

A tag with a suffix such as `v0.2.0-rc.1` is published as a prerelease. The default install command and
`social-mcp update` skip prereleases; install one with `SOCIAL_MCP_VERSION=v0.2.0-rc.1` or
`social-mcp update --version v0.2.0-rc.1`.

## License

MIT, see [LICENSE](LICENSE).
