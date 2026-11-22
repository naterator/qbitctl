#!/usr/bin/env bash
set -euo pipefail

if [[ ${INTEGRATION_OUTCOME:-} != failure ]]; then
	echo "No qBittorrent compatibility integration failure to report (outcome: ${INTEGRATION_OUTCOME:-unknown})"
	exit 0
fi

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GITHUB_SERVER_URL:?GITHUB_SERVER_URL is required}"
: "${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}"
: "${NEW_TAG:?NEW_TAG is required}"
: "${NEW_IMAGE:?NEW_IMAGE is required}"
: "${NEW_DIGEST:?NEW_DIGEST is required}"

if [[ ! "$NEW_TAG" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9]+)?$ ]]; then
	echo "invalid qBittorrent tag: $NEW_TAG" >&2
	exit 1
fi
if [[ ! "$NEW_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
	echo "invalid qBittorrent digest: $NEW_DIGEST" >&2
	exit 1
fi
expected_image="qbittorrentofficial/qbittorrent-nox:$NEW_TAG@$NEW_DIGEST"
if [[ "$NEW_IMAGE" != "$expected_image" ]]; then
	echo "image does not match tag and digest: $NEW_IMAGE" >&2
	exit 1
fi

run_url="$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID"
title="qBittorrent compatibility failure: $NEW_TAG ($NEW_DIGEST)"
guidance='Reproduce against this digest and fix the compatibility failure before adding the image to regular CI.'

write_disabled_summary() {
	if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
		{
			printf '%s\n\n' '### qBittorrent compatibility failure'
			printf '%s\n' "- Tag: $NEW_TAG" "- Immutable image: $NEW_IMAGE" "- Digest: $NEW_DIGEST"
			printf '\nIssues are disabled for this repository; no issue was created.\n'
			printf '%s\n' "$guidance"
			printf 'Compatibility run: %s\n' "$run_url"
		} >>"$GITHUB_STEP_SUMMARY"
	fi
	echo "Issues are disabled for $GITHUB_REPOSITORY; compatibility failure remains in the Actions summary."
}

# Check the live repository setting before any issue command. This is a
# read-only call and avoids relying on optional event-payload fields.
has_issues=$(gh api "repos/$GITHUB_REPOSITORY" --jq '.has_issues')
if [[ "$has_issues" != true ]]; then
	write_disabled_summary
	exit 0
fi

search="\"$title\" in:title"
existing=$(gh issue list --repo "$GITHUB_REPOSITORY" --state open --search "$search" --json number,title \
	--jq "first(.[] | select(.title == \"$title\") | .number) // empty")

if [[ -n "$existing" ]]; then
	body=$(mktemp "${RUNNER_TEMP:-/tmp}/qbittorrent-compatibility-repeat.XXXXXX.md")
	trap 'rm -f "$body"' EXIT
	{
		printf 'The integration failure was observed again in workflow run %s.\n' "$run_url"
		printf 'Immutable image: %s\n' "$NEW_IMAGE"
		printf '%s\n' "$guidance"
	} >"$body"
	gh issue comment "$existing" --repo "$GITHUB_REPOSITORY" --body-file "$body"
	echo "Updated existing qBittorrent compatibility issue #$existing"
	exit 0
fi

body=$(mktemp "${RUNNER_TEMP:-/tmp}/qbittorrent-compatibility.XXXXXX.md")
trap 'rm -f "$body"' EXIT
{
	printf '%s\n\n' 'Automated qBittorrent compatibility watch detected an integration-test failure.'
	printf '%s\n' "- Tag: $NEW_TAG" "- Immutable image: $NEW_IMAGE" "- Digest: $NEW_DIGEST"
	printf '\nCompatibility run: %s\n' "$run_url"
	printf '%s\n' "$guidance"
} >"$body"
gh issue create --repo "$GITHUB_REPOSITORY" --title "$title" --body-file "$body"
echo "Opened qBittorrent compatibility issue: $title"
