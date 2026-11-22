#!/bin/sh
set -eu

script_dir=$(cd "$(dirname "$0")" && pwd)
tmp_parent=${TMPDIR:-/tmp}
tmp_dir=$(mktemp -d "$tmp_parent/qbitctl-install-test.XXXXXX")
server_pid=

cleanup() {
	if [ -n "$server_pid" ]; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	rm -rf "$tmp_dir"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

mkdir "$tmp_dir/server" "$tmp_dir/bin"
awk -v start="bash <<'QBITCTL_INSTALL'" '
	$0 == start { copy = 1 }
	copy { print }
	copy && $0 == "QBITCTL_INSTALL" { exit }
' "$script_dir/../README.md" > "$tmp_dir/readme-install.sh"
sed '1d;$d' "$tmp_dir/readme-install.sh" > "$tmp_dir/readme-body.sh"
sed '1d' "$script_dir/install-snippet.sh" > "$tmp_dir/script-body.sh"
if ! cmp -s "$tmp_dir/readme-body.sh" "$tmp_dir/script-body.sh"; then
	echo "README install example differs from the checked script" >&2
	exit 1
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
machine=$(uname -m)
case "$os:$machine" in
	darwin:x86_64|darwin:amd64) asset=qbitctl-darwin-amd64 ;;
	darwin:arm64|darwin:aarch64) asset=qbitctl-darwin-arm64 ;;
	linux:x86_64|linux:amd64) asset=qbitctl-linux-amd64 ;;
	linux:arm64|linux:aarch64) asset=qbitctl-linux-arm64 ;;
	*)
		echo "unsupported test platform: $os/$machine" >&2
		exit 1
		;;
esac
printf 'fixture\n' > "$tmp_dir/server/$asset"

if command -v sha256sum >/dev/null 2>&1; then
	checksum=$(sha256sum "$tmp_dir/server/$asset" | awk '{print $1}')
else
	checksum=$(shasum -a 256 "$tmp_dir/server/$asset" | awk '{print $1}')
fi
printf '%s  %s\n' "$checksum" "$asset" \
	> "$tmp_dir/server/$asset.sha256"

port=$(python3 -c '
import socket

with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
')
python3 -m http.server "$port" --bind 127.0.0.1 \
	--directory "$tmp_dir/server" >/dev/null 2>&1 &
server_pid=$!

attempt=0
until curl --fail --silent --show-error \
	"http://127.0.0.1:$port/$asset" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 20 ]; then
		echo "local HTTP server did not become ready" >&2
		exit 1
	fi
	sleep 0.1
done

download_base="http://127.0.0.1:$port"
target="$tmp_dir/bin/qbitctl"
QBITCTL_DOWNLOAD_BASE="$download_base" \
	QBITCTL_INSTALL_PATH="$target" \
	bash "$script_dir/install-snippet.sh"
if ! cmp -s "$tmp_dir/server/$asset" "$target"; then
	echo "installer did not install the verified asset" >&2
	exit 1
fi

printf '%064d  %s\n' 0 "$asset" > "$tmp_dir/server/$asset.sha256"
cp "$target" "$tmp_dir/verified-asset"
printf 'invalid fixture\n' > "$tmp_dir/server/$asset"
if QBITCTL_DOWNLOAD_BASE="$download_base" \
	QBITCTL_INSTALL_PATH="$target" \
	bash "$tmp_dir/readme-install.sh"; then
	echo "installer accepted an invalid checksum" >&2
	exit 1
fi
if ! cmp -s "$tmp_dir/verified-asset" "$target"; then
	echo "invalid checksum changed the installed asset" >&2
	exit 1
fi

cp "$tmp_dir/verified-asset" "$tmp_dir/server/$asset"
printf '%s  %s\n%064d  extra\n' "$checksum" "$asset" 0 \
	> "$tmp_dir/server/$asset.sha256"
if QBITCTL_DOWNLOAD_BASE="$download_base" \
	QBITCTL_INSTALL_PATH="$target" \
	bash "$script_dir/install-snippet.sh"; then
	echo "installer accepted extra checksum records" >&2
	exit 1
fi
if ! cmp -s "$tmp_dir/server/$asset" "$target"; then
	echo "extra checksum records changed the installed asset" >&2
	exit 1
fi

echo "install snippet checksum failure is fail-closed"
