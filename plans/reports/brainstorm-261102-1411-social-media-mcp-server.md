# Brainstorm: social-mcp — multi-platform video upload MCP server

Date: 2026-11-02 · Status: accepted contract, ready for planning

## Evidence

- Greenfield: `/home/namnn/src/social-mcp` is empty and not a git repo.
- Toolchain: Go 1.26.1 (linux/amd64). Dev host is WSL2.

## Outcome

One Go binary, `social-mcp`, that runs as a CLI-managed background daemon. A human
connects multiple OAuth2 accounts per platform from the CLI. AI agents use MCP tools to
discover connected accounts, upload a local video to the accounts they choose, and track
each upload until it finishes.

## Decisions (from user)

| Decision | Choice |
|---|---|
| v1 platforms | YouTube, TikTok, Facebook Pages, Instagram Reels |
| Data boundary | OAuth tokens stored only on the user's machine; no backend, no relay, no telemetry |
| Target OS | Linux, macOS, Windows |
| Video source | Local file path only, restricted to allowed directories |
| UI | CLI now; UI later (out of v1) |

The data boundary implies **bring-your-own developer apps**: whoever runs the binary
registers an app on each platform and puts client ID/secret in local config. Token
exchange needs the client secret for TikTok and Meta, and a secret cannot be safely
shipped inside a distributed binary, so there is no shared app.

## Constraints

- Go, single static binary, `CGO_ENABLED=0` (pure-Go deps: `modelcontextprotocol/go-sdk`,
  `modernc.org/sqlite`). Cross-compiled for linux/darwin/windows × amd64/arm64.
- Tokens leave the machine only toward the issuing platform.
- Daemon must work without systemd (WSL2) and on Windows.
- Platform policy limits: see "Blocker check" below (verified 2026-11-02; supersedes
  the platform claims originally written here).
- OAuth redirect URIs: loopback on all platforms (TikTok desktop Login Kit allows
  `http://localhost:<port>` / `127.0.0.1` with PKCE; Google desktop clients use loopback;
  Meta allows localhost).

## Non-goals (v1)

- Web/desktop UI, hosted or multi-user service, any data leaving the machine.
- Agents adding/removing accounts (human-only, via CLI).
- Analytics, comments, post editing/deletion, transcoding, image/text posts.
- URL-sourced videos.
- OS service-manager integration (autostart); may be added later as `daemon install`.

## Architecture

```
CLI (social-mcp …) ──local control socket──┐
                                           ▼
MCP client ─HTTP 127.0.0.1 + bearer─▶ daemon ──▶ providers: youtube | tiktok | facebook | instagram
stdio-only clients: `social-mcp mcp`    ├─ OAuth loopback callback listener
(bridge subcommand → daemon)            ├─ upload job queue (resumable, per account)
                                        └─ background token refresher
                                           ▼
                      SQLite (accounts, jobs) + AES-GCM sealed tokens
```

- **Provider interface:** `AuthURL`, `Exchange`, `Refresh`, `ListIdentities`,
  `Upload(ctx, file, meta, progress)`, `Capabilities`. One login can produce several
  selectable accounts (Facebook → Pages, Instagram business accounts; Google → channels).
- **MCP tools:** `list_accounts`, `upload_video`, `get_upload_status`, `list_uploads`,
  `cancel_upload`. Uploads are async jobs because large videos exceed tool-call timeouts.
  `upload_video` takes common metadata (title, description, tags, privacy) plus an
  optional per-platform options map; per-account failures are isolated.
- **`list_accounts` returns:** platform, account ID, display name, health
  (`ok` / `needs_reauth`), and capabilities (allowed privacy levels, size/duration
  limits, audit-imposed restrictions such as "private only").
- **CLI:** `daemon start|stop|status|logs`, `account add|list|remove|reauth`,
  `config`, `mcp` (stdio bridge).
- **Daemon lifecycle:** self-managed re-exec in the background (Unix: setsid; Windows:
  detached process flags), pidfile + control socket probe for status/stale detection.
  Control channel is AF_UNIX on all three OSes (Go supports it on Windows 10 1803+).
- **Security:** bearer token + Host/Origin check on the MCP HTTP endpoint; file reads
  limited to configured allowed dirs and validated as video containers (blocks a
  prompt-injected agent from publishing arbitrary files); default privacy `private`
  unless the agent sets it explicitly.

## Trade-offs

**Daemon lifecycle**
- *Self-managed detach (chosen).* Assumes manual start or a shell-rc start is acceptable.
  Fails first: no autostart after reboot; stale pidfile after a crash (handled by
  probing the socket).
- *OS service manager (`kardianos/service`).* Assumes an init system exists. Fails first
  on WSL2 without systemd; needs install steps per OS. Can be layered on later.

**Token encryption key**
- *OS keyring (Keychain / Credential Manager / Secret Service).* Fails first on
  headless Linux/WSL2 without a Secret Service.
- *0600 key file in config dir.* Assumes file permissions protect it. Fails first when
  the home directory is synced or backed up together with the key.
- Chosen: keyring when available, key-file fallback, reported by `daemon status`.

**Better approaches:** none. The requested daemon design is the right one: it receives
OAuth callbacks, keeps long resumable uploads running after a tool call returns, and
refreshes tokens in one place without races.

## Acceptance criteria

1. `go build` yields one binary per OS/arch; `daemon start|stop|status|logs` works on
   Linux, macOS, Windows, and the daemon keeps running after the terminal closes.
2. `account add <platform>` completes browser OAuth via loopback callback and stores
   encrypted tokens; two or more accounts per platform work; `list|remove|reauth` work.
3. An MCP client (e.g. Claude Code) calls `list_accounts` and receives platform, ID,
   name, health, and capabilities for every connected account.
4. `upload_video` with one local file and several account IDs returns per-account job IDs;
   `get_upload_status` reports progress until it returns a post ID/URL; one platform's
   failure does not affect the others.
5. Expired access tokens refresh automatically; revoked tokens surface as `needs_reauth`.
6. Tokens are encrypted on disk; MCP requests without the bearer token are rejected;
   paths outside allowed dirs are rejected.

## Blocker check (verified 2026-11-02)

### TikTok: hard blocker for public posting by a personal tool
- App review rejects personal use: "Apps must not be for private or personal use." It
  also needs a fully built public website with Terms of Service and Privacy Policy, plus
  demo videos.
- Without an approved app, only a **sandbox** is available: up to 10 TikTok accounts you
  own, and "Sandbox mode does not offer access to Content Posting API for public videos."
- Even an approved app that hasn't passed the audit only gets Direct Post with
  `SELF_ONLY`, for at most 5 users per 24h, and those accounts must be **set to private**
  when posting.
- The audit also requires the poster to pick privacy manually with no default and to
  consent explicitly before upload. An agent choosing privacy for them conflicts with
  that.
- Viable route: **upload to inbox** (`video.upload`). The video arrives as a draft in
  the creator's TikTok inbox, and they finish posting in the app. Limits: 5 pending
  shares per 24h, 6 requests/min, and the upload URL is valid for 1 hour.
  **Unverified:** whether inbox upload works for sandbox accounts.
- Corrections: access token lasts 2h (not ~24h), refresh token 180 days. TikTok's docs
  say token calls carry the client secret and belong on a server; for a bring-your-own
  app, the user's local config plays that role.

### Facebook Pages + Instagram: likely blocker for public visibility
- In Development mode, data an app creates "can only be seen by role users", so videos
  posted to a Page are invisible to the public.
- Standard Access is approved automatically but only works for users with a role on the
  app. Advanced Access needs App Review **and Business Verification**. Meta says to
  switch to Live only after App Review.
- **Unverified:** whether a Live Business app that uses only Standard Access, with only
  role users, publishes posts the public can see. Cheapest test: make the app, post to
  your own Page, and view it logged out. Fallback: App Review plus Business
  Verification, which needs a business entity, a privacy policy URL and screencasts.
- Facebook has no refresh tokens. A long-lived user token lasts about 60 days; Page
  tokens derived from it don't expire. The resumable upload step uses the **user**
  token, so Facebook accounts need re-authorization about every 60 days.
- Instagram: local-file resumable upload (`rupload.facebook.com`) works only with
  **Facebook Login for Business**. Permissions: `instagram_basic`,
  `instagram_content_publish`, `pages_read_engagement`, `pages_show_list`. The doc
  states both 100 and 50 posts per 24h; read the real value from
  `content_publishing_limit`.

### YouTube: no blocker (earlier claims corrected)
- Docs now say "Videos uploaded from unverified API projects are not restricted to
  private viewing mode."
- `videos.insert` has its own Video Uploads quota bucket: 100 calls/day, 1 unit each.
  Max file size 256GB. Resumable upload is supported.
- Friction: an OAuth app in "Testing" status gets refresh tokens that expire after 7
  days. Plan to set the app to "In production" without verification; expect a warning
  screen and a 100-user cap (not yet checked against current Google docs). Limit: 100
  refresh tokens per Google account per client; older ones are revoked silently.

### Toolchain: no blocker (verified locally)
- `modelcontextprotocol/go-sdk` v1.8.0 (Streamable HTTP server, stdio, and client
  transports for the bridge), `modernc.org/sqlite` v1.61.1 and
  `zalando/go-keyring` v0.2.8 all build with `CGO_ENABLED=0` for linux, darwin and
  windows on amd64 and arm64. The binaries are static and about 17MB.
- On Linux, opening SQLite and listening on an AF_UNIX socket both work at runtime.
  Windows runtime is untested because this machine has no Windows host or Wine; it
  needs a Windows machine or CI runner.
- WSL2 has no `xdg-open`, so the CLI must open the browser through `explorer.exe` /
  `wslview` or print the URL.

### Decisions after blocker check
- **TikTok v1 = upload to inbox (drafts) only** (`video.upload`). Direct Post is out of
  v1. Gate: confirm inbox upload works for a sandbox target account.
- **Meta = test Live mode first.** Run the manual probe (Live Business app, Standard
  Access only, post to your own Page, view logged out) before starting the Facebook and
  Instagram phases. If posts aren't public, the plan switches to App Review plus
  Business Verification.

### Effect on acceptance criteria
- Criterion 4 ("returns post ID/URL") cannot hold for TikTok inbox uploads. Those
  finish as "sent to drafts", and the user completes the post in the app.
- `list_accounts` capabilities must report per-account restrictions such as
  `draft_only`, `private_only` and `visible_to_app_roles_only`, so an agent never claims
  a public post that didn't happen.
