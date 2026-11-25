---
title: "PR gate, test-gated CI build, Makefile and GoReleaser"
description: "Pull requests run the tests on Linux through a shared composite action, main runs them on three OSes and builds with GoReleaser only after they pass, and every build, test, release-check and local-run command goes through make."
status: in-progress
priority: P1
effort: 4.5h
branch: feat/pr-gate-make
tags: [infra, ci, tooling, release, docs]
blockedBy: []
blocks: []
created: 2026-11-25
---

# PR gate, test-gated CI build, Makefile and GoReleaser

## Overview

Today `ci.yml` runs on both pushes to main and pull requests, and its `build` job runs in parallel with
`test`, so a build can succeed while tests fail. Commands are typed directly in workflows and the README,
and the release is hand-written shell (`scripts/build-all.sh` plus a `gh release create` step).

After this plan:

- **pr-gate** (`pull_request`) runs the tests once on `ubuntu-latest`, then compile-checks the Windows and
  macOS code (`make vet-cross`), lints the installer (`make lint`) and validates the GoReleaser config
  (`make release-check`). A PR that only touches docs or plans skips those steps but still reports a green
  `test` check. It is the check that must pass before merge.
- **ci** (`push` to main, plus `workflow_call` for release) runs the tests on Linux, macOS and Windows, then
  builds every target with a GoReleaser snapshot only when every test job succeeds (`build: needs: test`).
- Both workflows call one **composite action**, `.github/actions/test`.
- **GoReleaser** replaces `scripts/build-all.sh` and the publish shell. `.goreleaser.yaml` owns targets,
  version stamping, `SHA256SUMS`, the GitHub Release, prerelease detection and grouped release notes. Asset
  names stay `social-mcp-<os>-<arch>[.exe]`, so `install.sh` and `install.ps1` keep working unchanged.
- A root **Makefile** is the only entry point for vet, tests, lint, release checks, local build, release
  build and local run.
- **`make run`** runs the daemon locally against an isolated dev home (`./.dev`).

```
pull_request ──> pr-gate.yml ─ ubuntu-latest ─> checkout ─> ./.github/actions/test ─> make test
                 (docs/plans-only PR: steps skipped,     ├─> make vet-cross (GOOS=windows, darwin)
                  job still reports success)             ├─> make lint
                                                         └─> make release-check (goreleaser check)
push main ─────> ci.yml ────── matrix(ubuntu, macos, windows) ─> checkout ─> ./.github/actions/test ─> make test
                               └─ build (needs: test) ─> make lint ─> make dist (goreleaser snapshot) ─> upload-artifact
tag v* ────────> release.yml ─ uses ci.yml ─> release (needs: ci) ─> goreleaser release --clean
```

## Locked decisions

- Shared tests are a composite action (user choice). It takes no `os` input: it runs on whatever runner the
  calling job selects and reads `runner.os` where needed.
- pr-gate runs on `ubuntu-latest` only (user choice). ci keeps the 3-OS matrix because it builds and ships
  per-OS binaries.
- pr-gate adds `make vet-cross` so the seven OS-specific files (`internal/paths/owner_*`,
  `internal/daemon/lock_*`, `internal/daemon/detach_*`) are compiled and vetted before merge. Their runtime
  behavior is first tested by ci on main.
- pr-gate runs `make lint`; ci's build job keeps it too, since admins can push to main without a PR.
- Docs-only PRs skip the work inside the job, not the job itself, so the required `test` check always
  reports. Anything outside `*.md`, `docs/**` and `plans/**` counts as code.
- Branch protection: required check `test`, not strict, admins may bypass (`enforce_admins: false`).
- The race detector is chosen by the Makefile (`$(OS)` is `Windows_NT` on Windows).
- `ci` runs on push to main only and keeps `workflow_call` so `release.yml` gates publishing on it.
- `make run` uses `SOCIAL_MCP_HOME=./.dev` and `SOCIAL_MCP_KEYRING=off`; `.dev/` is gitignored.
- GoReleaser replaces `scripts/build-all.sh` (deleted) and the `gh release create` step (user choice). The
  publish itself stays in `release.yml` (`goreleaser release --clean`), never a make target.
- GoReleaser's version is pinned once, in the Makefile (`GORELEASER_VERSION`). Locally make runs it with
  `go run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)` (user choice: nothing to install). CI
  installs that same version's prebuilt binary through `.github/actions/setup-goreleaser`, which reads the
  version from `make -s goreleaser-version`, and passes `GORELEASER=goreleaser` to make.
- pr-gate runs only `goreleaser check` (seconds); the full snapshot build runs in ci on main (user choice).
- Release notes are grouped from conventional commits (Features, Bug fixes, Other; docs, test, chore, ci
  and merge commits hidden) (user choice).
- Installation stays the curl/irm one-liner only. Package-manager channels (Homebrew, winget, deb/rpm) are
  out of scope (user decision); `261125-1434-install-channels` is cancelled and kept for reference.

## Phases

| # | Phase | Effort | Depends on | Status |
|---|-------|--------|------------|--------|
| 1 | [Makefile and local dev run](phase-01-makefile.md) | 1h | none | completed |
| 2 | [GoReleaser build and release config](phase-02-goreleaser.md) | 1h | 1 | completed |
| 3 | [Composite test action and workflows](phase-03-workflows.md) | 1h | 1, 2 | completed (live runs pending) |
| 4 | [Docs, live verification and merge protection](phase-04-docs-and-verification.md) | 1.5h | 1, 2, 3 | in-progress (docs and LICENSE done; push, PR, rc tag, protection pending) |

## Acceptance criteria

- `make help` lists every target; `make test`, `make lint`, `make release-check`, `make build`, `make dist`
  and `make run` work on Linux. `make test` runs vet, unit tests (with `-race` when cgo is available, never
  on Windows) and the e2e test.
- `make run` starts the daemon in the foreground against `./.dev`, creating `./.dev/config.toml` on first
  use; `make run ARGS="account list"` runs any CLI command against the same dev home.
- `scripts/build-all.sh` is deleted and nothing outside `plans/` references it.
- `make dist` produces `dist/bin/social-mcp-{linux,darwin,windows}-{amd64,arm64}[.exe]` and
  `dist/SHA256SUMS` in the same `<sha256>  <file>` format as before, and the release uploads the binaries
  under those same names; a snapshot binary's `version` prints a snapshot version.
- No workflow step calls `go vet`, `go test`, `go build` or `goreleaser check`/`build` directly; only the
  release publish step calls `goreleaser release`.
- Opening a PR triggers `pr-gate` with one `test` job on `ubuntu-latest` that runs `make test`,
  `make vet-cross`, `make lint` and `make release-check`. `ci` does not run on the PR.
- A PR that changes only `*.md`, `docs/**` or `plans/**` gets a green `test` check without running them.
- A push to main runs `ci`; `build` starts only after all three test jobs succeed and uploads the snapshot
  binaries and `SHA256SUMS`.
- Pushing a `vX.Y.Z-rc.N` tag (`v0.1.0-rc.1` already exists, so the next is `v0.1.0-rc.2`) (after user confirmation) publishes a prerelease with the six binaries,
  `SHA256SUMS`, `install.sh` and `install.ps1`, grouped release notes, and `social-mcp version` printing
  the tag; `SOCIAL_MCP_VERSION=<tag>` with the one-liner installs it.
- README `## Development` documents the make targets, `make run`, the dev home and how releases are built.
- `LICENSE` (MIT, `Copyright (c) 2026 nhatnam1507`) exists and GitHub shows the repo as MIT.
- After user confirmation, main requires the pr-gate `test` check to pass before merge.

## Risks

- **OS-specific regressions found after merge.** pr-gate compiles but does not run Windows and macOS code;
  a behavior bug in an OS-specific file first fails `ci` on main. Fix forward; release still gates on ci.
- **Installer compatibility.** The installers depend on asset names and the `SHA256SUMS` format and have no
  base-URL override. Phase 2 pins both in config; phase 4 proves them with a real prerelease before any
  stable tag.
- **GoReleaser version drift and upgrades.** One pin in the Makefile feeds both local `go run` and CI. A
  major GoReleaser upgrade can rename config keys; `make release-check` on the upgrade PR catches it.
- **First local `make dist` is slow.** `go run` compiles GoReleaser once (a minute or two), then the Go
  build cache makes later runs fast.
- **make on Windows runners.** The composite action installs GNU make with Chocolatey when missing.
- **POSIX shell in recipes.** Local Windows developers need Git Bash or WSL; README says so.
- **Port clash in `make run`.** README explains stopping the real daemon or changing `listen_port`.
- **Merge protection is a repo setting.** Enabled only after the user confirms (phase 4). Not strict: two
  PRs that pass separately can still break main together; ci on main catches it.
- **Required-check name.** Branch protection matches the job name `test`.
- **Uncommitted plan edits on main.** `plans/261119-1402-release-pipeline-install-scripts/*` has
  uncommitted changes; they are committed first, as their own `docs(plans)` commit on `feat/pr-gate-make`
  (user choice).

## Validation Log

### Session 1 (2026-11-25)

- Shared mechanism: composite action, with the matrix in the workflows (user choice). The proposed `os`
  input is unnecessary because the action runs on the selected runner.
- `make run`: isolated dev home `./.dev` with keyring off. (Recommended option chosen.)
- `ci` trigger: push to main only. (Recommended option chosen.)
- Release: build through make, publish stays in YAML. (Recommended option chosen.)

### Session 2 (2026-11-25)

- pr-gate runs on `ubuntu-latest` only; the 3-OS matrix stays in ci (user decision).
- pr-gate adds `make vet-cross`. (Recommended option chosen.)

### Session 3 (2026-11-25)

- PR branch need not be up to date with main (`strict: false`). (User choice; recommended was strict.)
- pr-gate runs `make lint`. (Recommended option chosen.)
- Docs-only PRs skip the tests while the `test` check still reports. (User choice.)
- Admins can bypass protection. (Recommended option chosen.)

### Session 4 (2026-11-25)

- Adopt GoReleaser for build and release, replacing `scripts/build-all.sh` and the publish shell. This
  reverses the "No GoReleaser" decision in `261119-1402-release-pipeline-install-scripts` (user decision).
- Scope: GoReleaser build and release folded into this plan; install channels in a separate plan.
  (Recommended option chosen.)
- Local GoReleaser: `go run` of a pinned version. (Recommended option chosen.)
- Pre-tag checking: `goreleaser check` on PRs, snapshot build on main. (Recommended option chosen.)
- Release notes grouped by commit type. (Recommended option chosen.)

### Session 5 (2026-11-25)

- Package-manager install channels dropped; the one-liner installers are the only install path (user
  decision). The channels plan is cancelled.
- Add an MIT `LICENSE` in this plan. (Recommended option chosen.)
- Commit the pending release-pipeline plan edits first, as a separate commit on the feature branch (user
  choice).

## Implementation notes (2026-11-25)

- GoReleaser pinned at v2.20.3 (latest). It requires Go 1.27.4 while `go.mod` says 1.26.1; local `go run`
  works because `GOTOOLCHAIN=auto` downloads the newer toolchain on first use. CI uses the prebuilt binary
  through `goreleaser/goreleaser-action@v7` (`install-only`).
- Binaries build into `dist/bin/` (`binary: bin/social-mcp-<os>-<arch>`, `no_unique_dist_dir`). Building
  straight into `dist/` made GoReleaser warn that the binary and its upload shared a name.
- The race detector turns on only when `go env CGO_ENABLED` is 1 (never on Windows). This WSL machine has no
  C compiler, so `-race` failed locally; CI's Linux and macOS runners keep it.
- Verified locally (make and shellcheck extracted from Ubuntu packages into the session scratchpad, since sudo
  and Docker were unavailable): `make help`, `make test` (all packages and e2e pass), `make lint`,
  `make vet-cross` (and that it catches a deliberate Windows-only error Linux vet misses), `make build`,
  `make run` (dev home created once, daemon reachable, `.dev/` ignored), `make clean`, `make release-check`,
  a snapshot build (six binaries, `SHA256SUMS` verifies) and a local-tag dry run (`version` prints the tag,
  notes grouped, docs/test/merge commits hidden). actionlint passes on all workflows and actions.
- Not verified locally: `make dist` itself (a session hook blocks commands that touch `dist/`; the identical
  GoReleaser command was run against a scratch output folder), the Windows runner, and publishing.
