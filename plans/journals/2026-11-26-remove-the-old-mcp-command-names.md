---
title: Remove the old mcp command names
date: 2026-11-26
summary: "Dropped the mcp and config mcp aliases, made config reject unknown subcommands, synced README and architecture doc (PR #15)"
---

# Remove the old mcp command names

## What happened
Follow-up to PR #14. The hidden aliases kept for v0.2.0 compatibility (`social-mcp mcp` for `relay`,
`config mcp` for plain `connect`) were removed at the maintainer's request to cut naming confusion.

## Changes (PR #15, merged as 75f6c90)
- Removed both aliases; they now exit 1 with `unknown command`.
- Found while testing: cobra prints help and exits 0 for an unknown subcommand of a group command, so the old
  `config mcp` would have looked like success. `config` now has `Args: cobra.NoArgs` and shows help only when
  run bare. `daemon` and `account` still have the old cobra behavior (not changed, out of scope).
- User-facing messages that still pointed at `config mcp` (invalid MCP token, retired listen port) now point at
  `connect --rotate-token` / `connect <app>`.
- README: v0.2.0 upgrade note (re-run `connect <app>`; HTTP entries keep working), relay named in the security
  model. Architecture doc: relay replaces "stdio MCP mode", diagram shows the AI app starting it, Windows-path
  conversion and relay-started daemon documented.

## Decision
Breaking change accepted: apps connected with v0.2.0 through `social-mcp mcp` must run `connect <app>` again.

## Next steps
- Mention the breaking change in the next release notes (`refactor!:` commit carries a BREAKING CHANGE footer).
- Optional: give `daemon` and `account` the same unknown-subcommand rejection.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
