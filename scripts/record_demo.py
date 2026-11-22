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
from email import policy
from email.parser import BytesParser
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
SEARCH_PLUGINS = [
    {"name": "communityarchive", "fullName": "Community Archive", "enabled": False},
    {"name": "distroarchive", "fullName": "Distro Archive", "enabled": True},
    {"name": "linuxreleases", "fullName": "Linux Releases", "enabled": True},
]
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
        elif parsed.path == "/api/v2/search/plugins":
            self.reply(body=SEARCH_PLUGINS)
        elif parsed.path == "/api/v2/search/status":
            job_id = int(query["id"][0])
            status = "Running" if self.fixture.search_running else "Stopped"
            self.reply(body=[{"id": job_id, "status": status}])
        elif parsed.path == "/api/v2/search/results":
            self.reply(body={"results": self.search_results(int(query["id"][0]))})
        else:
            self.reply(404)

    def do_POST(self):
        path = urlsplit(self.path).path
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)
        content_type = self.headers.get("Content-Type", "")
        if content_type.startswith("multipart/form-data"):
            headers = f"Content-Type: {content_type}\r\n\r\n".encode("ascii")
            message = BytesParser(policy=policy.default).parsebytes(headers + raw)
            form = {}
            for part in message.iter_parts():
                field = part.get_param("name", header="Content-Disposition")
                form.setdefault(field, []).append(part.get_content())
        else:
            form = parse_qs(raw.decode("utf-8"), keep_blank_values=True)
        if path == "/api/v2/auth/login":
            if form.get("username", [""])[0] != self.fixture.username or form.get("password", [""])[0] != self.fixture.password:
                self.reply(401, body=b"Fails.", content_type="text/plain")
                return
            self.reply(204, headers={"Set-Cookie": "QBT_SID_8080=demo; Path=/"})
            return
        if not self.protected():
            return
        if path == "/api/v2/search/start":
            job_id = len(self.fixture.search_jobs) + 1
            self.fixture.search_jobs[job_id] = (form["pattern"][0], form["plugins"][0])
            self.reply(body={"id": job_id})
        elif path == "/api/v2/search/delete":
            self.fixture.deleted_jobs.append(int(form.get("id", [0])[0]))
            self.reply(204)
        elif path == "/api/v2/torrents/delete":
            self.fixture.deleted_torrents.append(form)
            self.reply(204)
        elif path == "/api/v2/torrents/fetchMetadata":
            self.reply(body={})
        elif path == "/api/v2/torrents/add":
            # Keep the fixture queue stable so the recording stays focused on
            # the search workflow. The TUI still shows its submitted notice.
            self.fixture.downloads.append(form)
            self.reply(body={"success_count": 1})
        elif path == "/api/v2/auth/logout":
            self.fixture.logouts += 1
            self.reply(204)
        else:
            self.reply(404)

    def search_results(self, job_id):
        pattern, selector = self.fixture.search_jobs.get(job_id, ("", "enabled"))
        if selector == "enabled":
            selected = {p["name"] for p in SEARCH_PLUGINS if p["enabled"]}
        elif selector == "all":
            selected = {p["name"] for p in SEARCH_PLUGINS}
        else:
            selected = set(selector.split("|"))
        return [
            result for result in self.fixture.search_results.get(pattern, [])
            if result["engineName"] in selected
        ]


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
            "QBITCTL_PASSWORD",
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
            # Keep the recording independent of the host Keychain while still
            # exercising the encrypted config path shown by the Config tab.
            "QBITCTL_MASTER_KEY": "qbitctl-demo-master-key",
        })
        config_path = "./config.json"
        setup = [
            str(binary),
            "--config", config_path,
            "--url", f"http://127.0.0.1:{port}",
            "--user", "demo",
            "--pass", "demo",
            "config", "write",
        ]
        result = subprocess.run(
            setup,
            cwd=base,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            check=False,
        )
        if result.returncode != 0:
            raise RuntimeError(
                f"could not prepare demo config ({result.returncode}): "
                f"{result.stderr.strip()}"
            )
        command = [
            str(binary),
            "--config", config_path,
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
                return text
        raise RuntimeError(f"Missing {marker!r}: {self.output[-1200:]!r}")

    def key(self, key, expected=None):
        if isinstance(key, str):
            key = KEYS.get(key, key.encode("utf-8"))
        elif key.decode("utf-8", "ignore") in KEYS:
            key = KEYS[key.decode("utf-8", "ignore")]
        start = len(self.output)
        self.send(key)
        if expected:
            return self.wait(expected, start)
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
    fixture.username = "demo"
    fixture.password = "demo"
    fixture.torrents = [
        torrent("Ubuntu 24.04 LTS", ALPHA, "downloading", 0.42, 4 * GIB, "ISO", "linux release"),
        torrent("Debian 12 Bookworm", BRAVO, "stoppedUP", 1.0, 2 * GIB, "ISO", "linux stable"),
    ]
    ubuntu_results = [
        {
            "fileName": "Ubuntu 24.04 Server amd64",
            "fileSize": 2 * GIB,
            "nbSeeders": 19,
            "nbLeechers": 5,
            "fileUrl": "https://releases.example/download/ubuntu-server",
            "siteUrl": "https://releases.example",
            "descrLink": "https://releases.example/ubuntu-server",
            "engineName": "linuxreleases",
        },
        {
            "fileName": "Ubuntu 24.04.3 Desktop arm64",
            "fileSize": 6 * GIB,
            "nbSeeders": 62,
            "nbLeechers": 7,
            "fileUrl": "https://archive.example/download/ubuntu-arm",
            "siteUrl": "https://archive.example",
            "descrLink": "https://archive.example/ubuntu-arm",
            "engineName": "distroarchive",
        },
        {
            "fileName": "Ubuntu 24.04.3 Desktop amd64",
            "fileSize": 4 * GIB,
            "nbSeeders": 84,
            "nbLeechers": 12,
            "fileUrl": "https://releases.example/download/ubuntu-amd64",
            "siteUrl": "https://releases.example",
            "descrLink": "https://releases.example/ubuntu",
            "engineName": "linuxreleases",
        },
    ]
    fixture.search_results = {
        "Ubuntu 24.04": ubuntu_results,
        "Debian": [{
            "fileName": "Debian 12.9 DVD amd64",
            "fileSize": 4 * GIB,
            "nbSeeders": 68,
            "nbLeechers": 4,
            "fileUrl": "https://archive.example/download/debian",
            "siteUrl": "https://archive.example",
            "descrLink": "https://archive.example/debian",
            "engineName": "distroarchive",
        }],
    }
    fixture.search_jobs = {}
    fixture.search_running = False
    fixture.deleted_jobs = []
    fixture.deleted_torrents = []
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

            # Show the masked credential metadata and startup preference, then
            # let the saved-preference confirmation expire in the footer.
            recording.key(b"4", "Configuration")
            recording.hold(1.4)
            recording.key(b"d", "TUI startup preference saved")
            recording.hold(1.0)
            recording.hold(4.3)

            # Open Config and preview a color scheme without saving it.
            recording.key(b"c", "Color scheme")
            recording.key(b"down")
            recording.hold(1.2)
            recording.key(b"escape")
            recording.key(b"1")

            # Let the open transfer details show progress from the next poll.
            recording.key(b"enter", "Full Path")
            refresh_start = len(recording.output)
            fixture.torrents[0] = torrent(
                "Ubuntu 24.04 LTS", ALPHA, "downloading", 0.685317, 4 * GIB, "ISO", "linux release"
            )
            recording.wait("685317", refresh_start)
            recording.hold(0.8)
            recording.key(b"escape")
            recording.hold(0.5)

            # Search enabled providers for Ubuntu, then sort and filter the
            # loaded rows without issuing another search.
            recording.key(b"2", "Search results")
            recording.key(b"/", "Timeout")
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

            # Copy the filtered result's ID before running another search.
            add_hint = "Use a recent result ID from this server and user"
            dialog = recording.key(b"enter", add_hint)
            result_id = re.search(rb"Result ID: ([0-9]+)\b", dialog)
            if result_id is None:
                raise RuntimeError("could not read the Ubuntu result ID")
            recording.hold(1.4)
            recording.key(b"escape")
            recording.key(b"escape")
            picker_start = len(recording.output)
            recording.key(b"p", "Choose search provider")
            recording.wait("Community Archive", picker_start)
            recording.hold(1.3)
            recording.key(b"down")
            recording.key(b"down")
            recording.key(b"enter", "Provider: Distro Archive")
            saved_config = json.loads((base / "config.json").read_text())
            if saved_config.get("default_search_plugin") != "distroarchive":
                raise RuntimeError("the picker did not save the provider's plugin ID")
            recording.hold(0.7)
            recording.key(b"/", "Search Distro Archive")
            recording.key(b"\x15")  # Ctrl+U clears the previous query.
            recording.type_text("Debian")
            recording.key(b"enter", "Debian 12.9 DVD amd64")
            recording.hold(1.0)

            # Replace the selected Debian ID with the earlier Ubuntu ID.
            recording.key(b"enter", add_hint)
            recording.key(b"\x15")
            recording.paste_text(result_id.group(1).decode("ascii"))
            recording.hold(1.4)
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
                "urls": [ubuntu_results[-1]["fileUrl"]],
                "downloader": [ubuntu_results[-1]["engineName"]],
            }
            if (
                fixture.search_jobs != {1: ("Ubuntu 24.04", "enabled"), 2: ("Debian", "distroarchive")}
                or fixture.logouts != 1
                or fixture.deleted_jobs != [1, 2]
                or fixture.downloads != [expected_download]
            ):
                raise RuntimeError(
                    f"fixture mismatch: searches={fixture.search_jobs}, logout={fixture.logouts}, "
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
