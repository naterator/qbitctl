#!/usr/bin/env bash
set -euo pipefail

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|dragonfly|freebsd|illumos|linux|netbsd|openbsd|solaris) ;;
  *)
    printf 'unsupported operating system: %s\n' "$os" >&2
    exit 1
    ;;
esac

machine=$(uname -m)
case "$machine" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7|armv7l) arch=arm ;;
  *)
    printf 'unsupported architecture: %s\n' "$machine" >&2
    exit 1
    ;;
esac

asset="qbitctl-$os-$arch"
base="${QBITCTL_DOWNLOAD_BASE:-https://github.com/naterator/qbitctl/releases/latest/download}"
base=${base%/}
target="${QBITCTL_INSTALL_PATH:-/usr/local/bin/qbitctl}"
checksum_asset="$asset.sha256"

tmp_parent=${TMPDIR:-/tmp}
tmp_dir=$(mktemp -d "$tmp_parent/qbitctl-install.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT

binary_path="$tmp_dir/$asset"
checksum_path="$tmp_dir/$checksum_asset"
curl --fail --location --retry 3 --retry-delay 1 --silent --show-error \
  --output "$binary_path" "$base/$asset"
curl --fail --location --retry 3 --retry-delay 1 --silent --show-error \
  --output "$checksum_path" "$base/$checksum_asset"

# Check that the downloaded checksum names this exact asset before invoking
# the platform checksum tool, so a malformed file cannot name another path.
if ! LC_ALL=C awk -v asset="$asset" '
  NF == 0 { next }
  {
    records++
    if (NF == 2 && length($1) == 64 && $1 !~ /[^[:xdigit:]]/ && $2 == asset) {
      matching++
    }
  }
  END { exit records == 1 && matching == 1 ? 0 : 1 }
' "$checksum_path"; then
  printf 'checksum file does not name %s\n' "$asset" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp_dir" && sha256sum -c "$checksum_asset")
elif command -v shasum >/dev/null 2>&1; then
  (cd "$tmp_dir" && shasum -a 256 -c "$checksum_asset")
else
  printf 'neither sha256sum nor shasum is available\n' >&2
  exit 1
fi

chmod 0755 "$binary_path"
target_dir=$(dirname "$target")
if [[ -d "$target_dir" && -w "$target_dir" ]]; then
  install -m 0755 "$binary_path" "$target"
else
  sudo install -m 0755 "$binary_path" "$target"
fi
printf 'installed %s to %s\n' "$asset" "$target"
