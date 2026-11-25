---
title: "Plan: self-update command and one-time notice"
date: 2026-11-25
summary: "Planned social-mcp update plus a once-per-version notice in the CLI and agent, fed by a daily daemon check."
---

# Plan: self-update command and one-time notice

## What happened
Planned `plans/261125-1703-self-update-command`. It adds `social-mcp update` (in-place install with SHA256SUMS verification, then a daemon restart) and a once-per-version new-release notice in the CLI (stderr) and in the agent (a `notice` field on `list_accounts`).

## Decisions
- The daemon checks once a day using the `releases/latest` redirect the installers already use, so there's no API call or rate limit. The CLI and MCP only read the result.
- `update` restarts a running daemon through the new binary's path. `os.Executable()` would point at the deleted file on Linux after the swap.
- The agent notice goes through `Service.TakeUpdateNotice`, so it works over both HTTP MCP and the stdio proxy.
- Opt-out is `[updates] check = false` or `SOCIAL_MCP_NO_UPDATE_CHECK=1`. Development builds never check.
- Each state file has a single writer: the daemon owns `update-check.json`, and the CLI owns `update-notice-cli`.

## Next steps
Run `/ak:cook` on the plan. Live verification needs two prereleases that contain the command (rc.3 and rc.4), and pushing those tags needs confirmation. The stable-only notice path stays covered by unit tests until real stable releases exist.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
