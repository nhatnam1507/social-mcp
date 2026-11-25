---
phase: 1
title: "Release lookup and in-place install"
status: completed
priority: P2
effort: "2h"
dependencies: []
---

# Phase 1: Release lookup and in-place install

## Goal

A new `internal/update` package can find the latest stable release, decide whether a version is newer, and
safely install a release binary over the current executable.

## Files to Create / Modify

- Create: `/home/namnn/src/social-mcp/internal/update/update.go` (lookup, version gating, download, verify)
- Create: `/home/namnn/src/social-mcp/internal/update/replace_unix.go`, `replace_windows.go` (executable swap)
- Create: `/home/namnn/src/social-mcp/internal/update/update_test.go`
- Modify: `/home/namnn/src/social-mcp/go.mod`, `go.sum` (add `golang.org/x/mod`)

## Design

- `Latest(ctx) (tag string, err error)`: GET `https://github.com/nhatnam1507/social-mcp/releases/latest`
  without following redirects; take the tag from the `Location` header (`.../releases/tag/<tag>`). "No stable
  release yet" is a distinct, non-fatal result. The base URL is a package variable so tests point it at an
  `httptest` server.
- `IsRelease(v string) bool`: true only for clean release tags (`semver.IsValid` and no extra `-` parts beyond
  one prerelease segment, so `v0.1.0-rc.1-1-gfd2b9a9-dirty` and `dev` are false).
- `Newer(current, latest string) bool`: both release tags and `semver.Compare(latest, current) > 0`.
- `AssetName(goos, goarch)`: `social-mcp-<os>-<arch>` plus `.exe` on Windows, matching `.goreleaser.yaml` and
  the installers.
- `Download(ctx, tag, dir) (path string, err error)`: fetch the asset and `SHA256SUMS` from
  `releases/download/<tag>/`, verify the hash, write the binary to a temp file in `dir` (same folder as the
  target so the final rename is atomic), `chmod 0755`, then run `<tmp> version` and require it to print
  `tag`. Any failure removes the temp file.
- `Replace(tmp, target string) error`: Unix renames `tmp` over `target`. Windows renames `target` to
  `target + ".old"` (removing an older `.old` first), then renames `tmp` to `target`, and restores the
  original if the second rename fails. `CleanupOld(target)` removes a leftover `.old`, best effort.
- HTTP: one client with a timeout and a `User-Agent` of `social-mcp/<version>` (no personal data).

## Tasks & Steps

1. Add `golang.org/x/mod` and implement the functions above.
2. Unit tests with `httptest`:
   - redirect parsing (tag found, no release, unexpected response);
   - `IsRelease` and `Newer` tables (stable, prerelease, dev, `git describe` strings, equal versions);
   - download success, checksum mismatch, missing asset, missing `SHA256SUMS` entry, version self-check
     mismatch (the test serves a small script or a built helper binary that prints a version);
   - `Replace` in a temp dir, including the Windows path (runs on the Windows CI job) and restore-on-failure.

## Verification

- `make test` and `make vet-cross` pass locally; the Windows-specific replace test runs in ci on main.
