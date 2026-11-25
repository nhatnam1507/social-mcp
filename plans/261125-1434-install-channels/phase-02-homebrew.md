---
phase: 2
title: "Homebrew tap"
status: pending
priority: P2
effort: "1h"
dependencies: [1]
---

# Phase 2: Homebrew tap

## Goal

Each stable release updates `nhatnam1507/homebrew-tap` so `brew install nhatnam1507/tap/social-mcp` installs
a working, unquarantined binary.

## Files to Create / Modify

- Modify: `/home/namnn/src/social-mcp/.goreleaser.yaml` (add a `homebrew_casks` entry, plus an archive entry
  only if casks require one)

## Sketch

Check keys against the GoReleaser docs for the pinned version; GoReleaser v2 generates casks
(`homebrew_casks`) and deprecated the older formula generator.

```yaml
homebrew_casks:
  - name: social-mcp
    repository:
      owner: nhatnam1507
      name: homebrew-tap
      token: "{{ .Env.CHANNELS_GITHUB_TOKEN }}"
    homepage: https://github.com/nhatnam1507/social-mcp
    description: MCP server and CLI daemon for uploading videos to YouTube, TikTok, Facebook Pages and Instagram Reels
    license: MIT
    skip_upload: auto            # prereleases never update the tap
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/social-mcp"]
          end
    caveats: |
      After upgrading, restart a running daemon: social-mcp daemon stop && social-mcp daemon start
```

## Tasks & Steps

1. Add the cask entry; if the cask needs an archive, add `archives` entry `id: homebrew` (`tar.gz`, darwin
   and linux) referenced by the cask via `ids`, leaving the raw-binary entry untouched.
2. Confirm whether Homebrew casks install on Linux; record the answer in the plan and README.
3. `make release-check` and `make dist` pass; inspect the generated cask file under `dist/`.

## Verification

- `make dist` writes the cask locally (snapshot, not pushed); its URLs point at the release assets and its
  SHA-256 values match `SHA256SUMS` (or the archive checksums).
- End-to-end install is verified in phase 5 with a stable release.
