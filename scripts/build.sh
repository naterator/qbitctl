#!/usr/bin/env bash
set -euo pipefail

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

if [[ "${1:-}" == "--list-targets" ]]; then
  printf '%s\n' "${release_targets[@]}"
  exit 0
fi

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
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$asset"
    else
      shasum -a 256 "$asset"
    fi > "$asset.sha256"
  )
done <<< "$targets"
