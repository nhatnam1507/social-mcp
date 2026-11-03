---
title: "Phase 2: Core engine: provider contract, OAuth, file guard, runner, refresher"
status: completed
priority: P1
effort: 8h
dependencies: [1]
---

# Phase 2: Core engine

<!-- Updated: Red Team Session 1 - import-cycle-free registry, subject, refreshable, commit phases and reconcile, outcome-unknown, CAS transitions, cancel vs shutdown, stale-token-safe refresh, health rule, typed options, sanitizer, file re-validation, format enum, DTOs -->
<!-- Updated: Validation Session 1 - allowed_dirs optional; no server-side approval gate -->

## Overview

Define the provider contract and build the platform-independent machinery: OAuth sessions
with PKCE, video file guard, service layer, asynchronous upload runner and token refresher.
Providers arrive in phases 5–7; this phase uses a test-double provider in `_test.go` files only.

## Requirements

### Provider contract (`internal/provider`)

- [x] Types:
      - `Token{AccessToken, RefreshToken, Expiry, RefreshExpiry time.Time, Extra map[string]string}`
      - `Identity{ExternalID, DisplayName string, AccountToken *Token, Meta map[string]string}`
      - `Capabilities{PublishMode ("direct"|"draft"), PrivacyLevels []string, Restrictions []string, MaxFileBytes int64, MaxDurationSec int, Formats []string}`
        — `Formats` uses the enum `mp4|mov|webm|mkv`; an empty list rejects every file.
      - `OptionSpec{Name, Kind ("string"|"bool"), Description}`
      - `VideoMeta{Title, Description string, Tags []string, Privacy string, Options map[string]any}`
      - `UploadRequest{AccountExternalID string, Token Token, File io.ReaderAt, FileSize int64, Container string, Meta VideoMeta, Phase string, Resume json.RawMessage}`
      - `ProgressUpdate{BytesSent int64, Phase string, Resume json.RawMessage}` and
        `Progress func(ProgressUpdate) error` — a phase change is persisted synchronously before
        `Progress` returns; byte-only updates are throttled (2s).
      - `Result{RemoteID, URL string, Outcome ("published"|"unpublished"|"sent_to_drafts"), Note string}`
      - Errors: `ErrReauthRequired`, `ErrUnauthorized`, `ErrOutcomeUnknown` (commit may or may not
        have happened), `RateLimitError{RetryAfter}`, `PermanentError`; anything else is transient.
- [x] `Provider` interface:
      `Platform() string`, `RedirectHost() string` (`127.0.0.1` | `localhost`), `Refreshable() bool`,
      `AuthURL(state, pkceVerifier, redirectURI string) string`,
      `Exchange(ctx, code, pkceVerifier, redirectURI) (Token, error)`,
      `Refresh(ctx, Token) (Token, error)`,
      `Identities(ctx, Token) (subject string, ids []Identity, err error)` — `subject` is the stable
      login identity used as `credentials.subject`,
      `Capabilities(identityMeta map[string]string) Capabilities` (providers own all restrictions,
      including `visible_to_app_roles_only`),
      `Options() []OptionSpec`, `ValidateMeta(VideoMeta) error`,
      `Upload(ctx, UploadRequest, Progress) (Result, error)`.
- [x] Commit contract (all providers): before any non-idempotent remote call (final chunk that
      creates the video, finish/publish call), report `Phase: "committing"` with enough resume state
      to reconcile. When called with `Phase` ≥ `committing`, a provider must reconcile with the remote
      (status lookup) and must never create a new session/container/draft. If the outcome cannot be
      determined, return `ErrOutcomeUnknown`.
- [x] `provider.Registry` is a plain `map[string]Provider` type with lookup helpers; it imports no
      platform package. Construction from config lives in `internal/daemon/providers.go` (phase 3).

### OAuth sessions (`internal/oauth`)

- [x] `Sessions` begin/complete/get; state = 32 random bytes base64url, single use, 10-minute TTL;
      verifier 64 chars; `ChallengeS256Base64URL` (RFC 7636) and `ChallengeS256Hex` (TikTok desktop).
      Session states: `pending`, `succeeded{accounts}`, `failed{error}` (sanitized), `expired`.
      A reauth session carries the target account's `external_id`.

### File guard (`internal/videofile`)

- [x] `Check(path, allowedDirs) (Info, error)`: absolute path; `EvalSymlinks`; when `allowedDirs` is
      non-empty the resolved path must be inside one (separator-boundary compare, case-insensitive on
      Windows); regular file; size > 0. Container sniff: ISO-BMFF `ftyp` with major/compatible brand in
      an allowlist (`isom iso2 iso4 iso5 iso6 mp41 mp42 avc1 M4V ` → `mp4`; `qt  ` → `mov`), rejecting
      image brands (`heic heix mif1 msf1 avif`); EBML `1A45DFA3` with DocType `webm` → `webm`,
      `matroska` → `mkv`. `Info{Path, Size, ModTime, Fingerprint, Container}`; fingerprint = sha256 of
      the first and last 1 MiB.
- [x] `Open(info, allowedDirs) (*os.File, error)` used by the runner at every attempt: re-run
      `Check`, open, `Stat` the handle, require size, mtime and fingerprint to match the stored values
      (else `PermanentError("file changed since submit")`). Providers read only through this handle.

### Service (`internal/service`)

- [x] `Service` interface (control API, MCP tools and stdio client all use it):
      `Status() (StatusView, error)`, `ListAccounts(platform string) ([]AccountView, error)`,
      `RemoveAccount(id string) error`, `BeginAuth(platform, reauthAccountID string) (AuthStart, error)`,
      `AuthSession(id string) (AuthSessionView, error)`, `SubmitUpload(UploadInput) (UploadSubmission, error)`,
      `GetUpload(id string) (JobView, error)`, `ListUploads(UploadFilter) ([]JobView, error)`,
      `CancelUpload(id string) (JobView, error)`.
- [x] DTOs (JSON tags snake_case; shared verbatim by control API and MCP):
      - `UploadInput{FilePath, AccountIDs []string, Title, Description string, Tags []string, Privacy string, PlatformOptions map[string]map[string]any}`
        — `PlatformOptions[platform]` becomes `VideoMeta.Options` for that platform's accounts; keys for
        platforms not among the selected accounts are rejected.
      - `UploadSubmission{Jobs []JobRef{JobID, AccountID, Platform, Status}, Rejected []Rejection{AccountID, Reason}}`
      - `UploadFilter{Status, AccountID string, Limit int}`
      - `AccountView{ID, Platform, DisplayName, ExternalID, Health, HealthDetail string, Capabilities, Options []OptionSpec}`
      - `JobView{ID, AccountID, Platform, FilePath, Status, Phase string, BytesSent, FileSize int64, Percent float64, Outcome, RemoteID, URL, Note, Error string, CreatedAt, UpdatedAt}`
      - `StatusView{Version, PID, StartedAt, ListenURL, KeyBackend, Accounts map[platform]map[health]int, ActiveUploads int}`
      - `AuthStart{SessionID, AuthURL}`, `AuthSessionView{ID, Platform, State, Accounts []AccountView, Error}`
- [x] Health rule (one function): `needs_reauth` if the account status says so, or the account has no
      account token and its credential status says so. Otherwise compute the deadline: account token
      expiry when an account token exists; else `refresh_expires_at` for refreshable credentials or
      `expires_at` for non-refreshable ones. Past deadline → `needs_reauth`; within 7 days →
      `expiring`; unknown deadline → `ok`.
- [x] `SubmitUpload`: `videofile.Check` once (with configured `allowed_dirs`); per account: exists,
      health ≠ `needs_reauth`, privacy (default `private`) in `PrivacyLevels`, container in `Formats`,
      size ≤ `MaxFileBytes` when set, options: every key in the provider's `Options()` with the right
      kind (unknown keys rejected), `ValidateMeta`. One job per accepted account (stores file
      size/mtime/fingerprint/container). Returns `{jobs, rejected}`; all rejected → error listing reasons.
- [x] `RemoveAccount` refuses while the account has queued or uploading jobs.
- [x] `CancelUpload`: queued → `cancelled` (CAS); uploading in phase `pending`/`transferring` →
      cancel with user cause; phase ≥ `committing` → error "already being published; cannot cancel".

### Runner (`internal/runner`)

- [x] `max_concurrent` workers claim jobs with `ClaimNextUpload` (serialized platforms: `tiktok`,
      `instagram`). Each job runs under `context.WithCancelCause`.
- [x] `Start` runs `ResetInterrupted`, then claims. `Stop(ctx)` stops claiming, cancels running jobs with
      `errShutdown`, waits for workers (bounded by ctx). Jobs cancelled by shutdown keep status
      `uploading` and `attempts` unchanged.
- [x] Per attempt: `videofile.Open`; token via `internal/tokens`; call `Upload` with the latest stored
      `phase` and `resume_state`.
- [x] Outcome handling (all status writes via `TransitionUpload` from `uploading`):
      - success → `succeeded` with remote id/url/outcome/note, phase `done`.
      - user cancel → `cancelled`.
      - `ErrUnauthorized` → if `Refreshable()`, refresh once and retry in the same attempt; otherwise
        handle as `ErrReauthRequired`.
      - `ErrReauthRequired` → `failed`; mark the token owner: the account when it used an account
        token, else the credential.
      - `ErrOutcomeUnknown` → `needs_review` with a note telling the user to check the platform.
      - `RateLimitError` → back to `queued` with `not_before = now + RetryAfter` (default 60s);
        `attempts` unchanged.
      - transient → `attempts+1`; if < 3, back to `queued` with `not_before` = now + 5s/30s/2m;
        else `failed`.
      - `PermanentError` → `failed`.
- [x] Error text stored on jobs passes through `logx.Sanitize`.

### Tokens and refresher

- [x] `internal/tokens`: `Get(account) (Token, version, owner)`; `Refresh(credentialID, staleVersion)`:
      per-credential lock; reload the row; if its `version` differs from `staleVersion`, return the
      stored token without calling the provider; else call `Provider.Refresh`, then persist with
      `UpdateCredentialToken` (CAS, retry on SQLITE_BUSY up to 5 times; on final failure log an error
      and still return the new token for this attempt). Always persist the full returned token
      (rotated refresh tokens).
- [x] `internal/refresher`: every 5 minutes refresh refreshable credentials expiring within 15 minutes
      via `internal/tokens`; `ErrReauthRequired` → credential `needs_reauth` (accounts with their own
      token are unaffected by the health rule).

### Sanitizer (`internal/logx`)

- [x] `Sanitize(string) string`: masks values of `access_token`, `client_secret`, `code`,
      `fb_exchange_token`, `refresh_token`, `code_verifier` query/form params and `Authorization`
      `Bearer|OAuth` values; `*url.Error` is reduced to `op host: err`. Applied to job errors/notes,
      OAuth session errors, callback page text, and through a slog handler wrapper.
- [x] Providers send access tokens in the `Authorization` header wherever the API documents it.

## Files

- Create: `internal/provider/provider.go`, `internal/provider/registry.go`, `internal/provider/errors.go`
- Create: `internal/oauth/sessions.go`, `internal/oauth/pkce.go`, `internal/oauth/oauth_test.go`
- Create: `internal/videofile/check.go`, `internal/videofile/open.go`, `internal/videofile/check_test.go` (tests write tiny header files into `t.TempDir()`)
- Create: `internal/service/service.go` (interface + DTOs), `internal/service/health.go`, `internal/service/local.go`, `internal/service/local_test.go`, `internal/service/health_test.go`
- Create: `internal/tokens/tokens.go`, `internal/tokens/tokens_test.go`
- Create: `internal/runner/runner.go`, `internal/runner/runner_test.go`
- Create: `internal/refresher/refresher.go`, `internal/refresher/refresher_test.go`
- Create: `internal/logx/sanitize.go`, `internal/logx/sanitize_test.go`

## Implementation Steps (test first)

1. PKCE tests: RFC 7636 appendix B vector (base64url) and hex of the same digest; state single use and
   TTL with an injected clock.
2. videofile tests: outside allowed dir rejected (when configured); empty `allowed_dirs` accepts any
   absolute path; relative path rejected; symlink escape rejected when restricted (skip if symlinks
   unsupported); sibling prefix rejected; HEIC/AVIF `ftyp` rejected; `qt  ` → mov; webm vs mkv DocType;
   `Open` fails after the file is rewritten (size or mtime or fingerprint change).
3. health tests: table covering Facebook-like account token (credential expired → account `ok`),
   Instagram-like (non-refreshable credential within 7 days → `expiring`, past → `needs_reauth`),
   refreshable credential (deadline = refresh expiry; unknown → `ok`).
4. service tests: privacy default and rejection; unknown option key rejected; option kind mismatch;
   platform_options for an unselected platform rejected; mixed accept/reject; `needs_reauth` account
   rejected; all-rejected error; remove refused with active job; cancel refused in `committing`.
5. tokens tests: concurrent refresh calls the provider once; a stale caller after a rotation gets the
   stored token without a provider call; a CAS conflict with a concurrent reauth write does not
   overwrite the newer token.
6. runner tests: success persists result; transient → requeued with `not_before`, then succeeds;
   rate limit does not consume attempts; permanent not retried; `ErrUnauthorized` refreshes once
   (refreshable) and maps to reauth (non-refreshable); reauth with account token marks only the account;
   `ErrOutcomeUnknown` → `needs_review`; user cancel → `cancelled`; `Stop` leaves the job `uploading`
   with unchanged attempts, and a restarted runner resumes it with the stored phase/resume state;
   a provider failing after reporting `committing` is called again with `Phase: committing`; one
   account failing does not affect another; two jobs for the same TikTok account never run concurrently.
7. refresher tests: refreshes near-expiry; `ErrReauthRequired` marks the credential.
8. sanitize tests: planted secrets in URLs, form bodies, headers and `*url.Error` are masked.
9. Implement until green.

## Todo

- [x] provider contract + registry map
- [x] oauth sessions + PKCE
- [x] videofile check/open + format enum
- [x] health rule
- [x] service + DTOs
- [x] tokens helper
- [x] runner
- [x] refresher
- [x] sanitizer

## Verification

- `go test -race ./internal/...`
- `go vet ./...`

## Risks

- Commit/reconcile correctness depends on each provider honoring the contract; phases 5–7 each include
  a "lost response after commit" test.
- Polling (TikTok/Instagram processing) runs inside the worker and holds a slot; acceptable with
  `max_concurrent = 2` and per-account serialization; documented.

## Success Criteria

All tests pass with `-race`; no code path can re-run a commit step after `committing` was recorded;
user cancel and shutdown are distinguishable; secrets never reach stored job or session errors.
