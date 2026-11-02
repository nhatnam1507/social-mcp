---
title: "Phase 7: Meta providers: Facebook Pages, Instagram Reels"
status: todo
priority: P1
effort: 6h
dependencies: [2, 3]
---

# Phase 7: Meta providers (Facebook Pages, Instagram Reels)

<!-- Updated: Red Team Session 1 - Page-token flow recorded as a deliberate choice and marked unverified, account-scoped reauth, commit/reconcile for finish and media_publish, Authorization header, typed options, subjects, registration in daemon/providers.go -->
<!-- Updated: Validation Session 1 - loopback-only login (no manual paste fallback) -->

## Overview

One Meta app (Facebook Login for Business, `config_id`) backs two providers. `facebook` accounts are
Pages and upload with Page tokens via the Page `/videos` chunked flow. `instagram` accounts are
Instagram professional accounts linked to Pages and upload Reels via the resumable `rupload` flow
with a long-lived user token (~60 days, no refresh).

## Decision record: Facebook upload flow

- The brainstorm's "Facebook accounts need re-authorization about every 60 days" applies to Meta's
  generic Resumable Upload API (`/<APP_ID>/uploads`), which authenticates with a **user** token.
- This plan uses the Page `/videos` edge with `upload_phase=start|transfer|finish`, listed in the Graph
  API Page Videos reference, which authenticates with a **Page** token. Page tokens derived from a
  long-lived user token do not expire, so Facebook account health does not follow the user token.
- **[UNVERIFIED live]** Added to the human Meta probe. If the probe shows `upload_phase` no longer works,
  switch the Facebook provider to the Resumable Upload API with the user token; then Facebook accounts
  carry no account token and their health follows the credential (no other code changes needed).

## Requirements

- [ ] Shared `internal/provider/meta`: graph base `https://graph.facebook.com/<graph_version>`, video base
      `https://graph-video.facebook.com/<graph_version>`, rupload base
      `https://rupload.facebook.com/ig-api-upload/<graph_version>`. Access tokens go in
      `Authorization: OAuth <token>`. Graph error parsing (`error.code`, `error_subcode`, `message`,
      `is_transient`): code 190 → `ErrReauthRequired` (Meta tokens cannot be refreshed); codes 4/17/32/613
      or `is_transient` → `RateLimitError`/transient; codes 10 and 200–299 → `PermanentError`.
      `Refreshable() = false`, `RedirectHost() = "localhost"`.
- [ ] OAuth: dialog `https://www.facebook.com/<graph_version>/dialog/oauth` with `client_id`,
      `redirect_uri` (`http://localhost:<port>/oauth/callback`), `state`, `config_id`, `response_type=code`.
      Exchange `GET /oauth/access_token?client_id&client_secret&redirect_uri&code` → short-lived user
      token → `GET /oauth/access_token?grant_type=fb_exchange_token&client_id&client_secret&fb_exchange_token`
      → long-lived user token (`expires_in` ≈ 60 days). These query strings carry secrets: errors pass
      through `logx.Sanitize`, and the HTTP client never logs URLs. `subject` = `GET /me?fields=id` id.
- [ ] Facebook identities: `GET /me/accounts?fields=id,name,access_token,tasks` (follow paging) → one
      identity per Page with `CREATE_CONTENT` in `tasks`; `AccountToken` = Page token without expiry.
- [ ] Instagram identities: `GET /me/accounts?fields=id,name,instagram_business_account{id,username}` →
      one identity per linked IG account (`id`, `@username`); no account token (uses the credential).
- [ ] Capabilities (both add `visible_to_app_roles_only` when `meta.app_mode = "development"`):
      facebook: direct; privacy `public` (→ `published=true`) and `private` (→ unpublished Page video,
      `Outcome: unpublished`); formats `mp4 mov`. instagram: direct; privacy `public` only; restriction
      `no_private_option`; formats `mp4 mov`.
- [ ] Options: facebook none; instagram `share_to_feed` (bool, default true). ValidateMeta: facebook
      title ≤ 255 chars; instagram caption = title + "\n\n" + description + hashtags from tags, ≤ 2200
      chars, ≤ 30 hashtags.
- [ ] Facebook upload: `POST {video}/{page_id}/videos` `upload_phase=start&file_size` →
      `upload_session_id, video_id, start_offset, end_offset` (phase `transferring`, resume
      `{upload_session_id, video_id}`); loop `upload_phase=transfer` multipart (`upload_session_id`,
      `start_offset`, `video_file_chunk` = bytes start..end) until `start_offset == end_offset`; report
      phase `committing`; `upload_phase=finish` with `title`, `description`, `published`.
      Result URL `https://www.facebook.com/<page_id>/videos/<video_id>`.
      Resume: phase `transferring` → start a new session (cross-process resume undocumented). Phase
      `committing` (or a lost finish response) → `GET /{video_id}?fields=id,status,published` — exists with
      upload complete → success; not found or upload incomplete → `ErrOutcomeUnknown`.
- [ ] Instagram upload: pre-check `GET /{ig_id}/content_publishing_limit?fields=quota_usage,config`; at
      quota → `PermanentError`. `POST {graph}/{ig_id}/media` with `media_type=REELS`,
      `upload_type=resumable`, `caption`, `share_to_feed` → container `id` (phase `transferring`,
      resume `{container_id}`). `POST {rupload}/{container_id}` with headers `Authorization`, `offset`,
      `file_size`, body = bytes from offset. Resume during transfer: `GET /{container_id}?fields=status_code,video_status`
      → `video_status.uploading_phase.bytes_transferred` → re-POST from that offset. Poll `status_code`
      every 10s up to 15 min until `FINISHED` (`ERROR`/`EXPIRED` → `PermanentError`; deadline →
      `ErrOutcomeUnknown`). Report phase `committing`, then `POST /{ig_id}/media_publish creation_id` →
      media id; `GET /{media_id}?fields=permalink` → URL.
      Reconcile in phase `committing` / lost publish response: container `status_code == PUBLISHED` →
      success (`Outcome: published`, URL from permalink when a media id is known, else note "published;
      permalink unavailable"); `FINISHED` → call publish; anything else → `ErrOutcomeUnknown`.
- [ ] Register `facebook` and `instagram` in `internal/daemon/providers.go` when `[meta]` is configured.

## Files

- Create: `internal/provider/meta/client.go`, `internal/provider/meta/oauth.go`, `internal/provider/meta/meta_test.go`
- Create: `internal/provider/facebook/facebook.go`, `internal/provider/facebook/upload.go`, `internal/provider/facebook/facebook_test.go`
- Create: `internal/provider/instagram/instagram.go`, `internal/provider/instagram/upload.go`, `internal/provider/instagram/instagram_test.go`
- Modify: `internal/daemon/providers.go`

## Implementation Steps (test first)

1. meta client: error classification table; auth URL has `config_id` and no `scope`; exchange performs
   the short→long-lived swap; `subject` from `/me`; a transport error containing the exchange URL is
   sanitized.
2. facebook: identities filter by `CREATE_CONTENT` and follow paging; upload against a fake graph-video
   server that hands out offsets and verifies chunk bytes; `private` sends `published=false`; lost
   finish response → reconcile via `GET /{video_id}` → success with one video; unknown video →
   `ErrOutcomeUnknown`.
3. instagram: identities skip Pages without linked IG; caption limits; upload fake: container → rupload
   (headers/offset asserted) → polling to `FINISHED` → publish → permalink; resume uses
   `bytes_transferred`; quota exhausted permanent; `ERROR` permanent; lost publish response with container
   `PUBLISHED` → success without a second publish call (call count asserted).
4. Service-level test with real instagram capabilities: `private` request rejected.
5. Implement until green.

## Todo

- [ ] shared Meta client + error mapping + OAuth + subject
- [ ] facebook identities + upload + reconcile
- [ ] instagram identities + caption + upload + resume + reconcile
- [ ] registration

## Verification

- `go test -race ./internal/provider/meta/... ./internal/provider/facebook/... ./internal/provider/instagram/...`

## Risks

- Development-mode visibility and Live-mode eligibility are unverified (human probe); surfaced via the
  `app_mode` restriction flag.
- Live-mode acceptance of `http://localhost` redirects is unverified; the user chose loopback only.
- Container `status_code` values and the `upload_phase` flow follow the published references; the live
  probe confirms them.

## Success Criteria

Contract tests pass for both providers; Instagram never publishes when `private` or `unlisted` was
requested; a lost `finish`/`media_publish` response never produces a second post; Facebook health is
independent of the user-token expiry.
