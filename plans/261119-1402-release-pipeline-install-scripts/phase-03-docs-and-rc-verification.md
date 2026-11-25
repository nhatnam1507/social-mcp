---
phase: 3
title: "Docs and release-candidate verification"
status: in-progress
priority: P2
effort: "1h"
dependencies: [1, 2]
---

# Phase 3: Docs and release-candidate verification

## Goal

Document the new install path and release procedure, then prove the whole chain on a real prerelease.

## Files to Create / Modify

- Modify: `README.md` — `## Install` and `## Development`.

## Tasks & Steps

### Docs

- [x] `## Install` leads with the `curl … | sh` and `irm … | iex` one-liners, explains detection,
      checksum and install locations, `SOCIAL_MCP_VERSION` and `SOCIAL_MCP_INSTALL_DIR`, links the releases
      page and keeps `go install`. The "CI build artifacts" wording is gone.
- [x] `## Development` → `### Releasing`: tag and push; a suffix publishes a prerelease.

### Release-candidate verification (outward-facing: needs the user's go-ahead)

- [x] Merged as PR #5 (`02df017`), pushed `v0.1.0-rc.1`. Confirm the Release is a prerelease with 9 assets
      (6 binaries, `SHA256SUMS`, 2 installers).
- [x] Linux (WSL): `curl -fsSL …/releases/download/v0.1.0-rc.1/install.sh | SOCIAL_MCP_VERSION=v0.1.0-rc.1 sh`;
      `social-mcp version` prints `v0.1.0-rc.1`.
- [x] Windows (PowerShell 5.1 and 7.6, from the real release): `$env:SOCIAL_MCP_VERSION='v0.1.0-rc.1'; irm …/releases/download/v0.1.0-rc.1/install.ps1 | iex`.
- [ ] macOS: unverified (no Mac available in the session).
- [x] Cut `v0.1.0`: released 2026-11-25 (built by GoReleaser since `261125-1413-pr-gate-make-targets`); the
      default one-liner installs it.
- [ ] Keep or delete the `v0.1.0-rc.1` and `v0.1.0-rc.2` prereleases (`gh release delete <tag> --cleanup-tag`);
      kept until the user decides.

## Risk / Rollback

Revert the README hunks. A bad prerelease does not affect `latest` installs and can be deleted.

## Results (2026-11-25)

- Release `v0.1.0-rc.1` is a prerelease with 9 assets: 6 binaries, `SHA256SUMS`, `install.sh`, `install.ps1`.
- Linux: the `curl | sh` one-liner installed `social-mcp-linux-amd64`; `social-mcp version` printed `v0.1.0-rc.1`.
- Windows: the `irm | iex` one-liner installed under both PowerShell versions; `social-mcp version` resolved
  through PATH and printed `v0.1.0-rc.1`; the second run took the upgrade path. User PATH restored afterwards.
- `releases/latest/download/...` returns 404 until a non-prerelease release exists, so the default install
  command starts working with the first full release (`v0.1.0`).
- The first PR CI run failed on Windows in `TestMismatchedKeyFailsCheck` (`internal/secrets`), unrelated to
  this change: the test overwrites the first hex byte of a random key with `00`, which is a no-op about 1 run
  in 256. A re-run passed.
