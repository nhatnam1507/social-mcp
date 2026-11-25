---
title: Built the release workflow and one-line installers
date: 2026-11-25
summary: "install.sh and install.ps1 pick, verify and install the right binary; a v* tag publishes the GitHub Release; tested locally on Linux and real Windows."
---

# Built the release workflow and one-line installers

## What happened

Implemented `plans/261119-1402-release-pipeline-install-scripts` on branch `feat/release-pipeline`. The plan
folder, the 2026-11-19 planning journal and the untracked eval-suite files (`eval/`, `.github/workflows/eval.yml`)
were missing from disk when work started. The plan was restored from the planning session's text; the eval
work was not touched.

## Changes

- `scripts/install.sh`: POSIX sh, curl or wget, sha256sum or shasum, Rosetta-aware arch detection,
  copy-then-rename install into `~/.local/bin`.
- `scripts/install.ps1`: PowerShell 5.1 and 7, registry-based native CPU detection, renames a running exe
  aside, edits the raw user PATH registry value and keeps its kind (the machine's value was `REG_SZ`;
  always writing `REG_EXPAND_SZ` would have changed it).
- `.github/workflows/release.yml` reuses `ci.yml` via `workflow_call`; `ci.yml` gains a shellcheck step.
- README Install and Releasing sections.

## Verification

- Installers were tested without a real release: a stand-in `curl` (sh) and an overridden
  `Invoke-WebRequest` function (PowerShell) served `scripts/build-all.sh` output.
- install.ps1 ran on real Windows from WSL through interop. The Windows user PATH was backed up first and
  restored byte-for-byte afterwards.
- Running the harness under zsh first gave false "unsupported OS" results because zsh does not word-split
  `$var`. Running it under bash fixed that.
- actionlint was blocked by the session's permission policy. The workflows were only YAML-parsed.

## Next steps

Commit and open a PR, merge, then push `v0.1.0-rc.1` and run the one-liners against it on Linux, Windows
and macOS.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
