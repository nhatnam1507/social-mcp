---
title: "Phase 8: CI, packaging, end-to-end test, docs"
status: completed
priority: P1
effort: 4h
dependencies: [3, 4, 5, 6, 7]
---

# Phase 8: CI, packaging, end-to-end test, docs

<!-- Updated: Red Team Session 1 - survival-after-parent-exit check, short temp dirs, docs on revocation and permission prompts -->
<!-- Updated: Validation Session 1 - docs reflect optional allowed_dirs and client-side approval -->

## Overview

Prove the binary works end to end on all three operating systems, produce the six static binaries,
and document setup and limits.

## Requirements

- [x] End-to-end test (`e2e/e2e_test.go`, build tag `e2e`): builds `./cmd/social-mcp` with
      `CGO_ENABLED=0` into a short temp dir (`os.MkdirTemp("", "sm")`); temp `SOCIAL_MCP_HOME`; config
      with a free `listen_port`; a small file with an mp4 `ftyp` header and a text file. Steps:
      - `daemon start` (the subprocess exits after printing the PID); then, from new processes,
        `daemon status` exits 0 (daemon survived its parent).
      - MCP client over HTTP with the bearer token: `list_accounts` empty; `upload_video` with the text
        file → error (not a video); with the mp4 and an unknown account → rejected; request without
        token → 401; foreign Host → 403.
      - `social-mcp mcp` over stdio (SDK `CommandTransport`) lists the five tools.
      - `daemon stop` returns; `daemon status` exits 3; a second `daemon start` succeeds and stops again.
- [x] `scripts/build-all.sh`: linux/darwin/windows × amd64/arm64, `CGO_ENABLED=0`, `-trimpath`,
      `-ldflags "-s -w -X main.version=$VERSION"` → `dist/social-mcp-<os>-<arch>[.exe]` and
      `dist/SHA256SUMS`.
- [x] `.github/workflows/ci.yml`: push/PR; matrix `ubuntu-latest`, `macos-latest`, `windows-latest`;
      `actions/setup-go` with `go-version-file: go.mod`; `go vet ./...`; `go test -race ./...`
      (Linux/macOS) / `go test ./...` (Windows); `go test -tags e2e ./e2e/...`; a Linux job runs
      `scripts/build-all.sh` and uploads `dist/`.
- [x] Docs:
      - `README.md`: what it is; install (binary or `go install`); quick start (`config init`, edit
        config, `daemon start`, `account add`, `config mcp`, then ask your agent "upload /path/video.mp4
        to my YouTube channel"); CLI reference; MCP tools; security model (local-only tokens, encrypted
        at rest, loopback + bearer, the MCP client's permission prompt is the approval step for uploads,
        optional `allowed_dirs`, content checks); platform limitations table (from the brainstorm blocker
        check); revoking access at each platform after `account remove`.
      - `docs/platform-setup.md`: Google (desktop OAuth client, YouTube Data API, consent screen "In
        production"), TikTok (Login Kit for Desktop, Content Posting API, sandbox target users, redirect
        `http://127.0.0.1:<port>/oauth/callback`), Meta (Business app, Facebook Login for Business
        configuration with `pages_show_list`, `pages_read_engagement`, `pages_manage_posts`,
        `instagram_basic`, `instagram_content_publish`; `config_id`; Valid OAuth Redirect URI
        `http://localhost:<port>/oauth/callback`; app modes and the visibility caveat; the unverified
        Live-mode localhost risk; Instagram professional account linked to a Page).
      - `docs/architecture.md`: components, data flow, storage and encryption (key backend record),
        upload lifecycle (status and phase, commit/reconcile, `needs_review`).

## Files

- Create: `e2e/e2e_test.go`
- Create: `scripts/build-all.sh`
- Create: `.github/workflows/ci.yml`
- Create: `README.md`, `docs/platform-setup.md`, `docs/architecture.md`

## Implementation Steps (test first)

1. Write the e2e test; run it on Linux until green.
2. Build script; run it; `file dist/*` shows static executables.
3. CI workflow; verify on the PR that all three OS jobs pass (Windows runtime evidence for acceptance
   criterion 1).
4. Docs; verify every CLI command and config key mentioned exists (`grep` against source).

## Todo

- [x] e2e test
- [x] build-all script
- [x] CI workflow
- [x] README + docs

## Verification

- `go test -tags e2e ./e2e/...`
- `bash scripts/build-all.sh && ls -la dist/`
- CI green on ubuntu, macos, windows.

## Risks

- Windows CI may expose detach, lock or AF_UNIX issues; fix in phase 3 files, never by skipping tests.
- Port collisions: the e2e test picks a free port by listening on `:0` first.

## Success Criteria

CI green on three OSes including e2e; six binaries built; docs accurate against source.
