# Repository Engineering Guide

This repository contains out-of-tree CLIProxyAPI provider plugins implemented as Go `c-shared` modules. Treat this file as the repository-local engineering contract for contributors and coding agents.

## Scope and repository layout

- `plugins/nous-portal`, `plugins/nous-portal-free`, `plugins/opencode-free`, and `plugins/kilo-free` are production plugins.
- `shared` is a reusable Go module used by the production plugins.
- `plugins/playground` is experimental/development tooling and is not part of the production `Makefile` build. Its local CLIProxyAPI replacement path is machine-specific; do not assume it is reproducible in a clean checkout until that dependency is made portable.
- There is no root `go.mod` or `go.work`. Go commands must be run per module; a root-level `go test ./...` is not a valid repository check.
- Build outputs (`.so`, generated headers, verifier binaries, coverage files) are generated artifacts and must not be committed.
- `plugins/playground` is excluded from production quality gates until its CLIProxyAPI dependency is reproducible; do not add machine-specific paths to make it pass locally.

## Before changing code

1. Read the relevant plugin README and nearby implementation/tests before introducing new abstractions.
2. Preserve the CLIProxyAPI plugin ABI and exported entry points. Changes to registration payloads, method names, schema versions, or lifecycle behavior require explicit tests and documentation updates.
3. Check `git status` before and after work. Do not overwrite unrelated user changes.
4. Keep provider-specific behavior inside its plugin where possible; put code in `shared` only when it is genuinely reusable by multiple plugins.

## Formatting, tests, and static checks

For every changed production module, run the checks from that module directory. The repository-wide equivalent is `make check`:

```bash
gofmt -w $(find . -name '*.go' -not -path './vendor/*')
go test ./...
go vet ./...
```

Do not format unrelated modules merely for convenience. Before submitting a change that affects shared code, run the checks for `shared` and all four production plugin modules because their `replace` directives resolve `shared` from the checkout:

```bash
for dir in shared plugins/nous-portal plugins/nous-portal-free plugins/opencode-free plugins/kilo-free; do
  (cd "$dir" && go test ./... && go vet ./...)
done
```

Use the repository build and ABI smoke check when changing plugin entry points, cgo boundaries, registration, lifecycle, or build configuration:

```bash
make verify
```

If a check cannot be run, state the exact command and reason in the handoff; do not claim it passed. Network-dependent tests must use `httptest` or another deterministic mock where practical and must not require real provider credentials.

## Unit-test expectations

- Every behavior change should include or update focused unit tests.
- Test registration/configuration, model prefixing/filtering, auth parsing and error paths, HTTP status handling, streaming/envelope conversion, refresh concurrency, and lifecycle changes as applicable.
- Prefer table-driven tests, deterministic clocks/data, and `httptest.Server` for upstream interactions.
- Avoid tests that call live provider APIs, depend on local credentials, or rely on timing sleeps.
- For c-shared/ABI changes, add a smoke-level verification in addition to pure unit tests; unit tests alone do not prove that the host can load and call the shared object.
- Run race detection for concurrency-sensitive changes when the toolchain and cgo target permit it:

```bash
go test -race ./...
```

## Build, release, and deployment safety

- Use `make build` for the four production plugins, `make build PLUGIN=<name>` for one plugin, and `make verify` for the native verifier. Keep `CGO_ENABLED=1` for c-shared builds.
- CPA loads `.so` files directly from `~/.cli-proxy-api/plugins` (or the configured `PLUGIN_DIR`) and does not recurse into architecture subdirectories.
- Do not hand-rename or hand-copy versioned shared objects. Use the repository deployment flow and verify the embedded version with `make verify-deploy`.
- Do not hot-swap or remove a loaded Go plugin while CLIProxyAPI is running. Restart the host service after deployment; see `docs/postmortem-plugin-hotswap-segv.md`.
- Treat release artifacts, registry metadata, checksums, and version strings as one consistency boundary. If supported platforms change, update the release workflow, registry generation, README, and tests together.
- Do not put credentials, access tokens, private URLs, or local machine paths into committed source, tests, logs, or documentation.
- Review release-workflow changes carefully: release automation publishes binaries and updates `registry.json`.

## Documentation and compatibility

- Keep the root README, plugin READMEs, registry metadata, and operational documentation consistent with actual behavior.
- Document configuration defaults, model catalog refresh behavior, supported platforms, and deployment paths when they change.
- Preserve backward compatibility for existing plugin configuration unless a deliberate migration is documented.
- Record externally observable behavior and failure modes rather than implementation trivia.

## Change review checklist

Before handing off a change, confirm:

- [ ] Scope is limited to the requested behavior.
- [ ] Relevant Go files are formatted.
- [ ] Focused unit tests cover the change and negative/error paths.
- [ ] Per-module `go test ./...` and `go vet ./...` pass, or exceptions are reported.
- [ ] `make verify` was run for ABI/build/lifecycle changes.
- [ ] No secrets, generated binaries, or machine-specific paths were added.
- [ ] Documentation and registry/release metadata were updated when applicable.
- [ ] `git diff` and `git status` contain only intentional changes.
