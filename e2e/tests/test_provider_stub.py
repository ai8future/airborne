#!/usr/bin/env python3
import json
import socket
import sys
import threading
import time
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "stub"))
import server as provider_stub  # noqa: E402


class ProviderStubTest(unittest.TestCase):
    def setUp(self):
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), provider_stub.Handler)
        self.server.requests = []
        self.server.timeout_delay_seconds = 0.25
        self.server.daemon_threads = True
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = f"http://127.0.0.1:{self.server.server_port}/v1/responses"

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)

    def request(self, prompt, timeout=2):
        body = json.dumps({"input": prompt}).encode()
        request = urllib.request.Request(self.url, body, {"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            return error.code, error.read()

    def test_success_is_openai_responses_compatible(self):
        status, body = self.request("hello")
        self.assertEqual(status, 200)
        response = json.loads(body)
        self.assertEqual(response["output"][0]["content"][0]["text"], "deterministic-e2e-response")
        self.assertEqual(self.server.requests[0]["scenario"], "success")

    def test_error_scenarios_are_deterministic(self):
        expected = {"fixture-429": 429, "fixture-500": 500}
        for prompt, want_status in expected.items():
            with self.subTest(prompt=prompt):
                status, body = self.request(prompt)
                self.assertEqual(status, want_status)
                self.assertIn("error", json.loads(body))

        status, body = self.request("fixture-malformed")
        self.assertEqual(status, 200)
        with self.assertRaises(json.JSONDecodeError):
            json.loads(body)

    def test_timeout_scenario_exceeds_the_test_client_budget(self):
        started = time.monotonic()
        with self.assertRaises((TimeoutError, socket.timeout)):
            self.request("fixture-timeout", timeout=0.05)
        self.assertLess(time.monotonic() - started, 0.2)
        self.assertEqual(self.server.requests[-1]["scenario"], "timeout")


if __name__ == "__main__":
    unittest.main()
