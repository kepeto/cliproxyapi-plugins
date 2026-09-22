# OpenCode Zen Free Plugin

CLIProxyAPI plugin for [OpenCode Zen](https://opencode.ai) free-tier models.

## What It Does

- Provides access to OpenCode Zen free models
- No authentication required (keyless upstream)
- Dynamically refreshes the free model catalog from the upstream `/models` endpoint
- Supports streaming chat completions

## Auth Flow

- No OAuth is required. The plugin creates a local keyless profile and sends
  `Authorization: Bearer public` to the OpenCode chat endpoint.

## Model Catalog

The plugin refreshes the free model catalog from the configured upstream
`/v1/models` endpoint. The catalog is held in memory and refreshed periodically;
new upstream models can become available without rebuilding the plugin. OpenCode
has no static fallback: an unavailable empty catalog returns a model-refresh
error. Models that repeatedly fail model-specific inference are temporarily
quarantined and automatically retried after the cooldown.

### Free/chat classification

Upstream `/v1/models` returns the full Zen catalog, including paid models. Each
refresh also fetches the public model metadata document
(`https://models.opencode.ai/api.json`) and classifies every live model by its
`opencode` provider entry:

- free: `cost.input == 0` and `cost.output == 0`
- chat-compatible: provider npm is `@ai-sdk/openai-compatible`
- not retired: `status != "deprecated"`

The metadata document is authoritative: a live model absent from it is not
published, and a metadata fetch failure fails the refresh rather than guessing
from the model name. This prevents stale or paid models from entering the free
catalog and lets a later refresh recover automatically.

The metadata document lists the same bare model ID under many unrelated
gateways whose prices and formats differ from Zen. Only the `opencode` provider
entry is consulted, otherwise a paid Zen model such as `deepseek-v4-flash` would
be exposed as free because another gateway prices it at zero.

Catalog entries report live `created`/`owned_by` values from `/v1/models`, the
official `limit.context` and `limit.output`, display names and descriptions, and
input/output modalities and capability flags from the metadata document. For
example, `mimo-v2.5-free` advertises text, image, audio, and video input rather
than a plugin-defined text-only default. Aliases inherit the target model's
metadata.

## Build

```bash
cd plugins/opencode-free
CGO_ENABLED=1 go build -buildmode=c-shared -o opencode-free.so .
```

## Deploy

From the repository root:

```bash
make deploy
systemctl --user restart cli-proxy-api.service
```

## Configuration

```yaml
plugins:
  configs:
    opencode-free:
      enabled: true
      priority: 1
      opencode_base_url: "https://opencode.ai/zen"
      # Optional explicit overrides:
      # opencode_chat_url: "https://opencode.ai/zen/v1/chat/completions"
      # opencode_models_url: "https://opencode.ai/zen/v1/models"
      # Optional: client-visible alias -> upstream model ID.
      model_aliases:
        deep-free: "deepseek-v4-flash-free"
```

Aliases appear in `/v1/models` alongside configured entries; their targets must
exist in the current live catalog. They use the active prefix, for example
`opencode-free/deep-free`, while config values remain bare upstream IDs.
`plugin.reconfigure` applies alias changes without restart.

## Upstream Endpoint

- Chat: `https://opencode.ai/zen/v1/chat/completions`
- Models: `https://opencode.ai/zen/v1/models`

## Runtime Guarantees

Model health checking is disabled by default. Set `health_check: true` to run
probes every 15 minutes and hide a model after failed probes or inference
failures. A later successful probe restores it. When disabled, health state never
removes models from the dashboard list. SSE is buffered with 100,000 chunks, 100 MiB
total, and 1 MiB line limits.

A buffered stream is only forwarded when it contains both a JSON event carrying
`choices` and a `[DONE]` terminator. A stream that ends early is rejected with
`executor_stream_failed` and marks the model unhealthy, so a silently truncated
upstream stream is not presented to the client as a complete response.

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — Dummy auth (no login needed)
- `model.go` — Dynamic model catalog refresh and model entries
- `executor.go` — OpenAI-compatible request translation
- `util.go` — HTTP helpers

## License

MIT
