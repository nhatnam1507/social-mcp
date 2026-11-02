---
title: "Phase 5: YouTube provider"
status: completed
priority: P1
effort: 4h
dependencies: [2, 3]
---

# Phase 5: YouTube provider

<!-- Updated: Red Team Session 1 - subject = channel id, commit/reconcile on final chunk, typed options, publish_at removed, registration in daemon/providers.go -->

## Overview

Implement the YouTube Data API v3 provider: Google OAuth (desktop app, loopback, PKCE), channel
discovery, and resumable upload.

## Requirements

- [x] OAuth: auth `https://accounts.google.com/o/oauth2/v2/auth` with `client_id`, `redirect_uri`,
      `response_type=code`, `scope="https://www.googleapis.com/auth/youtube.upload https://www.googleapis.com/auth/youtube.readonly"`,
      `access_type=offline`, `prompt=consent`, `state`, `code_challenge` (S256 base64url),
      `code_challenge_method=S256`. Token `https://oauth2.googleapis.com/token` (POST form:
      `authorization_code` with `code_verifier` and `client_secret`; `refresh_token` grant).
      `invalid_grant` on refresh → `ErrReauthRequired`. Keep the old refresh token when a refresh response
      omits it. `Refreshable() = true`, `RedirectHost() = "127.0.0.1"`.
- [x] Identities: `GET https://www.googleapis.com/youtube/v3/channels?part=snippet&mine=true` (Bearer
      header) → identities per returned channel (`id`, `snippet.title`). The consent screen binds the
      token to the chosen channel, so normally exactly one is returned. `subject` = the channel id, or
      the sorted channel ids joined with `,` if several are returned. Zero → error "this Google account
      has no YouTube channel".
- [x] Capabilities: direct; privacy `private|unlisted|public`; formats `mp4 mov webm mkv`; max 256GB;
      restriction `quota_100_uploads_per_day_per_project`.
- [x] Options: `category_id` (string, default `"22"`), `made_for_kids` (bool, default false).
      ValidateMeta: title required, ≤ 100 chars, no `<`/`>`; description ≤ 5000 bytes.
- [x] Upload (resumable): `POST https://www.googleapis.com/upload/youtube/v3/videos?uploadType=resumable&part=snippet,status`
      with JSON `{snippet:{title, description, tags, categoryId}, status:{privacyStatus, selfDeclaredMadeForKids}}`,
      headers `X-Upload-Content-Length`, `X-Upload-Content-Type`; session URI from `Location`
      (reported as phase `transferring`, resume `{session_uri}`). PUT 8 MiB chunks (multiple of 256 KiB)
      with `Content-Range: bytes a-b/total`; 308 → next offset from `Range`. Before the final chunk,
      report phase `committing`. 200/201 → video resource → `Result{RemoteID, URL: https://www.youtube.com/watch?v=<id>, Outcome: published (public/unlisted) | unpublished (private)}`.
- [x] Resume / reconcile: on any resume, or on a network error/timeout during a chunk, query the session
      with `Content-Range: bytes */total`: 200/201 → success with the returned resource; 308 → continue
      from `Range`; 404/410 → in phase `transferring` start a new session, in phase `committing` return
      `ErrOutcomeUnknown`.
- [x] Errors: 401 → `ErrUnauthorized`; 403 `quotaExceeded`/`uploadLimitExceeded`/`forbidden` and 400 →
      `PermanentError` with the API reason; 429 → `RateLimitError` (honor `Retry-After`); 5xx/network →
      transient (after the reconcile query above).
- [x] Register in `internal/daemon/providers.go` when `[youtube]` is configured.

## Files

- Create: `internal/provider/youtube/youtube.go`, `internal/provider/youtube/upload.go`, `internal/provider/youtube/youtube_test.go`
- Modify: `internal/daemon/providers.go`

## Implementation Steps (test first)

1. AuthURL params and S256 challenge.
2. Exchange/Refresh form fields; `invalid_grant` → reauth; omitted refresh token kept.
3. Identities: one channel → subject = id; two channels → deterministic subject; none → error.
4. Upload against a fake resumable server validating headers/ranges: 20 MiB in 3 chunks succeeds and
   reports `committing` before the last chunk; final-chunk response lost (connection closed after the
   server stored the video) → reconcile query returns 201 → success with exactly one video created;
   session 404 in `transferring` restarts; session 404 in `committing` → `ErrOutcomeUnknown`;
   403 quotaExceeded permanent; 429 → `RateLimitError`.
5. Implement until green.

## Todo

- [x] OAuth
- [x] identities + subject
- [x] options + metadata validation
- [x] resumable upload with commit/reconcile
- [x] error mapping
- [x] registration

## Verification

- `go test -race ./internal/provider/youtube/...`

## Risks

- OAuth app in "Testing" status → refresh tokens expire after 7 days with no advance signal; docs tell
  users to set the consent screen to "In production". Expiry surfaces as `needs_reauth` after the
  refresh fails.

## Success Criteria

Contract tests cover auth, discovery, chunked/resumed upload, lost-final-response reconciliation and
error mapping; provider registered only when configured.
