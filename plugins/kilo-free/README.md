# KiloCode Free Plugin

CLIProxyAPI plugin for [KiloCode](https://kilo.ai) free-tier models.

## What It Does

- Provides access to KiloCode free models
- No authentication required (keyless upstream)
- Dynamic model catalog fetched from upstream `/models` endpoint
- 5-minute model cache TTL
- Supports streaming chat completions

## Auth Flow

No OAuth needed. Plugin returns a dummy auth response; executor sends requests directly to upstream without credentials.

## Model Catalog

Dynamic fetch from upstream with 5-minute cache:
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
      kilo_base_url: "https://api.kilo.ai/api/gateway"
```

## Upstream Endpoints

- Chat: `https://api.kilo.ai/api/gateway/chat/completions`
- Models: `https://api.kilo.ai/api/gateway/models`

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — Dummy auth (no login needed)
- `model.go` — Dynamic model catalog with cache
- `executor.go` — OpenAI-compatible request translation
- `util.go` — HTTP helpers

## License

MIT
