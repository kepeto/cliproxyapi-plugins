# Nous Portal Free Plugin

CLIProxyAPI plugin for Nous Portal free-tier models only.

## What It Does

- Authenticates via OAuth 2.0 device-code flow (same as `nous-portal`)
- Filters upstream model catalog to show **only free models**
- Free models identified by `:free` suffix or "free" in model name
- Shares auth infrastructure with `nous-portal` but independent config

## Auth Flow

Same as `nous-portal`:
1. User clicks "Login" in CPA dashboard
2. Device-code flow opens at Nous Portal
3. After authorization, credentials are stored as `nous-portal-free.json`,
   `nous-portal-free-2.json`, etc.; refresh preserves the account, file, and
   cached free catalog.
## Model Filtering

Only models matching these criteria are shown:
- Model ID ends with `:free`
- OR model name contains "free" (case-insensitive)

Example free models:
- `stepfun/step-3.7-flash:free`
- `poolside/laguna-s-2.1:free`
- `meituan/longcat-2.0:free`

## Build

```bash
cd plugins/nous-portal-free
CGO_ENABLED=1 go build -buildmode=c-shared -o nous-portal-free.so .
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
    nous-portal-free:
      enabled: true
      priority: 1
      portal_base_url: "https://portal.nousresearch.com"
      inference_base_url: "https://inference-api.nousresearch.com/v1"
      client_id: "hermes-cli"
      scope: "inference:invoke"
      model_aliases:
        free-model: "minimax/minimax-m2.5:free"
```

Config values are bare upstream IDs. The client-facing alias is prefixed, for
example `nous-portal-free/free-model`; its target must be present in the live
free catalog. Configuration aliases override host aliases and apply after
`plugin.reconfigure` without restart.

## Model IDs

Models use the prefix `nous-portal-free/`:
- `nous-portal-free/minimax/minimax-m2.5:free`
- `nous-portal-free/stepfun/step-3.7-flash:free`

## Runtime Guarantees

Health probes run every 15 minutes for known authenticated models. A failed
probe or limit/server/timeout/invalid response hides that model immediately; a
successful later probe restores it. Normal model-specific 4xx failures use the
three-failure threshold. SSE is buffered with 100,000-chunk, 100 MiB total, and
1 MiB line limits.
Expired access tokens remain parseable so CPA can invoke `auth.refresh`; refresh
requests are serialized per account. A new login is required only when the
refresh token is rejected or revoked.

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — OAuth device-code flow, token refresh
- `model.go` — Model catalog fetching with free-only filter
- `executor.go` — Inference request execution
- `nous.go` — Shared types and config

## License

MIT
