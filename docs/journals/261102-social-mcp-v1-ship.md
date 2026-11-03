# social-mcp v1 shipped on feat/social-mcp-v1, and most of it has never touched a real platform

**Date**: 2026-11-02 19:10 UTC
**Severity**: Medium
**Component**: social-mcp daemon, upload runner, YouTube/TikTok/Facebook/Instagram providers
**Status**: Resolved in code. Live verification still open.

## What Happened

The branch has 13 commits (`e865401`..`674dd28`) and they cover all eight plan phases. It adds one `CGO_ENABLED=0` Go binary that runs as a CLI-managed daemon. The daemon stores AES-GCM sealed OAuth tokens in SQLite. It serves MCP over Streamable HTTP on 127.0.0.1 with a bearer token, plus a stdio bridge (`social-mcp mcp`). It uploads local videos to YouTube, to TikTok (inbox drafts only, `video.upload`), to Facebook Pages and to Instagram Reels. Three code reviews followed, and each one ended in a fix commit (`3eade3f`, `facb46b`, `ad3af10`).

## The Brutal Truth

All four providers pass contract tests against `httptest` servers built from the docs. None of them has uploaded a byte to a real platform. The blocker check already showed that TikTok rejects personal apps for public posting and that Meta Development mode hides posts from everyone who lacks an app role. We built all four anyway, because the user chose that. That was the right call for momentum. But "tests green" here means "matches our reading of the docs", and the docs contradict themselves: Instagram's limit is listed as both 100 and 50 posts per 24h.

## Key Decisions

- **All four providers now; the live probes become human follow-ups.** The user turned the Meta Live-mode probe and the TikTok sandbox-inbox probe from blocking gates into follow-ups. We rejected waiting for the probes because it would have stalled three of the four providers.
- **The MCP client's permission prompt approves public posts.** `upload_video` is annotated destructive/open-world, so clients will not auto-approve it. We rejected a server-side `awaiting_approval` queue as extra state the user did not want. Privacy defaults to `private`. Instagram has no private Reels, so it requires an explicit `public`.
- **Meta login is loopback-only (`http://localhost`, bound on `[::1]` as well).** The user declined a manual-token fallback. Nobody has checked whether Meta accepts this redirect in Live mode.
- **`allowed_dirs` has no default.** An empty list means any path is accepted. The guards that remain are the container sniff, a no-change check between submit and upload, and FIFO rejection.

## What the Red Team and the Reviews Caught

- **Duplicate publishing after the commit step.** This was red-team Critical #3: retries and resume could repeat publish steps. We fixed it in two places. First, an invariant: no step that may already have published runs again (final chunk, finish, `media_publish`, TikTok after the final chunk). Second, `3eade3f`: jobs past the commit step now end in `needs_review`, never `failed`. They cannot be cancelled while queued, and status writes check a version number.
- **Facebook reconcile trusted `uploading_phase: complete`.** That status means only that the bytes arrived; it says nothing about whether `finish` ran. In `facb46b`, reconcile reads `publishing_phase`. If the status is `complete`/`in_progress`, the job succeeds. If it is `not_started`, reconcile may retry `finish`. Anything else ends as `OutcomeUnknown`.
- **Meta business rate limits were classified as hard errors.** In `facb46b`, `classify()` treats codes 80000-80014 (which covers 80001/80002) and 341 as `RateLimitError`. A rate-limited `finish` now returns instead of falling into reconcile.
- **Fallback socket in shared `/tmp`.** Another local user could have planted or owned that socket. In `ad3af10`, the socket moved to a private per-user directory. The CLI refuses sockets owned by other users, and Windows homes get an owner-only access list.
- **False "stopped" status.** A daemon whose socket was gone was reported as stopped. `start` could then spawn a second daemon, and `stop` would wait for a shutdown that never arrived. Now `status` and `start` check the process lock and return `ErrUnresponsive`, and `stop` kills the lock holder directly.

## Lessons Learned

- Any step that may have published must be treated as already published. When the outcome is unknown, record `needs_review`; never retry and never report `failed`.
- A status field from a platform API only means what its docs say. "Upload complete" is not "posted".
- Every fallback path gets the same threat model as the main path. The `/tmp` socket was a fallback, so nobody looked at it closely.

## What Remains Unverified

- Windows and macOS runtime behavior: detach, AF_UNIX socket, ACLs. Only Linux/WSL2 was exercised. Nothing is confirmed until the CI matrix runs.
- `go test -race` was not run locally because the host has no C compiler. CI must run it.
- The live probes: Meta Live-mode visibility, Meta acceptance of the localhost redirect in Live mode, the Page `upload_phase` flow with a Page token, and TikTok inbox upload for a sandbox user.
- Real uploads to all four platforms using bring-your-own developer apps.

## Next Steps

1. Repo owner: push the branch and get the Linux/macOS/Windows CI matrix green with `-race` before merging.
2. User: run the Meta and TikTok probes with real apps. If Meta posts are not public, plan App Review plus Business Verification.
3. User: do one real upload per platform and compare the final states with what `list_accounts` reports as restrictions.
