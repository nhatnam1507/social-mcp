---
title: "Ship the owner's OAuth app identity inside the binary"
description: "Users connect accounts with zero platform setup: release binaries carry the owner's YouTube, TikTok and Meta app identity behind one token-endpoint seam that can later move to a hosted broker."
status: completed
priority: P1
effort: 13h
branch: feat/embedded-app-identity
tags: [feature, auth, security, release, docs]
blockedBy: []
blocks: []
created: 2026-11-26
---

# Ship the owner's OAuth app identity inside the binary

## Overview

Today every user registers their own developer apps and pastes client IDs and secrets into `config.toml`.
After this plan:

- Release binaries carry the owner's app identity for YouTube, TikTok and Meta, injected at build time from
  GitHub Actions secrets through `-ldflags`. Nothing is committed to the repo.
- Users run `daemon start` → `account add <platform>` → `config mcp`. `config.toml` has no platform
  sections and no port setting.
- Every call that needs an app secret (code exchange, refresh, Meta long-lived exchange) goes through one
  `appauth.TokenEndpoint` seam. Providers only see the public client ID. A hosted broker can later replace
  the embedded endpoint per platform without touching providers, the OAuth flow or storage.
- Power users override the embedded identity with `SOCIAL_MCP_*` env vars (all-or-nothing per platform).
- The loopback port is fixed at 8765, matching the redirect URIs registered on the owner's apps.
- Accounts connected through a user's own app are marked `needs_reauth` after upgrade, unless the env
  override points at that same app.
- User tokens stay only on the user's machine; nothing is sent anywhere except to the platform itself.

```
account add ─▶ daemon: state + PKCE ─▶ browser: platform consent (public client_id)
            ◀─ 127.0.0.1:8765/oauth/callback?code&state
daemon ─▶ appauth.TokenEndpoint.Do(grant params)        ◀── only place secrets are used
            embedded now: adds client_id + secret, calls the platform token URL
            hosted broker later: same call, forwarded over HTTPS
       ─▶ tokens sealed in local SQLite (unchanged)
```

## Decisions (accepted 2026-11-26)

| Topic | Decision |
|---|---|
| Secrets | Embedded in release binaries for all three platforms; accepted risk: Meta/TikTok terms, extractable, rotation needs a release |
| Future broker | Not built now; the seam keeps the move to a hosted broker a per-platform swap |
| Override | Env vars only; `[youtube]`, `[tiktok]`, `[meta]` removed from config |
| Port | Fixed 8765; `server.listen_port` removed |
| Existing accounts | Marked `needs_reauth`; env-override users on the same app keep working |
| Docs | `docs/platform-setup.md` becomes the maintainer doc `docs/maintainer-oauth-apps.md` |

## Non-goals

- Building or hosting the remote broker.
- Obfuscating the embedded secrets.
- Platform reviews (Google verification, TikTok audit, Meta App Review); tracked in the maintainer doc only.

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | [App identity and token endpoint seam](./phase-01-start.md) | Pending |
| 2 | [Config cleanup and fixed port](./phase-02-config-and-fixed-port.md) | Pending |
| 3 | [Legacy account migration](./phase-03-legacy-account-migration.md) | Pending |
| 4 | [Release injection and owner apps](./phase-04-release-injection-and-owner-apps.md) | Pending |
| 5 | [Docs and end-to-end verification](./phase-05-docs-and-verification.md) | Pending |

Phases run in order; 2 and 3 depend on 1, 4 depends on 1, 5 depends on all.

## Success Criteria

- [ ] A release binary with no `config.toml` can `account add` each platform and upload.
- [ ] `grep -rn "client_secret\|app_secret" internal/provider` finds no secret fields; only `internal/appauth` handles secrets.
- [ ] Source builds without embedded values report a clear "set SOCIAL_MCP_* to use your own app" error per platform.
- [ ] An old `config.toml` with removed keys still loads, with a one-line warning naming the ignored keys.
- [ ] Legacy accounts show `needs_reauth` with platform-specific reconnect guidance.
- [ ] A tag release fails fast if any identity secret is missing from GitHub Actions.
- [ ] `make test` and `make lint` pass; no code, token, verifier or secret appears in logs or errors.

## Implementation notes (2026-11-26)

- Scope for now: only YouTube has a registered app. The release guard requires only the YouTube secrets;
  TikTok and Meta are wired through but optional, so releases without them don't offer those platforms.
- Local development: `make run` sources a git-ignored `./.env` (template `.env.example`) into the daemon's
  environment, so the developer's app is used through the `SOCIAL_MCP_*` override with no build-time secrets.
- Meta's token calls moved from GET with query parameters to POST with a form body, so no secret can appear
  in a URL or transport error.
- The legacy-login check skips platforms with no app in the running build; it runs again on the start where
  an app becomes available.
- `SOCIAL_MCP_LISTEN_PORT` stays as a development/test-only override of the fixed port 8765.
- The maintainer verified sign-in and a private upload with the real YouTube app through `make run`.
