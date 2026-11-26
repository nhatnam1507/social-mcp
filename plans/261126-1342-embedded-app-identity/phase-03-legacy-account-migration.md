---
title: "Phase 3: Legacy account migration"
status: done
priority: P1
effort: 2.5h
dependencies: [1, 2]
---

# Phase 3: Legacy account migration

## Goal

Record which app (client ID) issued each login, and on startup mark logins from a different app as
`needs_reauth` with guidance that works for each platform.

## Key Insights

- Tokens are bound to the app that issued them. After upgrade, logins made with a user's own app can't be
  refreshed with the owner's secret.
- Facebook Page tokens never expire and would keep working until the user deletes their old app. They are
  still marked, so behavior is predictable and nothing silently breaks later.
- Account external IDs differ by platform:
  - YouTube channel IDs, Facebook Page IDs and Instagram business IDs are global, so `account reauth <id>`
    through the new app finds the same account. `UpsertLogin` already moves accounts to the new credential.
  - TikTok `open_id` is per app, so a reauth through the new app returns a different ID and the existing
    "signed-in account does not include the account being re-authorized" check fails. TikTok legacy
    accounts need `account add tiktok` followed by `account remove <old-id>`.
- Old config files still hold the user's client IDs (now retired keys). Reading only those public IDs lets
  env-override users on the same app keep working.

## Files to Create / Modify

- Create: `internal/store/migrations/0003_credential_client_id.sql`: `ALTER TABLE credentials ADD COLUMN client_id TEXT NOT NULL DEFAULT '';`
- Modify: `internal/store/accounts.go`: `Credential.ClientID`; `UpsertLogin` writes it; add `MarkLegacyCredentials(platform, activeClientID string, backfillClientID string)` which, in one transaction, backfills empty `client_id` with `backfillClientID` when given, then sets `status = 'needs_reauth'` on credentials and their accounts whose `client_id` differs from `activeClientID` (or the platform has no active app).
- Modify: `internal/store/store_test.go`
- Modify: `internal/config/config.go`: expose the retired public IDs (`youtube.client_id`, `tiktok.client_key`, `meta.app_id`) as `LegacyClientIDs map[string]string`; never the secrets.
- Modify: `internal/service/local.go`: `completeAuth` stores the active client ID on the credential.
- Modify: `internal/daemon/daemon.go`: after the store opens, reconcile each platform (`youtube`, `tiktok`, `facebook`, `instagram`) against the resolved apps.
- Modify: `internal/service/health.go`, `internal/cli/account.go`: a legacy-marked account's hint says "reconnect with `social-mcp account reauth <id>`", or for TikTok "run `social-mcp account add tiktok`, then `social-mcp account remove <id>`".

## Tasks & Steps

1. Back up is automatic (migrations take a `VACUUM INTO` copy); confirm the new migration runs through that path.
2. Store tests first: backfill only touches empty rows; mismatch marks credential and accounts; matching client ID leaves them `ok`; a fresh login clears the mark.
3. Implement migration, store changes and `completeAuth` change.
4. Daemon reconcile at startup, idempotent on every start.
5. Health/CLI hint text, with a test for the TikTok variant.

## Verification

- `go test ./internal/store/... ./internal/service/... ./internal/daemon/... ./internal/cli/...`
- Manual: copy a pre-upgrade home, start the new daemon, check `account list` shows `needs_reauth` with the right hint; set the env override to the old app and confirm the accounts stay `ok`.

## Success Criteria

- No legacy account is refreshed with the wrong app's secret.
- Every legacy account has a one-command path back to working.

## Risk Assessment

- **Reconcile marks everything when a source build has no apps:** expected (no app means no refresh); the hint points to the env override.
- **Migration on a large DB:** single `ALTER TABLE ADD COLUMN` with a default, constant time in SQLite.

## Security Considerations

Only public client IDs are read from retired config keys; retired secrets are ignored and never stored.
