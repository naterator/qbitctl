"""Check missing setup and both legacy and modern qBittorrent login responses."""

import json
import os
import subprocess
import sys
import tempfile
import threading
from http.cookies import SimpleCookie
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import parse_qs


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def reply(self, status, body=b"", cookie=None):
        self.send_response(status)
        if cookie:
            self.send_header("Set-Cookie", cookie + "=smoke-session; Path=/; HttpOnly")
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        form = parse_qs(self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode())
        if self.path != "/api/v2/auth/login" or form != {"username": ["nate"], "password": ["a&b+c= d"]}:
            self.reply(401)
            return
        body = b"Ok." if self.server.login_status == 200 else b""
        self.reply(self.server.login_status, body, self.server.cookie_name)

    def do_GET(self):
        cookies = SimpleCookie(self.headers.get("Cookie", ""))
        cookie = cookies.get(self.server.cookie_name)
        if not cookie or cookie.value != "smoke-session":
            self.reply(403)
            return
        responses = {
            "/api/v2/app/version": b"v5.3.0",
            "/api/v2/app/webapiVersion": b"2.15.0",
            "/api/v2/transfer/info": b'{"connection_status":"connected"}',
            "/api/v2/torrents/info": b'[{"hash":"0123456789abcdef0123456789abcdef01234567","name":"Smoke torrent"}]',
        }
        self.reply(200, responses[self.path])


def main():
    binary = str(Path(sys.argv[1]).resolve())
    with tempfile.TemporaryDirectory(prefix="qbitctl-smoke-") as temp, HTTPServer(("127.0.0.1", 0), Handler) as server:
        env = dict(os.environ, XDG_CONFIG_HOME=str(Path(temp) / "config"))
        env.pop("QBITCTL_PASSWORD", None)

        def run(*args, input_text=None):
            return subprocess.run([binary, *args], input=input_text, cwd=temp, env=env, check=True, capture_output=True, text=True, timeout=15).stdout

        version = run("version").strip()
        if env.get("RELEASE_TAG") and version != "qbitctl " + env["RELEASE_TAG"]:
            raise RuntimeError(f"binary version mismatch: {version!r}")
        if "update" not in run("--help") or "--dry-run" not in run("update", "--help"):
            raise RuntimeError("update command missing from binary help")

        bare_add = subprocess.run(
            [binary, "add"], cwd=temp, env=dict(env, NO_COLOR="1"), capture_output=True, text=True, timeout=15
        )
        if bare_add.returncode != 4 or bare_add.stderr.splitlines() != [
            "[ERROR] accepts 1 arg(s), received 0",
            "Run qbitctl profile setup to configure a server.",
        ]:
            raise RuntimeError(f"bare add did not report its argument error and setup hint: {bare_add.stderr!r}")

        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            for status, cookie in ((200, "SID"), (204, "QBT_SID_8080")):
                server.login_status, server.cookie_name = status, cookie
                flags = ("--url", f"http://127.0.0.1:{server.server_port}", "--user", "nate")
                if "connected" not in run(*flags, "--pass", "a&b+c= d", "info"):
                    raise RuntimeError(f"server info failed with login HTTP {status}")
                torrents = json.loads(run(*flags, "--pass-stdin", "list", "--server-json", input_text="a&b+c= d\n"))
                if torrents[0]["name"] != "Smoke torrent":
                    raise RuntimeError(f"torrent list failed with login HTTP {status}")
        finally:
            server.shutdown()
            thread.join()
    print("native CLI smoke passed (bare add, 200/SID and 204/QBT_SID login, info, list)")


if __name__ == "__main__":
    main()
