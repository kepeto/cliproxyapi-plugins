# CLIProxyAPI Plugins

Monorepo untuk plugin-plugin CLIProxyAPI.

## Struktur

```
plugins/
  └── nous-portal/
      ├── go.mod
      ├── plugin.go
      ├── auth.go
      ├── model.go
      ├── executor.go
      ├── nous.go
      └── util.go
```

Setiap plugin adalah module Go independen di bawah `plugins/<nama-plugin>/`.

## Build

```bash
make build
```

## Verify

```bash
make verify
```

## Multi-arch release artifacts

```bash
make dist
```

Artifacts akan tersedia di direktori `dist/`:
- `nous-portal-linux-amd64.so`
- `nous-portal-linux-arm64.so`
- `nous-portal-linux-arm.so`

## GitHub Actions

Workflow `.github/workflows/build.yml` otomatis build untuk `linux/amd64`, `linux/arm64`, dan `linux/arm` saat tag `v*` di-push.
