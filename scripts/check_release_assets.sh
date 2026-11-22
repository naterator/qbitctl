#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=release_assets.sh
. "$script_dir/release_assets.sh"

dist_dir=${1:-dist}
if [[ ! -d "$dist_dir" ]]; then
  printf 'release asset directory does not exist: %s\n' "$dist_dir" >&2
  exit 1
fi

fail() {
  printf 'release asset check failed: %s\n' "$1" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

expected_assets=()
targets_file=$(mktemp)
trap 'rm -f "$targets_file"' EXIT HUP INT TERM
bash "$script_dir/build.sh" --list-targets > "$targets_file"
while IFS= read -r target; do
  while IFS= read -r asset; do
    expected_assets+=("$asset")
  done < <(release_target_assets "$target")
done < "$targets_file"

expected_count=${#expected_assets[@]}
if [[ "$expected_count" -ne 88 ]]; then
  fail "expected today's release matrix to contain 88 files, found $expected_count"
fi

contains_expected() {
  local candidate=$1 expected
  for expected in "${expected_assets[@]}"; do
    if [[ "$expected" == "$candidate" ]]; then
      return 0
    fi
  done
  return 1
}

shopt -s nullglob dotglob
entries=("$dist_dir"/*)
for entry in "${entries[@]}"; do
  if [[ ! -f "$entry" || -L "$entry" ]]; then
    fail "unexpected non-regular entry $entry"
  fi
  name=${entry##*/}
  if ! contains_expected "$name"; then
    fail "unexpected file $name"
  fi
done
for expected in "${expected_assets[@]}"; do
  [[ -f "$dist_dir/$expected" && ! -L "$dist_dir/$expected" ]] ||
    fail "missing file $expected"
done
if [[ "${#entries[@]}" -ne "$expected_count" ]]; then
  fail "expected $expected_count files, found ${#entries[@]}"
fi

verify_checksum() {
  local asset=$1
  local checksum="$dist_dir/$asset.sha256"
  local line_count line digest named actual
  [[ -f "$dist_dir/$asset" ]] || fail "missing asset $asset"
  [[ -f "$checksum" ]] || fail "missing checksum $asset.sha256"
  line_count=$(wc -l < "$checksum" | tr -d ' ')
  [[ "$line_count" -eq 1 ]] || fail "$asset.sha256 must contain exactly one record"
  line=$(<"$checksum")
  if [[ ! "$line" =~ ^([[:xdigit:]]{64})[[:space:]]+(\*?[^[:space:]]+)[[:space:]]*$ ]]; then
    fail "$asset.sha256 is not a single SHA-256 record"
  fi
  digest=$(printf '%s' "${BASH_REMATCH[1]}" | tr '[:upper:]' '[:lower:]')
  named=${BASH_REMATCH[2]#\*}
  [[ "$named" == "$asset" ]] || fail "$asset.sha256 names $named"
  actual=$(sha256_file "$dist_dir/$asset")
  [[ "$actual" == "$digest" ]] || fail "$asset.sha256 does not match $asset"
}

verify_archive() {
  local target=$1
  local goos=${target%%/*}
  local raw archive
  raw=$(release_target_asset "$target")
  if [[ "$goos" == windows ]]; then
    archive="$dist_dir/qbitctl-$goos-${target##*/}.zip"
    local entries
    entries=$(unzip -Z1 "$archive" 2>/dev/null) || fail "unable to inspect $archive"
    [[ "$entries" == qbitctl.exe ]] || fail "$archive must contain only qbitctl.exe"
    unzip -p "$archive" qbitctl.exe | cmp -s - "$dist_dir/$raw" ||
      fail "$archive does not unpack to $raw"
    return
  fi
  archive="$dist_dir/$raw.gz"
  gzip -dc "$archive" | cmp -s - "$dist_dir/$raw" ||
    fail "$raw.gz does not unpack to $raw"
  if release_target_has_xz "$goos"; then
    archive="$dist_dir/$raw.xz"
    xz -dc "$archive" | cmp -s - "$dist_dir/$raw" ||
      fail "$raw.xz does not unpack to $raw"
  fi
}

while IFS= read -r target; do
  raw=$(release_target_asset "$target")
  verify_checksum "$raw"
  if [[ "$target" == windows/* ]]; then
    archive="qbitctl-windows-${target##*/}.zip"
    verify_checksum "$archive"
  else
    archive="$raw.gz"
    verify_checksum "$archive"
    if release_target_has_xz "${target%%/*}"; then
      archive="$raw.xz"
      verify_checksum "$archive"
    fi
  fi
  verify_archive "$target"
done < "$targets_file"

printf 'release asset contract passed (%s files)\n' "$expected_count"
