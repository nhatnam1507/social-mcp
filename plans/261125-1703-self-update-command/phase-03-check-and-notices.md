---
phase: 3
title: "Daily check and one-time notices"
status: completed
priority: P2
effort: "2h"
dependencies: [1]
---

# Phase 3: Daily check and one-time notices

## Goal

The daemon learns about new stable releases once a day, and the user hears about each new version exactly
once in the CLI and once through their agent, unless they opted out.

## Files to Create / Modify

- Modify: `/home/namnn/src/social-mcp/internal/config/config.go`, `template.go` (`[updates] check = true`)
- Modify: `/home/namnn/src/social-mcp/internal/paths/paths.go` (`UpdateCheck(home)` → `update-check.json`,
  `UpdateNoticeCLI(home)` → `update-notice-cli`)
- Create: `/home/namnn/src/social-mcp/internal/update/check.go` (state file, checker loop, notice text)
- Modify: `/home/namnn/src/social-mcp/internal/daemon/daemon.go` (start the checker with the refresher)
- Modify: `/home/namnn/src/social-mcp/internal/service/service.go`, `local.go` (`TakeUpdateNotice`)
- Modify: `/home/namnn/src/social-mcp/internal/control/server.go`, `client.go` (`POST /v1/update-notice`)
- Modify: `/home/namnn/src/social-mcp/internal/mcpserver/tools.go` (`notice` on `list_accounts`)
- Modify: `/home/namnn/src/social-mcp/internal/cli/root.go` (CLI notice after commands)
- Tests next to each change

## Design

- **Opt-out:** the checker does nothing when `cfg.Updates.Check` is false or `SOCIAL_MCP_NO_UPDATE_CHECK` is
  set to a non-empty value, or when the daemon's version is not a release build.
- **Checker:** runs in the daemon alongside the refresher. First check shortly after start (about a minute,
  so a crash loop does not hammer GitHub), then every 24 hours. Skips the network if the last check is under
  24 hours old (survives restarts). Failures are logged at debug level and retried next cycle; they never
  affect uploads.
- **State file** `update-check.json` (daemon-owned, atomic writes): `checked_at`, `latest`,
  `agent_notified`.
- **Agent notice:** `Service.TakeUpdateNotice() (string, error)`. The daemon returns the notice text when
  `latest` is newer than its own version and differs from `agent_notified`, then records `agent_notified`.
  `list_accounts` calls it and sets `notice` in its result when non-empty; an error (for example an older
  daemon without the route behind `social-mcp mcp`) is ignored. The server instructions gain one sentence:
  if `list_accounts` returns a `notice`, pass it on to the user.
- **CLI notice:** a root `PersistentPostRun` (skipped for `mcp`, `daemon run`, `update`, `version`) reads
  `update-check.json`; when `latest` is newer than the CLI's version and differs from the version stored in
  `update-notice-cli`, it prints one line to stderr and stores `latest`. Missing or unreadable files are
  ignored silently.
- **Notice text** (shared): `social-mcp vX.Y.Z is available (you have vA.B.C). Run "social-mcp update" to
  install it.`

## Tasks & Steps

1. Config key with default `true`, template comment, validation unchanged otherwise.
2. Checker and state file with an injectable clock and HTTP base URL; tests for the 24-hour gate, opt-out
   (config and env), dev builds, failure handling and atomic writes.
3. `TakeUpdateNotice` across service, control server/client and the stdio path; tests that it returns the
   notice once per version and empty afterwards.
4. `list_accounts` notice field and instructions sentence; mcpserver tests for present-once and absent.
5. CLI post-run notice; tests that it prints once to stderr, never to stdout, and not for the skipped
   commands.

## Verification

- `make test` and `make vet-cross` pass.
- Local manual check: `make run` with a hand-written `./.dev/update-check.json` naming a newer version shows
  the CLI notice once (`make run ARGS="account list"` twice); a `list_accounts` call through the dev daemon's
  MCP endpoint shows the `notice` once. (`make run` builds a dev version, so the check uses a release-tag
  `VERSION`, for example `make run VERSION=v0.1.0`.)
