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

Health probes run every 15 minutes for models in the live catalog. A failed
probe or limit/server/timeout/invalid response hides that model immediately; a
successful later probe restores it. Normal model-specific 4xx failures use the
three-failure threshold. SSE is buffered with 100,000-chunk, 100 MiB total, and
1 MiB line limits.

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — Dummy auth (no login needed)
- `model.go` — Dynamic model catalog refresh and model entries
- `executor.go` — OpenAI-compatible request translation
- `util.go` — HTTP helpers

## License

MIT
