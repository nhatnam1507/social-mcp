---
title: Reject unknown subcommands in every command group
date: 2026-11-26
summary: "daemon, account and config now exit 1 on a mistyped subcommand; missing plans recorded"
---

# Reject unknown subcommands in every command group

## What happened
Cobra prints a group command's help and exits 0 for an unknown subcommand, so `social-mcp daemon strat`
looked successful. PR #15 had fixed only `config`.

## Changes
- `groupCmd(use, short)` in `internal/cli/root.go` builds every command group with `Args: cobra.NoArgs` and a
  help-printing `RunE`: bare groups still show help (exit 0), unknown subcommands fail with
  `unknown command` (exit 1). Used by `config`, `daemon` and `account`.
- Plans recorded: the plan for PR #14 (`plans/261126-1600-register-mcp-once/`) was missing and is now written
  as a completed record; the PR #15 plan got the frontmatter the other plans use; this change has its own plan.

## Lesson
Every delivered change gets a plan under `plans/`, including retrospectively; plans and journals are tracked
files and belong in the PR.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
