---
phase: 2
title: "Release workflow"
status: in-progress
priority: P2
effort: "1h"
dependencies: [1]
---

# Phase 2: Release workflow

## Goal

Pushing a `v*` tag runs the full test matrix, builds every target with the tag as version, and publishes a
GitHub Release carrying the binaries, `SHA256SUMS` and both installers.

## Files to Create / Modify

- Create: `.github/workflows/release.yml`
- Modify: `.github/workflows/ci.yml` — add `workflow_call:` trigger; add `shellcheck scripts/install.sh` to
  the `build` job.

## Tasks & Steps

- [x] `ci.yml`: `workflow_call:` trigger and a `shellcheck scripts/install.sh` step in the `build` job.
- [x] `release.yml`: `on: push: tags: ['v*']`, `concurrency: release-${{ github.ref }}`, job `ci` reusing
      `ci.yml`, job `release` (`needs: ci`, `contents: write`) that runs `scripts/build-all.sh` with
      `VERSION` = tag, copies both installers into `dist/`, and runs `gh release create` with
      `--verify-tag --generate-notes`, plus `--prerelease` for tags containing `-`.
- [x] Tag passed through `env:`, never interpolated inside `run:`.
- [ ] First live run (phase 3 release candidate).

## Validation (2026-11-25)

- Both workflows parse as YAML with the expected triggers and jobs; the publish step's bash passes `bash -n`.
- actionlint was not run: executing a downloaded tool was blocked by the session's permission policy.
- A tag push does not also trigger ci.yml's own `push` run (that trigger is limited to `main`).

## Risk / Rollback

- A failed release leaves the tag without a release: fix, delete the tag locally and remotely, re-tag.
- Roll back by deleting `release.yml` and the two `ci.yml` additions.
