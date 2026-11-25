---
phase: 4
title: "Docs, live verification and merge protection"
status: in-progress
priority: P1
effort: "1.5h"
dependencies: [1, 2, 3]
---

# Phase 4: Docs, live verification and merge protection

## Goal

Developers learn the make workflow from the README, the new pipelines are proven on real GitHub runs, and
the GoReleaser release path is proven with a real prerelease, and main requires pr-gate to pass before
merge.

## Files to Create / Modify

- Create: `/home/namnn/src/social-mcp/LICENSE` (MIT, `Copyright (c) 2026 nhatnam1507`)
- Modify: `/home/namnn/src/social-mcp/README.md` (`## Development`, `### Releasing`, and a one-line `## License` section)
- Modify: `/home/namnn/src/social-mcp/plans/261119-1402-release-pipeline-install-scripts/plan.md` (one note
  that GoReleaser superseded its "No GoReleaser" decision; do this on the feature branch, separately from
  the uncommitted edits already on main)
- Repo setting (no file): branch protection on `main`, only after user confirmation

## Tasks & Steps

1. Rewrite `## Development` in README:
   - Prerequisites: Go (version from `go.mod`), GNU make, a POSIX shell (Git Bash or WSL on Windows),
     `shellcheck` for `make lint`.
   - A short target table, or the `make help` output: `make test`, `make test-unit`, `make test-e2e`,
     `make lint`, `make release-check`, `make build`, `make dist`, `make run`, `make clean`.
   - `make dist` / `make release-check` run GoReleaser through `go run` at the pinned version; the first
     run compiles it.
   - `make vet-cross`: compile-checks the Windows and macOS code from any machine.
   - `make run`: runs the daemon in the foreground against `./.dev` with keyring off; first run writes
     `./.dev/config.toml` (fill in dev app credentials per `docs/platform-setup.md`);
     `make run ARGS="account add youtube"` and similar run any command against the dev home. Stop a real
     daemon first or change `listen_port` in `./.dev/config.toml`, and register that port's redirect URIs.
   - CI: PRs run `pr-gate` (tests on Linux plus `make vet-cross` and `make lint`; docs-only PRs skip
     them, plus `make release-check`); pushes to main run `ci` (tests on Linux,
     macOS and Windows, then build). Both use `.github/actions/test`, which runs `make test`.
2. Add the MIT `LICENSE` and a `## License` line in README ("MIT, see LICENSE").
3. Update `### Releasing`: pushing a tag runs ci, then GoReleaser builds and publishes the release with
   grouped notes from conventional commits; a tag with a suffix is a prerelease.
4. Check `docs/architecture.md` and `docs/platform-setup.md` for direct `go test` / `build-all.sh`
   commands and switch them to make (a grep today finds none).
5. Commit on `feat/pr-gate-make` (exclude the unrelated plan edits from the release-pipeline plan) and open
   a PR. The pending release-pipeline plan edits go in first as their own `docs(plans)` commit. Confirm
   with the user before pushing.
6. On the PR, verify:
   - `pr-gate` runs one `test` job on `ubuntu-latest`; `make test`, `make vet-cross`, `make lint` and
     `make release-check` all pass.
   - `ci` does not run on the PR.
   - Docs-only skip: a throwaway PR (or a temporary commit on this PR, reverted afterwards) that changes only
     a `.md` file shows a green `test` check with the test steps skipped and the summary line
     "Only docs or plans changed; tests skipped." Do not merge it.
7. After merge, verify on main: `ci` runs; `build` starts only after the three test jobs finish green, and
   uploads `social-mcp-binaries` containing exactly the six binaries and `SHA256SUMS`. The Windows job log shows `make test` ran (and whether Chocolatey
   installed make).
8. Prerelease (outward-facing; ask the user first, and agree the tag, for example `v0.1.0-rc.2`, since `v0.1.0-rc.1` exists):
   push the tag and verify the release run and its output:
   - The release is marked prerelease and has the six binaries, `SHA256SUMS`, `install.sh`, `install.ps1`.
   - Release notes are grouped (Features, Bug fixes, Other) with no docs, test, chore, ci or merge entries.
   - `SOCIAL_MCP_VERSION=<tag>` with the curl one-liner installs on Linux and `social-mcp version` prints
     the tag; the same with `install.ps1` on Windows if a Windows machine is available.
9. Merge protection (outward-facing; ask the user first): require the pr-gate status check `test` on
   `main`, either in Settings → Branches or with:

   ```sh
   gh api -X PUT repos/nhatnam1507/social-mcp/branches/main/protection --input - <<'JSON'
   {"required_status_checks":{"strict":false,"contexts":["test"]},
    "enforce_admins":false,"required_pull_request_reviews":null,"restrictions":null}
   JSON
   ```

   `strict: false`: a PR need not be up to date with main (user choice). `enforce_admins: false` lets the
   repo owner bypass in an emergency (user choice).

## Verification

- README commands match `make help` output exactly.
- Links in README resolve to existing files.
- `gh repo view --json licenseInfo` reports MIT after merge.
- PR, main and tag runs behave as in steps 6 to 8 (record run URLs in the PR description).
- `grep -rn build-all --exclude-dir=plans .` returns nothing.
- `gh api repos/nhatnam1507/social-mcp/branches/main/protection` lists the `test` context (after approval).
