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
specific release, or `SOCIAL_MCP_INSTALL_DIR` to choose the folder. Running the command again upgrades in place.

You can also download a binary from the [releases page](https://github.com/nhatnam1507/social-mcp/releases),
or build from source with Go 1.26.1 or newer:

```sh
go install github.com/nhatnam1507/social-mcp/cmd/social-mcp@latest
```

Supported: Linux, macOS and Windows 10 1803+ on amd64 and arm64.

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
  `0600` key file when no keyring is available.
- The home directory is private to you. On Unix it must be owned by you and is kept at mode `0700`. On
  Windows it gets an access list that allows only your account, even when `SOCIAL_MCP_HOME` points outside
  your profile.
- The CLI talks to the daemon over a local socket in the home directory. When that path would be too long for
  the OS, the socket goes in a private per-user folder in the temp directory. On Unix the CLI refuses a socket
  owned by another user. On Windows the folder name is derived from your account's SID.
- On Windows, the access list is only applied when `SOCIAL_MCP_HOME` is a directory used only by social-mcp; a
  shared directory is left unchanged with a warning.
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
writes `./.dev/config.toml`; add your development app credentials there (see
[Platform setup](docs/platform-setup.md)). Run any other command against the same dev home with `ARGS`:

```sh
make run                              # daemon in the foreground; Ctrl+C stops it
make run ARGS="account add youtube"   # connect a test account to the dev home
make run ARGS="config mcp"            # MCP endpoint and token of the dev daemon
```

The dev home listens on port 8765 like a normal install. Stop your real daemon first, or change `listen_port`
in `./.dev/config.toml` and register that port's OAuth redirect URIs for your development apps.

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
git tag v0.1.0 && git push origin v0.1.0
```

A tag with a suffix such as `v0.1.0-rc.1` is published as a prerelease. The default install command skips
prereleases; install one with `SOCIAL_MCP_VERSION=v0.1.0-rc.1`.

## License

MIT, see [LICENSE](LICENSE).
