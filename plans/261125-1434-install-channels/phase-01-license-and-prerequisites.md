---
phase: 1
title: "License and publishing prerequisites"
status: pending
priority: P2
effort: "0.5h"
dependencies: []
---

# Phase 1: License and publishing prerequisites

## Goal

The repo is MIT-licensed, the tap repo and winget fork exist, and the release workflow has a token that can
push to both.

## Files to Create / Modify

- Create: `/home/namnn/src/social-mcp/LICENSE` (MIT; `Copyright (c) 2026 nhatnam1507`)
- Modify: `/home/namnn/src/social-mcp/.github/workflows/release.yml` (pass the channel token to GoReleaser)
- External (after user confirmation): `nhatnam1507/homebrew-tap` (new public repo with a README),
  `nhatnam1507/winget-pkgs` (fork of `microsoft/winget-pkgs`)
- External (user only): repo secret `CHANNELS_GITHUB_TOKEN`

## Tasks & Steps

1. Add the MIT `LICENSE` with `Copyright (c) 2026 nhatnam1507`.
2. Ask the user, then run:
   - `gh repo create nhatnam1507/homebrew-tap --public --description "Homebrew tap for social-mcp" --add-readme`
   - `gh repo fork microsoft/winget-pkgs --clone=false`
3. Give the user exact token steps. Expected: a classic personal access token with the `public_repo` scope,
   because opening a PR on `microsoft/winget-pkgs` (a repo the user does not own) is not possible with a
   fine-grained token. Confirm this against current GitHub and GoReleaser docs before handing over the steps.
   The user stores it with `gh secret set CHANNELS_GITHUB_TOKEN` (interactive prompt, so the value never
   appears in shell history or this session) and notes its expiry date.
4. In `release.yml`, add `CHANNELS_GITHUB_TOKEN: ${{ secrets.CHANNELS_GITHUB_TOKEN }}` to the publish step's
   `env`. `GITHUB_TOKEN` stays the default token for the release itself.

## Verification

- `gh repo view nhatnam1507/social-mcp --json licenseInfo` reports MIT.
- `gh repo view nhatnam1507/homebrew-tap` and `gh repo view nhatnam1507/winget-pkgs` succeed.
- `gh secret list` shows `CHANNELS_GITHUB_TOKEN`.
