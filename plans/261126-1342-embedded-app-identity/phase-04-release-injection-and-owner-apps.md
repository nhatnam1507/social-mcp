---
title: "Phase 4: Release injection and owner apps"
status: done
priority: P1
effort: 1.5h
dependencies: [1]
---

# Phase 4: Release injection and owner apps

## Goal

Inject the owner's app identity into tag-release binaries from GitHub Actions secrets, keep snapshot and
source builds working without them, and prepare the owner's platform apps for shared use.

## Key Insights

- `.goreleaser.yaml` already injects `main.version` through `ldflags`; the identity vars follow the same path.
- `make dist` and CI `release-check` run snapshot builds without secrets. Templates must use
  `{{ index .Env "NAME" }}`, which yields an empty string when unset, not `{{ .Env.NAME }}`.
- `-s -w` strips symbols, not string constants; the values remain extractable. That is the accepted risk.
- Rotation: changing a GitHub secret and cutting a release. Old binaries then fail with the
  `ErrAppCredentials` message from phase 1, and the existing update notice tells users to run
  `social-mcp update`.

## Files to Create / Modify

- Modify: `.goreleaser.yaml`: add `-X github.com/nhatnam1507/social-mcp/internal/appauth.<var>={{ index .Env "SOCIAL_MCP_<VAR>" }}` for each identity var.
- Modify: `.github/workflows/release.yml`: pass the secrets as env to the GoReleaser step; add a preceding step that fails the job, naming the missing secret names (never values), when any required secret is empty.
- Modify: `Makefile`: no secrets; leave `build`/`dist` unchanged and note in a comment that local builds use the env override.

GitHub Actions secrets to create (repository settings):

`SOCIAL_MCP_YOUTUBE_CLIENT_ID`, `SOCIAL_MCP_YOUTUBE_CLIENT_SECRET`, `SOCIAL_MCP_TIKTOK_CLIENT_KEY`,
`SOCIAL_MCP_TIKTOK_CLIENT_SECRET`, `SOCIAL_MCP_META_APP_ID`, `SOCIAL_MCP_META_APP_SECRET`,
`SOCIAL_MCP_META_LOGIN_CONFIG_ID`, `SOCIAL_MCP_META_APP_MODE`.

## Owner checklist (outside the repo)

1. Google: Desktop-app OAuth client in the owner's project; consent screen "In production" with the
   `youtube.upload` and `youtube.readonly` scopes.
2. TikTok: Login Kit Desktop redirect `http://127.0.0.1:8765/oauth/callback`; Content Posting API.
3. Meta: Business app, Facebook Login for Business configuration with the five permissions; valid OAuth
   redirect `http://localhost:8765/oauth/callback`.
4. Verify Meta accepts the `http://localhost` redirect in Live mode before switching `SOCIAL_MCP_META_APP_MODE`
   to `live`. If it doesn't, stop and revisit (the fallback needs a hosted page, which is out of scope here).
5. Create the GitHub secrets above.

## Tasks & Steps

1. Add ldflags and confirm `make dist` still succeeds with no env set.
2. Add the release guard step and secret env mapping.
3. Locally build with the env vars set to test values and confirm `appauth` reports `embedded` for each platform (`daemon run` startup log), then confirm a plain `make build` reports `unavailable`.

## Verification

- `make release-check` and `make dist` pass without secrets.
- `strings dist/bin/social-mcp-linux-amd64 | grep -c <test-client-id>` is 1 for a build made with test values, 0 without.

## Success Criteria

- A tag release cannot publish binaries missing an app identity.
- Secrets live only in GitHub Actions secrets and in release binaries.

## Risk Assessment

- **Secret appears in CI logs:** GitHub masks registered secrets; the guard step prints names only.
- **Meta localhost rejected in Live mode:** checklist step 4 catches it before users hit it.

## Security Considerations

Never echo the env values; never write them to files in the workspace; never commit them.
