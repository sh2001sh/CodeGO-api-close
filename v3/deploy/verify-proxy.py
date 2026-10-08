#!/usr/bin/env python3
"""Verify the deploy proxy against a disposable local HTTP fixture (Docker)."""

import gzip
import http.client
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Upstream(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    def do_GET(self):
        if self.path in ("/api/notifications/events", "/v1/stream"):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(b"data: first\n\n")
            self.wfile.flush()
            time.sleep(0.75)
            self.wfile.write(b"data: last\n\n")
            self.close_connection = True
            return
        payload = json.dumps({
            "xff": self.headers.get("X-Forwarded-For"),
            "real_ip": self.headers.get("X-Real-IP"),
            "connection": self.headers.get("Connection"),
            "client_port": self.client_address[1],
            "padding": "fixture" * 1000,
        }).encode()
        self.send_response(503 if self.path == "/api/fail" else 200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


def main():
    docker = shutil.which("docker")
    if not docker and os.name == "nt":
        docker = str(Path(os.environ["ProgramFiles"]) / "Docker/Docker/resources/bin/docker.exe")
    if not docker:
        raise RuntimeError("docker is required")
    run = lambda *args: subprocess.check_output([docker, *args], text=True).strip()
    name = "codego-proxy-check-" + uuid.uuid4().hex[:10]
    fixtures = [ThreadingHTTPServer(("0.0.0.0", 0), Upstream) for _ in range(2)]
    for fixture in fixtures:
        threading.Thread(target=fixture.serve_forever, daemon=True).start()
    try:
        with tempfile.TemporaryDirectory(prefix="codego-proxy-") as temp:
            config = Path(__file__).resolve().with_name("nginx.test.conf")
            text = config.read_text(encoding="utf-8")
            text = text.replace("gateway:3001", f"host.docker.internal:{fixtures[0].server_port}")
            text = text.replace("control:3000", f"host.docker.internal:{fixtures[1].server_port}")
            Path(temp, "default.conf").write_text(text, encoding="utf-8")
            Path(temp, "nginx.conf").write_text(
                "worker_processes 1; events { worker_connections 1024; } "
                "http { include /etc/nginx/mime.types; include /etc/nginx/conf.d/*.conf; }",
                encoding="utf-8",
            )
            host_options = [] if os.name == "nt" else ["--add-host", "host.docker.internal:host-gateway"]
            run("run", "-d", "--name", name, *host_options,
                "-p", "127.0.0.1::80", "--mount", f"type=bind,source={temp}/default.conf,target=/etc/nginx/conf.d/default.conf,readonly",
                "--mount", f"type=bind,source={temp}/nginx.conf,target=/etc/nginx/nginx.conf,readonly",
                "nginx:1.28.0-alpine")
            port = int(run("port", name, "80/tcp").rsplit(":", 1)[1])
            def get(path, headers=None):
                conn = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
                conn.request("GET", path, headers=headers or {})
                response = conn.getresponse()
                data = response.read()
                status, encoding = response.status, response.getheader("Content-Encoding")
                conn.close()
                if encoding == "gzip":
                    data = gzip.decompress(data)
                return status, encoding, json.loads(data)
            for _ in range(50):
                try:
                    first = get("/api/check")
                    break
                except (OSError, http.client.HTTPException):
                    time.sleep(0.1)
            else:
                raise AssertionError("disposable proxy did not become ready")
            second = get("/api/check", {"Accept-Encoding": "gzip", "X-Forwarded-For": "203.0.113.99", "X-Real-IP": "203.0.113.99"})
            assert second[0] == 200 and second[1] == "gzip", second[:2]
            assert second[2]["xff"] == second[2]["real_ip"] != "203.0.113.99", second[2]
            assert first[2]["client_port"] == second[2]["client_port"], "control upstream connection was not reused"
            assert second[2]["connection"] in (None, ""), "caller Connection header reached upstream"
            a, b = get("/v1/check"), get("/v1/check")
            assert a[2]["client_port"] == b[2]["client_port"], "gateway upstream connection was not reused"
            assert get("/api/fail")[0] == 503, "upstream failures must remain visible"
            for path in ("/api/notifications/events", "/v1/stream"):
                conn = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
                started = time.monotonic()
                conn.request("GET", path, headers={"Accept-Encoding": "gzip"})
                response = conn.getresponse()
                assert response.status == 200 and response.getheader("Content-Encoding") is None
                assert response.readline() == b"data: first\n"
                assert time.monotonic() - started < 0.65, "SSE first event was buffered"
                assert b"data: last" in response.read()
                conn.close()
            print("PASS: control/gateway keepalive, JSON gzip, forwarding spoof rejection, SSE immediate flush, upstream 503")
    finally:
        subprocess.run([docker, "rm", "-f", name], stdout=subprocess.DEVNULL, check=False)
        for fixture in fixtures:
            fixture.shutdown()
            fixture.server_close()


if __name__ == "__main__":
    main()
