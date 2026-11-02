---
title: "Phase 3: Daemon and CLI"
status: completed
priority: P1
effort: 7h
dependencies: [2]
---

# Phase 3: Daemon and CLI

<!-- Updated: Red Team Session 1 - process lock, bind-before-runner order, stop waits for lock, short socket path fallback, IPv6 loopback, breakaway detach, reauth identity check, URL allowlist, token load fail-closed, no hot reload -->
<!-- Updated: Validation Session 1 - loopback-only Meta login; no allowed-dir default -->

## Overview

Compose the daemon (process lock, control API on AF_UNIX, loopback HTTP listener for the OAuth
callback and later MCP, runner, refresher) and the cobra CLI that manages it and connects accounts.

## Requirements

- [x] Dependency: `github.com/spf13/cobra`.
- [x] `internal/daemon/providers.go`: `buildProviders(cfg) provider.Registry` — the only place that
      imports platform packages. Created here with no platforms; phases 5, 6, 7 each add one
      registration (serialize edits to this file).
- [x] `internal/daemon.Run(ctx, Options)` startup order:
      1. Acquire an exclusive lock on `daemon.lock` (`flock` on Unix, `LockFileEx` on Windows via
         `golang.org/x/sys`), held for the process lifetime. Already locked → "already running".
      2. Home ownership check (phase 1).
      3. Remove a stale socket file (safe: we hold the lock); listen on the control socket (file 0600 on Unix).
      4. Bind `127.0.0.1:<listen_port>`; also bind `[::1]:<listen_port>` when the host has IPv6
         loopback (if `[::1]` is unavailable skip; if it is in use, fail). Bind failure → error naming
         the port and how to find the owner (`lsof -i :PORT` / `netstat -ano`); never pick another port.
      5. Load `mcp-token` (create atomically on first start: temp file + rename, 0600). It must be
         exactly 64 hex chars, else refuse to start with guidance to run `config mcp --rotate`.
      6. Write pidfile; open store and keystore; build registry; start runner (which runs
         `ResetInterrupted`) and refresher; serve.
      `Options` allows `Home`, `SocketPath`, `ListenPort` overrides for tests.
- [x] Socket path: `home/daemon.sock`, or when that exceeds 100 bytes,
      `<os.TempDir()>/social-mcp-<first 12 hex of sha256(home)>.sock`. `paths.SocketPath(home)` is the
      single source used by daemon and CLI.
- [x] Shutdown (SIGINT/SIGTERM, Windows `os.Interrupt`, or control `POST /v1/shutdown`): stop accepting
      HTTP/control requests, `runner.Stop` (10s bound), stop refresher, close store, remove pidfile and
      socket, release lock.
- [x] Control API (JSON over HTTP on the socket, mirrors `service.Service` with the phase 2 DTOs):
      `GET /v1/status`, `GET /v1/accounts?platform=`, `DELETE /v1/accounts/{id}`, `POST /v1/auth`
      `{platform, reauth_account_id?}` → `AuthStart`, `GET /v1/auth/{id}`, `POST /v1/uploads`,
      `GET /v1/uploads/{id}`, `GET /v1/uploads`, `POST /v1/uploads/{id}/cancel`, `POST /v1/shutdown`.
      `internal/control.Client` implements `service.Service` over the socket.
- [x] OAuth callback `GET /oauth/callback?state=&code=` (and `error=`): look up session by state;
      exchange; `Identities`; for a reauth session the target `external_id` must be among the returned
      identities, else fail the session ("the signed-in account does not include <name>") without writing
      anything; otherwise `UpsertLogin` keyed by `subject`. Render a minimal HTML page (escaped,
      sanitized). Redirect URI = `http://<provider.RedirectHost()>:<port>/oauth/callback`.
- [x] Detach: `daemon start` re-executes the binary as `daemon run --detached`, stdout/stderr →
      `daemon.log` (append), stdin from the null device. Unix: `SysProcAttr{Setsid: true}`. Windows:
      `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP | CREATE_BREAKAWAY_FROM_JOB`, retrying without
      breakaway on access denied (logged). Parent waits up to 10s for `GET /v1/status`, then prints PID,
      MCP URL and key backend, and exits.
- [x] `daemon stop`: `POST /v1/shutdown`, then wait up to 30s until `daemon.lock` can be acquired
      (process exited). If it never releases: send SIGTERM (Unix) / `Process.Kill` (Windows) to the
      pidfile PID only if the lock is still held; report what was done.
- [x] `daemon status`: running/stopped, PID, version, uptime, listen URL, key backend, accounts by
      platform and health, active uploads. Exit code 0 running, 3 stopped.
- [x] `daemon logs [-n 100] [-f]` tails `daemon.log` (follow by polling size). `daemon run` runs in the
      foreground, logging to stderr.
- [x] `account add <youtube|tiktok|facebook|instagram> [--no-browser]`: requires a running daemon;
      `POST /v1/auth`; open the browser only for `https://` URLs whose host is on the provider allowlist
      (`accounts.google.com`, `www.tiktok.com`, `www.facebook.com`) using `xdg-open` / `open` /
      `rundll32 url.dll,FileProtocolHandler`, or on WSL (`/proc/sys/kernel/osrelease` contains
      `microsoft`) `wslview` or `explorer.exe`; always print the URL; poll `GET /v1/auth/{id}` every
      second up to 10 minutes; print connected accounts.
- [x] `account list [--platform X] [--json]`, `account remove <id> [--yes]` (refused while jobs are
      active; deletes the credential when no account references it; prints a reminder that platform-side
      access can be revoked in the platform's app settings), `account reauth <id>`.
- [x] `config init [--force]` writes the template (0600), refuses to overwrite without `--force`;
      `config path`; `config mcp` prints the MCP URL, bearer token, and ready-to-paste client config
      (Claude Code `claude mcp add --transport http …` and a stdio JSON snippet). `config mcp --rotate`
      writes a new token atomically and tells the user to restart the daemon (no hot reload).
- [x] `version` command; version injected via `-ldflags "-X main.version=..."`.

## Files

- Create: `cmd/social-mcp/main.go`
- Create: `internal/cli/root.go`, `internal/cli/daemon.go`, `internal/cli/account.go`, `internal/cli/config.go`, `internal/cli/version.go`, `internal/cli/cli_test.go`
- Create: `internal/daemon/daemon.go`, `internal/daemon/providers.go`, `internal/daemon/lock_unix.go`, `internal/daemon/lock_windows.go`, `internal/daemon/detach_unix.go`, `internal/daemon/detach_windows.go`, `internal/daemon/pidfile.go`, `internal/daemon/daemon_test.go`
- Create: `internal/control/server.go`, `internal/control/client.go`, `internal/control/control_test.go`
- Create: `internal/oauth/callback.go`, `internal/oauth/callback_test.go`
- Create: `internal/browser/open.go`, `internal/browser/open_test.go`
- Create: `internal/mcptoken/token.go`, `internal/mcptoken/token_test.go`
- Modify: `internal/paths/paths.go` (`SocketPath`)

## Implementation Steps (test first)

1. control tests: temp socket (short `os.MkdirTemp("", "sm")` dir) with a fake `service.Service`; the
   client round-trips every method including error mapping (404, 400).
2. callback tests: unknown state → 400; `error=access_denied` → session failed; success → accounts
   upserted and listed; state reuse rejected; reauth with a different identity fails and writes nothing;
   a planted secret in a provider error is masked on the page and in the session.
3. daemon tests (in-process, short temp dirs, port 0): two concurrent `Run` calls → exactly one runs;
   stale socket with no lock holder is replaced; shutdown leaves an uploading job `uploading`; startup
   fails cleanly when the port is taken and the runner never started (job states untouched).
4. paths test: long home path yields the temp-dir socket path, short home keeps `home/daemon.sock`.
5. mcptoken tests: created 64-hex 0600; empty/missing/malformed file at load → error.
6. browser tests: command selection per GOOS/WSL with an injected runner; non-allowlisted or non-https
   URL is printed, never executed.
7. cli tests: `config init` refuses overwrite; `account list --json` renders from a fake client;
   `daemon status` exit 3 without daemon.
8. Implement until green.

## Todo

- [x] process lock + startup/shutdown order
- [x] socket path fallback
- [x] control server + client
- [x] OAuth callback with reauth identity check
- [x] detach (unix/windows breakaway)
- [x] CLI: daemon, account, config, version
- [x] browser opener with allowlist
- [x] MCP token file

## Verification

- `go test -race ./internal/control/... ./internal/daemon/... ./internal/cli/... ./internal/oauth/... ./internal/browser/... ./internal/mcptoken/... ./internal/paths/...`
- Manual: build, then with one temp `SOCIAL_MCP_HOME`: `daemon start`, `daemon status`, `daemon logs -n 5`, `daemon stop`.
- `GOOS=windows CGO_ENABLED=0 go build ./...`

## Risks

- AF_UNIX on Windows requires Windows 10 1803+; documented.
- WSL2: the Windows browser reaches `127.0.0.1:<port>` through WSL localhost forwarding (default on);
  the printed URL lets users finish manually.
- Meta in Live mode may reject `http://localhost` redirects (unverified; user chose loopback only).
  Documented as a known risk in `docs/platform-setup.md`.

## Success Criteria

Only one daemon can run per home; a failed start never touches job state; `daemon stop` returns only
after the process exited; account add flow completes against a test-double provider in tests.
