# KiloCode Free Plugin

CLIProxyAPI plugin for [KiloCode](https://kilo.ai) free-tier models.

## What It Does

- Provides access to KiloCode free models through keyless chat requests.
- The chat endpoint requires no Authorization header; the catalog endpoint uses
  `Bearer kilo-free`.
- Dynamic model catalog fetched from upstream `/models` endpoint
- Refreshes the model catalog every 3 hours, with short retries while empty
- Supports streaming chat completions

## Auth Flow

No OAuth needed. The plugin creates a dummy local auth profile; chat requests
are sent without Authorization, while catalog requests use `Bearer kilo-free`.

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

From the repository root:

```bash
make deploy
systemctl --user restart cli-proxy-api.service
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
      model_aliases:
        default-free: "kilo-auto/free" # Example; target must be present in live /models.
```

Aliases are client-visible names; their targets must be present in the live
catalog before execution. Chat requests remain keyless; the catalog endpoint
uses `Bearer kilo-free`.
Config values are bare upstream IDs. The client-facing alias is prefixed, for
example `kilo-free/default-free`; the target must exist in the live catalog.
Alias changes apply after `plugin.reconfigure` without restart.

## Upstream Endpoints

- Chat: `https://api.kilo.ai/api/gateway/v1/chat/completions`
- Models: `https://api.kilo.ai/api/gateway/models`

## Runtime Guarantees

Model-specific failures quarantine after 3 failures for 15 minutes, with
exponential backoff up to 1 hour; auth, rate-limit, provider-wide, 5xx, and
caller errors do not quarantine models. SSE is buffered with 100,000-chunk,
100 MiB total, and 1 MiB line limits; empty/read/limit failures are errors.

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — Dummy auth (no login needed)
- `model.go` — Dynamic model catalog with cache
- `executor.go` — OpenAI-compatible request translation
- `util.go` — HTTP helpers

## License

MIT
