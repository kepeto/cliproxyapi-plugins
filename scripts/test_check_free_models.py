#!/usr/bin/env python3
import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from types import SimpleNamespace
from unittest.mock import patch

import check_free_models as checker


class Handler(BaseHTTPRequestHandler):
    response = {"choices": [{"message": {"content": "ok"}}]}
    status = 200

    def do_POST(self):  # noqa: N802
        body = json.dumps(self.response).encode()
        self.send_response(self.status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


class CheckFreeModelsTests(unittest.TestCase):
    def test_filters_supported_free_providers(self):
        models, error = checker.models_from_response(
            {"data": [{"id": "opencode-free/a"}, {"id": "paid/b"}, {"id": "kilo-free/c"}]},
            {"opencode-free", "kilo-free"},
        )
        self.assertIsNone(error)
        self.assertEqual(models, ["kilo-free/c", "opencode-free/a"])

    def test_check_model_reports_provider_error_code_and_message(self):
        Handler.status = 400
        Handler.response = {"error": {"code": "model_unavailable", "message": "not available"}}
        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            result = checker.check_model(
                f"http://127.0.0.1:{server.server_port}", "test", "opencode-free/a", "hi", 1, 5
            )
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
        self.assertFalse(result.ok)
        self.assertEqual(result.code, "model_unavailable")
        self.assertEqual(result.message, "not available")
        self.assertEqual(result.http_status, 400)

    def test_check_model_accepts_choices(self):
        Handler.status = 200
        Handler.response = {"choices": [{"message": {"content": "ok"}}]}
        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            result = checker.check_model(
                f"http://127.0.0.1:{server.server_port}", "test", "kilo-free/a", "hi", 1, 5
            )
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
        self.assertTrue(result.ok)
        self.assertEqual(result.code, "ok")

    def test_classifies_health_failures(self):
        self.assertEqual(checker.classify_status(429), "rate_limited")
        self.assertEqual(checker.classify_status(503), "service_unavailable")
        self.assertEqual(checker.classify_status(500), "server_error")
        self.assertEqual(checker.classify_status(401), "auth_error")

    def test_catalog_diff_tracks_reappearance(self):
        diff = checker.catalog_diff(
            ["kilo-free/healthy", "kilo-free/recovering"],
            ["kilo-free/healthy"],
            {"kilo-free/healthy", "kilo-free/recovering"},
        )
        self.assertEqual(diff["removed"], ["kilo-free/recovering"])
        self.assertEqual(diff["reappeared"], [])
        diff = checker.catalog_diff(
            ["kilo-free/healthy"],
            ["kilo-free/healthy", "kilo-free/recovering"],
            {"kilo-free/healthy", "kilo-free/recovering"},
        )
        self.assertEqual(diff["reappeared"], ["kilo-free/recovering"])

    def test_check_model_rejects_invalid_choices(self):
        Handler.status = 200
        Handler.response = {"choices": [None]}
        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            result = checker.check_model(
                f"http://127.0.0.1:{server.server_port}", "test", "kilo-free/a", "hi", 1, 5
            )
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
        self.assertFalse(result.ok)
        self.assertEqual(result.category, "invalid_response")

    def test_watch_pass_reports_hidden_and_reappeared_models(self):
        args = SimpleNamespace(
            api_key="test",
            timeout=1,
            model=None,
            message="hi",
            max_tokens=1,
            delay=0,
        )
        snapshots = iter(
            [
                (["kilo-free/a", "kilo-free/b"], None, 200),
                (["kilo-free/a"], None, 200),
                (["kilo-free/a"], None, 200),
                (["kilo-free/a", "kilo-free/b"], None, 200),
            ]
        )

        def fake_fetch(*_args):
            return next(snapshots)

        def fake_check(_base, _key, model, _message, _max_tokens, _timeout):
            return checker.Result(model, "kilo-free", True, "ok", "ok", 200, "healthy")

        known = set()
        with patch.object(checker, "fetch_visible_models", side_effect=fake_fetch), patch.object(
            checker, "check_model", side_effect=fake_check
        ):
            first, known = checker.run_watch_pass(args, "http://test/v1", {"kilo-free"}, known)
            second, _ = checker.run_watch_pass(args, "http://test/v1", {"kilo-free"}, known)

        self.assertEqual(first["diff"]["removed"], ["kilo-free/b"])
        self.assertEqual(second["diff"]["reappeared"], ["kilo-free/b"])


if __name__ == "__main__":
    unittest.main()
