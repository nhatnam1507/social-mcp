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

- [ ] Merge phases 1–2 to `main`, push `v0.1.0-rc.1`. Confirm the Release is a prerelease with 10 assets
      (6 binaries, `SHA256SUMS`, 2 installers).
- [ ] Linux (WSL): `curl -fsSL …/releases/download/v0.1.0-rc.1/install.sh | SOCIAL_MCP_VERSION=v0.1.0-rc.1 sh`;
      `social-mcp version` prints `v0.1.0-rc.1`.
- [ ] Windows: `$env:SOCIAL_MCP_VERSION='v0.1.0-rc.1'; irm …/releases/download/v0.1.0-rc.1/install.ps1 | iex`.
- [ ] macOS: run by the user, or recorded as unverified.
- [ ] Keep or delete the rc (`gh release delete v0.1.0-rc.1 --cleanup-tag`); cut `v0.1.0` when ready.

## Risk / Rollback

Revert the README hunks. A bad prerelease does not affect `latest` installs and can be deleted.
