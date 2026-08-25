# OpenCode Zen Free Plugin

CLIProxyAPI plugin for [OpenCode Zen](https://opencode.ai) free-tier models.

## What It Does

- Provides access to OpenCode Zen free models
- No authentication required (keyless upstream)
- Dynamically refreshes the free model catalog from the upstream `/models` endpoint
- Supports streaming chat completions

## Auth Flow

No OAuth needed. Plugin returns a dummy auth response; executor sends requests directly to upstream without credentials.

## Model Catalog

The plugin refreshes the free model catalog from the configured upstream
`/models` endpoint. The catalog is cached and refreshed periodically, so new
upstream models can become available without rebuilding the plugin. If the
upstream endpoint is unavailable and no cached catalog exists, the plugin
returns a model-refresh error.

## Build

```bash
cd plugins/opencode-free
CGO_ENABLED=1 go build -buildmode=c-shared -o opencode-free.so .
```

## Deploy

```bash
cp plugins/opencode-free/opencode-free.so ~/.cli-proxy-api/plugins/
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
      # Optional: client-visible alias -> upstream model ID.
      model_aliases:
        ox-alpha: "x-preview-f-free"
```

Aliases appear in `/v1/models` alongside the live catalog; requests to an
alias are routed to its upstream model. Applied on hot reload via
`plugin.reconfigure` — no restart needed.

## Upstream Endpoint

- Chat: `https://opencode.ai/zen/v1/chat/completions`
- Models: Dynamic catalog from the configured upstream `/models` endpoint

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — Dummy auth (no login needed)
- `model.go` — Dynamic model catalog refresh and model entries
- `executor.go` — OpenAI-compatible request translation
- `util.go` — HTTP helpers

## License

MIT
