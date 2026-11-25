---
phase: 3
title: "winget"
status: pending
priority: P2
effort: "1h"
dependencies: [1]
---

# Phase 3: winget

## Goal

Each stable release pushes a winget manifest for `nhatnam1507.SocialMCP` to the user's fork and opens a PR
to `microsoft/winget-pkgs`.

## Files to Create / Modify

- Modify: `/home/namnn/src/social-mcp/.goreleaser.yaml` (add a `winget` entry, plus a Windows `zip`
  archive entry if winget requires one)

## Sketch

Check keys against the GoReleaser docs for the pinned version.

```yaml
winget:
  - name: social-mcp
    publisher: nhatnam1507
    package_identifier: nhatnam1507.SocialMCP
    short_description: MCP server and CLI daemon for uploading videos to social platforms
    license: MIT
    homepage: https://github.com/nhatnam1507/social-mcp
    release_notes_url: "https://github.com/nhatnam1507/social-mcp/releases/tag/{{ .Tag }}"
    skip_upload: auto            # prereleases never open a winget PR
    repository:
      owner: nhatnam1507
      name: winget-pkgs
      token: "{{ .Env.CHANNELS_GITHUB_TOKEN }}"
      branch: "social-mcp-{{ .Version }}"
      pull_request:
        enabled: true
        base:
          owner: microsoft
          name: winget-pkgs
          branch: master
```

## Tasks & Steps

1. Add the winget entry. If GoReleaser requires a zip for winget, add `archives` entry `id: winget` (`zip`,
   windows only) and reference it with `ids`; the raw `.exe` assets stay for `install.ps1`.
2. Make sure the manifest declares a portable install with the command alias `social-mcp`, so it is on PATH
   after `winget install`.
3. `make release-check` and `make dist` pass; inspect the generated manifests under `dist/`.
4. If `winget` is available on a Windows machine, validate the generated manifest locally with
   `winget validate --manifest <dir>`.

## Verification

- Generated manifests contain the identifier, version, MIT license, installer URL and SHA-256 that match
  the release asset.
- PR creation and install are verified in phase 5 with a stable release; the merge depends on Microsoft's
  review.
