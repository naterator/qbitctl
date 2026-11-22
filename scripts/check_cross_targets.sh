#!/usr/bin/env bash
# Type-check qbitctl for every release operating system without linking.
#
# CI runs this instead of building all release binaries: `go vet` compiles
# each OS's build-tagged files, so a platform-specific break still fails CI,
# while the full cross-build runs only in the release workflow. One target per
# GOOS is checked, plus every target whose architecture is not amd64 or arm64
# (currently 32-bit linux/arm), since those can differ in int size.
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)

targets=()
seen_goos=" "
while IFS=/ read -r goos goarch; do
  if [[ "$seen_goos" != *" $goos "* ]]; then
    targets+=("$goos/$goarch")
    seen_goos+="$goos "
  elif [[ "$goarch" != amd64 && "$goarch" != arm64 ]]; then
    targets+=("$goos/$goarch")
  fi
done < <(bash "$script_dir/build.sh" --list-targets)

failed=0
for target in "${targets[@]}"; do
  goos=${target%/*}
  goarch=${target#*/}
  env_vars=(GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0)
  if [[ "$goarch" == arm ]]; then
    env_vars+=(GOARM=7)
  fi
  printf 'Checking %s\n' "$target"
  if ! env "${env_vars[@]}" go vet ./...; then
    printf 'type-check failed for %s\n' "$target" >&2
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi
printf 'type-checked %d release targets\n' "${#targets[@]}"
