---
title: "Plan: PR gate, test-gated CI build and Makefile"
date: 2026-11-25
summary: "Planned pr-gate and ci workflows sharing a composite test action, with make as the only command entry point."
---

# Plan: PR gate, test-gated CI build and Makefile

## What happened
Planned `plans/261125-1413-pr-gate-make-targets/`. Today `ci.yml` runs on PRs and main, and its `build` job runs in parallel with `test`, so a build can pass while tests fail. Workflows and README call `go test` and `bash scripts/build-all.sh` directly.

## Decision
- `pr-gate.yml` (pull_request) and `ci.yml` (push to main, plus workflow_call for release) each hold the 3-OS matrix and call a composite action at `.github/actions/test`, which runs `make test`. The user proposed passing `os` into the action; it is unnecessary because the action runs on the matrix runner.
- `ci` build gets `needs: test`.
- The root Makefile owns vet, tests (race off on Windows via `$(OS)`), lint, build, dist (wraps build-all.sh) and run.
- `make run` builds the binary and runs it against `./.dev` with the keyring off. It uses a built binary rather than `go run` because `daemon start` re-executes itself.
- The release publish step stays in YAML.

## Next steps
- Run `/ak:cook` on the plan.
- Verify make on the Windows runner (the action falls back to Chocolatey).
- After user confirmation, require the three `test (<os>)` checks on main. Main is unprotected today.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
