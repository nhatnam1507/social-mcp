---
phase: 1
title: "Makefile and local dev run"
status: completed
priority: P1
effort: "1h"
dependencies: []
---

# Phase 1: Makefile and local dev run

## Goal

A root `Makefile` becomes the single entry point for vet, tests, lint, local build, release build and local
run, with the same `make test` working on Linux, macOS and Windows.

## Files to Create / Modify

- Create: `/home/namnn/src/social-mcp/Makefile`
- Modify: `/home/namnn/src/social-mcp/.gitignore` (add `/.dev/`)
- `dist`, `release-check` and the GoReleaser variables are added in phase 2, once `.goreleaser.yaml` exists.

## Targets

| Target | Does | Replaces |
|---|---|---|
| `help` (default) | Lists targets with their `## ` descriptions | |
| `vet` | `go vet ./...` | `go vet` step in ci.yml |
| `vet-cross` | `GOOS=windows go vet ./...` and `GOOS=darwin go vet ./...` (compile-checks the other OSes' files from any host) | |
| `test-unit` | `go test $(RACE) -count=1 ./...` | the two OS-specific unit-test steps |
| `test-e2e` | `go test -tags e2e -count=1 -v ./e2e/...` | e2e step |
| `test` | `vet` + `test-unit` + `test-e2e` (what CI runs) | |
| `lint` | `shellcheck scripts/install.sh` | lint step in ci.yml |
| `build` | Local binary `./social-mcp$(EXE)` with `-X main.version=$(VERSION)` | |
| `run` | `build`, ensure dev config, then run `./social-mcp $(ARGS)` against the dev home | |
| `clean` | Remove `dist/` and the local binary; keeps `.dev/` (dev accounts live there) | |

## Sketch

```make
.DEFAULT_GOAL := help

GO       ?= go
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DEV_HOME ?= $(CURDIR)/.dev
ARGS     ?= daemon run

ifeq ($(OS),Windows_NT)
  RACE :=           # the race detector needs cgo on Windows
  EXE  := .exe
else
  RACE := -race
  EXE  :=
endif

BIN     := ./social-mcp$(EXE)
DEV_ENV := SOCIAL_MCP_HOME="$(DEV_HOME)" SOCIAL_MCP_KEYRING=off

.PHONY: help vet vet-cross test-unit test-e2e test lint build run clean   # phase 2 adds dist, release-check, goreleaser-version

test: vet test-unit test-e2e ## Run vet, unit tests and the end-to-end test (what CI runs)

vet-cross: ## Compile-check and vet the Windows and macOS builds from this machine
	GOOS=windows $(GO) vet ./...
	GOOS=darwin $(GO) vet ./...

build: ## Build ./social-mcp for this machine
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/social-mcp

run: build $(DEV_HOME)/config.toml ## Run the daemon in the foreground against ./.dev (ARGS="..." for other commands)
	$(DEV_ENV) $(BIN) $(ARGS)

$(DEV_HOME)/config.toml:
	$(DEV_ENV) $(BIN) config init
```

`run` executes the built binary instead of `go run` on purpose: `go run` deletes its temporary binary on
exit, which would break `make run ARGS="daemon start"` (the detached daemon re-executes itself), and a
real binary receives Ctrl+C directly.

## Tasks & Steps

1. Write the Makefile per the table and sketch. Every public target gets a `## ` description; `help` prints
   them with `awk` (no extra tools).
2. Make `$(DEV_HOME)/config.toml` an order-only prerequisite of `run` so `config init` runs once and never
   overwrites an edited dev config (it also refuses without `--force`).
3. Add `/.dev/` to `.gitignore` (it holds the dev database, MCP token and, with keyring off, the encryption
   key file).
4. Leave `scripts/build-all.sh` in place for now; phase 2 replaces it with GoReleaser and deletes it.

## Verification

- Install make locally first if missing (`sudo apt install make`; make and shellcheck are not installed in
  this WSL environment today).
- `make` prints the help list.
- `make test` passes and the output shows `-race` on Linux.
- `make vet-cross` passes. Temporarily adding a syntax error to `internal/daemon/lock_windows.go` makes it
  fail while `make test` on Linux still passes (revert afterwards).
- `make build VERSION=v0.0.0-test && ./social-mcp version` prints `v0.0.0-test`.
- `make run` creates `./.dev/config.toml` and starts the daemon in the foreground; Ctrl+C stops it.
  `make run ARGS="daemon status"` from a second shell reports it running. `git status` shows no `.dev` files.
- `make clean` removes `dist/` and `./social-mcp` but leaves `.dev/`.
