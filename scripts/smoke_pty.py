#!/usr/bin/env python3
"""Exercise the interactive TUI through a real pseudo-terminal.

This deliberately drives the same first-run, stale-credential recovery,
search-cancellation, and destructive-confirmation paths a user sees.  The
HTTP server is the fixture used by ``record_demo.py`` so the smoke check and
the published terminal recording keep one API contract.
"""

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
from http.server import ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from types import SimpleNamespace
from urllib.parse import urlsplit

SCRIPT_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPT_DIR))
from record_demo import ALPHA, BRAVO, Fixture, torrent  # noqa: E402


WIDTH = 112
HEIGHT = 32
ANSI_RE = re.compile(rb"\x1b\[[0-?]*[ -/]*[@-~]")


class DetailsFixture(Fixture):
    def do_GET(self):
        path = urlsplit(self.path).path
        if path == "/api/v2/torrents/files":
            if self.protected():
                self.reply(body=[{
                    "index": 0, "name": "fixture-image.iso", "size": 1048576,
                    "priority": 1, "progress": 0.42,
                }])
        elif path == "/api/v2/sync/torrentPeers":
            if self.protected():
                self.reply(body={
                    "rid": 1,
                    "full_update": True,
                    "peers": {"192.0.2.9:51413": {
                        "ip": "192.0.2.9", "port": 51413, "client": "Fixture peer", "progress": 0.68,
                    }},
                })
        else:
            super().do_GET()


class Session:
    def __init__(self, binary: Path, config: Path, server_url: str, base: Path, command=("tui",)):
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
            [str(binary), "--config", str(config), *command],
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
            "f2": b"\x1bOQ",
            "ctrl+c": b"\x03",
        }
        start = len(self.output)
        self.send(keys.get(name, name.encode()))
        if name == "escape":
            # Standalone Escape is also the prefix for Alt key sequences.
            # Give the terminal parser time to distinguish it before the
            # next simulated keystroke.
            time.sleep(0.15)
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


def inspect_torrent_details(session: Session):
    session.key("enter", "1 Overview")
    session.key("3", "fixture-image.iso")
    session.key("4", "192.0.2.9:51413")
    session.key("escape")


def inspect_tracker_and_add_forms(session: Session):
    session.key("enter", "1 Overview")
    session.key("2", "v full URL")
    session.key("v", "https://tracker.example/announce")
    session.key("escape")
    session.key("a", "Add torrent")
    session.key("f2", "Advanced: Content layout")
    session.key("escape")


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


def save_ocean_theme(session):
    session.key("4", "Color scheme")
    session.key("jjjjjjj")  # The Color scheme row is the eighth setting.
    session.key("enter", "Ocean")
    session.key("down")
    session.key("enter")


def check_cli_theme(binary, config, server_url, base):
    def run(*command, exit_code=0):
        session = Session(binary, config, server_url, base, command)
        try:
            session.wait_exit()
            # Drain output queued by the PTY even if the child already exited.
            session.read(0)
            if session.proc.returncode != exit_code:
                raise RuntimeError(f"CLI {command} exited with {session.proc.returncode}")
            return bytes(session.output)
        finally:
            session.close()

    if json.loads(config.read_text())["ui"]["theme"] != "ocean":
        raise RuntimeError("the TUI did not persist Ocean to ui.theme")
    if b"\x1b[38;2;229;242;255;1mUsage:" not in run("--help"):
        raise RuntimeError("interactive CLI help is missing Ocean's text color")
    config_help = run("config", "--help")
    if not re.search(rb"\x1b\[38;2;142;166;194m *Choose the default search provider", config_help):
        raise RuntimeError("nested CLI help is missing Ocean's description color")
    if b"\x1b[38;2;103;232;249mName" not in run("list"):
        raise RuntimeError("interactive CLI list is missing Ocean's accent")
    if b"\x1b[38;2;229;242;255;1mINDEX" not in run("files", ALPHA):
        raise RuntimeError("interactive CLI table is missing Ocean's heading color")
    schemes = run("config", "color-scheme", "list")
    if b"SWATCHES" not in schemes or b"\x1b[48;2;46;52;64m \x1b[m" not in schemes:
        raise RuntimeError("the color scheme table is missing its swatches")
    providers = run("config", "default-search-plugin", "list")
    if b"\x1b[38;2;229;242;255;1mSTATUS" not in providers or b"disabled" not in providers:
        raise RuntimeError("the provider table is missing its heading color or disabled status")
    missing_provider = run("config", "default-search-plugin", "set", exit_code=4)
    if b"missing required parameter PROVIDER" not in missing_provider:
        raise RuntimeError("setting a missing provider did not fail immediately")
    rows = json.loads(run("list", "--json"))
    if {row["Name"] for row in rows} != {"Ubuntu 24.04 LTS", "Debian 12 Bookworm"}:
        raise RuntimeError("JSON output changed with the theme")
    if b"\x1b" in run("list", "--fields", "hash-full,name"):
        raise RuntimeError("selected-field output contains theme escapes")
    batch = base / "theme-batch.txt"
    batch.write_text("list\n")
    if b"\x1b" in run("batch", str(batch)):
        raise RuntimeError("batch output contains theme escapes")


def check_cli_delete(binary, config, server_url, base, fixture):
    for answer in ("delete", "DELETE", None):
        command = ("delete", ALPHA)
        if answer is None:
            command += ("--yes",)
        request_count = len(fixture.deleted_torrents)
        session = Session(binary, config, server_url, base, command)
        try:
            if answer is not None:
                session.wait("Type DELETE to confirm")
                output = session.clean_output()
                if ALPHA.encode() not in output or b"Ubuntu 24.04 LTS" not in output:
                    raise RuntimeError("delete preview omitted the target hash or name")
                if len(fixture.deleted_torrents) != request_count:
                    raise RuntimeError("CLI delete sent a request before confirmation")
                session.send(answer.encode() + b"\r")
            session.wait_exit()
            if session.proc.returncode != 0:
                raise RuntimeError(f"CLI delete exited with {session.proc.returncode}")
            expected = [] if answer == "delete" else [{"hashes": [ALPHA], "deleteFiles": ["true"]}]
            if fixture.deleted_torrents[request_count:] != expected:
                raise RuntimeError(f"unexpected CLI delete requests for {answer!r}")
        finally:
            session.close()


def check_interactive_profile_add(binary, base, server_url, fixture):
    config = base / "interactive-profile.json"
    previous = fixture.password
    fixture.password = "interactive-setup-secret"
    session = Session(binary, config, server_url, base, ("profile", "add", "nate"))
    try:
        session.wait("URL [")
        session.send(server_url.encode() + b"\r")
        session.wait("Username [")
        session.send(b"demo\r")
        session.wait("Password:")
        session.send(fixture.password.encode() + b"\r")
        session.wait("Saved profile nate")
        session.wait_exit()
        saved = json.loads(config.read_text())
        if saved["default_profile"] != "nate" or not saved["profiles"]["nate"]["password"].startswith("enc:v2:"):
            raise RuntimeError("interactive profile creation did not save encrypted credentials")
        if fixture.password.encode() in session.clean_output():
            raise RuntimeError("interactive profile password was echoed")
    finally:
        session.close()
        fixture.password = previous


def main():
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {Path(sys.argv[0]).name} /path/to/qbitctl")
    binary = Path(sys.argv[1]).resolve(strict=True)
    fixture = make_fixture()
    with tempfile.TemporaryDirectory(prefix="qbitctl-pty-") as temp:
        base = Path(temp)
        config = base / "config.json"
        server = ThreadingHTTPServer(("127.0.0.1", 0), DetailsFixture)
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
            save_ocean_theme(session)
            session.key("q")
            session.wait_exit(8)
            session.close()
            session = None

            write_config(binary, config, server_url, "stale", base)
            session = Session(binary, config, server_url, base)
            stale_credential_recovery(session)
            inspect_torrent_details(session)
            inspect_tracker_and_add_forms(session)
            cancel_search_and_confirm_delete(session, fixture)
            session.key("q")
            session.wait_exit(8)
            if session.proc.returncode != 0:
                raise RuntimeError(f"qbitctl TUI exited with {session.proc.returncode}")
            if fixture.logouts != 2:
                raise RuntimeError(f"logout count = {fixture.logouts}, want 2")
            session.close()
            session = None
            check_cli_delete(binary, config, server_url, base, fixture)
            check_cli_theme(binary, config, server_url, base)
            check_interactive_profile_add(binary, base, server_url, fixture)
            print("PTY smoke passed (profile setup, recovery, details, search cancellation, deletion, shared color scheme)")
        finally:
            if session is not None:
                session.close()
            server.shutdown()
            worker.join(timeout=5)
            server.server_close()


if __name__ == "__main__":
    main()
