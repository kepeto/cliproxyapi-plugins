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
- Linux amd64 is the locally tested host; release artifacts also cover Linux
  arm64/arm, macOS amd64/arm64, and Windows amd64.
- CLIProxyAPI host using plugin ABI v1.

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

Use the version-safe deployment target. It normalizes the embedded release
version, installs versioned files directly into CPA's plugin directory, updates
the configured store version, and verifies the installed artifacts.

```bash
make deploy
systemctl --user restart cli-proxy-api.service
```

Do not hand-copy unversioned `.so` files or hot-swap/remove a loaded Go plugin.

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
      model_aliases:
        all-models: "google/gemini-3-flash-preview"
    nous-portal-free:
      enabled: true
      priority: 1
      portal_base_url: "https://portal.nousresearch.com"
      inference_base_url: "https://inference-api.nousresearch.com/v1"
      client_id: "hermes-cli"
      scope: "inference:invoke"
      model_aliases:
        free-model: "minimax/minimax-m2.5:free"
    opencode-free:
      enabled: true
      priority: 1
      opencode_base_url: "https://opencode.ai/zen"
      model_aliases:
        fast-free: "deepseek-v4-flash-free"
    kilo-free:
      enabled: true
      priority: 1
      kilo_chat_url: "https://api.kilo.ai/api/gateway/v1/chat/completions"
      kilo_models_url: "https://api.kilo.ai/api/gateway/models"
      model_aliases:
        default-free: "kilo-auto/free" # Example; target must exist in the live catalog.
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

# Test inference with an ID returned by /v1/models (example only)
curl -s -X POST http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer mipu" \
  -H "Content-Type: application/json" \
  -d '{"model":"opencode-free/deepseek-v4-flash-free","messages":[{"role":"user","content":"2*2"}],"max_tokens":50}'
```

## Plugin ABI Notes

- All plugins use `schema_version: 2` in `plugin.register` response.
- Each plugin exports `cliproxyPluginCall`, `cliproxyPluginFree`, `cliproxyPluginShutdown`.
- Supported methods: `plugin.register`, `plugin.reconfigure`, `auth.identifier`, `auth.parse`, `auth.login.start`, `auth.login.poll`, `auth.refresh`, `model.static`, `model.for_auth`, `executor.identifier`, `executor.execute`, `executor.execute_stream`, `executor.count_tokens`, `executor.http_request`.

- Runtime `plugin.register` uses `schema_version: 2`; release `registry.json`
  uses store schema version 1 and is generated only from complete release
  artifacts. Runtime `ConfigFields` do not belong in the registry snapshot.

## Dynamic Model Refresh

- `opencode-free` and `kilo-free` refresh live catalogs every 3 hours and retry
  sooner while the catalog is empty.
- `nous-portal-free` refreshes an authenticated/free catalog and falls back to
  a vetted free-only list; authenticated catalog data is retained per account.
- `nous-portal` exposes the upstream catalog and has a static fallback.
- New upstream models are discovered without rebuilding the plugin.
- `model_aliases` are applied on `plugin.reconfigure`; configuration entries
  override host aliases. OpenCode/Kilo require a live-catalog target, Nous full
  forwards the configured upstream ID, and Nous Free requires a cached,
  allowlisted, or `:free` target.
- Repeated model-specific failures quarantine a model after three failures for
  15 minutes. Authentication, rate-limit, transport/provider-wide, and caller
  errors do not quarantine individual models.
- SSE responses are buffered; each stream accepts at most 100,000 chunks, 100 MiB
  of payload, and 1 MiB per scanner line. `data: ` is stripped and blank/comment
  lines are skipped; empty, oversized, or read-failed streams return errors.

## License

MIT
