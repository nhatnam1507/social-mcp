---
phase: 4
title: "Linux .deb and .rpm"
status: pending
priority: P2
effort: "1h"
dependencies: [1]
---

# Phase 4: Linux .deb and .rpm

## Goal

Each release, including prereleases, attaches `.deb` and `.rpm` packages for amd64 and arm64 that install
`/usr/bin/social-mcp`.

## Files to Create / Modify

- Modify: `/home/namnn/src/social-mcp/.goreleaser.yaml` (add an `nfpms` entry)

## Sketch

```yaml
nfpms:
  - id: linux-packages
    package_name: social-mcp
    file_name_template: "{{ .ConventionalFileName }}"
    formats: [deb, rpm]
    bindir: /usr/bin
    vendor: nhatnam1507
    homepage: https://github.com/nhatnam1507/social-mcp
    maintainer: "nhatnam1507 <69423360+nhatnam1507@users.noreply.github.com>"
    description: MCP server and CLI daemon for uploading videos to YouTube, TikTok, Facebook Pages and Instagram Reels
    license: MIT
```

## Tasks & Steps

1. Add the `nfpms` entry (Linux builds only; GoReleaser applies it to linux targets).
2. Make sure the packages are included in `SHA256SUMS` so users can verify them.
3. `make dist`, then install each package in a clean container:
   - `docker run --rm -v "$PWD/dist:/d" ubuntu:24.04 sh -c 'apt-get update -qq && apt-get install -y /d/social-mcp_*_amd64.deb && social-mcp version'`
   - `docker run --rm -v "$PWD/dist:/d" fedora:41 sh -c 'dnf install -y /d/social-mcp-*.x86_64.rpm && social-mcp version'`

## Verification

- Both container commands print the snapshot version.
- The raw binaries and their `SHA256SUMS` lines are unchanged.
