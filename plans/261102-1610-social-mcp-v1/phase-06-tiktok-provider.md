---
title: "Phase 6: TikTok provider (inbox)"
status: completed
priority: P1
effort: 4h
dependencies: [2, 3]
---

# Phase 6: TikTok provider (upload to inbox)

<!-- Updated: Red Team Session 1 - decimal chunk thresholds, processing phase never re-inits, outcome-unknown on poll timeout, rate limit type, note via Result, registration in daemon/providers.go -->

## Overview

Implement TikTok Login Kit for Desktop (loopback + PKCE with hex challenge) and the Content Posting
API **upload to inbox** flow. Videos land as drafts in the creator's TikTok inbox; the user finishes
posting in the app. Direct Post is out of v1.

## Requirements

- [x] OAuth: auth `https://www.tiktok.com/v2/auth/authorize/` with `client_key`,
      `scope=user.info.basic,video.upload`, `response_type=code`, `redirect_uri`, `state`,
      `code_challenge` = **hex** SHA-256 of the verifier, `code_challenge_method=S256`.
      Token `POST https://open.tiktokapis.com/v2/oauth/token/` (form: `client_key`, `client_secret`,
      `code`, `grant_type=authorization_code`, `redirect_uri`, `code_verifier`; refresh with
      `grant_type=refresh_token`). Use `expires_in` (~2h) and `refresh_expires_in` (~180 days) from
      responses. Persist rotated refresh tokens. `error=invalid_grant` → `ErrReauthRequired`.
      `Refreshable() = true`, `RedirectHost() = "127.0.0.1"`.
- [x] Identities: `GET https://open.tiktokapis.com/v2/user/info/?fields=open_id,display_name,avatar_url`
      → one identity; `subject` = `open_id`.
- [x] Capabilities: `publish_mode=draft`; privacy levels `private unlisted public` accepted but not
      applied; restrictions `draft_only`, `privacy_set_in_app`, `max_5_pending_drafts_per_24h`;
      formats `mp4 mov webm`; max 4,000,000,000 bytes; duration limit 600s documented, not enforced.
- [x] Options: none. ValidateMeta accepts title/description/tags (not sent by the inbox API). `Result.Note`
      = "Draft sent to the TikTok inbox. Open the TikTok app notification to add the caption, choose
      privacy and post."
- [x] Chunk plan (decimal bytes, valid under either MB reading): size < 10,000,000 → one chunk
      (`chunk_size = size`); else `chunk_size = 10,000,000`, `total_chunk_count = floor(size / chunk_size)`,
      final chunk = chunk_size + remainder (< 20,000,000). Max 400 chunks at 4 GB. Reject sizes above
      4,000,000,000.
- [x] Upload:
      1. `POST https://open.tiktokapis.com/v2/post/publish/inbox/video/init/` (Bearer) JSON
         `{source_info:{source:"FILE_UPLOAD", video_size, chunk_size, total_chunk_count}}` →
         `publish_id`, `upload_url`; report phase `transferring` with resume
         `{publish_id, upload_url, next_chunk, started_at}`.
      2. PUT chunks with `Content-Type` from container (`video/mp4`, `video/quicktime`, `video/webm`),
         `Content-Length`, `Content-Range: bytes a-b/total`; 206 until the last chunk. Before the last
         chunk report phase `committing`; 201 → phase `processing`.
      3. Poll `POST https://open.tiktokapis.com/v2/post/publish/status/fetch/` `{publish_id}` every 5s,
         up to 10 minutes: `SEND_TO_USER_INBOX` → `Outcome: sent_to_drafts`; `PUBLISH_COMPLETE` →
         `published`; `FAILED` → `PermanentError(fail_reason)`; still processing at the deadline →
         `ErrOutcomeUnknown` (note: check the TikTok inbox).
- [x] Resume: phase `processing` or `committing` → poll status only (any age), never re-init. Phase
      `transferring` within 55 minutes of `started_at` → continue at `next_chunk`; older → new init (the
      old upload never completed, so no draft exists). 403 on PUT in `transferring` → new init once.
- [x] Errors: 401 / `access_token_invalid` → `ErrUnauthorized`; `spam_risk_too_many_pending_share`,
      `spam_risk_user_banned_from_posting`, `scope_not_authorized`, `url_ownership_unverified` →
      `PermanentError` with a clear message; 429 `rate_limit_exceeded` → `RateLimitError` (60s default).
- [x] Register in `internal/daemon/providers.go` when `[tiktok]` is configured.

## Files

- Create: `internal/provider/tiktok/tiktok.go`, `internal/provider/tiktok/upload.go`, `internal/provider/tiktok/tiktok_test.go`
- Modify: `internal/daemon/providers.go`

## Implementation Steps (test first)

1. AuthURL uses `client_key`, comma-separated scopes, hex challenge for a known verifier.
2. Exchange/Refresh form fields; rotated refresh token returned; `invalid_grant` → reauth.
3. Chunk plan table: 1, 4,999,999, 9,999,999, 10,000,000, 64,000,001, 200,000,000, 4,000,000,000,
   4,000,000,001 bytes.
4. Upload against a fake server enforcing Content-Range order (416 on mismatch) and 206/201; status
   polling to `SEND_TO_USER_INBOX`; `FAILED` permanent; pending-share error permanent; poll deadline →
   `ErrOutcomeUnknown`; resume in `processing` polls without calling init (init call count asserted);
   resume in `transferring` within window continues, after window re-inits.
5. Implement until green.

## Todo

- [x] OAuth with hex PKCE
- [x] identity + subject
- [x] chunk planner
- [x] inbox upload + phases + polling + resume
- [x] error mapping
- [x] registration

## Verification

- `go test -race ./internal/provider/tiktok/...`

## Risks

- Inbox upload for sandbox target users is unverified (human probe).
- Status-fetch field names follow the published reference; tests encode the documented response.

## Success Criteria

Contract tests pass; a completed job reports `sent_to_drafts` with the in-app note; no path creates a
second draft once the final chunk was sent.
