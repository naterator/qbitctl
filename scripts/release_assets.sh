#!/usr/bin/env bash

# Keep the release target matrix and archive policy in one place. This file is
# sourced by build.sh and by the release asset contract checker.

release_targets=(
  darwin/amd64
  darwin/arm64
  dragonfly/amd64
  freebsd/amd64
  freebsd/arm64
  illumos/amd64
  linux/amd64
  linux/arm
  linux/arm64
  netbsd/amd64
  netbsd/arm64
  openbsd/amd64
  openbsd/arm64
  plan9/amd64
  solaris/amd64
  windows/amd64
  windows/arm64
)

# These operating systems ship xz in their base installation. They receive a
# .xz asset in addition to the .gz asset used by every non-Windows target.
release_xz_operating_systems=(
  linux
  freebsd
  netbsd
  dragonfly
  illumos
  solaris
)

release_target_asset() {
  local target=$1
  local goos=${target%%/*}
  local goarch=${target##*/}
  local suffix=
  if [[ "$goos" == windows ]]; then
    suffix=.exe
  fi
  printf 'qbitctl-%s-%s%s\n' "$goos" "$goarch" "$suffix"
}

release_target_has_xz() {
  local goos=$1
  local candidate
  for candidate in "${release_xz_operating_systems[@]}"; do
    if [[ "$candidate" == "$goos" ]]; then
      return 0
    fi
  done
  return 1
}

release_target_assets() {
  local target=$1
  local goos=${target%%/*}
  local raw
  raw=$(release_target_asset "$target")
  printf '%s\n' "$raw" "$raw.sha256"
  if [[ "$goos" == windows ]]; then
    printf 'qbitctl-%s-%s.zip\n' "$goos" "${target##*/}"
    printf 'qbitctl-%s-%s.zip.sha256\n' "$goos" "${target##*/}"
    return
  fi
  printf '%s.gz\n' "$raw"
  printf '%s.gz.sha256\n' "$raw"
  if release_target_has_xz "$goos"; then
    printf '%s.xz\n' "$raw"
    printf '%s.xz.sha256\n' "$raw"
  fi
}

release_expected_asset_count() {
  local target count=0
  for target in "${release_targets[@]}"; do
    count=$((count + $(release_target_assets "$target" | wc -l)))
  done
  printf '%s\n' "$count"
}
