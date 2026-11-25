---
title: "Self-update command and one-time new-version notice"
description: "`social-mcp update` installs the latest release in place and restarts a running daemon, and when a new stable release appears the user is told once in the CLI and once through their agent."
status: completed
priority: P2
effort: 7h
branch: feat/self-update
tags: [feature, cli, release]
blockedBy: []
blocks: []
created: 2026-11-25
---

# Self-update command and one-time new-version notice

## Overview

Today a user upgrades by re-running the curl/irm installer, and nothing tells them a new version exists. After
this plan:

- **`social-mcp update`** downloads the release binary for the current OS and CPU, verifies it against the
  release's `SHA256SUMS`, replaces the running executable in place, and, if the daemon was running, restarts
  it on the new version. `--version <tag>` installs a specific release, including a prerelease.
- **Update check:** the daemon checks GitHub once a day for the latest stable release and saves the result in
  the home folder. Nothing waits on the network.
- **One-time notice, two channels:**
  - CLI: after any command, a single line on stderr ("social-mcp vX.Y.Z is available (you have vA.B.C).
    Run `social-mcp update`."), shown once per new version.
  - Agent: the next `list_accounts` result carries a `notice` field once per new version, so the agent can
    tell the user to run `social-mcp update`.
- **Opt-out:** `[updates] check = false` in `config.toml` or `SOCIAL_MCP_NO_UPDATE_CHECK=1` turns off the
  automatic check (and therefore the notices). `social-mcp update` still works manually.

```
daemon (every 24h) ──GET github.com/<repo>/releases/latest (redirect only, no API)──> latest tag
        └─ writes <home>/update-check.json {checked_at, latest, agent_notified}
CLI command ends ──reads update-check.json + <home>/update-notice-cli──> one stderr line, once per version
agent list_accounts ──Service.TakeUpdateNotice()──> daemon returns the notice once, records agent_notified
social-mcp update ──download asset + SHA256SUMS──> verify ──> swap executable ──> restart daemon via new binary
```

## Locked decisions

- Notice in both the CLI and the agent channel, each once per new version (user choice).
- The daemon does the daily check; the CLI and MCP only read its result (user choice). If the daemon is not
  running, no new check happens and no new notice appears until it runs again.
- `update` restarts a running daemon automatically: stop (uploads resume from their saved phase, already
  supported) then start the new binary (user choice).
- Opt-out via config key and environment variable (user choice).
- Latest-version lookup uses the `github.com/<repo>/releases/latest` redirect, the same route the installers
  use: no GitHub API calls, no rate limit, no token. `releases/latest` excludes prereleases, so notices are
  only for stable releases.
- Only release builds take part. A version that is not a clean release tag (`vX.Y.Z` or `vX.Y.Z-pre` without
  extra `-` parts) is treated as a development build: no check, no notice. `update` on such a build still
  works but says the current version is unknown.
- The CLI notice goes to stderr, never stdout, and is skipped for `mcp` (stdio protocol), `daemon run`
  (service managers), `update` and `version`.
- Single writer per state file: the daemon owns `update-check.json` (including the agent-notified version);
  the CLI owns `update-notice-cli`. Both are written atomically (temp file + rename).
- Version comparison uses `golang.org/x/mod/semver` (one small, official dependency) instead of hand-written
  parsing.
- The executable swap mirrors `install.ps1`: on Windows the running `.exe` is renamed aside to `.old` before
  the new one moves in, and stale `.old` files are removed on the next `update` or daemon start. On Unix the
  new file is renamed over the old one.
- The restarted daemon is launched through the new binary's path (`<exe> daemon start`), not
  `os.Executable()` of the updating process, which on Linux points at the replaced (deleted) file.

## Phases

| # | Phase | Effort | Depends on | Status |
|---|-------|--------|------------|--------|
| 1 | [Release lookup and in-place install](phase-01-update-core.md) | 2h | none | completed |
| 2 | [`social-mcp update` command](phase-02-update-command.md) | 1.5h | 1 | completed |
| 3 | [Daily check and one-time notices](phase-03-check-and-notices.md) | 2h | 1 | completed |
| 4 | [Docs and live verification](phase-04-docs-and-verification.md) | 1.5h | 2, 3 | completed |

## Acceptance criteria

- `social-mcp update` on an older release build installs the latest stable release in the same location,
  verified against `SHA256SUMS`; afterwards `social-mcp version` prints the new tag.
- A checksum mismatch, a missing asset or an unwritable install folder aborts without touching the installed
  binary and exits non-zero with a clear message.
- When the daemon was running, `update` leaves it running on the new version (`daemon status` reports the
  new version); when it was stopped, it stays stopped.
- `update` when already on the latest version prints that and changes nothing; `--version <tag>` installs
  that tag, including a prerelease.
- With a newer stable release known to the daemon, the next CLI command prints the notice once on stderr;
  later commands do not, until a newer version appears. `mcp`, `daemon run`, `update` and `version` never
  print it, and stdout (including `--json` output) is never affected.
- The next `list_accounts` call returns a `notice` naming the new version and `social-mcp update`; later
  calls do not, until a newer version appears. This works for both the HTTP endpoint and `social-mcp mcp`.
- With `[updates] check = false` or `SOCIAL_MCP_NO_UPDATE_CHECK=1`, the daemon makes no update request and
  no notice appears.
- Development builds (`dev`, `git describe` versions) never check or notify.
- `make test` and `make vet-cross` pass; the new code has unit tests using a local HTTP server, not GitHub.
- README and `docs/architecture.md` describe the command, the notice, the opt-out and the new home files.

## Risks

- **Windows file locking.** A running daemon holds the `.exe` open. Renaming a running executable is allowed
  on Windows, deleting it is not; the `.old` dance handles that, and the Windows CI job covers the swap in a
  unit test.
- **Daemon on a different version than the CLI.** After a manual binary replacement the daemon keeps the old
  version until restarted. `update` restarts it; the agent notice compares against the daemon's own version.
- **Live verification needs two releases with this feature.** The first release that contains `update` can
  only update to a later one. Phase 4 uses two prereleases with `--version` for the live swap, and a local
  test server for the stable-only notice path. Tags are outward-facing and need confirmation.
- **Interrupted update.** The new binary is fully downloaded, verified and test-run (`<tmp> version`) before
  the swap, so an interruption leaves the old binary in place.
- **Branch protection.** `main` requires pr-gate; all changes land through a PR.

## Implementation notes (2026-11-25)

- The generated `config.toml` shows `[updates]` only commented out. A live downgrade test showed why: older
  releases reject unknown settings, so a config written by `config init` with an active `[updates]` section
  stopped an older daemon from starting. The check defaults to on, so the section is only needed to opt out.
- The new home files start with `update-`, which was added to the Windows list of files social-mcp owns;
  without it, the home folder's access list would stop being applied once the first check ran.
- The CLI notice follows successful commands only (cobra's post-run hook does not run after an error).
- Live checks needed no new tags: a binary built as `v0.1.0-rc.1` updated itself to the published
  `v0.1.0-rc.2` (verified download, swap, daemon restarted on rc.2, checksum identical to the asset), and a
  binary built as `v0.1.0` ran the real daily check (no stable release found) and showed the CLI and agent
  notices once each for a recorded newer release. So the planned `rc.3`/`rc.4` tags are not required.
- Not verified live: the Windows swap (covered by the unit test on the Windows ci job) and a notice for a
  real stable release (none exists yet).
