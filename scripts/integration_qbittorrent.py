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
import threading
import time
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
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


def write_fixture(path: Path, name: str = "qbitctl-compat-fixture.bin") -> None:
    """Write a valid trackerless, one-byte torrent with no external peers."""

    content = b"q"
    info = {
        b"length": len(content),
        b"name": name.encode("utf-8"),
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


class FixtureHandler(BaseHTTPRequestHandler):
    """Serve the checked-in search plugins and deterministic torrents."""

    def log_message(self, *_args):
        pass

    def do_GET(self):
        if self.path == "/qbitctl_fixture.py":
            body = self.server.plugin.read_bytes()
            content_type = "text/x-python; charset=utf-8"
        elif self.path == "/qbitctl_fixture_alt.py":
            body = self.server.alt_plugin.read_bytes()
            content_type = "text/x-python; charset=utf-8"
        elif self.path == "/qbitctl-search.torrent":
            body = self.server.torrent.read_bytes()
            content_type = "application/x-bittorrent"
        elif self.path == "/alt/qbitctl-search-alt.torrent":
            body = self.server.alt_torrent.read_bytes()
            content_type = "application/x-bittorrent"
        elif self.path == "/qbitctl-search-description":
            body = b"qbitctl compatibility fixture"
            content_type = "text/plain; charset=utf-8"
        elif self.path == "/alt/qbitctl-search-alt-description":
            body = b"qbitctl alternate compatibility fixture"
            content_type = "text/plain; charset=utf-8"
        else:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def authenticated_opener(url: str, password: str):
    """Return a cookie-preserving opener authenticated to qBittorrent."""

    cookies = http.cookiejar.CookieJar()
    opener = urlrequest.build_opener(urlrequest.HTTPCookieProcessor(cookies))
    login = urlparse.urlencode({"username": "admin", "password": password}).encode()
    login_request = urlrequest.Request(url.rstrip("/") + "/api/v2/auth/login", data=login)
    login_request.add_header("Content-Type", "application/x-www-form-urlencoded")
    with opener.open(login_request, timeout=30) as response:
        body = response.read().strip()
        if response.status not in (200, 204) or (body and body != b"Ok."):
            raise RuntimeError("qBittorrent search compatibility login failed")
    return opener


def search_plugins(opener, url: str) -> list[dict[str, object]]:
    with opener.open(url.rstrip("/") + "/api/v2/search/plugins", timeout=30) as response:
        try:
            plugins = json.loads(response.read())
        except json.JSONDecodeError as error:
            raise RuntimeError("qBittorrent returned invalid search plugin JSON") from error
    if not isinstance(plugins, list) or not all(isinstance(plugin, dict) for plugin in plugins):
        raise RuntimeError("qBittorrent returned invalid search plugin entries")
    return plugins


def wait_for_search_plugin(
    opener,
    url: str,
    short_name: str,
    full_name: str,
    enabled: bool = True,
) -> None:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        plugins = search_plugins(opener, url)
        for plugin in plugins:
            if (
                str(plugin.get("name", "")).casefold() == short_name.casefold()
                and str(plugin.get("fullName", "")).casefold() == full_name.casefold()
                and bool(plugin.get("enabled")) == enabled
            ):
                return
        time.sleep(0.5)
    state = search_plugins(opener, url)
    raise RuntimeError(
        f"qBittorrent search plugin {short_name!r} did not reach enabled={enabled}: {state!r}"
    )


def install_search_plugin(
    url: str,
    password: str,
    plugin_url: str,
    short_name: str,
    full_name: str,
) -> None:
    opener = authenticated_opener(url, password)
    payload = urlparse.urlencode({"sources": plugin_url}).encode()
    request = urlrequest.Request(
        url.rstrip("/") + "/api/v2/search/installPlugin", data=payload
    )
    request.add_header("Content-Type", "application/x-www-form-urlencoded")
    with opener.open(request, timeout=30) as response:
        body = response.read().strip()
        if response.status != 200 or body not in (b"", b"Ok."):
            raise RuntimeError("qBittorrent rejected the fixture search plugin")
    wait_for_search_plugin(opener, url, short_name, full_name)


def set_search_plugin_enabled(
    url: str, password: str, name: str, full_name: str, enabled: bool
) -> None:
    opener = authenticated_opener(url, password)
    payload = urlparse.urlencode({"names": name, "enable": str(enabled).lower()}).encode()
    request = urlrequest.Request(
        url.rstrip("/") + "/api/v2/search/enablePlugin", data=payload
    )
    request.add_header("Content-Type", "application/x-www-form-urlencoded")
    with opener.open(request, timeout=30) as response:
        body = response.read().strip()
        if response.status != 200 or body not in (b"", b"Ok."):
            raise RuntimeError(f"qBittorrent rejected the plugin enabled={enabled} request")
    wait_for_search_plugin(opener, url, name, full_name, enabled)


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
    image: str,
    container: str,
    config: Path,
    downloads: Path,
    web_port: int,
    network: str,
    fixture_base: str,
) -> None:
    command = [
        "docker",
        "run",
        "--detach",
        "--tty",
        "--name",
        container,
        "--network",
        network,
        "--network-alias",
        "qbit",
        "--add-host",
        "host.docker.internal:host-gateway",
        "--env",
        f"QBITCTL_FIXTURE_BASE={fixture_base}",
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


def start_https_proxy(
    network: str,
    proxy_container: str,
    web_port: int,
    https_port: int,
    root: Path,
) -> tuple[Path, Path]:
    """Start an nginx sidecar that exposes qBittorrent below /qbt/."""

    certificate = root / "qbitctl-compat-ca.pem"
    key = root / "qbitctl-compat-key.pem"
    config = root / "nginx.conf"
    openssl = subprocess.run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            str(key),
            "-out",
            str(certificate),
            "-days",
            "1",
            "-subj",
            "/CN=localhost",
            "-addext",
            "subjectAltName=DNS:localhost,IP:127.0.0.1",
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if openssl.returncode != 0:
        raise RuntimeError("failed to create the HTTPS test certificate: " + openssl.stderr.strip())
    config.write_text(
        f"""events {{}}
http {{
    access_log /dev/stdout;
    error_log /dev/stderr warn;
    server {{
        listen 443 ssl;
        ssl_certificate /etc/nginx/qbitctl-compat-ca.pem;
        ssl_certificate_key /etc/nginx/qbitctl-compat-key.pem;
        location = /qbt {{ return 301 /qbt/; }}
        location /qbt/ {{
            proxy_pass http://qbit:{web_port}/;
            proxy_http_version 1.1;
            # qBittorrent 5.3 validates the backend Host port before it
            # considers X-Forwarded-Host for the public reverse-proxy origin.
            proxy_set_header Host $proxy_host;
            proxy_set_header Origin $http_origin;
            proxy_set_header Referer $http_referer;
            proxy_set_header X-Forwarded-Host $http_host;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        }}
    }}
}}
""",
        encoding="utf-8",
    )
    image = os.environ.get(
        "QBITCTL_NGINX_IMAGE",
        "nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de",
    )
    result = subprocess.run(
        [
            "docker",
            "run",
            "--detach",
            "--name",
            proxy_container,
            "--network",
            network,
            "--read-only",
            "--tmpfs",
            "/var/cache/nginx",
            "--tmpfs",
            "/var/run",
            "--publish",
            f"127.0.0.1:{https_port}:443/tcp",
            "--volume",
            f"{config}:/etc/nginx/nginx.conf:ro",
            "--volume",
            f"{certificate}:/etc/nginx/qbitctl-compat-ca.pem:ro",
            "--volume",
            f"{key}:/etc/nginx/qbitctl-compat-key.pem:ro",
            image,
            "nginx",
            "-g",
            "daemon off;",
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError("failed to start nginx HTTPS sidecar: " + redact(result.stderr or result.stdout))
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", https_port), timeout=1):
                return certificate, key
        except OSError:
            time.sleep(0.25)
    raise RuntimeError("nginx HTTPS sidecar did not start listening")


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
    env_overrides: dict[str, str] | None = None,
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
            "XDG_CACHE_HOME": str(config_home.parent / "cache-home"),
            "NO_COLOR": "1",
            # Keep the compatibility container independent of host keyrings.
            "QBITCTL_MASTER_KEY": "qbitctl-compat-master",
        }
    )
    if env_overrides:
        env.update(env_overrides)
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


def list_search_plugins(
    binary: Path, url: str, password: str, workdir: Path, config_home: Path
) -> list[dict[str, object]]:
    result = run_cli(
        binary, url, password, workdir, config_home, "search", "--list-plugins", "--json"
    )
    try:
        value = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError(
            "qbitctl search --list-plugins returned invalid JSON: "
            + redact(result.stdout, password)
        ) from error
    if not isinstance(value, list) or not all(isinstance(plugin, dict) for plugin in value):
        raise RuntimeError("qbitctl search --list-plugins returned invalid plugin entries")
    return value


def search_results(
    binary: Path,
    url: str,
    password: str,
    workdir: Path,
    config_home: Path,
    plugin: str | None = None,
) -> list[dict[str, object]]:
    args = ["search", "fixture"]
    if plugin is not None:
        args.extend(("--plugin", plugin))
    args.extend(("--json", "--timeout", "60s"))
    result = run_cli(binary, url, password, workdir, config_home, *args)
    try:
        value = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError("qbitctl search returned invalid JSON") from error
    if not isinstance(value, list) or not all(isinstance(row, dict) for row in value):
        raise RuntimeError("qbitctl search returned invalid result entries")
    return value


def search_result_named(results: list[dict[str, object]], name: str) -> dict[str, object]:
    result = next((row for row in results if row.get("name") == name), None)
    if result is None:
        names = sorted(str(row.get("name")) for row in results)
        raise RuntimeError(f"qbitctl search result {name!r} was not returned: {names!r}")
    return result


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


def prepare_config(config: Path, web_port: int, proxy_subnet: str) -> None:
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
WebUI\ServerDomains="127.0.0.1;qbit"
WebUI\ReverseProxySupportEnabled=true
WebUI\TrustedReverseProxiesList={proxy_subnet}
[WebUI]
Port={web_port}
ServerDomains="127.0.0.1;qbit"
ReverseProxySupportEnabled=true
TrustedReverseProxiesList={proxy_subnet}
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
    proxy_container = f"qbitctl-compat-nginx-{os.getpid()}"
    network = f"qbitctl-compat-net-{os.getpid()}"
    web_port = reserve_port()
    https_port = reserve_port()
    with tempfile.TemporaryDirectory(prefix="qbitctl-qb-compat-") as temp:
        root = Path(temp)
        config = root / "config"
        downloads = root / "downloads"
        config_home = root / "config-home"
        downloads.mkdir()
        config_home.mkdir()
        (root / "cache-home").mkdir()
        fixture = root / "qbitctl-compat.torrent"
        search_fixture = root / "qbitctl-search.torrent"
        alt_search_fixture = root / "qbitctl-search-alt.torrent"
        invalid_fixture = root / "qbitctl-invalid.torrent"
        write_fixture(fixture)
        write_fixture(search_fixture, "qbitctl-search-fixture.bin")
        write_fixture(alt_search_fixture, "qbitctl-alt-search-fixture.bin")
        invalid_fixture.write_bytes(b"not a bencoded torrent")
        plugin = Path(__file__).with_name("fixtures") / "qbitctl_fixture.py"
        alt_plugin = Path(__file__).with_name("fixtures") / "qbitctl_fixture_alt.py"
        fixture_server = ThreadingHTTPServer(("0.0.0.0", 0), FixtureHandler)
        fixture_server.plugin = plugin
        fixture_server.alt_plugin = alt_plugin
        fixture_server.torrent = search_fixture
        fixture_server.alt_torrent = alt_search_fixture
        fixture_thread = threading.Thread(target=fixture_server.serve_forever, daemon=True)
        fixture_thread.start()
        fixture_base = f"http://host.docker.internal:{fixture_server.server_port}"

        password = ""
        previous_ssl_cert = os.environ.get("SSL_CERT_FILE")
        try:
            network_create = subprocess.run(
                ["docker", "network", "create", network],
                check=False,
                capture_output=True,
                text=True,
            )
            if network_create.returncode != 0:
                raise RuntimeError("failed to create compatibility Docker network: " + network_create.stderr.strip())
            proxy_subnet = subprocess.check_output(
                ["docker", "network", "inspect", "--format", "{{(index .IPAM.Config 0).Subnet}}", network],
                text=True,
            ).strip()
            prepare_config(config, web_port, proxy_subnet)
            start_container(
                args.image,
                container,
                config,
                downloads,
                web_port,
                network,
                fixture_base,
            )
            password = wait_for_password(container)
            url = f"http://127.0.0.1:{web_port}"
            print(f"qBittorrent {args.version}: authenticating")
            wait_for_ready(binary, url, password, root, config_home)
            certificate, _ = start_https_proxy(network, proxy_container, web_port, https_port, root)
            os.environ["SSL_CERT_FILE"] = str(certificate)
            # Completion reads the saved setup because Cobra's hidden
            # __complete command skips the normal pre-run password handling.
            run_cli(binary, url, password, root, config_home, "config", "write")
            install_search_plugin(
                url,
                password,
                fixture_base + "/qbitctl_fixture.py",
                "qbitctl_fixture",
                "Qbitctl Fixture",
            )
            install_search_plugin(
                url,
                password,
                fixture_base + "/qbitctl_fixture_alt.py",
                "qbitctl_fixture_alt",
                "Qbitctl Fixture Alt",
            )

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

            plugins = list_search_plugins(binary, url, password, root, config_home)
            expected_plugins = {
                "qbitctl_fixture": "Qbitctl Fixture",
                "qbitctl_fixture_alt": "Qbitctl Fixture Alt",
            }
            for short_name, full_name in expected_plugins.items():
                plugin = next(
                    (item for item in plugins if item.get("name") == short_name), None
                )
                if plugin is None or plugin.get("fullName") != full_name:
                    raise RuntimeError(
                        "qbitctl search plugin listing did not expose "
                        f"{short_name!r} as {full_name!r}"
                    )
                if plugin.get("enabled") is not True:
                    raise RuntimeError(f"qbitctl search plugin {short_name!r} was not enabled")

            # The default provider selection is enabled plugins only.  Disable the
            # alternate fixture to prove a disabled provider is not searched, then
            # restore it before testing explicit full-name and multi-provider input.
            set_search_plugin_enabled(
                url,
                password,
                "qbitctl_fixture_alt",
                "Qbitctl Fixture Alt",
                False,
            )
            disabled_alt = next(
                (item for item in list_search_plugins(binary, url, password, root, config_home)
                 if item.get("name") == "qbitctl_fixture_alt"),
                None,
            )
            if disabled_alt is None or disabled_alt.get("enabled") is not False:
                raise RuntimeError("qbitctl search fixture alternate plugin was not disabled")
            default_results = search_results(binary, url, password, root, config_home)
            search_result_named(default_results, "qbitctl search torrent")
            if any(
                str(row.get("name", "")).startswith("qbitctl alt search")
                for row in default_results
            ):
                raise RuntimeError("qbitctl default search included a disabled provider")
            all_results = search_results(binary, url, password, root, config_home, "all")
            search_result_named(all_results, "qbitctl search torrent")
            search_result_named(all_results, "qbitctl alt search torrent")

            explicit_short_results = search_results(
                binary, url, password, root, config_home, "qbitctl_fixture"
            )
            search_result_named(explicit_short_results, "qbitctl search torrent")

            set_search_plugin_enabled(
                url,
                password,
                "qbitctl_fixture_alt",
                "Qbitctl Fixture Alt",
                True,
            )
            explicit_full_results = search_results(
                binary, url, password, root, config_home, "Qbitctl Fixture Alt"
            )
            search_result_named(explicit_full_results, "qbitctl alt search torrent")

            multi_results = search_results(
                binary,
                url,
                password,
                root,
                config_home,
                "qbitctl_fixture|qbitctl_fixture_alt",
            )
            torrent_result = search_result_named(multi_results, "qbitctl search torrent")
            alt_result = search_result_named(multi_results, "qbitctl alt search torrent")
            if not torrent_result.get("id") or not alt_result.get("id"):
                raise RuntimeError("qbitctl did not cache both providers' search results")

            search_id = str(torrent_result["id"])
            run_cli(binary, url, password, root, config_home, "add", "--search-id", search_id)
            search_torrent = wait_for_torrent(
                binary,
                url,
                password,
                root,
                config_home,
                "qbitctl-search-fixture.bin",
            )
            if search_torrent is None or not search_torrent.get("hash"):
                raise RuntimeError("qbitctl did not add the .torrent search result")
            search_hash = str(search_torrent["hash"])

            # Both cached results came from one search. The alternate plugin
            # rejects URLs from the primary plugin, so the adds check that
            # each result retains its own downloader.
            alt_search_id = str(alt_result["id"])
            run_cli(
                binary,
                url,
                password,
                root,
                config_home,
                "add",
                "--search-id",
                alt_search_id,
            )
            alt_search_torrent = wait_for_torrent(
                binary,
                url,
                password,
                root,
                config_home,
                "qbitctl-alt-search-fixture.bin",
            )
            if alt_search_torrent is None or not alt_search_torrent.get("hash"):
                raise RuntimeError("qbitctl did not add the alternate search result")
            alt_search_hash = str(alt_search_torrent["hash"])

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

            https_url = f"https://127.0.0.1:{https_port}/qbt"
            https_env = {"SSL_CERT_FILE": str(certificate)}
            wait_for_ready(
                binary,
                https_url,
                password,
                root,
                config_home,
                timeout=30,
            )
            https_torrents = list_torrents(binary, https_url, password, root, config_home)
            if not any(item.get("hash") == torrent_hash for item in https_torrents):
                raise RuntimeError("HTTPS path did not expose the existing torrent list")
            run_cli(
                binary,
                https_url,
                password,
                root,
                config_home,
                "set",
                "tags",
                "qbitctl-https",
                torrent_hash,
                env_overrides=https_env,
            )
            wait_for_tags_and_limits(
                binary,
                https_url,
                password,
                root,
                config_home,
                "qbitctl-compat-fixture.bin",
                {"qbitctl-https"},
                2,
                1.5,
            )
            run_cli(
                binary,
                https_url,
                password,
                root,
                config_home,
                "delete",
                torrent_hash,
                env_overrides=https_env,
            )
            wait_for_torrent(
                binary,
                https_url,
                password,
                root,
                config_home,
                "qbitctl-compat-fixture.bin",
                present=False,
            )

            for search_hash_value, search_name in (
                (search_hash, "qbitctl-search-fixture.bin"),
                (alt_search_hash, "qbitctl-alt-search-fixture.bin"),
            ):
                run_cli(binary, url, password, root, config_home, "delete", search_hash_value)
                wait_for_torrent(
                    binary,
                    url,
                    password,
                    root,
                    config_home,
                    search_name,
                    present=False,
                )
            print(f"qBittorrent {args.version}: compatibility checks passed")
        except Exception:
            logs = redact(docker_logs(container), password).strip()
            if logs:
                print("qBittorrent container logs:\n" + logs, file=sys.stderr)
            proxy_logs = redact(docker_logs(proxy_container), password).strip()
            if proxy_logs:
                print("nginx sidecar logs:\n" + proxy_logs, file=sys.stderr)
            raise
        finally:
            if previous_ssl_cert is None:
                os.environ.pop("SSL_CERT_FILE", None)
            else:
                os.environ["SSL_CERT_FILE"] = previous_ssl_cert
            subprocess.run(
                ["docker", "rm", "--force", proxy_container],
                check=False,
                capture_output=True,
                text=True,
            )
            subprocess.run(
                ["docker", "rm", "--force", container],
                check=False,
                capture_output=True,
                text=True,
            )
            subprocess.run(
                ["docker", "network", "rm", network],
                check=False,
                capture_output=True,
                text=True,
            )
            fixture_server.shutdown()
            fixture_thread.join(timeout=5)


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
