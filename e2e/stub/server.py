"""Deterministic, no-egress OpenAI Responses API stub for E2E."""

import json
import os
import ssl
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def scenario_for(payload):
    serialized = json.dumps(payload, sort_keys=True).lower()
    for scenario in ("429", "500", "malformed", "timeout"):
        if f"fixture-{scenario}" in serialized:
            return scenario
    return "success"


class Handler(BaseHTTPRequestHandler):
    def _raw(self, status, content_type, encoded):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def _json(self, status, body):
        self._raw(status, "application/json", json.dumps(body).encode())

    def do_GET(self):
        if self.path == "/healthz":
            return self._json(200, {"status": "ok"})
        if self.path == "/requests":
            return self._json(200, {"requests": self.server.requests})
        return self._json(404, {"error": "not found"})

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        try:
            data = json.loads(body or b"{}")
        except json.JSONDecodeError:
            return self._json(400, {"error": {"message": "malformed request JSON"}})

        scenario = scenario_for(data)
        self.server.requests.append({"path": self.path, "scenario": scenario, "body": data})
        if self.path != "/v1/responses":
            return self._json(404, {"error": {"message": "not found"}})
        if scenario == "429":
            return self._json(429, {"error": {"message": "fixture rate limit", "type": "rate_limit_error", "code": "rate_limit_exceeded"}})
        if scenario == "500":
            return self._json(500, {"error": {"message": "fixture upstream failure", "type": "server_error", "code": "fixture_500"}})
        if scenario == "timeout":
            time.sleep(self.server.timeout_delay_seconds)
        if scenario == "malformed":
            return self._raw(200, "application/json", b'{"id":"broken",')

        content = "deterministic-e2e-response"
        return self._json(200, {
            "id": "resp_e2e_fixed",
            "object": "response",
            "status": "completed",
            "model": "e2e-model",
            "output": [{
                "type": "message",
                "role": "assistant",
                "content": [{"type": "output_text", "text": content}],
            }],
            "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
        })

    def log_message(self, *_):
        pass


def serve(port, requests, tls=False):
    server = ThreadingHTTPServer(("0.0.0.0", port), Handler)
    server.requests = requests
    server.timeout_delay_seconds = float(os.environ.get("STUB_TIMEOUT_DELAY_SECONDS", "5"))
    if tls:
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain("certs/provider-stub-cert.pem", "certs/provider-stub-key.pem")
        server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


def main():
    requests = []
    threading.Thread(target=serve, args=(8080, requests), daemon=True).start()
    serve(8443, requests, tls=True)


if __name__ == "__main__":
    main()
