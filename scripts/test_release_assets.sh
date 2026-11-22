#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=release_assets.sh
. "$script_dir/release_assets.sh"
checker="$script_dir/check_release_assets.sh"
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT HUP INT TERM

write_checksum() {
  local directory=${1%/*}
  local name=${1##*/}
  (cd "$directory" && sha256sum "$name" > "$name.sha256")
}

make_fixture() {
  rm -rf "$fixture"
  mkdir -p "$fixture"
  while IFS= read -r target; do
    local goos=${target%%/*}
    local goarch=${target##*/}
    local raw
    raw=$(release_target_asset "$target")
    printf 'qbitctl fixture %s\n' "$target" > "$fixture/$raw"
    write_checksum "$fixture/$raw"
    if [[ "$goos" != windows ]]; then
      gzip -9 -n -c "$fixture/$raw" > "$fixture/$raw.gz"
      write_checksum "$fixture/$raw.gz"
      if release_target_has_xz "$goos"; then
        xz -9 -T1 -c "$fixture/$raw" > "$fixture/$raw.xz"
        write_checksum "$fixture/$raw.xz"
      fi
    fi
    if [[ "$goos" == windows ]]; then
      local stage archive
      stage=$(mktemp -d)
      archive="$fixture/qbitctl-$goos-$goarch.zip"
      cp "$fixture/$raw" "$stage/qbitctl.exe"
      (cd "$stage" && zip -q -X "$archive" qbitctl.exe)
      rm -rf "$stage"
      write_checksum "$archive"
    fi
  done < <(bash "$script_dir/build.sh" --list-targets)
}

expect_failure() {
  local expected=$1 output
  if output=$(bash "$checker" "$fixture" 2>&1); then
    printf 'expected release asset check to fail for %s\n' "$expected" >&2
    exit 1
  fi
  case "$output" in
    *"$expected"*) ;;
    *)
      printf 'failure for %s did not name the broken asset:\n%s\n' "$expected" "$output" >&2
      exit 1
      ;;
  esac
}

make_fixture
bash "$checker" "$fixture" | grep -Fq 'release asset contract passed (88 files)'

make_fixture
rm "$fixture/qbitctl-linux-amd64.xz" "$fixture/qbitctl-linux-amd64.xz.sha256"
expect_failure 'qbitctl-linux-amd64.xz'

make_fixture
printf 'unexpected\n' > "$fixture/stray.txt"
expect_failure 'stray.txt'

make_fixture
printf '%064d  qbitctl-linux-amd64\n' 0 > "$fixture/qbitctl-linux-amd64.sha256"
expect_failure 'qbitctl-linux-amd64.sha256'

make_fixture
printf 'corrupt archive\n' > "$fixture/qbitctl-linux-amd64.gz"
write_checksum "$fixture/qbitctl-linux-amd64.gz"
expect_failure 'qbitctl-linux-amd64.gz'

make_fixture
stage=$(mktemp -d)
cp "$fixture/qbitctl-windows-amd64.exe" "$stage/qbitctl.exe"
printf 'extra entry\n' > "$stage/extra.txt"
(cd "$stage" && zip -q -X "$fixture/qbitctl-windows-amd64.zip" qbitctl.exe extra.txt)
rm -rf "$stage"
write_checksum "$fixture/qbitctl-windows-amd64.zip"
expect_failure 'qbitctl-windows-amd64.zip'

echo 'release asset checker regression tests passed'
