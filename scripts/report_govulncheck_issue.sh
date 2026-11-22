#!/usr/bin/env bash
set -euo pipefail

if [[ ${GITHUB_EVENT_NAME:-} != schedule ]]; then
	echo "Skipping issue reporting for ${GITHUB_EVENT_NAME:-unknown} event"
	exit 0
fi
if [[ ${GOVULNCHECK_FINDINGS:-false} != true ]]; then
	echo 'No vulnerability findings to report'
	exit 0
fi

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GITHUB_SERVER_URL:?GITHUB_SERVER_URL is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
: "${GOVULNCHECK_FINDING_FINGERPRINT:?GOVULNCHECK_FINDING_FINGERPRINT is required}"
: "${REPORT_FILE:?REPORT_FILE is required}"

if [[ ! -s "$REPORT_FILE" ]]; then
	echo "missing vulnerability report: $REPORT_FILE" >&2
	exit 1
fi

run_url="$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID"
title="Go vulnerability findings (${GOVULNCHECK_FINDING_FINGERPRINT})"

write_disabled_summary() {
	if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
		{
			printf '%s\n\n' '### Go vulnerability findings'
			cat "$REPORT_FILE"
			printf '\nIssues are disabled for this repository; no issue was created.\n'
			printf 'Scheduled run: %s\n' "$run_url"
		} >>"$GITHUB_STEP_SUMMARY"
	fi
	cat "$REPORT_FILE"
	echo "Issues are disabled for $GITHUB_REPOSITORY; findings remain in the Actions summary."
}

# Do not attempt issue writes when the repository has disabled Issues. This
# check also avoids relying on the repository-event payload's optional fields.
has_issues=$(gh api "repos/$GITHUB_REPOSITORY" --jq '.has_issues')
if [[ "$has_issues" != true ]]; then
	write_disabled_summary
	exit 0
fi

search="\"$title\" in:title"
existing=$(gh issue list --repo "$GITHUB_REPOSITORY" --state open --search "$search" --json number,title \
	--jq "first(.[] | select(.title == \"$title\") | .number) // empty")

if [[ -n "$existing" ]]; then
	body=$(mktemp "${RUNNER_TEMP:-/tmp}/govulncheck-repeat.XXXXXX.md")
	trap 'rm -f "$body"' EXIT
	{
		printf 'The same findings were observed again in [scheduled run](%s).\n' "$run_url"
		printf 'Finding IDs: %s\n' "${GOVULNCHECK_FINDING_IDS:-unknown}"
	} >"$body"
	gh issue comment "$existing" --repo "$GITHUB_REPOSITORY" --body-file "$body"
	echo "Updated existing vulnerability issue #$existing"
	exit 0
fi

body=$(mktemp "${RUNNER_TEMP:-/tmp}/govulncheck-issue.XXXXXX.md")
trap 'rm -f "$body"' EXIT
{
	cat "$REPORT_FILE"
	printf '\nDetected by [scheduled govulncheck run](%s).\n' "$run_url"
	printf 'Finding fingerprint: %s\n' "$GOVULNCHECK_FINDING_FINGERPRINT"
} >"$body"
gh issue create --repo "$GITHUB_REPOSITORY" --title "$title" --body-file "$body"
echo "Opened vulnerability issue: $title"
