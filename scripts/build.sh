#!/usr/bin/env bash
set -euo pipefail

output_dir="${1:-dist}"
version="${VERSION:-dev}"
mkdir -p "$output_dir"

# Follow the installed Go toolchain's supported desktop/server targets.
# Android and iOS require platform SDKs and CGO rather than pure Go builds.
targets=$(go tool dist list | awk -F/ '$1 != "android" && $1 != "ios" && ($2 == "amd64" || $2 == "arm64")')
test -n "$targets"
while IFS=/ read -r goos goarch; do
  asset="qbitctl-$goos-$goarch"
  if [[ "$goos" == windows ]]; then
    asset+=".exe"
  fi
  printf 'Building %s/%s\n' "$goos" "$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X github.com/naterator/qbitctl/pkg/client.Version=$version" \
    -o "$output_dir/$asset" ./cmd/qbitctl
  (
    cd "$output_dir"
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$asset"
    else
      shasum -a 256 "$asset"
    fi > "$asset.sha256"
  )
done <<< "$targets"
