# CLIProxyAPI Connectors

Native `c-shared` Go plugin modules for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).  
Not a fork of CPA — these are out-of-tree provider plugins loaded via CPA's plugin ABI.

## Plugins

| Plugin | Provider | Auth | Models |
|--------|----------|------|--------|
| `nous-portal` | Nous Portal | OAuth device-code | All upstream models |
| `nous-portal-free` | Nous Portal (free tier) | OAuth device-code | Free models only (`:free` suffix or name contains "free") |
| `opencode-free` | OpenCode Zen | None (keyless) | Static free model list |
| `kilo-free` | KiloCode | None (keyless) | Dynamic model catalog from upstream `/models` |

## Prerequisites

- Go 1.26+
- GCC / Clang (for `cgo` and `-buildmode=c-shared`)
- Linux amd64 (tested)
- CLIProxyAPI v7.2.128+

## Build

```bash
# Build all plugins
make build

# Or build individually
cd plugins/nous-portal && CGO_ENABLED=1 go build -buildmode=c-shared -o nous-portal.so .
```

Output: `<plugin>.so` in each plugin directory.

## Deploy

```bash
# Copy .so files to CPA plugins dir
cp plugins/*/*.so ~/cliproxyapi/plugins/

# Or use the deploy target
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
      kilo_base_url: "https://api.kilo.ai/api/gateway"
```

## Restart CPA

```bash
systemctl --user restart cliproxyapi.service
```

## Verify

```bash
# Check logs
journalctl --user -u cliproxyapi.service -f

# Verify plugin loaded
journalctl --user -u cliproxyapi.service | grep "plugin registered"

# Test models endpoint
curl -s -H "Authorization: Bearer mipu" http://localhost:8317/v1/models | jq '.data[] | .id'

# Test inference
curl -s -X POST http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer mipu" \
  -H "Content-Type: application/json" \
  -d '{"model":"nous-portal-free/minimax/minimax-m2.5:free","messages":[{"role":"user","content":"2*2"}]}'
```

## Plugin ABI Notes

- All plugins use `schema_version: 2` in `plugin.register` response.
- Each plugin exports `cliproxyPluginCall`, `cliproxyPluginFree`, `cliproxyPluginShutdown`.
- Supported methods: `plugin.register`, `plugin.reconfigure`, `auth.identifier`, `auth.parse`, `auth.login.start`, `auth.login.poll`, `auth.refresh`, `model.static`, `model.for_auth`, `executor.identifier`, `executor.execute`, `executor.execute_stream`, `executor.count_tokens`, `executor.http_request`.

## License

MIT
