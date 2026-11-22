#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/qbitctl-hygiene.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

git -C "$tmp" init -q
git -C "$tmp" config user.email test@example.com
git -C "$tmp" config user.name "Test User"
git -C "$tmp" config commit.gpgsign false
git -C "$tmp" commit --allow-empty -q -m $'ordinary contributor\n\nCo-Authored-By: Example <person@gmail.com>'
(cd "$tmp" && "$script_dir/check_commit_hygiene.sh" --range HEAD)

git -C "$tmp" commit --allow-empty -q -m $'generated change\n\nGenerated with Codex'
if (cd "$tmp" && "$script_dir/check_commit_hygiene.sh" --range HEAD); then
	echo "commit hygiene test: AI attribution was accepted" >&2
	exit 1
fi

echo "commit hygiene tests passed"
