---
title: "Phase 1: Foundation: module, config, paths, secrets, store"
status: todo
priority: P1
effort: 5h
dependencies: []
---

# Phase 1: Foundation

<!-- Updated: Red Team Session 1 - key-split protection, home ownership, schema for health/versioning/job phases, single-writer store, VACUUM INTO backup -->
<!-- Updated: Validation Session 1 - allowed_dirs optional (empty = any path) -->

## Overview

Create the Go module and the storage layer everything else builds on: home-directory
layout, TOML config, master-key management with AES-GCM sealing, and the SQLite store
with migrations and pre-migration backup.

## Requirements

- [ ] `go.mod` for `github.com/nhatnam1507/social-mcp`, Go 1.26; deps: `modernc.org/sqlite`,
      `github.com/zalando/go-keyring`, `github.com/BurntSushi/toml`, `golang.org/x/sys`.
- [ ] Home dir: `$SOCIAL_MCP_HOME` if set, else `os.UserConfigDir()/social-mcp`. Created with
      mode 0700. On Unix, startup checks with `Lstat`: not a symlink, owned by the current uid;
      if owned by us but group/other bits are set, chmod to 0700 and log a warning; if owned by
      someone else, refuse to start. Windows: no mode checks (documented reliance on the profile ACL).
      Files: `config.toml`, `social-mcp.db`, `master.key` (file backend only), `mcp-token`,
      `daemon.lock`, `daemon.pid`, `daemon.log`, `daemon.sock` (see phase 3 for long-path fallback),
      `backups/`.
- [ ] Config schema (TOML), with defaults and validation:
      - `[server] listen_port = 8765` (loopback listener for MCP and OAuth callback)
      - `[uploads] allowed_dirs = []` (optional; empty means any absolute path is accepted;
        entries must be absolute), `max_concurrent = 2`
      - `[youtube] client_id, client_secret`
      - `[tiktok] client_key, client_secret`
      - `[meta] app_id, app_secret, login_config_id, graph_version = "v26.0", app_mode = "development"`
      - Missing platform credentials make that platform unavailable, not a startup error.
- [ ] `config.Template()` produces a commented starter file (used by `config init` in phase 3);
      `allowed_dirs` appears as a commented example only.
- [ ] Secrets: master key = 32 random bytes, behind a `Keyring` interface (real: go-keyring; tests:
      in-memory fake — tests never touch the OS keyring). Keyring entry: service `social-mcp`, user
      `master-key-<first 12 hex of sha256(home path)>` so separate homes never share a key.
      The DB `meta` table records `key_backend` (`keyring` | `file`) and `key_check`
      (`Seal("social-mcp-key-check")`).
      - Fresh home (no `key_check`): try keyring; `ErrNotFound` → create key there; keyring
        unavailable or timed out (3s) → create `master.key` (0600). Record backend + check value.
      - Existing home: load only from the recorded backend. Unavailable/timeout → hard error with
        guidance ("keyring unavailable; retry or see docs"). A key that fails `key_check` → hard
        error. Never create a second key when `key_check` exists.
- [ ] `secrets.Sealer` with `Seal(plain []byte) ([]byte, error)` / `Open(sealed []byte)`;
      AES-256-GCM, version byte, random 12-byte nonce.
- [ ] Store (SQLite, `modernc.org/sqlite`, WAL, `busy_timeout=5000`): one write handle with
      `SetMaxOpenConns(1)` and immediate transactions (`_txlock=immediate`), separate read pool.
      Embedded migrations tracked by `PRAGMA user_version`. Before applying any migration to a DB
      with `user_version > 0`, write `VACUUM INTO 'backups/social-mcp-<unix>-v<from>.db'`; keep the
      newest 5 backups.
- [ ] Schema v1:
      - `meta(key PK, value)`
      - `credentials(id PK, platform, subject, sealed_token BLOB, expires_at NULL, refresh_expires_at NULL, refreshable INT, status, version INT, created_at, updated_at, UNIQUE(platform, subject))`
      - `accounts(id PK, platform, credential_id FK, external_id, display_name, sealed_account_token BLOB NULL, account_token_expires_at NULL, status, meta JSON, created_at, updated_at, UNIQUE(platform, external_id))`
      - `uploads(id PK, account_id FK, file_path, file_size, file_mtime, file_fingerprint, container, request JSON, status, phase, bytes_sent, remote_id, remote_url, outcome, note, error, resume_state JSON, attempts, not_before NULL, created_at, updated_at)`
      - Upload `status`: `queued | uploading | succeeded | failed | cancelled | needs_review`.
        Upload `phase`: `pending | transferring | committing | processing | done`.
- [ ] Store API:
      - Credentials/accounts CRUD; `UpsertLogin(credential, accounts)` in one transaction
        (credential matched by `(platform, subject)`, accounts by `(platform, external_id)`, IDs stable).
      - `UpdateCredentialToken(id, expectedVersion, sealed, expiries)` → compare-and-swap on `version`.
      - `TransitionUpload(id, fromStatus, toStatus, fields)` → `UPDATE … WHERE id=? AND status=?`,
        returns whether it applied.
      - `ClaimNextUpload(now, serializedPlatforms)` → atomically moves one eligible `queued` job
        (`not_before` null or past) to `uploading`; for platforms in `serializedPlatforms`, skips
        accounts that already have an `uploading` job.
      - `ListUploads(filter)`, `HasActiveUploads(accountID)`, `ResetInterrupted()` (uploading → queued,
        `attempts` unchanged).
- [ ] IDs: `acc_`/`cred_`/`job_` + 12 hex chars from `crypto/rand`.

## Files

- Create: `go.mod`, `go.sum`, `.gitignore`
- Create: `internal/paths/paths.go`, `internal/paths/owner_unix.go`, `internal/paths/owner_windows.go`, `internal/paths/paths_test.go`
- Create: `internal/config/config.go`, `internal/config/template.go`, `internal/config/config_test.go`
- Create: `internal/secrets/keystore.go`, `internal/secrets/sealer.go`, `internal/secrets/secrets_test.go`
- Create: `internal/store/store.go`, `internal/store/migrations.go`, `internal/store/migrations/0001_init.sql`,
  `internal/store/accounts.go`, `internal/store/uploads.go`, `internal/store/store_test.go`
- Create: `internal/ids/ids.go`

## Implementation Steps (test first)

1. paths tests: honors `SOCIAL_MCP_HOME`; creates 0700; tightens a 0755 dir we own; refuses a symlinked
   home (Unix-only assertions guarded by build tags).
2. config tests: template round-trips; rejects `listen_port` out of range and relative `allowed_dirs`;
   empty `allowed_dirs` valid; platform availability flags.
3. secrets tests (fake keyring): sealer round-trip, tamper and wrong-key failures; fresh home with
   keyring `ErrNotFound` creates keyring key; keyring unavailable on fresh home → file backend;
   existing home with recorded `keyring` backend and keyring timing out → error, and no file key is
   created; mismatched key fails `key_check`; two homes derive different keyring users.
4. store tests: fresh DB at v1; reopen no-op; with a test-only migration list (v1 + synthetic v2) a
   populated v1 DB gets a `VACUUM INTO` backup before v2 applies, and only 5 backups are retained;
   `UpsertLogin` keeps IDs stable; `TransitionUpload` refuses a stale `from`; `UpdateCredentialToken`
   refuses a stale version; `ClaimNextUpload` never returns the same job twice under 8 concurrent
   claimers (`-race`) and honors `not_before` and per-account serialization; `ResetInterrupted` flips
   only `uploading`.
5. Implement until green.

## Todo

- [ ] Module and `.gitignore` (ignore `dist/`, `*.db`)
- [ ] paths + ownership checks + tests
- [ ] config + template + tests
- [ ] secrets keystore (backend record, key check) + sealer + tests
- [ ] store (single writer, CAS helpers, claim, backup) + tests

## Verification

- `go test -race ./internal/paths/... ./internal/config/... ./internal/secrets/... ./internal/store/...`
- `go vet ./...`
- `CGO_ENABLED=0 GOOS=windows go build ./...` and `GOOS=darwin` compile.

## Risks

- A keyring that is slow at login now fails the daemon start instead of silently re-keying; the
  error tells the user to retry. This is the intended trade-off.
- Windows ignores Unix modes; docs say so.

## Success Criteria

All listed tests pass; a fresh home produces a v1 DB, a key from the recorded backend, and no
plaintext token on disk; a second key can never be created for a home with sealed data.
