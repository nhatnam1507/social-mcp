---
phase: 2
title: "GoReleaser build and release config"
status: completed
priority: P1
effort: "1h"
dependencies: [1]
---

# Phase 2: GoReleaser build and release config

## Goal

`.goreleaser.yaml` produces exactly the assets the installers expect, `make dist` and `make release-check`
drive it, and `scripts/build-all.sh` is deleted.

## Files to Create / Modify

- Create: `/home/namnn/src/social-mcp/.goreleaser.yaml`
- Modify: `/home/namnn/src/social-mcp/Makefile` (`dist`, `release-check`, `goreleaser-version`, GoReleaser vars)
- Delete: `/home/namnn/src/social-mcp/scripts/build-all.sh`
- Unchanged: `/home/namnn/src/social-mcp/scripts/install.sh`, `/home/namnn/src/social-mcp/scripts/install.ps1`

## Contract the config must keep

The installers build the asset name from OS and CPU and verify it against `SHA256SUMS`, so:

| Item | Required value |
|---|---|
| Targets | linux, darwin, windows × amd64, arm64; `CGO_ENABLED=0`; `-trimpath` |
| Asset names | `social-mcp-<os>-<arch>`, plus `.exe` on Windows, uploaded as raw binaries (no archives) |
| Checksums | `SHA256SUMS`, lines `<sha256>  <asset>` |
| Extra release files | `install.sh`, `install.ps1` at the release root |
| Version stamp | `-X main.version=<tag>` with the leading `v` (`{{ .Tag }}`, not `{{ .Version }}`) |
| Prerelease | a tag with a suffix (`v0.1.0-rc.1`) is a prerelease |

## Config sketch

Check every key against the GoReleaser v2 docs for the pinned version while implementing (notably binary
archive naming and the `.exe` suffix, and `IsSnapshot` in ldflags).

```yaml
version: 2
project_name: social-mcp

builds:
  - id: social-mcp
    main: ./cmd/social-mcp
    binary: social-mcp
    env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    flags: [-trimpath]
    ldflags:
      - -s -w -X main.version={{ if .IsSnapshot }}{{ .Version }}{{ else }}{{ .Tag }}{{ end }}
    mod_timestamp: "{{ .CommitTimestamp }}"

archives:
  - id: raw
    formats: [binary]
    name_template: "{{ .ProjectName }}-{{ .Os }}-{{ .Arch }}"

checksum:
  name_template: SHA256SUMS
  algorithm: sha256

snapshot:
  version_template: "{{ incpatch .Version }}-next+{{ .ShortCommit }}"

changelog:
  use: git
  sort: asc
  groups:
    - title: Features
      regexp: '^.*?feat(\(.+\))??!?:.+$'
      order: 0
    - title: Bug fixes
      regexp: '^.*?fix(\(.+\))??!?:.+$'
      order: 1
    - title: Other
      order: 999
  filters:
    exclude:
      - '^.*?(docs|test|chore|ci)(\(.+\))??!?:'
      - '^Merge (pull request|branch)'

release:
  prerelease: auto
  extra_files:
    - glob: scripts/install.sh
    - glob: scripts/install.ps1
```

## Makefile additions

```make
GORELEASER_VERSION := v2.X.Y   # pin the latest v2 release at implementation time; single source of truth
GORELEASER ?= $(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

goreleaser-version: ## Print the pinned GoReleaser version (read by CI)
	@echo $(GORELEASER_VERSION)

release-check: ## Validate .goreleaser.yaml
	$(GORELEASER) check

dist: ## Build every release target into dist/ without publishing
	$(GORELEASER) release --snapshot --clean
```

- `dist` uses `release --snapshot` rather than `build`, because only the release pipeline writes the raw
  binary assets and `SHA256SUMS`; `--snapshot` never publishes.
- The `VERSION` variable stays for `make build` and `make run`; `dist` no longer takes it (the tag comes
  from git in a real release, and snapshots get the snapshot version).
- CI overrides `GORELEASER=goreleaser` after installing the prebuilt binary (phase 3).

## Tasks & Steps

1. Look up the latest GoReleaser v2 release and set `GORELEASER_VERSION`.
2. Write `.goreleaser.yaml` per the contract and sketch; run `make release-check` until it passes with no
   deprecation warnings.
3. Run `make dist`, then compare its output with the contract table. Fix naming until it matches exactly.
4. Delete `scripts/build-all.sh`; `grep -rn build-all` over the repo (excluding `plans/`) returns nothing
   once README is updated in phase 4.
5. Add `dist/` handling: it is already gitignored; `make clean` keeps removing it.

## Verification

- `make release-check` passes.
- `make dist`: `ls dist/` shows the six `social-mcp-<os>-<arch>[.exe]` files and `SHA256SUMS`;
  `cd dist && sha256sum -c SHA256SUMS` passes; `./dist/social-mcp-linux-amd64 version` prints a snapshot
  version.
- The installers have no base-URL override (they always download from GitHub Releases), so their end-to-end
  check is the prerelease in phase 4. Here, confirm by inspection that the asset names in `dist/` match the
  names `scripts/install.sh` and `scripts/install.ps1` construct.
