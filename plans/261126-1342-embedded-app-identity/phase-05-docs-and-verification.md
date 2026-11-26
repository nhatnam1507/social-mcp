---
title: "Phase 5: Docs and end-to-end verification"
status: done
priority: P2
effort: 2h
dependencies: [1, 2, 3, 4]
---

# Phase 5: Docs and end-to-end verification

## Goal

Make the docs match zero-setup usage, move app registration into a maintainer doc, and verify the full flow
on a real release-style build.

## Files to Create / Modify

- Create: `docs/maintainer-oauth-apps.md` (moved from `docs/platform-setup.md` with `git mv`), rewritten for
  the owner: how each app is registered (scopes, redirect URIs on port 8765, consent/app mode), the GitHub
  secrets and the release guard, how to rotate a secret and what users see, platform review status and
  limits (shared YouTube quota of 100 uploads/day per project), the accepted embedding risks, and the
  future hosted-broker swap via `appauth.TokenEndpoint`. Add a short "Use your own app" section listing the
  `SOCIAL_MCP_*` override vars for power users.
- Modify: `README.md`:
  - Quick start drops the credential-editing step.
  - The "register your own developer app" sentence goes; the "tokens never leave your machine except to the
    platform" claim stays (still true).
  - Fixed port 8765 noted in the security model.
  - Upgrade note: existing accounts show `needs_reauth`; how to reconnect (TikTok: add, then remove).
  - Documentation links updated.
- Modify: `docs/architecture.md`: add `internal/appauth` to the component list and diagram; storage table
  says `config.toml` holds settings only; note `credentials.client_id`.
- Modify: `internal/config/template.go` header comment: point to the README, not platform-setup.

## Tasks & Steps

1. `git mv docs/platform-setup.md docs/maintainer-oauth-apps.md` and rewrite.
2. Update README and architecture; grep for stale references:
   `grep -rn "platform-setup\|listen_port\|client_secret\|app_secret" README.md docs internal --include=*.go --include=*.md`.
3. End-to-end on a build made with the owner's real identity (owner runs locally with the env vars, or a prerelease tag):
   - Fresh `SOCIAL_MCP_HOME`, no `config.toml`: `daemon start`, `account add` for each platform, one private upload each.
   - Upgrade path: old home with legacy accounts and old config → warnings, `needs_reauth`, reconnect works.
   - Rotation drill (optional): invalid secret in the override → the `ErrAppCredentials` message appears in CLI and `list_accounts`.
4. Run `make test`, `make lint`, `make test-e2e`.

## Verification

- All links in README and docs resolve.
- Every claim in the security model matches the code.

## Success Criteria

- A new user can go from install to first upload without visiting any developer console.
- The owner can re-register an app or rotate a secret using only `docs/maintainer-oauth-apps.md`.

## Risk Assessment

Docs claiming more protection than exists: the maintainer doc states plainly that embedded secrets are
extractable and why that was accepted.
