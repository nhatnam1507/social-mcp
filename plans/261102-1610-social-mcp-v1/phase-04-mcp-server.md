---
title: "Phase 4: MCP server"
status: todo
priority: P1
effort: 3h
dependencies: [3]
---

# Phase 4: MCP server

<!-- Updated: Red Team Session 1 - stateless HTTP, check order Host→Origin→bearer, empty bearer rejected, sanitized outputs -->
<!-- Updated: Validation Session 1 - public-post approval via the MCP client's tool permission prompt; instructions for path-driven uploads -->

## Overview

Expose the service layer as MCP tools with the official Go SDK, served over Streamable HTTP on
the daemon's loopback listener and over stdio by `social-mcp mcp`.

## Requirements

- [ ] Dependency: `github.com/modelcontextprotocol/go-sdk` v1.8.0 (pinned).
- [ ] `internal/mcpserver.New(svc service.Service, version string) *mcp.Server` with server
      instructions:
      - When the user gives a video file path, call `list_accounts`, pick the accounts the user asked for
        (ask the user if unclear), then call `upload_video` with that exact path.
      - Uploads are asynchronous: poll `get_upload_status` until a terminal status and report the result.
      - Respect `restrictions`: TikTok uploads land as drafts the user finishes in the TikTok app;
        `visible_to_app_roles_only` means the post is not public yet.
      - Use `privacy: "public"` only when the user asked for public; never claim a post is public unless
        the job outcome is `published` with public privacy. `needs_review` means the result is unknown;
        tell the user to check the platform.
- [ ] Tools (typed input/output structs with JSON schema descriptions; DTOs from phase 2):
      - `list_accounts {platform?}` → `{accounts: [AccountView]}` (includes per-provider `options`)
      - `upload_video {file_path, account_ids[], title, description?, tags?, privacy?, platform_options?}`
        → `UploadSubmission`
      - `get_upload_status {job_id}` → `JobView`
      - `list_uploads {status?, account_id?, limit? (default 20, max 100)}` → `{jobs: [JobView]}`
      - `cancel_upload {job_id}` → `JobView`
      Tool errors return results with `isError: true` and a sanitized message.
- [ ] Annotations (so MCP clients prompt the user before uploading — this is the approval gate for
      public posts, chosen by the user over a server-side gate): `list_accounts`, `get_upload_status`,
      `list_uploads` → `readOnlyHint: true`; `upload_video` → `readOnlyHint: false`,
      `destructiveHint: true`, `idempotentHint: false`, `openWorldHint: true`, and a description stating
      that it publishes to external platforms with the given privacy; `cancel_upload` →
      `readOnlyHint: false`, `destructiveHint: false`.
- [ ] HTTP: `mcp.NewStreamableHTTPHandler` with `StreamableHTTPOptions{Stateless: true}` (SDK localhost
      protection left enabled), mounted at `/mcp`, wrapped by middleware in this order:
      (1) `Host` must be `127.0.0.1:<port>`, `localhost:<port>` or `[::1]:<port>` → else 403;
      (2) a present `Origin` must be `http://` + one of those → else 403;
      (3) `Authorization: Bearer <token>`, non-empty, constant-time compare → else 401.
      No detail in error bodies.
- [ ] `social-mcp mcp`: stdio transport, `svc = control.Client`; daemon not running → exit with
      "daemon not running, run `social-mcp daemon start`". Same `mcpserver.New`.

## Files

- Create: `internal/mcpserver/server.go`, `internal/mcpserver/tools.go`, `internal/mcpserver/auth.go`, `internal/mcpserver/server_test.go`, `internal/mcpserver/auth_test.go`
- Create: `internal/cli/mcp.go`
- Modify: `internal/daemon/daemon.go` (mount `/mcp`)

## Implementation Steps (test first)

1. auth tests: foreign Host (`evil.example:8765`) → 403 even without a token; foreign Origin → 403;
   empty bearer → 401; wrong bearer → 401; correct → passes.
2. server tests with SDK in-memory transports and a fake service: five tools with schemas and the
   annotations above; `list_accounts` passes the filter; rejected accounts and errors map to `isError`;
   unknown job → `isError`; a planted secret in a fake job error is never returned unsanitized.
3. HTTP integration test: `httptest.Server` + `StreamableClientTransport` with a header-injecting
   `http.Client`; call `list_accounts` statelessly twice.
4. Implement until green.

## Todo

- [ ] tools + instructions + annotations
- [ ] host/origin/bearer middleware
- [ ] mount in daemon (stateless)
- [ ] stdio `mcp` command

## Verification

- `go test -race ./internal/mcpserver/...`
- Manual: with daemon running, add it to Claude Code with the command printed by `config mcp`, list tools,
  and confirm the client prompts before `upload_video`.

## Risks

- A user who marks `upload_video` as always-allowed in their client removes the approval prompt; docs
  say so.

## Success Criteria

HTTP (with token) and stdio clients see the same five tools and results; foreign Host/Origin and missing
or empty tokens are rejected; `upload_video` is never annotated read-only.
