#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=release_assets.sh
. "$script_dir/release_assets.sh"
dist_dir=${1:-dist}

fail() {
  printf 'release format check failed: %s\n' "$1" >&2
  exit 1
}

matches_format() {
  local target=$1 description=$2
  case "$target" in
    darwin/amd64) [[ "$description" == *'mach-o 64-bit'* && "$description" == *'x86_64'* ]] ;;
    darwin/arm64) [[ "$description" == *'mach-o 64-bit'* && "$description" == *'arm64'* ]] ;;
    linux/amd64|dragonfly/amd64|freebsd/amd64|illumos/amd64|netbsd/amd64|openbsd/amd64|solaris/amd64)
      [[ "$description" == *'elf 64-bit'* && "$description" == *'x86-64'* ]] ;;
    linux/arm) [[ "$description" == *'elf 32-bit'* && "$description" == *'arm'* ]] ;;
    linux/arm64|freebsd/arm64|netbsd/arm64|openbsd/arm64)
      [[ "$description" == *'elf 64-bit'* && ("$description" == *'aarch64'* || "$description" == *'arm64'*) ]] ;;
    plan9/amd64) return 0 ;;
    windows/amd64) [[ "$description" == *'pe32+'* && "$description" == *'x86-64'* ]] ;;
    windows/arm64) [[ "$description" == *'pe32+'* && ("$description" == *'arm64'* || "$description" == *'aarch64'*) ]] ;;
    *) return 2 ;;
  esac
}

while IFS= read -r target; do
  raw=$(release_target_asset "$target")
  path="$dist_dir/$raw"
  [[ -f "$path" ]] || fail "missing $raw"
  description=$(file -b "$path" | tr '[:upper:]' '[:lower:]')
  if [[ "$target" == plan9/amd64 ]]; then
    # Go's amd64 Plan 9 header is the big-endian 0x8a97 magic. Some versions
    # of file(1), including the macOS one, report this valid format as data.
    magic=$(od -An -tx1 -N4 "$path" | tr -d '[:space:]')
    [[ "$magic" == 00008a97 ]] || fail "$raw has Plan 9 amd64 magic $magic"
  elif ! matches_format "$target" "$description"; then
    fail "$raw reported '$description'"
  fi
done < <(bash "$script_dir/build.sh" --list-targets)

echo 'release binary formats passed'
