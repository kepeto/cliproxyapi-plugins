# CLIProxyAPI Connectors

Native `c-shared` Go plugin modules for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).  
Not a fork of CPA — these are out-of-tree provider plugins loaded via CPA's plugin ABI.

## Plugins

| Plugin | Provider | Auth | Models |
|--------|----------|------|--------|
| `nous-portal` | Nous Portal | OAuth device-code | All upstream models |
| `nous-portal-free` | Nous Portal (free tier) | OAuth device-code | Free models only (`:free` suffix or name contains "free") |
| `opencode-free` | OpenCode Zen | None (`Bearer public`) | Dynamic free model list from upstream `/models` |
| `kilo-free` | KiloCode | None (keyless) | Dynamic free model catalog from upstream `/models` |

## Prerequisites

- Go 1.26+
- GCC / Clang (for `cgo` and `-buildmode=c-shared`)
- Linux amd64 (tested)
- CLIProxyAPI v7.2.128+

## Build

```bash
# Build all production plugins
make build

# Build one production plugin
make build PLUGIN=opencode-free

# Run formatting, unit tests, and static checks
make check

# Build all plugins and run the native ABI smoke check
make verify

# Or build individually
cd plugins/opencode-free && CGO_ENABLED=1 go build -buildmode=c-shared -o opencode-free.so .
```

Output: `<plugin>.so` in each plugin directory.

## Deploy

```bash
# Copy .so files to CPA's direct plugin directory
cp plugins/*/*.so ~/.cli-proxy-api/plugins/

# Or use the version-safe deploy target
make deploy
```

## Configure

Edit `~/.cli-proxy-api/config.yaml`:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    nous-portal:
      enabled: true
      priority: 1
      portal_base_url: "https://portal.nousresearch.com"
      inference_base_url: "https://inference-api.nousresearch.com/v1"
      client_id: "hermes-cli"
      scope: "inference:invoke"
    nous-portal-free:
      enabled: true
      priority: 1
      portal_base_url: "https://portal.nousresearch.com"
      inference_base_url: "https://inference-api.nousresearch.com/v1"
      client_id: "hermes-cli"
      scope: "inference:invoke"
    opencode-free:
      enabled: true
      priority: 1
      opencode_base_url: "https://opencode.ai/zen"
    kilo-free:
      enabled: true
      priority: 1
      kilo_chat_url: "https://api.kilo.ai/api/gateway/v1/chat/completions"
      kilo_models_url: "https://api.kilo.ai/api/gateway/models"
      # Legacy kilo_base_url is also accepted.
```

## Restart CPA

CPA loads plugin shared objects directly from its plugin directory and does not
recurse into architecture subdirectories. After deployment, restart CPA rather
than hot-swapping or removing a loaded Go plugin:

```bash
systemctl --user restart cli-proxy-api.service
```

## Verify

```bash
# Check logs
journalctl --user -u cli-proxy-api.service -f

# Verify plugin loaded
journalctl --user -u cli-proxy-api.service | grep "plugin registered"

# Test models endpoint
curl -s -H "Authorization: Bearer mipu" http://localhost:8317/v1/models | jq '.data[] | .id'

# Test inference
curl -s -X POST http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer mipu" \
  -H "Content-Type: application/json" \
  -d '{"model":"opencode-free/deepseek-v4-flash-free","messages":[{"role":"user","content":"2*2"}],"max_tokens":50}'
```

## Plugin ABI Notes

- All plugins use `schema_version: 2` in `plugin.register` response.
- Each plugin exports `cliproxyPluginCall`, `cliproxyPluginFree`, `cliproxyPluginShutdown`.
- Supported methods: `plugin.register`, `plugin.reconfigure`, `auth.identifier`, `auth.parse`, `auth.login.start`, `auth.login.poll`, `auth.refresh`, `model.static`, `model.for_auth`, `executor.identifier`, `executor.execute`, `executor.execute_stream`, `executor.count_tokens`, `executor.http_request`.

## Dynamic Model Refresh

- `opencode-free` and `kilo-free` fetch live model catalogs from upstream `/models` on a 3-hour interval.
- `nous-portal-free` refreshes free models from upstream catalog; falls back to a static list if upstream is unreachable.
- New upstream models are automatically discovered and exposed without plugin updates.
- Models that produce repeated model-specific inference failures are temporarily quarantined after three consecutive failures and retried after a cooldown. Authentication, rate-limit, caller-validation, and provider-wide errors do not quarantine individual models.

## License

MIT
