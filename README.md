# cliproxy-nous-portal

A **Nous Portal** provider plugin for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).
Implements the native plugin ABI (`c-shared` + JSON method protocol) so the host loads it as
`nous-portal.so` and routes OpenAI-compatible requests to the Nous Research Portal inference API.

## What it does

- **Auth provider** — OAuth 2.0 device-code login against `https://portal.nousresearch.com`
  (`/api/oauth/device/code`, `/api/oauth/token`), returning a refreshable access token that
  becomes the OpenAI-compatible Bearer key for the inference base URL.
- **Model provider** — serves a static fallback catalog (29 models, mirrors the upstream
  `FALLBACK_MODEL_IDS`) via `model.static`, and fetches the live catalog from
  `<inference_base_url>/models` via `model.for_auth`, persisting it back into the auth record.
- **Executor** — forwards OpenAI `chat/completions` (streaming + non-streaming) and raw HTTP
  requests to `<inference_base_url>`, translating the host's `chat-completions` payload directly.

The login flow and endpoints are ported from the reference implementation at
[kepeto/pi-nous-portal-provider](https://github.com/kepeto/pi-nous-portal-provider).

## Build

Requires CGO and a C toolchain (the plugin is a `c-shared` library):

```bash
CGO_ENABLED=1 go build -buildmode=c-shared -o nous-portal.so .
```

This produces `nous-portal.so` (and a `nous-portal.h` header you can ignore).

## Install into CLIProxyAPI

1. Enable plugins and point `plugins.dir` at a directory (default `plugins`).
2. Drop `nous-portal.so` into that directory.
3. Enable it under `plugins.configs.nous-portal.enabled: true`.

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    nous-portal:
      enabled: true
      priority: 1
      # optional overrides (defaults shown)
      portal_base_url: "https://portal.nousresearch.com"
      inference_base_url: "https://inference-api.nousresearch.com/v1"
      client_id: "hermes-cli"
      scope: "inference:invoke"
```

Restart CLIProxyAPI. The provider appears as `nous-portal`; trigger login through the
Management API (`/v0/management/...` OAuth flow) or by creating an auth file with
`"type":"nous-portal"`.

## Verify (dev harness)

`verify.c` dlopen()s the built `.so` and drives the exact host protocol, asserting the
register/identifier/static-model/auth-parse/executor contracts:

```bash
gcc -D'SO="/abs/path/nous-portal.so"' -O2 -o verify verify.c -ldl && ./verify
```

## Files

| File | Responsibility |
|------|----------------|
| `plugin.go` | C-ABI exports, method dispatch, envelope marshaling |
| `nous.go` | Shared schemas, config resolution, login-state store |
| `auth.go` | Device-code login.start/poll, auth.parse, auth.refresh |
| `model.go` | Static + per-auth model catalog |
| `executor.go` | chat/completions execute/stream + http_request + count_tokens |
| `util.go` | URL/form/base64 helpers |

## Notes / risks

- The inference base URL is taken from the OAuth `inference_base_url` token field, falling back
  to `https://inference-api.nousresearch.com/v1`.
- Auth records store the access/refresh token JSON under `type: nous-portal`; the host persists
  and refreshes them via the plugin's `auth.refresh` (refresh-token grant).
- `count_tokens` is a heuristic (~4 chars/token); Nous Portal exposes no token endpoint.
- The plugin declares `executor_model_scope: "oauth"` — it only serves models bound to an
  authenticated `nous-portal` auth record, not anonymous static models.
