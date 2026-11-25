---
phase: 2
title: "social-mcp update command"
status: completed
priority: P2
effort: "1.5h"
dependencies: [1]
---

# Phase 2: `social-mcp update` command

## Goal

One command takes a user from any release build to the latest (or a chosen) release, including restarting a
running daemon on the new binary.

## Files to Create / Modify

- Create: `/home/namnn/src/social-mcp/internal/cli/update.go`
- Modify: `/home/namnn/src/social-mcp/internal/cli/root.go` (register the command)
- Modify: `/home/namnn/src/social-mcp/internal/cli/cli_test.go` (command tests with fakes)
- Modify: `/home/namnn/src/social-mcp/internal/daemon/lifecycle.go` (call `update.CleanupOld` at start, best
  effort)

## Behavior

```
social-mcp update [--version <tag>]
```

1. Resolve the target: `--version` or `update.Latest`. No stable release yet → message, exit 0.
2. Resolve the install path: `os.Executable()` then `filepath.EvalSymlinks`. Check the folder is writable;
   if not, fail with the path and a hint (re-run the installer or fix permissions).
3. If the current version equals the target → "social-mcp vX.Y.Z is already up to date", exit 0. A
   development build prints "current version unknown (development build)" and continues.
4. `update.CleanupOld`, then `update.Download` into the install folder.
5. Note whether the daemon is running (`daemon.Running(home)`); if so, `daemon.Stop` first (uploads resume
   later from their saved phase).
6. `update.Replace`.
7. If the daemon was running, start it through the new binary: run `<target> daemon start` as a child
   process and pass its output through. If the start fails, say so and tell the user to run
   `social-mcp daemon start`.
8. Print "updated social-mcp vA.B.C → vX.Y.Z" (and the daemon result).

Failure in steps 1–4 leaves the installed binary and the daemon untouched. Stopping the daemon happens only
after the new binary is downloaded and verified.

## Tasks & Steps

1. Implement the command with the steps above; the network, daemon and exec calls go through package
   variables so `cli_test.go` can fake them (the existing tests already use this pattern, e.g.
   `daemonRunning`, `resolveHome`).
2. Tests: already up to date; dev build; no release yet; download failure leaves the target untouched; daemon
   running → stopped, replaced, restarted via the target path; daemon stopped → not started; unwritable
   folder message.

## Verification

- `make test`; manual run of `make build` then `./social-mcp update --version v0.1.0-rc.2` against GitHub is
  not possible until a release contains this command (rc.2 has no `update`); live check is in phase 4.
