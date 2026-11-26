---
title: "Phase 2: Config cleanup and fixed port"
status: done
priority: P1
effort: 2h
dependencies: [1]
---

# Phase 2: Config cleanup and fixed port

## Goal

Remove platform credentials and the port from `config.toml`, fix the loopback port at 8765, and keep old
config files loading with a warning instead of failing.

## Key Insights

- `config.Load` rejects unknown keys (`md.Undecoded()`), so an upgraded user's old `config.toml` with
  `[youtube]`, `[tiktok]`, `[meta]` or `server.listen_port` would stop the daemon from starting. Those keys must
  be recognized as retired, not unknown.
- The port is used in `internal/daemon/daemon.go` (`listenLoopback`), `internal/service/local.go` (MCP URL,
  redirect URI) and `internal/cli/config.go` (`config mcp`).
- `e2e/e2e_test.go` writes `listen_port` to run daemons on free ports, and `internal/enginetest/env.go` sets
  `ListenPort: 8765`. Tests still need a non-default port, so a test-only, undocumented
  `SOCIAL_MCP_LISTEN_PORT` env var replaces the config key. Meta sign-in only works on 8765.

## Files to Create / Modify

- Modify: `internal/config/config.go`: delete `YouTube`, `TikTok`, `Meta`, `Server` structs, the `*Enabled` helpers and their validation; add `const ListenPort = 8765`; add a `Retired []string` result field filled from a fixed list of retired keys (`server.listen_port`, `youtube.*`, `tiktok.*`, `meta.*`), which are excluded from the unknown-key error.
- Modify: `internal/config/template.go`: drop `[server]`, `[youtube]`, `[tiktok]`, `[meta]`.
- Modify: `internal/config/config_test.go`
- Modify: `internal/daemon/daemon.go`: use `config.ListenPort` (or `SOCIAL_MCP_LISTEN_PORT` for tests); log retired keys once at startup; on "address in use" return an error naming port 8765 and suggesting `social-mcp daemon status` / stopping the other process.
- Modify: `internal/service/local.go`: `BeginAuth` error for a platform missing from the registry becomes "this build has no app for <platform>; set SOCIAL_MCP_… to use your own app"; drop the config.toml mention.
- Modify: `internal/cli/config.go`: `config mcp` uses the fixed port; `config init`/any CLI command that loads config prints the retired-keys warning once.
- Modify: `internal/enginetest/env.go`, `e2e/e2e_test.go`: use the env var instead of the config key.

## Tasks & Steps

1. Tests first in `config_test.go`: old file with every retired key loads and reports them in `Retired`; a genuinely unknown key still errors; template parses with no retired keys.
2. Implement config changes.
3. Replace port plumbing with the constant plus the test env var.
4. Warn when the old file had `server.listen_port` set to anything other than 8765: "listen_port is no longer used; social-mcp always uses 8765. Re-run `social-mcp config mcp` and update your MCP client."
5. Update e2e and enginetest.

## Verification

- `go test ./internal/config/... ./internal/daemon/... ./internal/service/... ./internal/cli/...`
- `make test-e2e`

## Success Criteria

- Fresh `config init` output has only `[uploads]` (and the commented `[updates]`).
- An old config with credentials and a custom port starts the daemon, logs the retired keys and serves on 8765.

## Risk Assessment

- **Users who changed the port** get a broken MCP client URL. Mitigated by the explicit warning in step 4.
- **Port 8765 taken by another program:** the daemon fails with a clear message; documented in the README.

## Security Considerations

Retired credential values from old files are never logged; only key names appear in the warning.
