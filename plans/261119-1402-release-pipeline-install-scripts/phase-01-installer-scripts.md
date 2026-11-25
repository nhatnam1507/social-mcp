---
phase: 1
title: "Installer scripts"
status: completed
priority: P2
effort: "2h"
dependencies: []
---

# Phase 1: Installer scripts

## Goal

Two scripts that pick, download, verify and install the right release binary: `install.sh` for Linux and
macOS, `install.ps1` for Windows.

## Files to Create / Modify

- Create: `scripts/install.sh` (POSIX `sh`, executable bit set)
- Create: `scripts/install.ps1`

## Tasks & Steps

### `scripts/install.sh`

- [x] Shebang `#!/bin/sh`, `set -eu`; all logic inside `main()` called on the last line, so a truncated
      `curl | sh` download never runs a partial script.
- [x] Inputs: `SOCIAL_MCP_VERSION` (default `latest`), `SOCIAL_MCP_INSTALL_DIR` (default `$HOME/.local/bin`).
- [x] OS from `uname -s`: `Linux` → `linux`, `Darwin` → `darwin`; MINGW/MSYS/Cygwin point to `install.ps1`;
      anything else exits 1.
- [x] CPU from `uname -m`: `x86_64|amd64` → `amd64`, `aarch64|arm64` → `arm64`; else exit 1 naming the value.
      On `darwin` + `amd64`, `sysctl -n hw.optional.arm64` = `1` switches to `arm64` (Rosetta).
- [x] Base URL from `releases/latest/download` or `releases/download/$SOCIAL_MCP_VERSION`.
- [x] Downloader: `curl -fsSL --retry 3`, else `wget -q`, else exit 1; failures name the URL.
- [x] Download asset and `SHA256SUMS` into `mktemp -d`, removed on exit or signal.
- [x] Verify against the `SHA256SUMS` entry with `sha256sum` or `shasum -a 256`; abort when the entry or
      the hash tool is missing, or the hashes differ.
- [x] Install by copying next to the target and renaming over it (safe for a running binary).
- [x] Print the installed version, a PATH hint when needed, and a daemon restart hint on upgrade.

### `scripts/install.ps1`

- [x] `$ErrorActionPreference = 'Stop'` inside `Install-SocialMcp`, invoked on the last line; never calls
      `exit`.
- [x] Same inputs via `$env:SOCIAL_MCP_VERSION` and `$env:SOCIAL_MCP_INSTALL_DIR` (default
      `$env:LOCALAPPDATA\Programs\social-mcp`).
- [x] CPU from the machine-wide `PROCESSOR_ARCHITECTURE` registry value (native even under emulation),
      falling back to the process environment.
- [x] Download with `Invoke-WebRequest -UseBasicParsing`, TLS 1.2 forced for 5.1, progress bar off.
- [x] Verify with `Get-FileHash`, case-insensitive.
- [x] Rename an existing (possibly running) `social-mcp.exe` aside before moving the new one in; delete the
      old copy when it is not locked.
- [x] Add the dir to the user PATH through the raw registry value (keeps `%VAR%` entries and the existing
      value kind), broadcast the change, and add it to the current session.
- [x] Print the installed version and the daemon restart hint on upgrade.

## Validation (2026-11-25)

- `sh -n` and `dash -n` pass. `shellcheck` is not installed locally; it runs in the CI `build` job.
- `install.sh` under `dash` against a local fake release (a stand-in `curl` mapping release URLs to
  `scripts/build-all.sh` output): fresh install, upgrade in place, pinned version, `curl | sh` from stdin,
  replacing a running binary, wget fallback — all pass. Missing version, tampered `SHA256SUMS`, missing
  entry, no hash tool and no downloader each exit 1 and install nothing. A script truncated mid-file
  installs nothing.
- OS/CPU mapping with a stand-in `uname`/`sysctl`: Darwin arm64, Darwin x86_64 under Rosetta → arm64,
  Darwin x86_64, Linux aarch64, Linux x86_64 pick the right asset; MINGW, FreeBSD, i686 and armv7l exit 1
  with the expected message.
- `install.ps1` on real Windows under PowerShell 7.6 and Windows PowerShell 5.1 via `irm | iex`-style
  `Get-Content -Raw | Invoke-Expression`, with `Invoke-WebRequest` overridden to serve the fake release:
  fresh install adds the PATH entry once and keeps the value kind, upgrade while the exe is running,
  pinned version, missing version and tampered checksum all behave as specified. The user PATH registry
  value was backed up first and restored byte-for-byte afterwards.

## Risk / Rollback

New files only; delete them to roll back.
