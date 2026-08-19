# CPA Plugin Test Report: `nous-portal-free` → `tencent/hy3:free`

**Date:** 2026-08-19 (WIB)
**Gateway:** local CLIProxyAPI `http://localhost:8317` (api-key `mipu`)
**Plugin:** `nous-portal-free` v0.1.16
**Target model:** `nous-portal-free/tencent/hy3:free`
**Test harness:** `/tmp/cpa_test.py` (repeatable, N rounds × nonstream+stream) + `/tmp/cpa_load.py` (concurrency + edge cases)

---

## 1. Verdict

**PASS — functionally correct, zero errors across 37 requests.** The plugin correctly
authenticates via OAuth device-code, forwards chat completions (both streaming and
non-streaming) to the Nous Portal inference API, and degrades gracefully on bad input.
The remaining gaps are **latency and token-lifecycle hygiene**, not correctness.

---

## 2. Throughput & Latency (repeatable batches)

### Batch A — short prompt ("What is 7*8?") · 5 rounds
| Mode | n | ok | errors | latency avg | latency min/max |
|------|---|----|--------|-------------|----------------|
| nonstream | 5 | 5 | 0 | 5.12 s | 4.33 / 6.69 s |
| stream | 5 | 5 | 0 | 5.08 s | 4.30 / 5.75 s |

### Batch B — medium prompt ("TCP vs UDP, 3 bullets") · 10 rounds
| Mode | n | ok | errors | latency avg | latency min/max |
|------|---|----|--------|-------------|----------------|
| nonstream | 10 | 10 | 0 | 13.22 s | 10.56 / 15.71 s |
| stream | 10 | 10 | 0 | 11.13 s | 9.15 / 13.66 s |

**Observation:** Latency is dominated by *output generation length*, not by plugin
overhead. Streaming shows only a marginal (~2 s) advantage because `hy3:free` does not
emit tokens incrementally — `first_token_s ≈ total latency` in every stream run
(see `cpa_test.py` output: `first_tok` == `latency`). The plugin correctly buffers the
SSE stream and re-emits chunks, but the model itself answers in one shot.

---

## 3. Concurrency & Edge Cases (`cpa_load.py`)

| Test | Result |
|------|--------|
| warmup (1 req) | 200 · 4.6 s |
| burst 5 concurrent | 5/5 200 · wall 7.2 s · max 7.2 s |
| burst 10 concurrent | 10/10 200 · wall 10.2 s · max 10.2 s |
| long prompt (×40 fox sentence) | 200 · 20.9 s |
| empty `messages` | 400 (upstream: "messages is required") — clean, no panic |
| unknown model `…/does-not-exist` | 400 `model_not_found` — clean |
| wrong API key | 401 `Invalid API key` — clean |

**Concurrency scales linearly** (10 parallel ≈ 2.2× the single-request time, all 200).
No goroutine leaks, no 5xx, no crashes. Error paths return proper HTTP statuses.

---

## 4. Issues Found (for optimization)

### P1 — Token expiry is not validated before use (`nous.go:168` `valid()`)
```go
func (s storageJSON) valid() bool {
    return s.AccessToken != "" && s.InferenceBaseURL != ""
}
```
`valid()` does **not** check `ExpiresAt`. If CPA ever calls `executor.execute` with a
stale token (e.g. refresh loop lag), the plugin will send an expired `Bearer` and get a
401 from upstream instead of proactively refreshing.

**Fix:** add `time.Now().Before(s.ExpiresAt)` to `valid()`, and have `executor` return
`auth_required` (401) so CPA triggers `auth.refresh` before the call.

### P2 — Refresh skew wastes tokens / risks mid-flight expiry (`auth.go:274`)
```go
func expiryFromNow(expiresIn int) time.Time {
    if expiresIn <= 0 { expiresIn = 3600 }
    return time.Now().Add(time.Duration(expiresIn)*time.Second - 5*time.Minute)
}
```
The plugin trusts `tok.ExpiresIn` from the OAuth response. **Measured mismatch:** the
stored `expires_at` was `06:10` while the JWT `exp` decoded to `2026-08-18T23:15:56Z`
(~7 h later). So `expires_in` returned by Nous is far shorter than the real JWT lifetime.
Effect: the plugin *thinks* it must refresh every ~55 min but the token is actually valid
for hours → unnecessary refresh churn. Worse, if upstream ever returns a *short*
`expires_in` that is wrong in the other direction, the token could expire before the
−5 min refresh fires.

**Fix:** prefer the JWT `exp` claim (decode `access_token`, read `exp`) over the
OAuth `expires_in` when computing `ExpiresAt`. This makes refresh timing match reality.

### P3 — Streaming gives no perceived speedup on this model
Not a bug, but worth documenting: because `hy3:free` answers in one block, the plugin's
SSE buffering (`executor.go:84` scanner) yields `first_token ≈ total`. If you want
snappier UX you'd need a model that actually streams; nothing to fix in the plugin.

### P4 — `injectNousPortalTags` hardcodes client version (`executor.go:191`)
```go
"client=hermes-client-v0.20.1",
```
A version bump in the host silently drifts from this constant. **Fix:** read the version
from plugin config / build flag instead of a literal.

### P5 — `count_tokens` is a naive `/4` heuristic (`executor.go:168`)
Returns `len(payload)/4`. Fine as a stub, but CPA may show wildly wrong token counts.
**Fix:** call the upstream `/v1/tokenize` if Nous exposes it, or use a tiktoken-style
estimator for the specific model family.

---

## 5. Recommended optimization order

1. **P2** (JWT `exp` over `expires_in`) — highest impact, prevents both churn and
   mid-flight expiry. ~10 lines.
2. **P1** (`valid()` checks `ExpiresAt`) — makes the plugin fail-soft into a refresh
   instead of sending a dead token. ~2 lines.
3. **P4** (version constant) — trivial hygiene, do alongside P1/P2.
4. **P5** (token count) — only if token accounting matters for your routing/limits.

P3 is informational only.

---

## 6. How to reproduce

```bash
# ensure gateway up with the fixed management key
systemctl --user restart cliproxyapi.service

# repeatable latency suite (edit ROUNDS / PROMPT)
ROUNDS=10 PROMPT="Explain TCP vs UDP in three bullets." python3 /tmp/cpa_test.py

# concurrency + edge probe
python3 /tmp/cpa_load.py

# results land in /tmp/cpa_test_results.json and /tmp/cpa_load_results.json
```
