# KiloCode Free Plugin

CLIProxyAPI plugin for [KiloCode](https://kilo.ai) free-tier models.

## What It Does

- Provides access to KiloCode free models
- No authentication required (keyless upstream)
- Dynamic model catalog fetched from upstream `/models` endpoint
- Refreshes the model catalog every 3 hours, with short retries while empty
- Supports streaming chat completions

## Auth Flow

No OAuth needed. Plugin returns a dummy auth response; executor sends requests directly to upstream without credentials.

## Model Catalog

Dynamic fetch from upstream with a 3-hour refresh. Models with repeated
model-specific inference failures are temporarily quarantined and retried
after a cooldown:
- `kilo-auto/free`
- `kilo-code/free`
- Additional models discovered via `/models`

## Build

```bash
cd plugins/kilo-free
CGO_ENABLED=1 go build -buildmode=c-shared -o kilo-free.so .
```

## Deploy

```bash
cp plugins/kilo-free/kilo-free.so ~/cliproxyapi/plugins/
systemctl --user restart cliproxyapi.service
```

## Configuration

```yaml
plugins:
  configs:
    kilo-free:
      enabled: true
      priority: 1
      kilo_chat_url: "https://api.kilo.ai/api/gateway/v1/chat/completions"
      kilo_models_url: "https://api.kilo.ai/api/gateway/models"
      # Legacy compatibility: kilo_base_url derives the two URLs above.
```

## Upstream Endpoints

- Chat: `https://api.kilo.ai/api/gateway/v1/chat/completions`
- Models: `https://api.kilo.ai/api/gateway/models`

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — Dummy auth (no login needed)
- `model.go` — Dynamic model catalog with cache
- `executor.go` — OpenAI-compatible request translation
- `util.go` — HTTP helpers

## License

MIT
