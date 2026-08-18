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
3. After authorization, credentials stored in `nous-portal-free.json`

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

```bash
cp plugins/nous-portal-free/nous-portal-free.so ~/cliproxyapi/plugins/
systemctl --user restart cliproxyapi.service
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
```

## Model IDs

Models use the prefix `nous-portal-free/`:
- `nous-portal-free/minimax/minimax-m2.5:free`
- `nous-portal-free/stepfun/step-3.7-flash:free`

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — OAuth device-code flow, token refresh
- `model.go` — Model catalog fetching with free-only filter
- `executor.go` — Inference request execution
- `nous.go` — Shared types and config

## License

MIT
