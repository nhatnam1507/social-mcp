# Every build, test, release-check and local-run command goes through this Makefile, locally and in CI.
# Recipes assume a POSIX shell (Git Bash or WSL on Windows).

.DEFAULT_GOAL := help

GO       ?= go
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DEV_HOME ?= $(CURDIR)/.dev
ARGS     ?= daemon run

# Single source of truth for the GoReleaser version. CI installs this exact version's prebuilt binary
# (read through `make goreleaser-version`) and passes GORELEASER=goreleaser. Locally, `go run` builds it on
# first use; GoReleaser needs a newer Go than go.mod, which GOTOOLCHAIN=auto (the default) downloads.
GORELEASER_VERSION := v2.20.3
GORELEASER ?= $(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

# The race detector needs cgo (a C compiler). CI's Linux and macOS runners have one; it stays off on Windows,
# as it always has in CI, and on machines where cgo is disabled.
ifeq ($(OS),Windows_NT)
  RACE :=
  EXE  := .exe
else
  RACE := $(if $(filter 1,$(shell $(GO) env CGO_ENABLED)),-race,)
  EXE  :=
endif

BIN     := ./social-mcp$(EXE)
DEV_ENV := SOCIAL_MCP_HOME="$(DEV_HOME)" SOCIAL_MCP_KEYRING=off

.PHONY: help vet vet-cross test-unit test-e2e test lint release-check goreleaser-version build dist run clean

help: ## List the available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

vet: ## Run go vet for this machine's OS
	$(GO) vet ./...

vet-cross: ## Compile-check and vet the Windows and macOS builds from any machine
	GOOS=windows $(GO) vet ./...
	GOOS=darwin $(GO) vet ./...

test-unit: ## Run unit and contract tests (with the race detector when cgo is available, never on Windows)
	$(if $(RACE),,@echo "note: race detector off (Windows, or cgo disabled: no C compiler)")
	$(GO) test $(RACE) -count=1 ./...

test-e2e: ## Build the binary and drive it end to end
	$(GO) test -tags e2e -count=1 -v ./e2e/...

test: vet test-unit test-e2e ## Run vet, unit tests and the end-to-end test (what CI runs)

lint: ## Lint the installer script with shellcheck
	shellcheck scripts/install.sh

release-check: ## Validate .goreleaser.yaml
	$(GORELEASER) check

goreleaser-version: ## Print the pinned GoReleaser version
	@echo $(GORELEASER_VERSION)

build: ## Build ./social-mcp for this machine
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/social-mcp

dist: ## Build every release target into dist/ without publishing
	$(GORELEASER) release --snapshot --clean

# `make run` loads ./.env (git-ignored; start from .env.example) into the environment, so the daemon signs
# in through your own OAuth app via the SOCIAL_MCP_* override variables. No secret goes into the build.
run: build | $(DEV_HOME)/config.toml ## Run the daemon in the foreground against ./.dev with ./.env (ARGS="..." runs other commands)
	@set -a; if [ -f .env ]; then . ./.env; else echo "note: no .env, so only platforms set in your environment are available (see .env.example)"; fi; set +a; \
	echo "$(DEV_ENV) $(BIN) $(ARGS)"; $(DEV_ENV) $(BIN) $(ARGS)

$(DEV_HOME)/config.toml: | build
	$(DEV_ENV) $(BIN) config init

clean: ## Remove dist/ and the local binary (keeps ./.dev)
	rm -rf dist $(BIN)
