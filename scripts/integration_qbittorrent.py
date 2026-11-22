#!/usr/bin/env python3
"""Exercise qbitctl against an official qBittorrent container.

The container is deliberately started by this script instead of as a GitHub
Actions service.  qBittorrent 5.0 and newer do not have a stable default
password: a clean profile prints a temporary password to stdout.  Starting
the container here lets the script consume that password without putting it in
workflow configuration or an Actions log.
"""

from __future__ import annotations

import argparse
import hashlib
import http.cookiejar
import json
import os
import re
import secrets
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from urllib import parse as urlparse
from urllib import request as urlrequest


TEMP_PASSWORD_RE = re.compile(
    r"temporary password is provided for this session:\s*(\S+)", re.IGNORECASE
)


def bencode(value: object) -> bytes:
    """Encode the small subset of bencode needed for the inert fixture."""

    if isinstance(value, int):
        return f"i{value}e".encode("ascii")
    if isinstance(value, bytes):
        return str(len(value)).encode("ascii") + b":" + value
    if isinstance(value, dict):
        parts = [b"d"]
        for key in sorted(value):
            if not isinstance(key, bytes):
                raise TypeError("fixture dictionary keys must be bytes")
            parts.extend((bencode(key), bencode(value[key])))
        parts.append(b"e")
        return b"".join(parts)
    raise TypeError(f"unsupported bencode value: {type(value)!r}")


def write_fixture(path: Path) -> None:
    """Write a valid trackerless, one-byte torrent with no external peers."""

    content = b"q"
    info = {
        b"length": len(content),
        b"name": b"qbitctl-compat-fixture.bin",
        b"piece length": 16384,
        b"pieces": hashlib.sha1(content).digest(),
    }
    path.write_bytes(bencode({b"info": info}))


def reserve_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def redact(value: str, password: str | None = None) -> str:
    if password:
        value = value.replace(password, "<redacted>")
    return TEMP_PASSWORD_RE.sub(r"temporary password is provided for this session: <redacted>", value)


def docker_logs(container: str) -> str:
    result = subprocess.run(
        ["docker", "logs", container],
        check=False,
        capture_output=True,
        text=True,
        errors="replace",
    )
    return result.stdout + result.stderr


def start_container(
    image: str, container: str, config: Path, downloads: Path, web_port: int
) -> None:
    command = [
        "docker",
        "run",
        "--detach",
        "--tty",
        "--name",
        container,
        "--user",
        f"{os.getuid()}:{os.getgid()}",
        # The older image wrapper changes ownership before launching qBittorrent.
        "--entrypoint",
        "qbittorrent-nox",
        "--read-only",
        "--stop-timeout",
        "1800",
        "--tmpfs",
        "/tmp",
        # qBittorrent 5.0 validates Host against the listening port.
        "--publish",
        f"127.0.0.1:{web_port}:{web_port}/tcp",
        "--volume",
        f"{config}:/config",
        "--volume",
        f"{downloads}:/downloads",
        image,
        "--profile=/config",
        f"--webui-port={web_port}",
        "--confirm-legal-notice",
    ]
    result = subprocess.run(command, check=False, capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(
            "failed to start qBittorrent container: "
            + redact(result.stderr or result.stdout)
        )


def wait_for_password(container: str, timeout: float = 60.0) -> str:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        logs = docker_logs(container)
        match = TEMP_PASSWORD_RE.search(logs)
        if match:
            return match.group(1)

        state = subprocess.run(
            ["docker", "inspect", "--format", "{{.State.Status}}", container],
            check=False,
            capture_output=True,
            text=True,
        )
        if state.returncode != 0 or state.stdout.strip() == "exited":
            raise RuntimeError(
                "qBittorrent container exited before exposing the temporary password:\n"
                + redact(logs)
            )
        time.sleep(0.5)

    raise RuntimeError(
        "timed out waiting for qBittorrent's temporary password:\n"
        + redact(docker_logs(container))
    )


def run_cli(
    binary: Path,
    url: str,
    password: str,
    workdir: Path,
    config_home: Path,
    *args: str,
    check: bool = True,
    timeout: float = 30.0,
) -> subprocess.CompletedProcess[str]:
    command = [
        str(binary),
        "--url",
        url,
        "--user",
        "admin",
        "--pass",
        password,
        *args,
    ]
    env = dict(os.environ)
    env.update(
        {
            "XDG_CONFIG_HOME": str(config_home),
            "NO_COLOR": "1",
            # Keep the compatibility container independent of host keyrings.
            "QBITCTL_MASTER_KEY": "qbitctl-compat-master",
        }
    )
    try:
        result = subprocess.run(
            command,
            cwd=workdir,
            env=env,
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired:
        raise RuntimeError(f"qbitctl {' '.join(args)} timed out") from None
    if check and result.returncode != 0:
        output = redact(result.stdout + result.stderr, password).strip()
        raise RuntimeError(
            f"qbitctl {' '.join(args)} failed with exit {result.returncode}: {output}"
        )
    return result


def complete_show(
    binary: Path, url: str, password: str, workdir: Path, config_home: Path
) -> list[str]:
    """Run Cobra's completion protocol and return the suggested hashes."""

    result = run_cli(binary, url, password, workdir, config_home, "__complete", "show", "")
    lines = result.stdout.splitlines()
    if not lines or lines[-1] != ":4":
        raise RuntimeError(
            "qbitctl hash completion returned an unexpected directive: "
            + redact(result.stdout, password)
        )
    return lines[:-1]


def list_torrents(
    binary: Path, url: str, password: str, workdir: Path, config_home: Path
) -> list[dict[str, object]]:
    result = run_cli(
        binary, url, password, workdir, config_home, "list", "--server-json"
    )
    try:
        value = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError(
            "qbitctl list returned invalid JSON: " + redact(result.stdout, password)
        ) from error
    if not isinstance(value, list):
        raise RuntimeError("qbitctl list returned a non-array JSON value")
    return value


def wait_for_ready(
    binary: Path,
    url: str,
    password: str,
    workdir: Path,
    config_home: Path,
    timeout: float = 30.0,
) -> None:
    deadline = time.monotonic() + timeout
    last_output = ""
    while time.monotonic() < deadline:
        try:
            result = run_cli(
                binary,
                url,
                password,
                workdir,
                config_home,
                "info",
                check=False,
                timeout=min(10.0, max(1.0, deadline - time.monotonic())),
            )
            if result.returncode == 0:
                return
            last_output = redact(result.stdout + result.stderr, password)
        except RuntimeError as error:
            last_output = str(error)
        time.sleep(0.5)
    raise RuntimeError("qBittorrent WebUI did not become ready: " + last_output.strip())


def verify_main_data(url: str, password: str) -> None:
    """Check the maindata envelope and field names on each supported release."""

    cookies = http.cookiejar.CookieJar()
    opener = urlrequest.build_opener(urlrequest.HTTPCookieProcessor(cookies))
    login = urlparse.urlencode({"username": "admin", "password": password}).encode()
    login_request = urlrequest.Request(url.rstrip("/") + "/api/v2/auth/login", data=login)
    login_request.add_header("Content-Type", "application/x-www-form-urlencoded")
    with opener.open(login_request, timeout=30) as response:
        login_body = response.read().strip()
        if response.status not in (200, 204) or (login_body and login_body != b"Ok."):
            raise RuntimeError("direct maindata compatibility login failed")

    with opener.open(url.rstrip("/") + "/api/v2/sync/maindata?rid=0", timeout=30) as response:
        payload = response.read()
    try:
        value = json.loads(payload)
    except json.JSONDecodeError as error:
        raise RuntimeError("maindata returned invalid JSON") from error
    if not isinstance(value, dict):
        raise RuntimeError("maindata returned a non-object response")
    required = {"rid", "full_update", "torrents"}
    missing = sorted(required.difference(value))
    if missing:
        raise RuntimeError("maindata response missing fields: " + ", ".join(missing))
    if not isinstance(value["rid"], int) or not isinstance(value["full_update"], bool):
        raise RuntimeError("maindata rid/full_update fields have unexpected types")
    if not isinstance(value["torrents"], dict):
        raise RuntimeError("maindata torrent fields have unexpected types")
    removed = value.get("torrents_removed", [])
    if not isinstance(removed, list):
        raise RuntimeError("maindata torrents_removed has an unexpected type")
    print(
        "maindata compatibility: "
        f"rid={value['rid']} full_update={value['full_update']} "
        f"torrents={len(value['torrents'])} removed={len(removed)}"
    )


def wait_for_torrent(
    binary: Path,
    url: str,
    password: str,
    workdir: Path,
    config_home: Path,
    name: str,
    present: bool = True,
    timeout: float = 30.0,
    state: str | None = None,
) -> dict[str, object] | None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        torrents = list_torrents(binary, url, password, workdir, config_home)
        match = next((item for item in torrents if item.get("name") == name), None)
        if not present and match is None:
            return None
        if present and match is not None and (state is None or match.get("state") == state):
            return match
        time.sleep(0.5)
    expected = (state or "present") if present else "absent"
    raise RuntimeError(f"timed out waiting for torrent {name!r} to be {expected}")


def wait_for_tags_and_limits(
    binary: Path,
    url: str,
    password: str,
    workdir: Path,
    config_home: Path,
    name: str,
    tags: set[str],
    seeding_time_limit: int,
    ratio_limit: float,
    timeout: float = 30.0,
) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        torrent = wait_for_torrent(
            binary,
            url,
            password,
            workdir,
            config_home,
            name,
            timeout=min(5.0, max(0.5, deadline - time.monotonic())),
        )
        if torrent is not None:
            actual_tags = {
                tag.strip() for tag in str(torrent.get("tags", "")).split(",") if tag.strip()
            }
            actual_seedtime = torrent.get("seeding_time_limit")
            actual_ratio = torrent.get("ratio_limit")
            if (
                actual_tags == tags
                and actual_seedtime == seeding_time_limit
                and isinstance(actual_ratio, (int, float))
                and abs(float(actual_ratio) - ratio_limit) < 1e-9
            ):
                return torrent
        time.sleep(0.5)
    raise RuntimeError(f"timed out waiting for qBittorrent state on {name!r}")


def prepare_config(config: Path, web_port: int) -> None:
    # The fixture has no tracker, and these settings prevent DHT/peer discovery
    # from making the compatibility job contact anything outside the runner.
    preferences = config / "qBittorrent" / "config" / "qBittorrent.conf"
    preferences.parent.mkdir(parents=True, exist_ok=True)
    preferences.write_text(
        rf"""[BitTorrent]
Session\DefaultSavePath=/downloads
Session\TempPath=/downloads/temp
Session\TempPathEnabled=true
Session\DHTEnabled=false
Session\LSDEnabled=false
Session\PeXEnabled=false
Session\Port=6881
[Meta]
MigrationVersion=9999
[LegalNotice]
Accepted=true
[Preferences]
WebUI\Port={web_port}
[WebUI]
Port={web_port}
""",
        encoding="utf-8",
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="qbitctl binary to exercise")
    parser.add_argument("--image", required=True, help="immutable qBittorrent image reference")
    parser.add_argument("--version", required=True, help="human-readable qBittorrent version")
    return parser.parse_args()


def run(args: argparse.Namespace) -> None:
    binary = args.binary.resolve()
    if not binary.is_file():
        raise RuntimeError(f"qbitctl binary does not exist: {binary}")

    container = f"qbitctl-compat-{os.getpid()}"
    web_port = reserve_port()
    with tempfile.TemporaryDirectory(prefix="qbitctl-qb-compat-") as temp:
        root = Path(temp)
        config = root / "config"
        downloads = root / "downloads"
        config_home = root / "config-home"
        downloads.mkdir()
        config_home.mkdir()
        prepare_config(config, web_port)

        password = ""
        try:
            start_container(args.image, container, config, downloads, web_port)
            password = wait_for_password(container)
            url = f"http://127.0.0.1:{web_port}"
            print(f"qBittorrent {args.version}: authenticating")
            wait_for_ready(binary, url, password, root, config_home)
            # Completion reads the saved setup because Cobra's hidden
            # __complete command skips the normal pre-run password handling.
            run_cli(binary, url, password, root, config_home, "config", "write")

            fixture = root / "qbitctl-compat.torrent"
            invalid_fixture = root / "qbitctl-invalid.torrent"
            write_fixture(fixture)
            invalid_fixture.write_bytes(b"not a bencoded torrent")

            category = "qbitctl-compat"
            destination = "/downloads/compat"
            (downloads / "compat").mkdir()
            run_cli(
                binary, url, password, root, config_home, "add", str(fixture),
                "--category", category, "--save-path", destination, "--stopped",
            )
            torrent = wait_for_torrent(
                binary,
                url,
                password,
                root,
                config_home,
                "qbitctl-compat-fixture.bin",
                state="stoppedDL",
            )
            if torrent is None or not torrent.get("hash"):
                raise RuntimeError("qBittorrent added the fixture without a hash")
            torrent_hash = str(torrent["hash"])
            if torrent.get("category") != category or str(torrent.get("save_path")).rstrip("/") != destination:
                raise RuntimeError("qBittorrent did not apply the add category and save path")
            verify_main_data(url, password)

            completions = complete_show(binary, url, password, root, config_home)
            expected_completion = f"{torrent_hash}\tqbitctl-compat-fixture.bin"
            if expected_completion not in completions:
                raise RuntimeError(
                    "qbitctl hash completion did not return the added torrent: "
                    + redact("; ".join(completions), password)
                )

            unknown_hash = secrets.token_hex(20)
            rejected_hash = run_cli(
                binary,
                url,
                password,
                root,
                config_home,
                "start",
                unknown_hash,
                check=False,
            )
            if rejected_hash.returncode != 4:
                output = redact(rejected_hash.stdout + rejected_hash.stderr, password).strip()
                raise RuntimeError(
                    "qbitctl accepted an unknown full hash for start: "
                    f"exit {rejected_hash.returncode}: {output}"
                )

            if args.version == "5.0.0":
                duplicate = run_cli(
                    binary, url, password, root, config_home, "add", str(fixture), check=False
                )
                if duplicate.returncode == 0 or "qBittorrent rejected the request" not in duplicate.stderr:
                    raise RuntimeError("qbitctl did not report the legacy duplicate add rejection")

            rejected = run_cli(
                binary,
                url,
                password,
                root,
                config_home,
                "add",
                str(invalid_fixture),
                check=False,
            )
            if rejected.returncode == 0:
                raise RuntimeError("qBittorrent accepted an invalid torrent fixture")

            first_tags = {"qbitctl-compat-a", "qbitctl-compat-b"}
            run_cli(
                binary,
                url,
                password,
                root,
                config_home,
                "set",
                "tags",
                ",".join(sorted(first_tags)),
                torrent_hash,
            )
            second_tags = {"qbitctl-compat-b", "qbitctl-compat-c"}
            run_cli(
                binary,
                url,
                password,
                root,
                config_home,
                "set",
                "tags",
                ",".join(sorted(second_tags)),
                torrent_hash,
            )
            run_cli(
                binary,
                url,
                password,
                root,
                config_home,
                "set",
                "seedtime-limit",
                "120",
                torrent_hash,
            )
            run_cli(
                binary,
                url,
                password,
                root,
                config_home,
                "set",
                "ratio-limit",
                "1.5",
                torrent_hash,
            )
            wait_for_tags_and_limits(
                binary,
                url,
                password,
                root,
                config_home,
                "qbitctl-compat-fixture.bin",
                second_tags,
                2,
                1.5,
            )

            run_cli(binary, url, password, root, config_home, "delete", torrent_hash)
            wait_for_torrent(
                binary,
                url,
                password,
                root,
                config_home,
                "qbitctl-compat-fixture.bin",
                present=False,
            )
            print(f"qBittorrent {args.version}: compatibility checks passed")
        except Exception:
            logs = redact(docker_logs(container), password).strip()
            if logs:
                print("qBittorrent container logs:\n" + logs, file=sys.stderr)
            raise
        finally:
            subprocess.run(
                ["docker", "rm", "--force", container],
                check=False,
                capture_output=True,
                text=True,
            )


def main() -> int:
    args = parse_args()
    try:
        run(args)
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"qBittorrent compatibility failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
