"""A fake OpenAI-compatible endpoint for the layer 2 tests (#37). A helper, not a test file."""

from __future__ import annotations

import json
import re
import threading
import time
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


@contextmanager
def FakeEndpoint(modes=None, default="answer", verdict="NOT-EN"):  # noqa: N802
    """Serve `/v1/chat/completions`; `.url` ends in `/v1`, `.requests` holds the JSON bodies.

    Mode per model: `answer` (`verdict` for each numbered item), `silent` (never replies),
    `drip` (headers, then one byte per 0.5 s), `http500`, `garbage` (a reply that is no JSON).
    """
    modes = modes or {}
    requests: list[dict] = []
    headers: list[dict] = []
    done = threading.Event()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):  # noqa: N802
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            requests.append(body)
            headers.append(dict(self.headers))
            mode = modes.get(body["model"], default)
            if mode == "silent":
                done.wait(30)
            elif mode == "http500":
                self.send_response(500)
                self.send_header("Content-Length", "0")
                self.end_headers()
            elif mode == "garbage":
                self._send(b"<html>not json</html>")
            elif mode == "drip":
                self.send_response(200)
                self.send_header("Content-Length", "1000")
                self.end_headers()
                try:
                    for _ in range(60):
                        self.wfile.write(b" ")
                        self.wfile.flush()
                        if done.wait(0.5):
                            break
                except OSError:
                    pass
            else:
                prompt = body["messages"][0]["content"]
                count = len(re.findall(r"^\d+\. ", prompt, re.M))
                answer = {str(i + 1): verdict for i in range(count)}
                reply = {"choices": [{"message": {"content": json.dumps(answer)}}]}
                self._send(json.dumps(reply).encode())

        def _send(self, data: bytes):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    threading.Thread(target=server.serve_forever, args=(0.05,), daemon=True).start()

    class Endpoint:
        url = f"http://127.0.0.1:{server.server_address[1]}/v1"

    Endpoint.requests = requests
    Endpoint.headers = headers
    try:
        yield Endpoint
    finally:
        done.set()
        server.shutdown()
        server.server_close()
