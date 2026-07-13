"""Deterministic, no-egress OpenAI-compatible provider stub for E2E."""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def _json(self, status, body):
        encoded = json.dumps(body).encode()
        self.send_response(status); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded))); self.end_headers(); self.wfile.write(encoded)
    def do_GET(self):
        if self.path == "/healthz": return self._json(200, {"status": "ok"})
        if self.path == "/requests": return self._json(200, {"requests": self.server.requests})
        self._json(404, {"error": "not found"})
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        try: data = json.loads(body or b"{}")
        except json.JSONDecodeError: return self._json(400, {"error": "malformed JSON"})
        self.server.requests.append({"path": self.path, "body": data})
        if self.path != "/v1/chat/completions": return self._json(404, {"error": "not found"})
        content = "deterministic-e2e-response"
        return self._json(200, {"id":"e2e-fixed-id","object":"chat.completion","created":0,"model":"e2e-model","choices":[{"index":0,"message":{"role":"assistant","content":content},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}})
    def log_message(self, *_): pass

server = HTTPServer(("0.0.0.0", 8080), Handler)
server.requests = []
server.serve_forever()
