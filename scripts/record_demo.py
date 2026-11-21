#!/usr/bin/env python3
r"""Record a qbitctl TUI walkthrough as an asciicast v2 file.

The recorder runs qbitctl against a loopback mock with isolated config and
cache directories.

Usage:
    python3 scripts/record_demo.py /path/to/qbitctl assets/demo.cast
    agg --theme github-dark --font-size 16 --line-height 1.32 --speed 0.8 \
        --fps-cap 20 --last-frame-duration 0.5 assets/demo.cast assets/demo.gif
"""

import codecs
import fcntl
import json
import os
import pty
import re
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from types import SimpleNamespace
from urllib.parse import parse_qs, urlsplit


WIDTH = 112
HEIGHT = 32
GIB = 1 << 30
ALPHA = "a" * 40
BRAVO = "b" * 40
KEYS = {
    "enter": b"\r",
    "escape": b"\x1b",
    "tab": b"\t",
    "down": b"\x1b[B",
}


def torrent(name, digest, state, progress, size, category, tags):
    return {
        "hash": digest,
        "name": name,
        "state": state,
        "size": size,
        "total_size": size,
        "progress": progress,
        "dlspeed": 2_450_000 if progress < 1 else 0,
        "upspeed": 180_000 if progress < 1 else 0,
        "category": category,
        "tags": tags,
        "content_path": "/downloads/" + name.lower().replace(" ", "-") + ".iso",
        "ratio": 0.34,
        "eta": 840 if progress < 1 else 8640000,
        "added_on": 1730000000,
        "completion_on": 0 if progress < 1 else 1730000400,
        "amount_left": int(size * (1 - progress)),
        "downloaded": int(size * progress),
        "uploaded": int(size * 0.1),
        "save_path": "/downloads/",
        "seq_dl": False,
        "super_seeding": False,
        "auto_tmm": True,
        "ratio_limit": -1,
        "seeding_time": -1,
        "seeding_time_limit": -1,
        "dl_limit": -1,
        "up_limit": -1,
        "private": False,
        "tracker": "https://tracker.example/announce",
        "trackers_count": 1,
    }


class Fixture(BaseHTTPRequestHandler):
    """The small subset of qBittorrent's Web API used by the TUI."""

    server_version = "qbitctl-demo/1"

    def log_message(self, *_):
        return

    @property
    def fixture(self):
        return self.server.fixture

    def reply(self, status=200, body=b"", headers=None, content_type="application/json"):
        if not isinstance(body, bytes):
            body = json.dumps(body).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        for name, value in (headers or {}).items():
            self.send_header(name, value)
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def protected(self):
        if self.headers.get("Cookie") == "QBT_SID_8080=demo":
            return True
        self.reply(403)
        return False

    def do_GET(self):
        parsed = urlsplit(self.path)
        query = parse_qs(parsed.query)
        if not self.protected():
            return
        if parsed.path == "/api/v2/app/version":
            self.reply(body=b"v5.3.0", content_type="text/plain")
        elif parsed.path == "/api/v2/app/webapiVersion":
            self.reply(body=b"2.16.2", content_type="text/plain")
        elif parsed.path == "/api/v2/transfer/info":
            self.reply(body={
                "connection_status": "connected",
                "dht_nodes": 61,
                "dl_info_speed": 2450000,
                "up_info_speed": 180000,
                "dl_info_data": 32400000000,
                "up_info_data": 1400000000,
            })
        elif parsed.path == "/api/v2/torrents/info":
            wanted = query.get("hashes", [""])[0]
            selected = self.fixture.torrents
            if wanted:
                selected = [item for item in selected if item["hash"] in wanted.split("|")]
            self.reply(body=selected)
        elif parsed.path == "/api/v2/torrents/categories":
            self.reply(body={
                "ISO": {"name": "ISO", "savePath": "/downloads"},
                "Linux": {"name": "Linux", "savePath": "/downloads/linux"},
            })
        elif parsed.path == "/api/v2/torrents/trackers":
            self.reply(body=[{"url": "https://tracker.example/announce", "status": 2, "tier": 0}])
        elif parsed.path == "/api/v2/search/status":
            job_id = int(query["id"][0])
            self.reply(body=[{"id": job_id, "status": "Stopped"}])
        elif parsed.path == "/api/v2/search/results":
            self.reply(body={"results": self.fixture.search_results})
        else:
            self.reply(404)

    def do_POST(self):
        path = urlsplit(self.path).path
        length = int(self.headers.get("Content-Length", "0"))
        form = parse_qs(self.rfile.read(length).decode("utf-8"), keep_blank_values=True)
        if path == "/api/v2/auth/login":
            self.reply(204, headers={"Set-Cookie": "QBT_SID_8080=demo; Path=/"})
            return
        if not self.protected():
            return
        if path == "/api/v2/search/start":
            self.fixture.searches += 1
            self.fixture.last_job = self.fixture.searches
            self.reply(body={"id": self.fixture.last_job})
        elif path == "/api/v2/search/delete":
            self.fixture.deleted_jobs.append(int(form.get("id", [0])[0]))
            self.reply(204)
        elif path == "/api/v2/search/downloadTorrent":
            # Keep the fixture queue stable so the recording stays focused on
            # the search workflow. The TUI still shows its submitted notice.
            self.fixture.downloads.append(form)
            self.reply(204)
        elif path == "/api/v2/auth/logout":
            self.fixture.logouts += 1
            self.reply(204)
        else:
            self.reply(404)


class Recording:
    def __init__(self, binary: Path, base: Path, port: int):
        self.events = []
        self.output = bytearray()
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.started = time.monotonic()
        self.timestamp = int(time.time())
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", HEIGHT, WIDTH, 0, 0))
        env = os.environ.copy()
        for key in (
            "QBITCTL_MASTER_KEY",
            "XDG_CONFIG_HOME",
            "XDG_CACHE_HOME",
            "NO_COLOR",
            "PROMPT_COMMAND",
            "EDITOR",
            "VISUAL",
        ):
            env.pop(key, None)
        config = base / "config"
        cache = base / "cache"
        config.mkdir()
        cache.mkdir()
        env.update({
            "XDG_CONFIG_HOME": str(config),
            "XDG_CACHE_HOME": str(cache),
            "TERM": "xterm-256color",
            "COLORTERM": "truecolor",
            "TERM_PROGRAM": "qbitctl-demo",
            "HISTFILE": os.devnull,
        })
        command = [
            str(binary),
            "--url", f"http://127.0.0.1:{port}",
            "--user", "demo",
            "--pass", "demo",
            "tui",
        ]

        self.proc = subprocess.Popen(
            command,
            cwd=base,
            env=env,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            start_new_session=True,
        )
        os.close(slave)

    def read(self, seconds=0.04):
        if select.select([self.master], [], [], seconds)[0]:
            try:
                chunk = os.read(self.master, 65536)
            except OSError:
                return
            if chunk:
                self.output.extend(chunk)
                text = self.decoder.decode(chunk)
                if text:
                    self.events.append([round(time.monotonic() - self.started, 4), "o", text])

    def hold(self, seconds):
        until = time.monotonic() + seconds
        while time.monotonic() < until:
            self.read(min(0.04, max(0, until - time.monotonic())))

    def send(self, value):
        if isinstance(value, str):
            value = value.encode("utf-8")
        self.events.append([round(time.monotonic() - self.started, 4), "i", value.decode("utf-8")])
        os.write(self.master, value)

    def wait(self, marker, start=0):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            self.read()
            text = re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]", b"", bytes(self.output[start:]))
            if marker.encode("utf-8") in text:
                self.hold(0.2)
                return
        raise RuntimeError(f"Missing {marker!r}: {self.output[-1200:]!r}")

    def key(self, key, expected=None):
        if isinstance(key, str):
            key = KEYS.get(key, key.encode("utf-8"))
        elif key.decode("utf-8", "ignore") in KEYS:
            key = KEYS[key.decode("utf-8", "ignore")]
        start = len(self.output)
        self.send(key)
        if expected:
            self.wait(expected, start)
        else:
            self.hold(0.3)

    def type_text(self, value, delay=0.055):
        for character in value:
            self.send(character)
            self.hold(delay)

    def paste_text(self, value):
        # Bubble Tea treats bracketed paste as one text edit. This keeps the
        # size editor from briefly rendering validation errors for partial
        # values such as "1 G" while the walkthrough is being recorded.
        self.send(b"\x1b[200~" + value.encode("utf-8") + b"\x1b[201~")
        self.hold(0.35)

    def close(self):
        if self.proc.poll() is None:
            self.send(b"q")
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.terminate()
                self.proc.wait(timeout=5)
        os.close(self.master)

    def save(self, path):
        header = {
            "version": 2,
            "width": WIDTH,
            "height": HEIGHT,
            "timestamp": self.timestamp,
            "title": "qbitctl: TUI search walkthrough",
            "env": {"TERM": "xterm-256color", "SHELL": "/bin/sh"},
        }
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            "\n".join(json.dumps(row, ensure_ascii=False) for row in [header, *self.events]) + "\n",
            encoding="utf-8",
        )


def main():
    if len(sys.argv) != 3:
        raise SystemExit(f"usage: {Path(sys.argv[0]).name} /path/to/qbitctl assets/demo.cast")
    binary = Path(sys.argv[1]).resolve(strict=True)
    output = Path(sys.argv[2])
    fixture = SimpleNamespace()
    fixture.torrents = [
        torrent("Ubuntu 24.04 LTS", ALPHA, "downloading", 0.42, 4 * GIB, "ISO", "linux release"),
        torrent("Debian 12 Bookworm", BRAVO, "stoppedUP", 1.0, 2 * GIB, "ISO", "linux stable"),
    ]
    fixture.search_results = [
        {
            "fileName": "Ubuntu 24.04 Server amd64",
            "fileSize": 2 * GIB,
            "nbSeeders": 19,
            "nbLeechers": 5,
            "fileUrl": "https://jackett.example/download/ubuntu-server?token=demo",
            "siteUrl": "https://jackett.example",
            "descrLink": "https://indexer.example/ubuntu-server",
            "engineName": "jackett",
        },
        {
            "fileName": "Ubuntu 24.04.3 Desktop arm64",
            "fileSize": 6 * GIB,
            "nbSeeders": 62,
            "nbLeechers": 7,
            "fileUrl": "https://jackett.example/download/ubuntu-arm?token=demo",
            "siteUrl": "https://jackett.example",
            "descrLink": "https://indexer.example/ubuntu-arm",
            "engineName": "jackett",
        },
        {
            "fileName": "Ubuntu 24.04.3 Desktop amd64",
            "fileSize": 4 * GIB,
            "nbSeeders": 84,
            "nbLeechers": 12,
            "fileUrl": "https://jackett.example/download/ubuntu-amd64?token=demo",
            "siteUrl": "https://jackett.example",
            "descrLink": "https://indexer.example/ubuntu",
            "engineName": "jackett",
        },
    ]
    fixture.searches = 0
    fixture.last_job = 0
    fixture.deleted_jobs = []
    fixture.downloads = []
    fixture.logouts = 0
    with tempfile.TemporaryDirectory(prefix="qbitctl-demo-") as temp:
        base = Path(temp)
        server = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
        server.fixture = fixture
        worker = Thread(target=server.serve_forever, daemon=True)
        worker.start()
        recording = None
        try:
            recording = Recording(binary, base, server.server_address[1])
            recording.wait("Ubuntu 24.04 LTS")
            recording.hold(1.6)

            recording.key(b"?", "Keyboard shortcuts")
            recording.hold(1.4)
            recording.key(b"escape")
            recording.key(b"\x10", "Command palette")
            recording.hold(1.3)
            recording.type_text("sort queue")
            recording.hold(0.8)
            recording.key(b"escape")

            # Open Config and preview a color scheme without saving it.
            recording.key(b"4", "Color scheme")
            recording.key(b"c", "Color scheme")
            recording.key(b"down")
            recording.hold(1.0)
            recording.key(b"escape")
            recording.key(b"1")

            # Inspect the selected transfer.
            recording.key(b"enter", "Full Path")
            recording.hold(0.8)
            recording.key(b"escape")
            recording.hold(0.5)

            # Search Jackett for Ubuntu, sort locally, then filter the loaded
            # rows without issuing another search.
            recording.key(b"2", "Search results")
            recording.key(b"/", "Search Jackett")
            recording.type_text("Ubuntu 24.04")
            recording.key(b"enter", "Ready")
            recording.hold(1.2)
            recording.key(b"s", "Sort search results")
            recording.key(b"down")
            recording.key(b"enter", "Most seeders")
            recording.hold(0.8)
            recording.key(b"f", "Filter Text")
            recording.type_text("Ubuntu")
            recording.key(b"tab", "Min seeders")
            recording.paste_text("50")
            recording.key(b"tab", "Min size")
            recording.paste_text("1 GiB")
            recording.key(b"tab", "Max size")
            recording.paste_text("5 GiB")
            recording.key(b"enter")
            recording.hold(1.0)

            # Submit the focused filtered result, then show the server page.
            recording.key(b"enter", "Add search result")
            recording.key(b"enter", "Search result submitted")
            recording.hold(0.8)
            recording.key(b"3", "v5.3.0")
            recording.hold(1.4)
            recording.key(b"q")
            deadline = time.monotonic() + 8
            while recording.proc.poll() is None and time.monotonic() < deadline:
                recording.read(0.05)
            if recording.proc.poll() is None:
                raise RuntimeError("qbitctl TUI did not exit")
            if recording.proc.returncode != 0:
                raise RuntimeError(f"qbitctl tui exited with status {recording.proc.returncode}")
            expected_download = {
                "torrentUrl": [fixture.search_results[-1]["fileUrl"]],
                "pluginName": ["jackett"],
            }
            if (
                fixture.searches != 1
                or fixture.logouts != 1
                or fixture.deleted_jobs != [1]
                or fixture.downloads != [expected_download]
            ):
                raise RuntimeError(
                    f"fixture cleanup mismatch: logout={fixture.logouts}, "
                    f"deleted={fixture.deleted_jobs}, downloads={fixture.downloads}"
                )
            recording.save(output)
            print(f"Recorded {recording.events[-1][0]:.1f}s to {output}", flush=True)
        finally:
            if recording is not None:
                recording.close()
            server.shutdown()
            worker.join(timeout=5)
            server.server_close()


if __name__ == "__main__":
    main()
