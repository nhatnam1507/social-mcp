---
phase: 5
title: "Docs and stable-release verification"
status: pending
priority: P2
effort: "1.5h"
dependencies: [2, 3, 4]
---

# Phase 5: Docs and stable-release verification

## Goal

README documents every channel, and a real stable release proves each one end to end.

## Files to Create / Modify

- Modify: `/home/namnn/src/social-mcp/README.md` (`## Install`, `### Releasing`)

## Tasks & Steps

1. README `## Install`: keep the two one-liners first. Then an "Other ways to install" list: Homebrew
   (macOS, and Linux if casks support it), winget (Windows), `.deb`/`.rpm` from the release page, and
   `go install`. Each channel notes how to upgrade and that a running daemon must be restarted afterwards.
2. README `### Releasing`: a stable tag also updates the Homebrew tap and opens a winget PR; prereleases do
   not; the `CHANNELS_GITHUB_TOKEN` secret must be valid (note where its expiry is recorded).
3. Prerelease dry run (ask the user first): push `vX.Y.Z-rc.N`; confirm the GitHub prerelease carries the
   `.deb`/`.rpm` files and that the tap and winget fork are untouched.
4. Stable release (ask the user first, agree the version, for example `v0.1.0`): push the tag, then verify:
   - the tap repo has a new cask commit; on macOS, `brew install nhatnam1507/tap/social-mcp` and
     `social-mcp version` print the tag with no Gatekeeper prompt;
   - a PR to `microsoft/winget-pkgs` exists and passes the validation bot; after it merges,
     `winget install nhatnam1507.SocialMCP` installs and `social-mcp version` prints the tag;
   - the `.deb` and `.rpm` from the release install in clean containers (phase 4 commands, using the
     downloaded files).
5. If any machine (macOS or Windows) is unavailable, record which checks remain unverified, as the release
   pipeline plan did for macOS.

## Verification

- README commands match the generated package names and identifiers exactly.
- Run URLs, the tap commit and the winget PR URL are recorded in the PR description or plan.
