# Post-mortem: SIGSEGV crashes during plugin hot-swaps (2026-08-21/22)

## Symptom

Four SIGSEGV crashes of `cli-proxy-api` within ~10 hours, coinciding with
intensive plugin version churn (dev builds + store auto-updates).

## Evidence

All four coredumps have the faulting thread INSIDE the embedded Go runtime
symbols of whichever plugin `.so` generation was being replaced at that minute:

| Crash (PID) | Backtrace frames | Swap event that minute |
|---|---|---|
| 20:12:06 (64352) | `sigaction`/`setsig`/`sigfwdgo` @ kilo-free-v0.1.20.so | 20:12:48 host unloaded all v0.1.20 files |
| 05:12:13 (69116) | host + n/a (mixed) | oauth-model-alias PUT churn + reloads |
| 05:18:57 (190285) | `morestack`/`newobject` @ opencode-free-v0.1.21.so | 05:14:00 host unloaded all v0.1.21 files |
| 06:08:39 (217692) | `goparkunlock`/`reflectcall` @ opencode-free-v0.1.22.so | 06:06:18 store watchdog "old plugin file removed" |

## Root cause

Go c-shared plugins each embed a full Go runtime (scheduler, GC, signal
handlers). CPA core hot-replaces plugin files with `dlclose` while the old
generation still has live state:

- our plugins start background refresher goroutines from `init()` on load,
- two Go runtimes chain signal handlers (`sigfwdgo`),
- parked goroutines and stacks live in the mapped `.so`.

Unloading/remapping the file under any of that is use-after-free → SEGV in
runtime frames. This is inherent to the host's plugin hot-swap design and
cannot be fixed plugin-side (core owns the dlclose policy).

## Exposure

Proportional to swap frequency. The incident window had abnormal churn:
manual dev deploys colliding with store auto-update. Steady-state releases
(one swap per release) carry far less exposure, and a service restart after
an update guarantees a clean process.

## Guardrails in place

- `make deploy`: single writer flow — tag-derived version, matching filename,
  prune superseded `.so`, sync `store.version/release-tag` in config.yaml.
- `make verify-deploy`: fails loudly on any embedded/filename version drift.
- Rule of thumb: never hand-copy `.so`; after a store update, prefer one
  explicit `systemctl --user restart cli-proxy-api` over letting the hot-swap
  linger.
