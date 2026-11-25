---
title: "Release pipeline and one-line installers"
description: "Pushing a v* tag publishes a GitHub Release with per-OS binaries, and curl/irm one-liners install the right binary for the user's OS and CPU."
status: in-progress
priority: P2
effort: 4h
branch: feat/release-pipeline
tags: [infra, release, docs]
blockedBy: []
blocks: []
created: 2026-11-19
---

# Release pipeline and one-line installers

## Overview

Today users install social-mcp from CI build artifacts or with `go install`. This plan adds a tag-triggered
GitHub Release and two installer scripts so a user runs one command and gets the binary that matches their
OS and CPU, checksum-verified:

```sh
curl -fsSL https://github.com/nhatnam1507/social-mcp/releases/latest/download/install.sh | sh
```

```powershell
irm https://github.com/nhatnam1507/social-mcp/releases/latest/download/install.ps1 | iex
```

The installers build the asset name from the detected OS and CPU and download it from GitHub's
`releases/latest/download/<asset>` redirect (or `releases/download/<tag>/<asset>` when a version is pinned).
They make no GitHub API calls, so they need no `jq` and hit no API rate limit.

```
git push origin v0.1.0
  → release.yml: reuse ci.yml (tests on 3 OSes) → scripts/build-all.sh → gh release create
  → Release v0.1.0: social-mcp-{linux,darwin,windows}-{amd64,arm64}[.exe], SHA256SUMS, install.sh, install.ps1
  → user one-liner → detect OS/CPU → download asset + SHA256SUMS → verify → install
```

## Reused pieces

- `scripts/build-all.sh` already builds all six static targets (`CGO_ENABLED=0`), names them
  `social-mcp-<os>-<arch>[.exe]`, injects `main.version` from `$VERSION`, and writes `dist/SHA256SUMS`.
  The release workflow calls it unchanged with `VERSION=<tag>`.
- `social-mcp version` (`internal/cli/version.go`) prints the version; the installers call it to confirm
  the install.
- `.github/workflows/ci.yml` test matrix is reused through `workflow_call` instead of being copied.

## Locked decisions

- Trigger: pushing a tag matching `v*`. A tag containing `-` (for example `v0.1.0-rc.1`) is published as a
  prerelease, which `releases/latest` ignores.
- `install.sh` defaults to `~/.local/bin` without sudo; `SOCIAL_MCP_INSTALL_DIR` overrides it; it prints a
  PATH hint when the folder is not on `PATH`.
- `install.ps1` ships alongside `install.sh` and installs to `%LOCALAPPDATA%\Programs\social-mcp`, adding it
  to the user PATH.
- `SOCIAL_MCP_VERSION` pins a release tag in both installers; default is the latest release.
- Integrity: both installers verify the asset against `SHA256SUMS` and abort on mismatch or when no hash
  tool is available. No build-provenance attestations.
- No GoReleaser, archives, Homebrew or Scoop in this plan. (Superseded 2026-11-25: GoReleaser now builds
  and publishes releases with the same asset names; see `261125-1413-pr-gate-make-targets`.)

## Phases

| # | Phase | Effort | Depends on | Status |
|---|-------|--------|------------|--------|
| 1 | [Installer scripts](phase-01-installer-scripts.md) | 2h | none | completed |
| 2 | [Release workflow](phase-02-release-workflow.md) | 1h | 1 | completed |
| 3 | [Docs and release-candidate verification](phase-03-docs-and-rc-verification.md) | 1h | 1, 2 | in-progress (v0.1.0 released; macOS install unverified; rc prereleases kept pending a decision) |

## Acceptance criteria

- Pushing `vX.Y.Z` produces a GitHub Release whose assets are the six binaries, `SHA256SUMS`, `install.sh`
  and `install.ps1`, only after the ci.yml test matrix passes; `social-mcp version` on a released binary
  prints `vX.Y.Z`.
- On Linux amd64, macOS arm64 and Windows amd64, the one-liner installs a working binary; on an
  unsupported OS or CPU it exits non-zero with a clear message.
- A corrupted download (hash mismatch) aborts without installing.
- `SOCIAL_MCP_VERSION=<tag>` installs that tag, including a prerelease.
- `shellcheck` passes on `scripts/install.sh` in CI.
- README `## Install` leads with the two one-liners; `## Development` documents how to cut a release.

## Risks

- `ci.yml` may also change on `feat/mcp-model-eval` (eval checks; that branch has no commits as of
  2026-11-25). Adding `workflow_call` is a one-line trigger change; rebase whichever branch merges second.
- Windows cannot overwrite a running `.exe` (a running daemon). `install.ps1` renames the old binary aside
  before copying, and both installers tell the user to restart the daemon after an upgrade.
- The installers cannot be exercised end to end until a release exists; phase 3 publishes
  `v0.1.0-rc.1` as a prerelease for that purpose (outward-facing: confirm with the user before pushing the tag).

## Validation Log

### Session 1 (2026-11-19)

- Release trigger: push a `v*` tag. (Recommended option chosen.)
- Default install dir: `~/.local/bin`, no sudo, overridable. (Recommended option chosen.)
- Windows: include `install.ps1`. (Recommended option chosen.)
- Provenance: SHA256 checksums only, no attestations. (Recommended option chosen.)

### Note (2026-11-25)

The plan directory and journal were missing from the working tree when implementation started (the files
were untracked and not found anywhere on disk). They were restored from the planning session's text on
branch `feat/release-pipeline`.
