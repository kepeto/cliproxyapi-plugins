# Nous Portal Plugin

CLIProxyAPI plugin for [Nous Portal](https://portal.nousresearch.com) OAuth authentication and inference.

## What It Does

- Authenticates users via OAuth 2.0 device-code flow
- Provides access to all Nous Portal inference models
- Handles token refresh automatically
- Supports streaming chat completions

## Auth Flow

1. User clicks "Login" in CPA dashboard
2. Plugin opens device-code flow at `https://portal.nousresearch.com`
3. User completes authorization in browser
4. Plugin polls token endpoint and stores credentials
5. Credentials saved to `~/.cli-proxy-api/nous-portal.json`

## Build

```bash
cd plugins/nous-portal
CGO_ENABLED=1 go build -buildmode=c-shared -o nous-portal.so .
```

## Deploy

```bash
cp plugins/nous-portal/nous-portal.so ~/cliproxyapi/plugins/
systemctl --user restart cliproxyapi.service
```

## Configuration

```yaml
plugins:
  configs:
    nous-portal:
      enabled: true
      priority: 1
      portal_base_url: "https://portal.nousresearch.com"
      inference_base_url: "https://inference-api.nousresearch.com/v1"
      client_id: "hermes-cli"
      scope: "inference:invoke"
```

## Model IDs

Models use the prefix `nous-portal/`:
- `nous-portal/minimax/minimax-m2.5:free`
- `nous-portal/google/gemini-3.7-flash`
- `nous-portal/stepfun/step-3.7-flash:free`

## Files

- `plugin.go` — ABI entry point, method dispatch
- `auth.go` — OAuth device-code flow, token refresh
- `model.go` — Model catalog fetching and filtering
- `executor.go` — Inference request execution
- `nous.go` — Shared types and config

## License

MIT
