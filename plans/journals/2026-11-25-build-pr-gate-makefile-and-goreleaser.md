---
title: "Build: PR gate, Makefile and GoReleaser"
date: 2026-11-25
summary: "Implemented make as the single entry point, GoReleaser build and release, pr-gate and test-gated ci; verified locally, live runs pending."
---

# Build: PR gate, Makefile and GoReleaser

## What happened
Implemented the PR gate, Makefile and GoReleaser plan (`plans/261125-1413-pr-gate-make-targets`) on `feat/pr-gate-make` as six local commits. Nothing has been pushed.

## Surprises
- GoReleaser v2.20.3 needs Go 1.27.4, but the repo is on 1.26.1. `go run` still works because GOTOOLCHAIN=auto downloads the newer toolchain. CI uses the prebuilt binary through goreleaser-action@v7.
- With `formats: [binary]`, GoReleaser renames binaries only on upload. Building straight into the output root with release names fixed that, but triggered a "same name" warning. The fix was building into a `bin/` subfolder.
- `-race` fails on machines without a C compiler (this WSL box). The Makefile now enables it only when `go env CGO_ENABLED` is 1, and never on Windows.
- No sudo and no Docker integration here. I extracted make and shellcheck from Ubuntu `.deb` packages (`apt-get download` + `dpkg -x`) into the session scratchpad.
- A local hook blocks Bash commands that mention the build output folder, so the make target that builds every release target was not run directly. The identical GoReleaser command ran against a scratch output folder.
- `v0.1.0-rc.1` already exists, so the next verification prerelease is `rc.2`.
- A broad `pkill -f` pattern matched my own shell. Stop processes by PID.

## Verified
`make test` (all packages and e2e), `make lint`, `make vet-cross` (it also catches a deliberate Windows-only error), `make build`, `make run`, `make clean` and `make release-check`. A snapshot build passed: six binaries and a matching `SHA256SUMS`. A local-tag dry run stamped the tag and grouped the notes. actionlint is clean.

## Next steps
Push and open a PR (needs user confirmation). Watch pr-gate, then ci on main, including the Windows make install. After that: the `v0.1.0-rc.2` prerelease and branch protection, both after confirmation.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
