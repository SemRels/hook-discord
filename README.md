# hook-discord

[![Latest Release](https://img.shields.io/github/v/release/SemRels/hook-discord?label=version&color=blue)](https://github.com/SemRels/hook-discord/releases/latest)
[![CI](https://github.com/SemRels/hook-discord/actions/workflows/ci.yml/badge.svg)](https://github.com/SemRels/hook-discord/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/SemRels/hook-discord)](LICENSE)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/SemRels/hook-discord/badge)](https://scorecard.dev/viewer/?uri=github.com/SemRels/hook-discord)
[![REUSE status](https://api.reuse.software/badge/github.com/SemRels/hook-discord)](https://api.reuse.software/info/github.com/SemRels/hook-discord)

Posts a release announcement to Discord using a webhook.

This plugin is distributed as the standalone Go binary `semrel-plugin-hook-discord`. Semrel executes the binary as a subprocess, provides plugin configuration through `SEMREL_PLUGIN_*` environment variables, provides release context through `SEMREL_*` environment variables, reads standard output, and treats exit code `0` as success and any non-zero exit code as failure. Install the binary in `~/.semrel/plugins/` or anywhere on your `$PATH`.

## Installation

### Binary

```bash
go install github.com/SemRels/hook-discord/cmd/plugin@latest
```

### Docker

Pre-built, multi-platform images (linux/amd64, linux/arm64) are published to the GitHub Container Registry on every release:

```bash
docker pull ghcr.io/semrels/hook-discord:latest
```

Images are signed with [cosign](https://github.com/sigstore/cosign) and include a full SBOM attestation. Verify the signature:

```bash
cosign verify ghcr.io/semrels/hook-discord:latest \
  --certificate-identity-regexp 'https://github.com/SemRels/hook-discord/.github/workflows/container.yml.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Configuration

```yaml
plugins:
  - uses: "@semrel/hook-discord"
    phase: release
    args:
      discord_webhook_url: "https://discord.com/api/webhooks/1234567890/abcdef"
      repository: "SemRels/hook-discord"
      first_time_contributors: "true"
      release_mvp: "true"
      max_retries: "3"
      retry_delay: "2s"
```

## Options

Every option can be set as an entry under `args` in `.semrel.yaml`. semrel passes each entry to the plugin as the environment variable shown in the table, so you can also set that variable directly (for example in your CI environment) instead of using `args`.

| Argument | Environment variable | Required | Description | Default |
| --- | --- | --- | --- | --- |
| `discord_webhook_url` | `SEMREL_PLUGIN_DISCORD_WEBHOOK_URL` | Required unless dry-run | Discord webhook URL. | None |
| `repository` | `SEMREL_PLUGIN_REPOSITORY` | Optional | Repository name or URL shown in the embed fields. | None |
| `release_url` | `SEMREL_PLUGIN_RELEASE_URL` | Optional | Explicit release URL used as the embed title link. When unset, the plugin derives one from `SEMREL_REPOSITORY_URL` or `SEMREL_PLUGIN_REPOSITORY`. | None |
| `first_time_contributors` | `SEMREL_PLUGIN_FIRST_TIME_CONTRIBUTORS` | Optional | Add a **New Contributors** field when contributor metadata is available. Supports the legacy alias `SEMREL_PLUGIN_NEW_CONTRIBUTORS`. | `false` |
| `release_mvp` | `SEMREL_PLUGIN_RELEASE_MVP` | Optional | Add a **🏆 Release MVP** field using the contributor with the most commits. Supports the legacy alias `SEMREL_PLUGIN_MVP`. | `false` |
| `max_retries` | `SEMREL_PLUGIN_MAX_RETRIES` | Optional | Retries on transient network failures and HTTP `5xx` responses. Discord `429` rate limits are retried once after `Retry-After`. | `3` |
| `retry_delay` | `SEMREL_PLUGIN_RETRY_DELAY` | Optional | Delay between retry attempts for transient failures. | `2s` |
| `contributors_json` | `SEMREL_PLUGIN_CONTRIBUTORS_JSON` | Optional | Legacy contributor JSON payload. `SEMREL_CONTRIBUTORS` is preferred when semrel core provides it. | None |

## `SEMREL_*` release context used

| Variable | Description |
| --- | --- |
| `SEMREL_VERSION` | Resolved release version for the current run. |
| `SEMREL_TAG_NAME` | Git tag name semrel will create or publish. |
| `SEMREL_NEXT_VERSION` | Next version computed by semrel for the release. |
| `SEMREL_CHANGELOG` | Generated changelog text for the release. |
| `SEMREL_CONTRIBUTORS` | Preferred contributor payload for the current release. |
| `SEMREL_REPOSITORY_URL` | Repository base URL used to derive the release link when needed. |
| `SEMREL_DRY_RUN` | When `true`, print the payload JSON to stdout instead of sending it. |

## Example embed

```text
Title: 🚀 Release v1.4.0
Description:
### Highlights
- Added Discord release notifications
- Improved webhook retry handling
- Thank you to everyone who contributed

Fields:
- Version: v1.4.0
- Repository: SemRels/hook-discord
- 🏆 Release MVP: Alice led this release with 3 commits.
- New Contributors:
  • Bob — first contribution in #42
```

If contributor metadata is missing or malformed JSON, the plugin skips the shoutout fields without failing the release.

## License

Apache-2.0
