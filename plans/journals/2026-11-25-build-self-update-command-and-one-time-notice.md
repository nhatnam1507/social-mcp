---
title: "Build: self-update command and one-time notice"
date: 2026-11-25
summary: "Implemented social-mcp update and the once-per-release CLI/agent notice; live-verified against real releases; PR #9 green, merge pending."
---

# Build: self-update command and one-time notice

## What happened
Implemented `social-mcp update` and the one-time new-release notice (`plans/261125-1703-self-update-command`), opened as PR #9. pr-gate passed; the merge is waiting on the user.

## Surprises
- A live downgrade test (a binary built as `v0.1.0-rc.1` updating to the published `rc.2`) proved the download, swap and daemon restart worked. But the restarted older daemon refused the config, because `config init` had written an active `[updates]` section and older releases reject unknown settings. The fix was to show the opt-out only as a comment in the template.
- On Windows, the home folder's permission restriction is only applied when every file in it is one social-mcp owns. The new `update-` files had to be added to that list.
- `root.go` registered both the command and the notice hook, so the first commit didn't build on its own. I restructured the (unpushed) commits so each one builds.
- The auto-mode classifier blocked `gh pr merge`, since the user hadn't approved merging this PR in the turn. Left for the user.

## Verified live
- The update matches the published `rc.2` byte for byte, and the daemon restarted on `rc.2`.
- The real daily check found no stable release, 67 seconds after start.
- With a recorded newer release, the CLI notice and the `list_accounts` notice (over real MCP HTTP) each appeared once.

## Next steps
- Merge PR #9 (user).
- Watch ci on main, including the Windows swap unit test.
- The first stable release, `v0.1.0`, will exercise the notice path for real.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
