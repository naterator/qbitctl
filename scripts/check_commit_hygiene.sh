#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat >&2 <<'EOF'
usage: check_commit_hygiene.sh [--range RANGE] [--require-verified]

Checks commit messages for generated or AI attribution. With
--require-verified, it also asks GitHub whether each commit is verified.
EOF
}

commit_range=
require_verified=0
while [ "$#" -gt 0 ]; do
	case "$1" in
		--range)
			[ "$#" -ge 2 ] || { usage; exit 2; }
			commit_range=$2
			shift 2
			;;
		--require-verified)
			require_verified=1
			shift
			;;
		*) usage; exit 2 ;;
	esac
done

if [ "${REQUIRE_VERIFIED_COMMITS:-0}" = 1 ]; then
	require_verified=1
fi

if [ -z "$commit_range" ]; then
	if [ -n "${GITHUB_BASE_SHA:-}" ] && git rev-parse --verify "$GITHUB_BASE_SHA" >/dev/null 2>&1; then
		commit_range="$GITHUB_BASE_SHA..HEAD"
	elif [ -n "${GITHUB_BEFORE:-}" ] && [ "$GITHUB_BEFORE" != "0000000000000000000000000000000000000000" ] && git rev-parse --verify "$GITHUB_BEFORE" >/dev/null 2>&1; then
		commit_range="$GITHUB_BEFORE..HEAD"
	else
		commit_range=HEAD
	fi
fi

mapfile -t commits < <(git rev-list --reverse "$commit_range")
if [ "${#commits[@]}" -eq 0 ]; then
	echo "commit hygiene: no commits in $commit_range"
	exit 0
fi

messages=$(git log --format=%B "$commit_range")
if printf '%s\n' "$messages" | grep -Eiq '(^|[[:space:]])co-authored-by:.*(ai assistant|anthropic|chatgpt|claude|codex|copilot|gpt([^[:alnum:]]|$)|llm|openai|language model)|generated with[[:space:]]'; then
	echo "commit hygiene: generated or AI attribution found" >&2
	printf '%s\n' "$messages" | grep -Ein 'co-authored-by:|generated with' >&2 || true
	exit 1
fi

if [ "$require_verified" -eq 1 ]; then
	command -v gh >/dev/null 2>&1 || { echo "commit hygiene: gh is required for signature checks" >&2; exit 1; }
	repository=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required for signature checks}
	unverified=0
	for sha in "${commits[@]}"; do
		verified=$(gh api "repos/$repository/commits/$sha" --jq '.commit.verification.verified' 2>/dev/null || printf 'unknown')
		if [ "$verified" != true ]; then
			echo "commit hygiene: $sha is not GitHub-verified ($verified)" >&2
			unverified=1
		fi
	done
	[ "$unverified" -eq 0 ] || exit 1
fi

echo "commit hygiene passed for ${#commits[@]} commit(s)"
