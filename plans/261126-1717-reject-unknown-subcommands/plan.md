---
title: "Reject unknown subcommands of daemon and account"
description: "Every command group fails with `unknown command` and exit 1 for a mistyped subcommand, instead of printing help and exiting 0."
status: completed
priority: P3
effort: 1h
branch: fix/reject-unknown-subcommands
tags: [fix, cli]
blockedBy: []
blocks: []
created: 2026-11-26
---

# Reject unknown subcommands of daemon and account

## Overview

Cobra prints a command group's help and exits 0 when the group gets an unknown subcommand, so
`social-mcp daemon strat` looks like it succeeded and scripts cannot detect the typo. PR #15 fixed this for
`config` alone; `daemon` and `account` still behave the old way.

## Requirements

- `daemon`, `account` and `config` fail with `unknown command "<name>"` and exit 1 for an unknown subcommand.
- Run without a subcommand, each group still prints its help and exits 0.
- One shared helper builds the groups, so a future group gets the same behavior.

## Files

- Modify: `internal/cli/root.go` (add `groupCmd`), `internal/cli/config.go`, `internal/cli/daemon.go`,
  `internal/cli/account.go`.
- Modify: `internal/cli/cli_test.go` (unknown subcommands fail; bare groups print help).

## Also in this change

- Commit the journal of PR #15 (`plans/journals/2026-11-26-remove-the-old-mcp-command-names.md`).
- Record the plan of PR #14, which shipped without one: `plans/261126-1600-register-mcp-once/plan.md`.
- Give the PR #15 plan the frontmatter the other plans use.

## Acceptance criteria

- `social-mcp daemon strat`, `social-mcp account lst` and `social-mcp config mcp` exit 1 with
  `unknown command`; `social-mcp daemon`, `account` and `config` exit 0 with help.
- `make vet vet-cross test-unit test-e2e` passes locally; the PR gate and post-merge CI are green; merged.

## Risk and rollback

Low: only the reaction to a wrong subcommand changes. Rollback is reverting the merge commit.
