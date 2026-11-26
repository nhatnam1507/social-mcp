---
title: "Connect an AI app once through a self-starting relay"
description: "Users connect each AI app once with `social-mcp connect <app>`; the app starts a stdio relay that starts the daemon when needed, so a stopped daemon no longer means re-registering."
status: completed
priority: P1
effort: 6h
branch: feat/register-mcp-once
tags: [feature, cli, mcp, wsl, docs]
blockedBy: []
blocks: [261126-1708-drop-mcp-alias-sync-docs]
created: 2026-11-26
---

# Connect an AI app once through a self-starting relay

Recorded after delivery: this work shipped in PR #14 (merge `07a7088`) without a plan file. It records the
outcome, decisions and verification as they happened.

## Overview

Users registered their agent against the daemon's HTTP endpoint. MCP clients connect once at session start and
do not retry, so whenever the daemon was not running the tools were missing for that session and users
re-registered. Claude Desktop could only reach the endpoint through `npx mcp-remote` (Node.js plus a pasted
bearer token). The token and port were stable; the timing was the problem.

```
AI app ──starts; MCP over stdio──▶ social-mcp relay ──control socket──▶ daemon ──▶ platforms
                                    (starts the daemon when nothing answers)
```

## Requirements

- Connect each app once; it keeps working whether or not the daemon runs when the app starts.
- Non-technical flow: one-line installer, `daemon start`, `account add`, `connect <app>`.
- Claude Desktop and Claude Code first. Also Cursor, Codex and Antigravity.
- Both setups: app and daemon on the same machine, and the daemon in WSL with the app on Windows.
- One user-facing setup command.

## Decisions

- **Relay over direct HTTP for every app.** Only the relay covers Claude Desktop (its settings file accepts
  only a command), a stopped daemon, no token in settings files, and Windows apps reaching a WSL daemon.
  HTTP stays as the fallback that plain `connect` prints.
- **`relay` as the name** (formerly `mcp`), hidden from help: apps run it, users never do. `serve` was rejected
  because it sounds like the daemon.
- **Retry once after a failed dial only.** The request never reached the daemon, so an upload cannot be
  submitted twice.
- **WSL rule:** desktop apps (Claude Desktop, Cursor, Antigravity) are configured in the Windows profile and
  start the relay through `wsl.exe -d <distro> --`; terminal tools (Claude Code, Codex) are configured in WSL.
- **Edit settings in place**, keeping other keys and their order, with a timestamped `.bak`; refuse invalid
  files. Codex TOML changes only the `[mcp_servers.social-mcp]` table.

## Delivered

- `internal/control/client.go`: `AutoStart` hook. `internal/cli/relay.go`: relay starts the daemon.
- `internal/cli/connect.go`, `settings.go`: one entry per app (settings location, writer, next step);
  Claude Code through `claude mcp add --scope user`.
- `internal/wsl`: WSL detection, Windows folder lookup, `wslpath` conversion; `SubmitUpload` accepts
  Windows paths.
- `config mcp` folded into plain `connect` (`--rotate-token`); installers print three next steps.
- README and `docs/architecture.md` updated; journal
  `plans/journals/2026-11-26-register-social-mcp-with-an-ai-app-once.md`.

## Verification

- `go vet` on three OSes, unit tests, e2e (daemon stopped at connect and mid-session); PR gate green,
  post-merge CI green on Linux, macOS and Windows.
- On Windows 11 + WSL2: detection found the real Claude Desktop settings in
  `%LOCALAPPDATA%\Claude-Data\claude_<pkg>\` (not `%APPDATA%\Claude`) and Cursor's `mcp.json`; merges on copies
  kept other servers; the real `wsl.exe` bridge started the daemon, answered `list_accounts`, and resolved
  `C:\Windows\win.ini` to `/mnt/c/Windows/win.ini`.

## Left for future work

- Whether Cursor and Antigravity reload settings without a restart (users are told to restart).
- The Codex app on Windows with a WSL daemon.
- Whether WSL idles out after the Windows app quits (uploads would pause and resume).
