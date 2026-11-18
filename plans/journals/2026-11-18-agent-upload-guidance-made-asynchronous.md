---
title: Agent upload guidance made asynchronous
date: 2026-11-18
summary: "Stopped MCP guidance from telling agents to poll uploads until done; added a queued note and a no-direct-daemon-access rule (issue #3)."
---

# Agent upload guidance made asynchronous

## What happened
GitHub issue #3: the MCP server instructions (`internal/mcpserver/server.go`) and the `upload_video`
description (`internal/mcpserver/tools.go`) told agents to poll `get_upload_status` until every job
finished. `upload_video` was already asynchronous, but agents took the wording literally. Agent tools
that can't pause between calls ended up polling with no gap, and in one session an agent wrote a
background script that read the MCP bearer token from the client config and called the daemon's HTTP
endpoint directly. The user also heard nothing for about 25 minutes after uploads that had finished
within a minute.

## Changes
- Server instructions step 3 now says uploads are asynchronous and survive restarts. It tells the
  agent to report the queued job IDs, not to wait or poll in a loop, and to check status only when the
  user asks.
- New rule: reach social-mcp only through its MCP tools, never through the daemon HTTP endpoint or the
  token in config files.
- The `upload_video` description now says the call returns immediately and to check later, when the
  user asks.
- `upload_video` output has a new top-level `note`, set only when at least one job was queued. It is
  added in the MCP layer (`uploadVideoOut`), not in `service.UploadSubmission`, because the CLI and the
  control socket share that type.
- README quick-start wording updated to match.
- Regression test `TestUploadGuidanceIsAsynchronous` checks the instructions, the description and the
  note, including that the note is left out when every account is rejected.

## Verification
`go vet ./...`, `go test ./...` and the e2e suite pass. The race detector couldn't run locally because
cgo is unavailable; CI runs it.

## Next steps
Commit, push and open a PR closing #3. Restart the daemon after merging so connected agents get the
new instructions.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
