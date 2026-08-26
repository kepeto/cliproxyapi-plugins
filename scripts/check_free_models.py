#!/usr/bin/env python3
"""Check and observe free-provider model health through CLIProxyAPI.

The default mode performs one pass. ``--watch`` repeats passes, keeping models
that disappeared from the visible catalog in its candidate set so recovery can
be observed after the plugin's background probe succeeds. Catalog changes are
observational: the host may cache model projections and upstream catalog churn
can look like a health transition.

Examples:
  python3 scripts/check_free_models.py
  python3 scripts/check_free_models.py --provider opencode-free --delay 2
  python3 scripts/check_free_models.py --watch --interval 900
  python3 scripts/check_free_models.py --watch --iterations 2 --json
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import sys
import time
from dataclasses import dataclass
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

DEFAULT_BASE_URL = "http://127.0.0.1:8317/v1"
DEFAULT_PROVIDERS = ("opencode-free", "kilo-free", "nous-portal-free")
MESSAGE_LIMIT = 4096


@dataclass
class Result:
    model: str
    provider: str
    ok: bool
    code: str
    message: str
    http_status: int | None = None
    category: str | None = None

    def as_dict(self) -> dict[str, Any]:
        return {
            "model": self.model,
            "provider": self.provider,
            "ok": self.ok,
            "code": self.code,
            "message": self.message,
            "http_status": self.http_status,
            "category": self.category,
        }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base-url",
        default=os.environ.get("CLIPROXY_BASE_URL", DEFAULT_BASE_URL),
        help=f"CPA OpenAI-compatible base URL (default: {DEFAULT_BASE_URL})",
    )
    parser.add_argument(
        "--api-key",
        default=os.environ.get("CLIPROXY_API_KEY", "mipu"),
        help="CPA API key; defaults to CLIPROXY_API_KEY or mipu",
    )
    parser.add_argument(
        "--provider",
        action="append",
        choices=DEFAULT_PROVIDERS,
        help="Only test this provider; repeat for multiple providers",
    )
    parser.add_argument("--model", action="append", help="Only test this exact model ID")
    parser.add_argument(
        "--delay",
        type=float,
        default=1.0,
        help="Seconds between chat requests (default: 1.0)",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=45.0,
        help="Per-request timeout in seconds (default: 45)",
    )
    parser.add_argument("--max-tokens", type=int, default=16, help="Chat max_tokens (default: 16)")
    parser.add_argument(
        "--message",
        default="Reply with OK.",
        help="Minimal user message sent to every model",
    )
    parser.add_argument("--json", action="store_true", help="Emit JSON output")
    parser.add_argument("--quiet", action="store_true", help="Only print failures and final summary")
    parser.add_argument("--watch", action="store_true", help="Repeat checks and show visible catalog changes")
    parser.add_argument(
        "--interval",
        type=float,
        default=900.0,
        help="Seconds between --watch passes (default: 900)",
    )
    parser.add_argument(
        "--iterations",
        type=int,
        default=0,
        help="Number of --watch passes; 0 means continue until interrupted",
    )
    return parser.parse_args()


def normalize_base_url(value: str) -> str:
    return value.rstrip("/")


def safe_message(value: object) -> str:
    text = str(value).replace("\x00", " ").strip()
    for marker in ("Bearer ", "access_token=", "api_key="):
        while marker in text:
            start = text.find(marker) + len(marker)
            end = start
            while end < len(text) and text[end] not in " ,;\"'\\}]":
                end += 1
            text = text[:start] + "<redacted>" + text[end:]
    if len(text) > MESSAGE_LIMIT:
        return text[:MESSAGE_LIMIT] + "..."
    return text


def request_json(url: str, api_key: str, timeout: float, payload: dict[str, Any] | None = None) -> tuple[int, Any]:
    data = None if payload is None else json.dumps(payload).encode()
    headers = {"Authorization": f"Bearer {api_key}", "Accept": "application/json"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    request = Request(url, data=data, headers=headers, method="GET" if data is None else "POST")
    with urlopen(request, timeout=timeout) as response:
        raw = response.read()
        if not raw:
            return response.status, None
        return response.status, json.loads(raw)


def error_result(
    model: str,
    provider: str,
    code: str,
    message: str,
    status: int | None = None,
    category: str | None = None,
) -> Result:
    return Result(model, provider, False, code, safe_message(message), status, category)


def models_from_response(payload: Any, providers: set[str]) -> tuple[list[str], Result | None]:
    if not isinstance(payload, dict) or not isinstance(payload.get("data"), list):
        return [], error_result("/v1/models", "cpa", "invalid_models_response", "response does not contain a data array")
    models: list[str] = []
    for entry in payload["data"]:
        if not isinstance(entry, dict):
            continue
        model_id = entry.get("id")
        if not isinstance(model_id, str):
            continue
        provider = model_id.split("/", 1)[0]
        if provider in providers:
            models.append(model_id)
    return sorted(set(models)), None


def provider_for(model: str) -> str:
    return model.split("/", 1)[0]


def classify_status(status: int | None) -> str | None:
    if status == 401 or status == 403:
        return "auth_error"
    if status == 408:
        return "timeout"
    if status == 429:
        return "rate_limited"
    if status is not None and 500 <= status < 600:
        return "server_error" if status != 503 else "service_unavailable"
    if status is not None and 400 <= status < 500:
        return "request_error"
    return None


def classify_exception(exc: BaseException) -> str:
    if isinstance(exc, (TimeoutError, socket.timeout)):
        return "timeout"
    if isinstance(exc, URLError) and isinstance(exc.reason, (TimeoutError, socket.timeout)):
        return "timeout"
    return "network_error"


def check_model(base_url: str, api_key: str, model: str, message: str, max_tokens: int, timeout: float) -> Result:
    provider = provider_for(model)
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": message}],
        "max_tokens": max_tokens,
        "stream": False,
    }
    try:
        status, response = request_json(f"{base_url}/chat/completions", api_key, timeout, payload)
    except HTTPError as exc:
        raw = safe_message(exc.read(MESSAGE_LIMIT).decode("utf-8", "replace"))
        exc.close()
        try:
            body = json.loads(raw)
        except json.JSONDecodeError:
            body = None
        category = classify_status(exc.code)
        if isinstance(body, dict) and isinstance(body.get("error"), dict):
            error = body["error"]
            code = str(error.get("code") or error.get("type") or f"http_{exc.code}")
            detail = str(error.get("message") or raw or exc.reason)
        else:
            code = f"http_{exc.code}"
            detail = raw or str(exc.reason)
        return error_result(model, provider, code, detail, exc.code, category)
    except (URLError, TimeoutError, OSError) as exc:
        return error_result(model, provider, classify_exception(exc), str(exc), category=classify_exception(exc))
    except json.JSONDecodeError as exc:
        return error_result(model, provider, "invalid_json_response", str(exc), status if "status" in locals() else None, "invalid_json")

    if not isinstance(response, dict):
        return error_result(model, provider, "invalid_response", "response is not a JSON object", status, "invalid_response")
    if response.get("error") is not None:
        if isinstance(response.get("error"), dict):
            error = response["error"]
            code = str(error.get("code") or error.get("type") or "provider_error")
            detail = str(error.get("message") or "provider returned an error")
        else:
            code = "provider_error"
            detail = "provider returned a non-object error"
        return error_result(model, provider, code, detail, status, classify_status(status) or "provider_error")
    choices = response.get("choices")
    if not isinstance(choices, list) or not choices:
        return error_result(model, provider, "empty_choices", "response contains no choices", status, "empty_choices")
    if any(not isinstance(choice, dict) for choice in choices):
        return error_result(model, provider, "invalid_choices", "response contains an invalid choice", status, "invalid_response")
    return Result(model, provider, True, "ok", "model responded", status, "healthy")


def fetch_visible_models(
    base_url: str, api_key: str, timeout: float, providers: set[str]
) -> tuple[list[str], Result | None, int | None]:
    try:
        status, payload = request_json(f"{base_url}/models", api_key, timeout)
    except HTTPError as exc:
        detail = safe_message(exc.reason)
        exc.close()
        return [], error_result("/v1/models", "cpa", f"http_{exc.code}", detail, exc.code), exc.code
    except (URLError, TimeoutError, OSError, json.JSONDecodeError) as exc:
        return [], error_result("/v1/models", "cpa", "models_request_failed", str(exc), category=classify_exception(exc)), None
    models, listing_error = models_from_response(payload, providers)
    if listing_error is not None:
        listing_error.http_status = status
    return models, listing_error, status


def emit(result: Result, as_json: bool, quiet: bool) -> None:
    if quiet and result.ok:
        return
    if as_json:
        print(json.dumps(result.as_dict(), ensure_ascii=False))
        return
    status = f" HTTP {result.http_status}" if result.http_status is not None else ""
    state = "OK" if result.ok else "FAIL"
    category = f" class={result.category}" if result.category else ""
    print(f"[{state}] {result.model} code={result.code}{category}{status}: {result.message}")


def catalog_diff(before: list[str], after: list[str], known: set[str]) -> dict[str, list[str]]:
    before_set = set(before)
    after_set = set(after)
    hidden_before = known - before_set
    return {
        "added": sorted(after_set - before_set),
        "removed": sorted(before_set - after_set),
        "reappeared": sorted(after_set & hidden_before),
    }


def run_watch_pass(
    args: argparse.Namespace,
    base_url: str,
    providers: set[str],
    known: set[str],
) -> tuple[dict[str, Any], set[str]]:
    before, listing_error, _ = fetch_visible_models(base_url, args.api_key, args.timeout, providers)
    if listing_error is not None:
        return {
            "visible_before": before,
            "visible_after": before,
            "checks": [],
            "diff": {"added": [], "removed": [], "reappeared": []},
            "listing_error": listing_error.as_dict(),
            "failures": 1,
        }, known

    known_before = set(known)
    candidates = sorted(known_before | set(before))
    if args.model:
        wanted = set(args.model)
        candidates = [model for model in candidates if model in wanted]
    results: list[Result] = []
    for index, model in enumerate(candidates):
        result = check_model(base_url, args.api_key, model, args.message, args.max_tokens, args.timeout)
        results.append(result)
        if index + 1 < len(candidates):
            time.sleep(args.delay)

    after, after_error, _ = fetch_visible_models(base_url, args.api_key, args.timeout, providers)
    if after_error is not None:
        after = before
    next_known = known_before | set(before) | set(after)
    return {
        "visible_before": before,
        "visible_after": after,
        "checks": [result.as_dict() for result in results],
        "diff": catalog_diff(before, after, known_before),
        "listing_error": after_error.as_dict() if after_error is not None else None,
        "failures": sum(not result.ok for result in results) + (1 if after_error is not None else 0),
    }, next_known


def emit_watch(report: dict[str, Any], as_json: bool, pass_number: int) -> None:
    payload = {"event": "pass", "pass": pass_number, **report}
    if as_json:
        print(json.dumps(payload, ensure_ascii=False))
        return
    print(f"=== health pass {pass_number} ===")
    print(f"VISIBLE BEFORE ({len(report['visible_before'])}):")
    for model in report["visible_before"]:
        print(f"  {model}")
    for raw in report["checks"]:
        emit(Result(**raw), False, False)
    if report["listing_error"] is not None:
        emit(Result(**report["listing_error"]), False, False)
    print(f"VISIBLE AFTER ({len(report['visible_after'])}):")
    for model in report["visible_after"]:
        print(f"  {model}")
    diff = report["diff"]
    print(f"DIFF added={diff['added']} removed={diff['removed']} reappeared={diff['reappeared']}")
    print(json.dumps({"ok": report["failures"] == 0, "models": len(report["checks"]), "failures": report["failures"]}))


def run_once(args: argparse.Namespace, base_url: str, providers: set[str]) -> int:
	visible_before, listing_error, _ = fetch_visible_models(base_url, args.api_key, args.timeout, providers)
	if listing_error is not None:
		emit(listing_error, args.json, False)
		return 2
	models = visible_before
	if args.model:
		wanted = set(args.model)
		models = [model for model in models if model in wanted]

	if not models:
		print(json.dumps({"ok": False, "code": "no_free_models", "message": "no matching free models found"}))
		return 1

	if not args.json:
		print(f"VISIBLE BEFORE ({len(visible_before)}):")
		for model in visible_before:
			print(f"  {model}")

	failures = 0
	for index, model in enumerate(models):
		result = check_model(base_url, args.api_key, model, args.message, args.max_tokens, args.timeout)
		emit(result, args.json, args.quiet)
		failures += not result.ok
		if index + 1 < len(models):
			time.sleep(args.delay)

	visible_after, after_error, _ = fetch_visible_models(base_url, args.api_key, args.timeout, providers)
	diff = catalog_diff(visible_before, visible_after, set())
	if args.json:
		summary = {
			"ok": failures == 0 and after_error is None,
			"models": len(models),
			"failures": failures,
			"visible_before": visible_before,
			"visible_after": visible_after,
			"hidden": diff["removed"],
			"reappeared": diff["reappeared"],
		}
		if after_error is not None:
			summary["catalog_after_error"] = after_error.as_dict()
		print(json.dumps(summary, ensure_ascii=False))
	elif after_error is not None:
		emit(after_error, False, False)
	else:
		print(f"VISIBLE AFTER ({len(visible_after)}):")
		for model in visible_after:
			print(f"  {model}")
		print(f"HIDDEN AFTER: {diff['removed']}")

	return 2 if after_error is not None else (1 if failures else 0)


def main() -> int:
    args = parse_args()
    if args.delay < 0 or args.timeout <= 0 or args.max_tokens < 1:
        print("invalid --delay, --timeout, or --max-tokens", file=sys.stderr)
        return 2
    if args.watch and (args.interval <= 0 or args.iterations < 0):
        print("--watch requires --interval > 0 and --iterations >= 0", file=sys.stderr)
        return 2

    base_url = normalize_base_url(args.base_url)
    providers = set(args.provider or DEFAULT_PROVIDERS)
    if not args.watch:
        return run_once(args, base_url, providers)

    known: set[str] = set()
    pass_number = 0
    exit_code = 0
    try:
        while args.iterations == 0 or pass_number < args.iterations:
            pass_number += 1
            report, known = run_watch_pass(args, base_url, providers, known)
            emit_watch(report, args.json, pass_number)
            if report["failures"]:
                exit_code = 1
            if args.iterations == 0 or pass_number < args.iterations:
                time.sleep(args.interval)
    except KeyboardInterrupt:
        if not args.json:
            print("watch stopped")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
