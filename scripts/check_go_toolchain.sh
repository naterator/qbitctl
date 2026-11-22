#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
go_version=$(awk '$1 == "go" {print $2; exit}' "$root/go.mod")
case "$go_version" in
	[0-9]*.[0-9]*.[0-9]*) ;;
	*) echo "go.mod has no patch-level Go directive: $go_version" >&2; exit 1 ;;
esac

feed=$(curl -fsSL --retry 3 'https://go.dev/dl/?mode=json')
latest=$(printf '%s' "$feed" | python3 -c '
import json, sys
minor = tuple(map(int, sys.argv[1].split(".")[:2]))
for release in json.load(sys.stdin):
    if not release.get("stable"):
        continue
    version = release["version"].removeprefix("go")
    if tuple(map(int, version.split(".")[:2])) == minor:
        print(version)
        break
' "$go_version")

if [ -z "$latest" ]; then
	echo "no stable Go release found for minor line $go_version" >&2
	exit 1
fi
if [ "$latest" != "$go_version" ]; then
	echo "Go $latest is available for the $go_version minor line (go.mod has $go_version)" >&2
	exit 1
fi
echo "Go $go_version is current"
