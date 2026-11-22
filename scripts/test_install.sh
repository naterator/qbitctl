#!/bin/sh
# Exercise scripts/install.sh against a local release server in a throwaway
# HOME: install location, PATH edits, version pinning, and fail-closed
# checksum handling.
set -eu

script_dir=$(cd "$(dirname "$0")" && pwd)
installer="$script_dir/install.sh"
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

fail() {
	echo "install test: $*" >&2
	exit 1
}

if [ "$(id -u)" = 0 ]; then
	fail "run as a regular user; root always installs to the system directory"
fi

expected_line='curl -fsSL https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh | bash'
grep -qxF "$expected_line" "$script_dir/../README.md" ||
	fail "README does not show the one-line install command"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
machine=$(uname -m)
case "$os:$machine" in
	darwin:x86_64|darwin:amd64) asset=qbitctl-darwin-amd64 ;;
	darwin:arm64|darwin:aarch64) asset=qbitctl-darwin-arm64 ;;
	linux:x86_64|linux:amd64) asset=qbitctl-linux-amd64 ;;
	linux:arm64|linux:aarch64) asset=qbitctl-linux-arm64 ;;
	*) fail "unsupported test platform: $os/$machine" ;;
esac

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

# Publish one asset under a release download directory.
publish() {
	release_dir=$1
	content=$2
	mkdir -p "$release_dir"
	printf '%s\n' "$content" > "$release_dir/$asset"
	printf '%s  %s\n' "$(sha256 "$release_dir/$asset")" "$asset" > "$release_dir/$asset.sha256"
}

server="$tmp_dir/server"
publish "$server/latest/download" "latest fixture"
publish "$server/download/1.2.3" "pinned fixture"

port=$(python3 -c '
import socket

with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
')
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$server" >/dev/null 2>&1 &
server_pid=$!
attempt=0
until curl --fail --silent "http://127.0.0.1:$port/latest/download/$asset" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	[ "$attempt" -lt 50 ] || fail "local HTTP server did not become ready"
	sleep 0.1
done
releases="http://127.0.0.1:$port"

# Run the installer as a user would: piped into bash, with a clean HOME,
# a minimal PATH, and a missing stand-in for /usr/local/bin.
run_installer() {
	home=$1
	shift
	mkdir -p "$home"
	env -i HOME="$home" PATH="/usr/bin:/bin:/usr/sbin:/sbin" TMPDIR="$tmp_dir" \
		QBITCTL_RELEASES_URL="$releases" QBITCTL_SYSTEM_BIN_DIR="$tmp_dir/no-system-bin" \
		"$@" bash < "$installer"
}

assert_installed() {
	target=$1
	content=$2
	[ -x "$target" ] || fail "$target was not installed as an executable"
	[ "$(cat "$target")" = "$content" ] || fail "$target does not contain the $content"
}

# 1. Default per-user install for zsh, with an idempotent PATH edit.
home="$tmp_dir/home-zsh"
run_installer "$home" SHELL=/bin/zsh > "$tmp_dir/out" 2>&1 || { cat "$tmp_dir/out"; fail "zsh install failed"; }
assert_installed "$home/.local/bin/qbitctl" "latest fixture"
# shellcheck disable=SC2016 # Match the literal $HOME line the installer writes.
grep -qxF 'export PATH="$HOME/.local/bin:$PATH"' "$home/.zshrc" || fail ".zshrc does not add ~/.local/bin to PATH"
run_installer "$home" SHELL=/bin/zsh > "$tmp_dir/out" 2>&1 || fail "zsh reinstall failed"
[ "$(grep -c '# Added by the qbitctl installer' "$home/.zshrc")" = 1 ] || fail "reinstall duplicated the PATH edit"

# 2. bash uses .bash_profile on macOS and .bashrc elsewhere.
home="$tmp_dir/home-bash"
run_installer "$home" SHELL=/bin/bash > "$tmp_dir/out" 2>&1 || fail "bash install failed"
bash_file="$home/.bashrc"
[ "$os" = darwin ] && bash_file="$home/.bash_profile"
grep -qF '.local/bin' "$bash_file" || fail "$bash_file does not add ~/.local/bin to PATH"

# 3. fish gets a conf.d snippet.
home="$tmp_dir/home-fish"
run_installer "$home" SHELL=/usr/bin/fish > "$tmp_dir/out" 2>&1 || fail "fish install failed"
# shellcheck disable=SC2016 # Match the literal $HOME line the installer writes.
grep -qxF 'fish_add_path -g "$HOME/.local/bin"' "$home/.config/fish/conf.d/qbitctl.fish" ||
	fail "fish conf.d snippet is missing"

# 4. QBITCTL_NO_MODIFY_PATH leaves startup files alone.
home="$tmp_dir/home-nopath"
run_installer "$home" SHELL=/bin/zsh QBITCTL_NO_MODIFY_PATH=1 > "$tmp_dir/out" 2>&1 || fail "no-path install failed"
[ ! -e "$home/.zshrc" ] || fail "QBITCTL_NO_MODIFY_PATH still edited .zshrc"
grep -qF 'is not on PATH' "$tmp_dir/out" || fail "installer did not explain the PATH step"

# 5. A writable system directory is used without touching startup files.
home="$tmp_dir/home-system"
system_bin="$tmp_dir/system-bin"
mkdir -p "$system_bin"
mkdir -p "$home"
env -i HOME="$home" PATH="$system_bin:/usr/bin:/bin" TMPDIR="$tmp_dir" SHELL=/bin/zsh \
	QBITCTL_RELEASES_URL="$releases" QBITCTL_SYSTEM_BIN_DIR="$system_bin" \
	bash < "$installer" > "$tmp_dir/out" 2>&1 || fail "system-directory install failed"
assert_installed "$system_bin/qbitctl" "latest fixture"
[ ! -e "$home/.zshrc" ] || fail "system-directory install edited .zshrc"

# 6. QBITCTL_VERSION and QBITCTL_INSTALL_DIR select a pinned release and location.
home="$tmp_dir/home-pinned"
custom="$tmp_dir/custom bin"
run_installer "$home" SHELL=/bin/zsh QBITCTL_VERSION=1.2.3 QBITCTL_INSTALL_DIR="$custom" QBITCTL_NO_MODIFY_PATH=1 \
	> "$tmp_dir/out" 2>&1 || { cat "$tmp_dir/out"; fail "pinned install failed"; }
assert_installed "$custom/qbitctl" "pinned fixture"
if run_installer "$home" QBITCTL_VERSION='../../etc' > "$tmp_dir/out" 2>&1; then
	fail "installer accepted a malformed QBITCTL_VERSION"
fi

# 7. Checksum failures are fail-closed and leave the existing binary alone.
home="$tmp_dir/home-zsh"
target="$home/.local/bin/qbitctl"
cp "$target" "$tmp_dir/verified"
latest="$server/latest/download"
good_sum=$(cat "$latest/$asset.sha256")

printf '%064d  %s\n' 0 "$asset" > "$latest/$asset.sha256"
if run_installer "$home" SHELL=/bin/zsh > "$tmp_dir/out" 2>&1; then
	fail "installer accepted a checksum mismatch"
fi
cmp -s "$tmp_dir/verified" "$target" || fail "a checksum mismatch replaced the installed binary"

printf '%s\n%064d  extra\n' "$good_sum" 0 > "$latest/$asset.sha256"
if run_installer "$home" SHELL=/bin/zsh > "$tmp_dir/out" 2>&1; then
	fail "installer accepted extra checksum records"
fi
cmp -s "$tmp_dir/verified" "$target" || fail "extra checksum records replaced the installed binary"

rm "$latest/$asset.sha256"
if run_installer "$home" SHELL=/bin/zsh > "$tmp_dir/out" 2>&1; then
	fail "installer succeeded without a checksum file"
fi
cmp -s "$tmp_dir/verified" "$target" || fail "a missing checksum replaced the installed binary"

echo "installer tests passed"
