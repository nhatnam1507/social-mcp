---
title: "Drop the old mcp commands and sync the docs with the code"
description: "Remove the `mcp` and `config mcp` aliases so `connect` and `relay` are the only names, and make the README and architecture doc match the code."
status: completed
priority: P2
effort: 1h
branch: chore/drop-mcp-alias-sync-docs
tags: [refactor, cli, docs]
blockedBy: [261126-1600-register-mcp-once]
blocks: []
created: 2026-11-26
---

# Drop the old `mcp` commands and sync the docs with the code

Shipped in PR #15 (merge `75f6c90`). Follows PR #14.

## Outcome

`connect` is the only setup command, and `relay` the only name apps start. The old names introduced before
#14 are gone, and the README and architecture doc describe exactly what the code does.

## Constraints and non-goals

- Old plans and journals are history records and stay unchanged.
- No behavior change beyond removing the two old names and rewording messages that point at them.
- Apps connected with v0.2.0 through `social-mcp mcp` stop working after the update; the README upgrade note
  tells users to run `social-mcp connect <app>` again. HTTP entries keep working.

## Changes

1. Code
   - `internal/cli/relay.go`: remove the `mcp` alias.
   - `internal/cli/config.go`: remove the hidden `config mcp` command.
   - `internal/mcptoken/token.go`: invalid-token hint points to `social-mcp connect --rotate-token`.
   - `internal/config/config.go`: retired-port warning points to `social-mcp connect`.
2. Tests
   - `cli_test.go`: plain `connect` only; `mcp` and `config mcp` are unknown commands.
   - `e2e_test.go`: both stdio sessions start `relay`.
3. Docs
   - README: drop the `config mcp` sentence; add the v0.2.0 upgrade note; the security model names the relay
     next to the CLI on the control socket (no token needed).
   - `docs/architecture.md`: the relay replaces "stdio MCP mode" and the alias note; the diagram shows an AI
     app starting the relay; Windows-path conversion in the upload lifecycle; the relay as a daemon starter.

## Acceptance criteria

- `social-mcp mcp` and `social-mcp config mcp` fail as unknown commands; `relay` and `connect` work.
- No living doc or user-facing message mentions `config mcp` or `social-mcp mcp`.
- `make vet vet-cross test-unit test-e2e` passes locally; the PR gate and post-merge CI are green; merged.
