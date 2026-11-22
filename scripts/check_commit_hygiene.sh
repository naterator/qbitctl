#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat >&2 <<'EOF'
usage: check_commit_hygiene.sh [--range RANGE] [--require-verified]

Checks commit messages for generated or AI attribution. In GitHub Actions,
pushes to main require GitHub-verified commits, while pull requests report
unverified commits as warnings. --range is intended for local checks and
overrides the event-derived commit range.
EOF
}

commit_range=
require_verified=0
explicit_range=0
while [ "$#" -gt 0 ]; do
	case "$1" in
		--range)
			[ "$#" -ge 2 ] || { usage; exit 2; }
			commit_range=$2
			explicit_range=1
			shift 2
			;;
		--require-verified)
			require_verified=1
			shift
			;;
		*) usage; exit 2 ;;
	esac
done

event=${GITHUB_EVENT_NAME:-}
head_sha=${GITHUB_SHA:-}
base_sha=${GITHUB_BASE_SHA:-}
before_sha=${GITHUB_BEFORE:-}

is_sha() {
	[[ "$1" =~ ^[0-9a-fA-F]{40}$ ]]
}

resolve_commit() {
	is_sha "$1" && git rev-parse --verify "$1^{commit}" >/dev/null 2>&1
}

require_local_commit() {
	local label=$1
	local sha=$2
	if ! is_sha "$sha"; then
		echo "commit hygiene: $label is not a full commit SHA" >&2
		exit 2
	fi
	if ! resolve_commit "$sha"; then
		echo "commit hygiene: $label is not available in the checkout" >&2
		exit 2
	fi
}

if [ "$event" = push ] && [ "${GITHUB_REF:-}" = refs/heads/main ]; then
	require_verified=1
fi

if [ "$explicit_range" -eq 0 ]; then
	case "$event" in
		push)
			[ "${GITHUB_REF:-}" = refs/heads/main ] || {
				echo "commit hygiene: push event is not for main" >&2
				exit 2
			}
			require_local_commit GITHUB_SHA "$head_sha"
			if [ -z "$before_sha" ]; then
				echo "commit hygiene: GITHUB_BEFORE is required for a main push" >&2
				exit 2
			fi
			if [ "$before_sha" = "0000000000000000000000000000000000000000" ]; then
				commit_range="$head_sha"
			else
				require_local_commit GITHUB_BEFORE "$before_sha"
				# A force push can make before a non-ancestor. Git's two-dot
				# range still identifies commits reachable from the new head
				# that were not reachable from the old head.
				commit_range="$before_sha..$head_sha"
			fi
			;;
		pull_request)
			require_local_commit GITHUB_BASE_SHA "$base_sha"
			require_local_commit GITHUB_HEAD_SHA "${GITHUB_HEAD_SHA:-}"
			commit_range="$base_sha..$GITHUB_HEAD_SHA"
			;;
		*)
			# Manual, scheduled, and local invocations have no authenticated push
			# boundary. They still enforce the authorship guard over the checked-out
			# tip, but never silently turn a different event into a main push.
			if [ -n "$head_sha" ] && resolve_commit "$head_sha"; then
				commit_range="$head_sha"
			else
				commit_range=HEAD
			fi
			;;
	esac
fi

commits=()
if ! commit_list=$(git rev-list --reverse "$commit_range"); then
	echo "commit hygiene: invalid commit range $commit_range" >&2
	exit 2
fi
if [ -n "$commit_list" ]; then
	mapfile -t commits <<<"$commit_list"
fi
if [ "${#commits[@]}" -eq 0 ]; then
	echo "commit hygiene: no commits in $commit_range"
	exit 0
fi

if ! messages=$(git log --format=%B "$commit_range"); then
	echo "commit hygiene: unable to read commit messages in $commit_range" >&2
	exit 2
fi
if grep -Eiq '(^|[[:space:]])co-authored-by:.*(ai assistant|anthropic|chatgpt|claude|codex|copilot|gpt([^[:alnum:]]|$)|llm|openai|language model)|generated with[[:space:]]' <<<"$messages"; then
	echo "commit hygiene: generated or AI attribution found" >&2
	grep -Ein 'co-authored-by:|generated with' <<<"$messages" >&2 || true
	exit 1
fi

signature_check=0
if [ "$require_verified" -eq 1 ] || [ "$event" = pull_request ]; then
	signature_check=1
fi

if [ "$signature_check" -eq 1 ]; then
	repository=${GITHUB_REPOSITORY:-}
	signature_failed=0
	report_signature_problem() {
		local message=$1
		if [ "$require_verified" -eq 1 ]; then
			echo "commit hygiene: $message" >&2
		else
		echo "::warning::commit hygiene: $message"
		fi
		signature_failed=1
	}
	if ! command -v gh >/dev/null 2>&1; then
		report_signature_problem "could not check GitHub signatures because gh is unavailable"
	elif [ -z "$repository" ]; then
		report_signature_problem "could not check GitHub signatures because the repository is unavailable"
	elif [ "$event" != "" ] && [ -z "${GH_TOKEN:-}" ]; then
		report_signature_problem "could not check GitHub signatures because GH_TOKEN is unavailable"
	fi
	if [ "$signature_failed" -ne 0 ]; then
		if [ "$require_verified" -eq 1 ]; then
			exit 1
		fi
		signature_check=0
	fi
fi

if [ "$signature_check" -eq 1 ]; then
	retry_delay=${COMMIT_HYGIENE_RETRY_DELAY:-2}
	if ! [[ "$retry_delay" =~ ^[0-9]+$ ]]; then
		echo "commit hygiene: retry delay is invalid" >&2
		exit 2
	fi
	for sha in "${commits[@]}"; do
		verified=
		for attempt in 1 2 3; do
			if response=$(gh api --method GET "repos/$repository/commits/$sha" --jq '.commit.verification.verified' 2>/dev/null); then
				case "$response" in
					true|false)
						verified=$response
						break
						;;
				esac
			fi
			if [ "$attempt" -lt 3 ] && [ "$retry_delay" -gt 0 ]; then
				sleep "$retry_delay"
			fi
		done
		case "$verified" in
			true) ;;
			false)
				report_signature_problem "$sha is not GitHub-verified"
				;;
			*)
				report_signature_problem "unable to verify $sha after three attempts"
				;;
		esac
	done
	if [ "$require_verified" -eq 1 ] && [ "$signature_failed" -ne 0 ]; then
		exit 1
	fi
fi

echo "commit hygiene passed for ${#commits[@]} commit(s) in $commit_range"
