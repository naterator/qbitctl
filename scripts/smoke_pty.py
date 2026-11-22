#!/usr/bin/env python3
"""Exercise the interactive TUI through a real pseudo-terminal.

This deliberately drives the same first-run, stale-credential recovery,
search-cancellation, and destructive-confirmation paths a user sees.  The
HTTP server is the fixture used by ``record_demo.py`` so the smoke check and
the published terminal recording keep one API contract.
"""

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
from http.server import ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from types import SimpleNamespace

SCRIPT_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPT_DIR))
from record_demo import ALPHA, BRAVO, Fixture, torrent  # noqa: E402


WIDTH = 112
HEIGHT = 32
ANSI_RE = re.compile(rb"\x1b\[[0-?]*[ -/]*[@-~]")


class Session:
    def __init__(self, binary: Path, config: Path, server_url: str, base: Path):
        self.master, slave = pty.openpty()
        fcntl_winsize(slave, WIDTH, HEIGHT)
        env = os.environ.copy()
        for key in ("QBITCTL_MASTER_KEY", "QBITCTL_PASSWORD", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "NO_COLOR"):
            env.pop(key, None)
        env.update(
            {
                "QBITCTL_MASTER_KEY": "qbitctl-pty-master-key",
                "XDG_CONFIG_HOME": str(base / "xdg-config"),
                "XDG_CACHE_HOME": str(base / "xdg-cache"),
                "TERM": "xterm-256color",
                "COLORTERM": "truecolor",
            }
        )
        self.proc = subprocess.Popen(
            [str(binary), "--config", str(config), "tui"],
            cwd=base,
            env=env,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            start_new_session=True,
        )
        os.close(slave)
        self.output = bytearray()

    def read(self, timeout=0.05):
        if select.select([self.master], [], [], timeout)[0]:
            try:
                chunk = os.read(self.master, 65536)
            except OSError:
                return
            if chunk:
                self.output.extend(chunk)

    def clean_output(self, start=0):
        return ANSI_RE.sub(b"", bytes(self.output[start:]))

    def wait(self, marker: str, timeout=12):
        start = len(self.output)
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.read()
            if marker.encode() in self.clean_output(start):
                return
        raise RuntimeError(f"missing {marker!r}: {self.clean_output(max(0, len(self.output) - 2000))!r}")

    def send(self, value: bytes):
        os.write(self.master, value)

    def type_text(self, value: str):
        for character in value:
            self.send(character.encode())
            self.read(0.015)

    def key(self, name: str, expected=None):
        keys = {
            "tab": b"\t",
            "enter": b"\r",
            "escape": b"\x1b",
            "up": b"\x1b[A",
            "down": b"\x1b[B",
            "ctrl+c": b"\x03",
        }
        start = len(self.output)
        self.send(keys.get(name, name.encode()))
        if expected:
            self.wait(expected, timeout=15)
        else:
            self.read(0.25)

    def close(self):
        if self.proc.poll() is None:
            self.send(b"q")
            try:
                self.wait_exit(8)
            except subprocess.TimeoutExpired:
                self.proc.terminate()
                self.wait_exit(8)
        os.close(self.master)

    def wait_exit(self, timeout=8):
        deadline = time.monotonic() + timeout
        while self.proc.poll() is None and time.monotonic() < deadline:
            self.read(0.05)
        if self.proc.poll() is None:
            error = subprocess.TimeoutExpired(self.proc.args, timeout)
            error.output = self.clean_output(max(0, len(self.output) - 3000))
            raise error


def fcntl_winsize(fd, width, height):
    import fcntl

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))


def write_config(binary: Path, config: Path, server_url: str, password: str, base: Path):
    env = os.environ.copy()
    env.update(
        {
            "QBITCTL_MASTER_KEY": "qbitctl-pty-master-key",
            "XDG_CONFIG_HOME": str(base / "xdg-config"),
            "XDG_CACHE_HOME": str(base / "xdg-cache"),
        }
    )
    result = subprocess.run(
        [
            str(binary),
            "--config",
            str(config),
            "--url",
            server_url,
            "--user",
            "demo",
            "--pass",
            password,
            "config",
            "write",
        ],
        cwd=base,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(f"config write failed: {result.stderr.strip()}")


def make_fixture():
    fixture = SimpleNamespace(
        username="demo",
        password="demo",
        torrents=[
            torrent("Ubuntu 24.04 LTS", ALPHA, "downloading", 0.42, 4 << 30, "ISO", "linux release"),
            torrent("Debian 12 Bookworm", BRAVO, "stoppedUP", 1.0, 2 << 30, "ISO", "linux stable"),
        ],
        search_results={"cancel me": []},
        search_jobs={},
        search_running=True,
        deleted_jobs=[],
        deleted_torrents=[],
        downloads=[],
        logouts=0,
    )
    return fixture


def first_launch(session: Session, server_url: str):
    session.wait("Configure qBittorrent")
    session.type_text(server_url)
    session.key("tab")
    session.type_text("demo")
    session.key("tab")
    session.type_text("demo")
    session.key("tab")
    session.key("enter", "Ubuntu 24.04 LTS")


def stale_credential_recovery(session: Session):
    session.wait("Connection failed")
    # Server and username are retained; password is intentionally blank after
    # a failed login so the stale secret cannot be accidentally resubmitted.
    session.key("tab")
    session.key("tab")
    session.type_text("demo")
    session.key("tab")
    session.key("enter", "Ubuntu 24.04 LTS")


def cancel_search_and_confirm_delete(session: Session, fixture):
    session.key("2")
    session.key("/", "Search Enabled search plugins")
    session.type_text("cancel me")
    session.key("enter", "Searching Enabled search plugins")
    session.key("ctrl+c", "Search canceled")
    if fixture.deleted_jobs != [1]:
        raise RuntimeError(f"canceled search cleanup = {fixture.deleted_jobs!r}, want [1]")

    session.key("1", "Ubuntu 24.04 LTS")
    session.key("D", "Delete torrents and files")
    session.key("enter", "Type DELETE exactly to confirm")
    if fixture.deleted_torrents:
        raise RuntimeError("delete request was sent before typed confirmation")
    session.type_text("DELETE")
    session.key("enter", "Deleted selected torrents and files")
    if len(fixture.deleted_torrents) != 1:
        raise RuntimeError(f"confirmed delete requests = {fixture.deleted_torrents!r}")


def main():
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {Path(sys.argv[0]).name} /path/to/qbitctl")
    binary = Path(sys.argv[1]).resolve(strict=True)
    fixture = make_fixture()
    with tempfile.TemporaryDirectory(prefix="qbitctl-pty-") as temp:
        base = Path(temp)
        config = base / "config.json"
        server = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
        server.fixture = fixture
        worker = Thread(target=server.serve_forever, daemon=True)
        worker.start()
        session = None
        try:
            server_url = f"http://127.0.0.1:{server.server_address[1]}"
            # First launch exercises the empty-config bootstrap and creates a
            # known-good profile for the subsequent stale-credential run.
            session = Session(binary, config, server_url, base)
            first_launch(session, server_url)
            session.key("q")
            session.wait_exit(8)
            session.close()
            session = None

            write_config(binary, config, server_url, "stale", base)
            session = Session(binary, config, server_url, base)
            stale_credential_recovery(session)
            cancel_search_and_confirm_delete(session, fixture)
            session.key("q")
            session.wait_exit(8)
            if session.proc.returncode != 0:
                raise RuntimeError(f"qbitctl TUI exited with {session.proc.returncode}")
            if fixture.logouts != 2:
                raise RuntimeError(f"logout count = {fixture.logouts}, want 2")
            print("PTY smoke passed (first launch, stale credentials, canceled search, confirmed delete)")
        finally:
            if session is not None:
                session.close()
            server.shutdown()
            worker.join(timeout=5)
            server.server_close()


if __name__ == "__main__":
    main()
