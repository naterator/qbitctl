#!/usr/bin/env bash
set -euo pipefail

# This reporter is intentionally limited to the scheduled workflow. The
# workflow job has the same guard, but keeping it here prevents an accidental
# manual invocation from opening an issue as well.
if [[ "${GITHUB_EVENT_NAME:-}" != "schedule" ]]; then
	echo "nightly flake reporting is disabled for ${GITHUB_EVENT_NAME:-unknown} runs"
	exit 0
fi

report_dir=${1:?usage: report_nightly_flake.sh REPORT_DIRECTORY}
shopt -s nullglob
reports=("$report_dir"/nightly-race-*.txt)
if (( ${#reports[@]} == 0 )); then
	echo "no scheduled race failure reports found; no issue needed"
	exit 0
fi

repo=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
server_url=${GITHUB_SERVER_URL:-https://github.com}
run_id=${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}
run_url="$server_url/$repo/actions/runs/$run_id"
title='Scheduled Go race/shuffle test failure'
tick='`'
body_file=$(mktemp)
trap 'rm -f "$body_file"' EXIT

{
	printf '%s\n\n' 'Automated scheduled Go race/shuffle test failure.'
	printf '%s\n' "- Run: [$run_url]($run_url)"
	printf '%s\n' '- Failed runner(s):'
} > "$body_file"

for report in "${reports[@]}"; do
	runner=${report##*/nightly-race-}
	runner=${runner%.txt}
	case "$runner" in
		ubuntu-*) runner_os='Linux' ;;
		macos-*) runner_os='macOS' ;;
		*) runner_os='unknown OS' ;;
	esac
	# The go command buffers each failed package's output and ends it with
	# a FAIL line naming the package. Each package chooses its own seed.
	failures=$(awk '
		/^-test\.shuffle[[:space:]]+[0-9]+$/ { seed = $2 }
		/^FAIL[[:space:]]+[[:alnum:]_.\/-]+/ {
			print (seed == "" ? "-" : seed), $2
			seed = ""
		}
		END { if (seed != "") print seed, "./..." }
	' "$report" | sort -u)
	while read -r seed package; do
		if [[ -z "$seed" || "$seed" == '-' ]]; then
			printf '  - %s%s%s (%s%s; no shuffle seed in test output)\n' "$tick" "$runner" "$tick" "$runner_os" "${package:+, $package}" >> "$body_file"
		else
			printf '  - %s%s%s (%s; reproduce with: %sgo test -race -count=3 -shuffle=%s %s%s)\n' "$tick" "$runner" "$tick" "$runner_os" "$tick" "$seed" "$package" "$tick" >> "$body_file"
		fi
	done <<< "$failures"
done

printf '\n%s\n' 'The full test output is attached to the workflow run.' >> "$body_file"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
	cat "$body_file" >> "$GITHUB_STEP_SUMMARY"
fi

has_issues=$(gh api "/repos/$repo" --jq '.has_issues')
if [[ "$has_issues" != true ]]; then
	echo 'repository Issues are disabled; left the failure details in the Actions summary'
	exit 0
fi

existing=$(gh issue list --repo "$repo" --state open --search "\"$title\" in:title" --json number,title \
	--jq 'first(.[] | select(.title == "Scheduled Go race/shuffle test failure") | .number) // empty')
if [[ -n "$existing" ]]; then
	gh issue comment "$existing" --repo "$repo" --body-file "$body_file"
	echo "updated existing issue #$existing"
else
	gh issue create --repo "$repo" --title "$title" --body-file "$body_file"
	echo 'created scheduled race failure issue'
fi
