---
phase: 4
title: "Docs and live verification"
status: completed
priority: P2
effort: "1.5h"
dependencies: [2, 3]
---

# Phase 4: Docs and live verification

## Goal

The docs describe updating and the notice, and a real release proves the in-place update end to end.

## Files to Create / Modify

- Modify: `/home/namnn/src/social-mcp/README.md` (`## Install`: "Run `social-mcp update` to upgrade"; CLI
  table row; environment variables; MCP tools note about `notice`)
- Modify: `/home/namnn/src/social-mcp/docs/architecture.md` (storage table: `update-check.json`,
  `update-notice-cli`; daemon lifecycle: the daily check and its opt-out)

## Tasks & Steps

1. Update the docs above; keep `config.toml` keys pointed at the generated template rather than copied.
2. Open a PR; merge when pr-gate is green (branch protection requires it); confirm ci on main is green,
   including the Windows replace test.
3. Live update (outward-facing; ask the user first, agree tags): publish two prereleases that contain the
   command, for example `v0.1.0-rc.3` then `v0.1.0-rc.4`:
   - install `rc.3` into a scratch folder with the one-liner (`SOCIAL_MCP_INSTALL_DIR`), start its daemon
     with a scratch `SOCIAL_MCP_HOME` and `SOCIAL_MCP_KEYRING=off`;
   - run `social-mcp update --version v0.1.0-rc.4`; check `version` prints `v0.1.0-rc.4`, `daemon status`
     reports the daemon on `v0.1.0-rc.4`, and the scratch daemon is stopped afterwards;
   - if a Windows machine is available, repeat with `install.ps1`.
4. The stable-only notice path (daily check against `releases/latest`) is covered by unit tests with a local
   server; the first real stable releases (`v0.1.0`, then a later one) will exercise it live. Record this as
   the remaining unverified piece.

## Verification

- README commands and file names match the code.
- Live run results recorded in the PR description or plan.

## Results (2026-11-25)

- PR #9 passed pr-gate (2m55s) and was merged as `70f17b1`. ci on main (45665145753) passed on all three OSes;
  the Windows job ran the `internal/update` tests, including the Windows executable swap.
- Live checks against the published releases are recorded in the plan's implementation notes; they replaced
  the planned `rc.3`/`rc.4` tags.
- `v0.1.0` was tagged on `70f17b1`, so it is the first release that can update itself. A scratch install of
  `v0.1.0` with the default one-liner reported "already up to date" from `social-mcp update`.
- PR #10 added the build journal that was left out of #9.
- Still unverified: a notice for a real stable release. It appears for `v0.1.0` users once a later stable
  release is published.
