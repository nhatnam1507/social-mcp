---
title: Register social-mcp with an AI app once
date: 2026-11-26
summary: "Relay auto-starts the daemon; one connect command sets up five AI apps, including Windows apps reaching a WSL daemon"
---

# Register social-mcp with an AI app once

## What happened
Users had to re-register social-mcp with their agent whenever the daemon was not running at session start.
Root cause: agents were registered against the daemon's HTTP endpoint (`127.0.0.1:8765/mcp`), and MCP clients
connect to HTTP servers once at session start without retrying. The token and port were already stable, so the
registration itself never went stale; the timing did. Claude Desktop could only reach that endpoint through
`npx mcp-remote` (Node.js plus a pasted bearer token).

## Changes (branch feat/register-mcp-once, uncommitted)
- `social-mcp relay` (renamed from `mcp`, which stays as a hidden alias for apps connected with v0.2.0):
  the stdio relay now starts the daemon when nothing answers on the control socket, and restarts it mid-session.
  `control.Client.AutoStart` retries a request once after a failed dial; a failed dial means the request never
  reached the daemon, so an upload cannot be submitted twice.
- `social-mcp connect <claude-desktop|claude-code|cursor|codex|antigravity>` writes the relay entry once.
  JSON settings are edited key-order-preserving, Codex TOML by replacing only `[mcp_servers.social-mcp]`,
  each after a timestamped `.bak`. Claude Code goes through `claude mcp add --scope user`.
- Inside WSL, desktop apps (Claude Desktop, Cursor, Antigravity) are configured in the Windows profile with
  `wsl.exe -d <distro> -- <exe> relay`; terminal tools (Claude Code, Codex) are configured inside WSL.
  `SubmitUpload` converts Windows paths with `wslpath`.
- Plain `social-mcp connect` prints paste-in settings plus the HTTP endpoint/token; `config mcp` is a hidden alias.
  `relay` and `config mcp` are hidden from help, so `connect` is the only user-facing setup command.
- Installers print three next steps: `daemon start`, `account add`, `connect claude-desktop`.

## Findings worth keeping
- Claude Desktop's settings file location varies by version on Windows. On the maintainer's machine it is
  `%LOCALAPPDATA%\Claude-Data\claude_<pkg>\claude_desktop_config.json`, not `%APPDATA%\Claude`; detection
  checks all known folders and picks the newest file.
- Antigravity's current global config is `~/.gemini/config/mcp_config.json` (official docs); early builds used
  `~/.gemini/antigravity/`, which still exists for tokens and caches, so it is used only when its file exists.
- Verified the full WSL bridge: with the daemon stopped, `wsl.exe` started the relay, the daemon came up,
  `list_accounts` answered, and `C:\Windows\win.ini` resolved to `/mnt/c/Windows/win.ini`.

## Decision
Keep the relay as the default for every app; HTTP is the fallback. Direct HTTP would work for the four apps that
accept a URL when the daemon always runs, but only the relay covers Claude Desktop, a stopped daemon, no token,
and WSL to Windows.

## Next steps
- Commit and open a PR; release so `connect` reaches users (installed binary is still v0.2.0).
- Unverified: whether Cursor/Antigravity reload settings without a restart; whether WSL idles out after the
  Windows app quits (uploads would pause and resume); Codex desktop app on Windows with a WSL daemon.
- A relay-started daemon inherits the app's environment, so `SOCIAL_MCP_*` own-app overrides need
  `daemon start` from a shell (documented in README).

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
