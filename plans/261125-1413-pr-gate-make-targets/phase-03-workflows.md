---
phase: 3
title: "Composite test action and workflows"
status: completed
priority: P1
effort: "1h"
dependencies: [1, 2]
---

# Phase 3: Composite test action and workflows

## Goal

One composite action runs the tests. pr-gate runs it once on Linux plus a cross-OS compile check and the
installer lint and a GoReleaser config check, skipping that work for docs-only PRs; ci runs it across a
3-OS matrix and builds a GoReleaser snapshot only after every test job passes; release publishes with
GoReleaser.

## Files to Create / Modify

- Create: `/home/namnn/src/social-mcp/.github/actions/test/action.yml`
- Create: `/home/namnn/src/social-mcp/.github/actions/setup-goreleaser/action.yml`
- Create: `/home/namnn/src/social-mcp/.github/workflows/pr-gate.yml`
- Modify: `/home/namnn/src/social-mcp/.github/workflows/ci.yml`
- Modify: `/home/namnn/src/social-mcp/.github/workflows/release.yml` (build and publish steps)

## Composite action

```yaml
# .github/actions/test/action.yml
name: test
description: Set up Go and run vet, unit tests and the end-to-end test through make. The caller checks out the repo first.
runs:
  using: composite
  steps:
    - uses: actions/setup-go@v5
      with:
        go-version-file: go.mod
    - name: ensure make is available
      if: runner.os == 'Windows'
      shell: bash
      run: command -v make >/dev/null || choco install make -y --no-progress
    - name: make test
      shell: bash
      run: make test
```

No `os` input: the calling job's `runs-on` already places the action on an OS, and the Makefile picks the
race flag itself. The Windows make step only matters for ci's Windows job. A local action must be
referenced after `actions/checkout`, because the action file comes from the checked-out repo.

## setup-goreleaser action

Installs the prebuilt GoReleaser binary at the version pinned in the Makefile, so CI never compiles it and
never drifts from local `make` runs.

```yaml
# .github/actions/setup-goreleaser/action.yml
name: setup-goreleaser
description: Install the GoReleaser version pinned in the Makefile. Run after checkout and setup-go.
runs:
  using: composite
  steps:
    - id: pin
      shell: bash
      run: echo "version=$(make -s goreleaser-version)" >> "$GITHUB_OUTPUT"
    - uses: goreleaser/goreleaser-action@v6
      with:
        distribution: goreleaser
        version: ${{ steps.pin.outputs.version }}
        install-only: true
```

Callers then run `make <target> GORELEASER=goreleaser`.

## pr-gate.yml

```yaml
name: pr-gate

on:
  pull_request:

concurrency:
  group: pr-gate-${{ github.event.pull_request.number }}
  cancel-in-progress: true

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 2 # the PR merge commit and its base parent
      - name: detect docs-only changes
        id: changes
        run: |
          if git diff --name-only HEAD^1 HEAD | grep -qvE '(\.md$|^docs/|^plans/)'; then
            echo "code=true" >> "$GITHUB_OUTPUT"
          else
            echo "code=false" >> "$GITHUB_OUTPUT"
            echo "Only docs or plans changed; tests skipped." >> "$GITHUB_STEP_SUMMARY"
          fi
      - if: steps.changes.outputs.code == 'true'
        uses: ./.github/actions/test
      - name: compile-check Windows and macOS
        if: steps.changes.outputs.code == 'true'
        run: make vet-cross
      - name: lint the installer script
        if: steps.changes.outputs.code == 'true'
        run: make lint
      - if: steps.changes.outputs.code == 'true'
        uses: ./.github/actions/setup-goreleaser
      - name: validate the release config
        if: steps.changes.outputs.code == 'true'
        run: make release-check GORELEASER=goreleaser
```

- `cancel-in-progress` drops a stale run when new commits land on the same PR.
- Go is set up by the action, so `make vet-cross` needs no extra setup step. `shellcheck` is preinstalled on
  `ubuntu-latest`.
- On `pull_request`, checkout gives the merge commit of the PR into its base, so `HEAD^1` is the base tip
  and the diff is exactly the PR's changes. `grep -v` succeeds when any changed file falls outside the
  docs patterns, so anything unrecognized counts as code. The detection step is CI plumbing (it writes
  `$GITHUB_OUTPUT`), so it stays in the workflow rather than the Makefile.
- The job is named `test` and always runs, so the required status check always reports, green for a
  docs-only PR.

## ci.yml

- Triggers: `push: branches: [main]` and `workflow_call:`; remove `pull_request:`.
- `test` job: `name: test (${{ matrix.os }})`, matrix `os: [ubuntu-latest, macos-latest, windows-latest]`
  with `fail-fast: false`, `runs-on: ${{ matrix.os }}`, steps `actions/checkout@v4` then
  `./.github/actions/test`.
- `build` job: add `needs: test`; keep `fetch-depth: 0` (GoReleaser reads tags) and `setup-go`. Steps:
  `make lint`, `./.github/actions/setup-goreleaser`, `make dist GORELEASER=goreleaser`, then
  `upload-artifact` with `path:` limited to `dist/social-mcp-*` and `dist/SHA256SUMS` (GoReleaser also
  writes `dist/` metadata and per-build folders that should not be uploaded).

## release.yml

- Keep `jobs.ci: uses: ./.github/workflows/ci.yml`, `needs: ci`, `contents: write` and the concurrency group.
- Replace the build and publish steps with:

  ```yaml
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: ./.github/actions/setup-goreleaser
      - name: build and publish the release
        env:
          GITHUB_TOKEN: ${{ github.token }}
        run: goreleaser release --clean
  ```

- This is the one place GoReleaser is called directly: publishing stays in the workflow by decision, so it
  can never run from a developer's `make`. GoReleaser verifies HEAD is the tag and the tree is clean.

## Tasks & Steps

1. Create the composite test action and the setup-goreleaser action.
2. Create `pr-gate.yml`.
3. Rewrite `ci.yml` per the bullets.
4. Replace the release build and publish steps.
5. Grep the workflows to confirm no direct commands remain:
   `grep -rnE 'go (vet|test|build)|build-all\.sh|shellcheck|goreleaser (check|build)' .github` returns
   nothing, and `goreleaser release` appears only in `release.yml`.

## Verification

- `actionlint` on `.github/` if available (`go run github.com/rhysd/actionlint/cmd/actionlint@latest`);
  otherwise check the YAML parses with `python3 -c 'import yaml,sys;[yaml.safe_load(open(f)) for f in sys.argv[1:]]' <files>`.
- The grep in step 5 is empty.
- Live behavior (PR, push to main, prerelease tag) is verified in phase 4.
