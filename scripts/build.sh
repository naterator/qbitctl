#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=release_assets.sh
. "$script_dir/release_assets.sh"

if [[ "${1:-}" == "--list-targets" ]]; then
  printf '%s\n' "${release_targets[@]}"
  exit 0
fi

for tool in gzip xz zip unzip; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'release builds need %s to write compressed assets\n' "$tool" >&2
    exit 1
  fi
done

write_checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1"
  else
    shasum -a 256 "$1"
  fi > "$1.sha256"
}

# Write compressed copies of one built binary, each with its own checksum,
# and confirm every archive unpacks to the exact binary that was built.
# gzip -n, xz -T1, and a fixed zip timestamp keep the archives reproducible.
compress_asset() {
  local goos=$1 goarch=$2 asset=$3
  if [[ "$goos" == windows ]]; then
    local archive="qbitctl-$goos-$goarch.zip" stage
    stage=$(mktemp -d)
    cp "$asset" "$stage/qbitctl.exe"
    TZ=UTC touch -t 198001010000 "$stage/qbitctl.exe"
    rm -f "$archive"
    (cd "$stage" && TZ=UTC zip -q -X -9 "$OLDPWD/$archive" qbitctl.exe)
    rm -rf "$stage"
    unzip -p "$archive" qbitctl.exe | cmp -s - "$asset" ||
      { printf 'zip round trip failed for %s\n' "$archive" >&2; exit 1; }
    write_checksum "$archive"
    return
  fi
  gzip -9 -n -c "$asset" > "$asset.gz"
  gzip -dc "$asset.gz" | cmp -s - "$asset" ||
    { printf 'gzip round trip failed for %s\n' "$asset.gz" >&2; exit 1; }
  write_checksum "$asset.gz"
  if release_target_has_xz "$goos"; then
    xz -9 -T1 -c "$asset" > "$asset.xz"
    xz -dc "$asset.xz" | cmp -s - "$asset" ||
      { printf 'xz round trip failed for %s\n' "$asset.xz" >&2; exit 1; }
    write_checksum "$asset.xz"
  fi
}

output_dir="${1:-dist}"
version="${VERSION:-dev}"
mkdir -p "$output_dir"

# Keep the published release matrix stable, but fail loudly if the selected
# Go toolchain can no longer build one of its explicitly supported targets.
supported_targets=$(go tool dist list)
for target in "${release_targets[@]}"; do
  if ! grep -Fqx "$target" <<< "$supported_targets"; then
    printf 'release target is not supported by this Go toolchain: %s\n' "$target" >&2
    exit 1
  fi
done

targets=$(printf '%s\n' "${release_targets[@]}")
while IFS=/ read -r goos goarch; do
  asset="qbitctl-$goos-$goarch"
  if [[ "$goos" == windows ]]; then
    asset+=".exe"
  fi
  printf 'Building %s/%s\n' "$goos" "$goarch"
  build_env=(env CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch")
  if [[ "$goos/$goarch" == linux/arm ]]; then
    build_env+=(GOARM=7)
  fi
  "${build_env[@]}" go build -trimpath \
    -ldflags "-s -w -X github.com/naterator/qbitctl/pkg/client.Version=$version" \
    -o "$output_dir/$asset" ./cmd/qbitctl
  (
    cd "$output_dir"
    write_checksum "$asset"
    compress_asset "$goos" "$goarch" "$asset"
  )
done <<< "$targets"
