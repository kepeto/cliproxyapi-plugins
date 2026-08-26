# Hermes Agent × Nous Portal Free Models — Reverse-Engineering Report

> Historical snapshot from 2026-08-19. The repository implementation has since
> moved to OAuth-authenticated Nous Free access, live catalog refresh, and
> per-account catalog caching; do not use this report's implementation
> recommendations as the current plugin contract.

**Date:** 2026-08-19  
**Analyst:** Automated reverse-engineering via local source inspection  
**Hermes installation:** `/home/mipu/.hermes/hermes-agent/`  
**Repository:** https://github.com/nousresearch/hermes-agent

---

## 1. Executive Summary

Hermes Agent **does not** access Nous Portal free models via special headers, hidden API keys, or undocumented auth schemes. The mechanism is:

1. **OAuth device-code flow** → stored refresh token in `~/.hermes/auth.json`
2. **Short-lived JWT minting** on each inference call from the refresh token
3. **Standard OpenAI SDK** client pointed at `https://inference-api.nousresearch.com/v1`
4. **Provider profile** injects product-attribution tags (`product=hermes-agent`, `client=hermes-client-vX.Y.Z`) into `extra_body.tags`
5. **Free model discovery** via Nous Portal's `/api/nous/recommended-models` endpoint — free models have `tokenPrice: "$0.00/1M"` and `source: "local"`

**Critical finding:** `tencent/hy3:free` and `stepfun/step-3.7-flash:free` **are** valid Nous Portal free models (confirmed in the `/api/nous/recommended-models` response). The 402/x402 error from our `nous-portal-free` plugin is caused by **missing authentication** (JWT), not by the model name itself.

---

## 2. Authentication Mechanism

### 2.1 OAuth Device Code Flow

Hermes authenticates to Nous Portal via OAuth 2.0 device code flow:

```python
# hermes_cli/auth.py
_AUTH_JSON_PATH = get_hermes_home() / "auth.json"
```

The `auth.json` stores:
```json
{
  "active_provider": "nous",
  "providers": {
    "nous": {
      "access_token": "<short-lived JWT>",
      "refresh_token": "<long-lived refresh token>",
      "expires_at": 1234567890,
      "client_id": "...",
      "portal_base_url": "https://portal.nousresearch.com",
      "inference_base_url": "https://inference-api.nousresearch.com/v1"
    }
  }
}
```

### 2.2 JWT Minting

On each inference call, the credential pool mints a fresh JWT:

```python
# agent/credential_pool.py
def runtime_api_key(self) -> str:
    if self.provider == "nous":
        # Nous stores the runtime inference credential in agent_key for
        # compatibility. It must be a NAS invoke JWT.
        for token, expires_at in (
            (self.agent_key, self.agent_key_expires_at),
            (self.access_token, self.access_token_expires_at),
        ):
            if token and (expires_at is None or expires_at > time.time()):
                return token
    return self.api_key
```

The JWT is passed as standard `Authorization: Bearer <jwt>` header.

### 2.3 No Special Headers for Free Models

There is **no** `X-Nous-Free`, `X-Portal-Tier`, or similar header. Free vs paid model access is determined by:
1. The model's pricing metadata (`tokenPrice: "$0.00/1M"`)
2. The account's subscription entitlement (checked server-side by Nous Portal)

---

## 3. Request Construction

### 3.1 OpenAI Client

```python
# agent/auxiliary_client.py
def _create_openai_client(api_key: str, base_url: str, **kwargs):
    kwargs.setdefault("max_retries", 0)
    return OpenAI(api_key=api_key, base_url=base_url, **kwargs)
```

### 3.2 Default Headers

```python
# agent/auxiliary_client.py
_AI_GATEWAY_HEADERS = {
    "HTTP-Referer": "https://hermes-agent.nousresearch.com",
    "X-Title": "Hermes Agent",
    "User-Agent": f"HermesAgent/{_HERMES_VERSION}",
}
```

These are **attribution headers** for Nous Portal's analytics dashboard — they identify the caller as Hermes Agent. They are NOT authentication headers.

### 3.3 Extra Body (Tags)

The Nous Portal provider profile injects product tags:

```python
# plugins/model-providers/nous/__init__.py
def build_extra_body(self, *, session_id: str | None = None, **context):
    body = {"tags": nous_portal_tags(session_id=session_id)}
    # ...
    return body
```

The tags look like:
```json
{
  "tags": [
    "product=hermes-agent",
    "client=hermes-client-v0.13.0",
    "conversation=<session-id>"
  ]
}
```

These are sent as `extra_body` in the OpenAI SDK call, which maps to additional JSON fields in the request body.

---

## 4. Free Model Discovery

### 4.1 Recommended Models Endpoint

```python
# hermes_cli/models.py
def union_with_portal_free_recommendations(
    curated_ids: list[str],
    pricing: dict[str, dict[str, str]],
    portal_base_url: str = "",
    *,
    force_refresh: bool = False,
) -> tuple[list[str], dict[str, dict[str, str]]]:
    """Augment curated list + pricing with the Portal's freeRecommendedModels."""
    payload = fetch_nous_recommended_models(portal_base_url, force_refresh=force_refresh)
    free_block = payload.get("freeRecommendedModels")
    # ...
```

### 4.2 Free Model Identification

Models are identified as free by:
1. Presence in `/api/nous/recommended-models` → `freeRecommendedModels` array
2. `tokenPrice: "$0.00/1M"` in the response
3. `source: "local"` (Nous Portal-hosted, not OpenRouter)

**Confirmed free models include:**
- `tencent/hy3:free`
- `stepfun/step-3.7-flash:free`
- `upstage/solar-pro4:free`
- `meituan/longcat-2.0:free`

### 4.3 `:free` Suffix Clarification

Hermes explicitly distinguishes:
- **OpenRouter `:free`** — models with `:free` suffix accessed via OpenRouter API
- **Nous Portal `:free`** — Nous Portal's own free tier models, accessed via Nous Portal API with OAuth

From `conversation_loop.py`:
```python
# ``:free`` is OpenRouter slug syntax; Nous Portal will reject
# the model name even after a successful re-auth.
if isinstance(_model, str) and _model.endswith(":free"):
    agent._vprint(f"⚠️  Note: `{_model}` looks like an OpenRouter slug (`:free` suffix).", force=True)
    agent._vprint(f"   Nous Portal won't recognize that model name. Either switch to a", force=True)
    agent._vprint(f"   Nous catalog model, or run `/model openrouter:{_model}` to use OpenRouter.", force=True)
```

**However**, this warning is misleading for Nous Portal's own free models. The Portal's `/api/nous/recommended-models` endpoint returns models with `:free` suffix as legitimate free models (`source: "local"`). The warning fires because Hermes cannot distinguish at runtime whether a `:free` model is an OpenRouter model or a Nous Portal free model.

---

## 5. Why `nous-portal-free` Plugin Fails

### 5.1 Root Cause: Missing Authentication

The `nous-portal-free` plugin is configured as **keyless** (no API key). Our implementation removes the `Authorization: Bearer` header entirely:

```go
// nous-portal-free/executor.go
// Keyless — no Bearer header sent
```

However, **Nous Portal requires authentication even for free models**. The 400/402 error is caused by the missing JWT, not by the model name or any missing header.

### 5.2 Evidence

1. **Portal free models require auth:** The `/api/nous/recommended-models` endpoint is public (returns free model catalog), but `/v1/chat/completions` requires a valid JWT.
2. **x402 micropayment:** Some models require Solana micropayment (`x402Version: 1`), but free models (`tokenPrice: "$0.00/1M"`) should not.
3. **400 Bad Request:** The error message "This request is not valid" suggests the request is rejected before payment processing — likely due to missing/invalid auth.

---

## 6. What Hermes Does Differently

| Aspect | Hermes (nous-portal) | Our Plugin (nous-portal-free) |
|--------|---------------------|-------------------------------|
| Auth | OAuth device code → JWT | None (keyless) |
| Headers | `HTTP-Referer`, `X-Title`, `User-Agent` | `HTTP-Referer`, `X-Title`, `User-Agent` |
| Extra Body | `tags` array | `tags` array |
| Model Discovery | `/api/nous/recommended-models` | Static catalog |
| Free Model Access | JWT + subscription | None |

Hermes **cannot** access Nous Portal free models without authentication. The "free" refers to $0 cost within a paid Nous Portal subscription, not "no auth required."

---

## 7. Recommendations

### 7.1 For `nous-portal-free` Plugin

**Option A: Accept that Nous Portal requires auth**
- The plugin name "free" is misleading. Nous Portal's free models are part of a paid subscription ($0 marginal cost, but requires OAuth).
- Implement OAuth device code flow (complex, requires browser interaction).

**Option B: Switch to OpenRouter for `:free` models**
- OpenRouter actually has keyless free models (no auth required for some).
- Route `tencent/hy3:free` → `openrouter/tencent/hy3:free` with OpenRouter API key or keyless tier.
- This aligns with Hermes' own warning: "run `/model openrouter:{_model}` to use OpenRouter."

**Option C: Drop `nous-portal-free` entirely**
- Use `nous-portal` (paid/OAuth) for Nous Portal models.
- Use `opencode-free`/`kilo-free` for keyless free models.
- Remove misleading "free" plugin.

### 7.2 For CPA Plugin Ecosystem

1. **Prefix-based routing is correct** — `nous-portal-free/tencent/hy3:free` → strip prefix → `tencent/hy3:free` sent to Nous Portal.
2. **Model catalog should be dynamic** — fetch from `/api/nous/recommended-models` instead of static list.
3. **Free model detection** — check `tokenPrice: "$0.00/1M"` and `source: "local"`.

---

## 8. Code References

| File | Lines | Purpose |
|------|-------|---------|
| `agent/auxiliary_client.py` | 1141-1164 | AI gateway headers + Nous extra body |
| `agent/portal_tags.py` | 1-120 | Nous Portal product tags |
| `plugins/model-providers/nous/__init__.py` | 1-80 | Nous provider profile |
| `hermes_cli/models.py` | 740-815 | `union_with_portal_free_recommendations` |
| `agent/conversation_loop.py` | 5970-5998 | `:free` model warning (misleading) |
| `agent/credential_pool.py` | 272-297 | Nous JWT runtime resolution |

---

## 9. Conclusion

**Hermes does NOT use special headers for free Nous Portal models.** The "free" access is subscription-based (Nous Portal paid plan with $0 marginal cost), authenticated via OAuth JWT. Our `nous-portal-free` plugin fails because it omits authentication entirely.

The `tencent/hy3:free` model IS a valid Nous Portal free model (confirmed via `/api/nous/recommended-models`), but accessing it requires a valid OAuth JWT — exactly the same as paid models. The "free" distinction only affects billing, not authentication.

**Recommended action:** Either implement OAuth in `nous-portal-free` (making it a full Nous Portal client), or repurpose it as an OpenRouter `:free` model proxy (which actually supports keyless access).
