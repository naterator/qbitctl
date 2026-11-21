#!/bin/sh

# Wait for the push-triggered CI workflow for an exact source commit.
#
# RELEASE_CI_TIMEOUT_SECONDS and RELEASE_CI_POLL_SECONDS keep the production
# polling policy configurable for tests. RELEASE_CI_MAX_ATTEMPTS is a bounded
# test-only escape hatch that avoids making failure-path tests wait minutes.
release_wait_for_ci() {
	if [ "$#" -ne 2 ]; then
		echo "usage: release_wait_for_ci <repository> <commit>" >&2
		return 2
	fi

	release_repository=$1
	release_commit=$2
	release_timeout_seconds=${RELEASE_CI_TIMEOUT_SECONDS:-1740}
	release_poll_seconds=${RELEASE_CI_POLL_SECONDS:-15}
	release_max_attempts=${RELEASE_CI_MAX_ATTEMPTS:-}
	release_deadline=$(( $(date +%s) + release_timeout_seconds ))
	release_attempt=0

	while [ "$(date +%s)" -lt "$release_deadline" ]; do
		release_attempt=$((release_attempt + 1))
		if ! release_run=$(gh api --method GET "repos/$release_repository/actions/workflows/ci.yml/runs" \
			-f event=push -f head_sha="$release_commit" -F per_page=1 \
			--jq '.workflow_runs[0] // empty | [.status, (.conclusion // "pending"), .html_url] | @tsv'); then
			echo "::error::Unable to query CI runs for $release_commit."
			return 1
		fi

		if [ -z "$release_run" ]; then
			echo "Waiting for a push-triggered CI run for $release_commit."
		else
			release_old_ifs=$IFS
			IFS="$(printf '\t')"
			read -r release_status release_conclusion release_url <<EOF
$release_run
EOF
			IFS=$release_old_ifs
			if [ "$release_status" = completed ]; then
				if [ "$release_conclusion" = success ]; then
					echo "CI passed for $release_commit: $release_url"
					return 0
				fi
				echo "::error::CI ended with $release_conclusion for $release_commit: $release_url"
				return 1
			fi
			echo "CI is $release_status for $release_commit: $release_url"
		fi

		if [ -n "$release_max_attempts" ] && [ "$release_attempt" -ge "$release_max_attempts" ]; then
			echo "::error::Timed out waiting for CI for $release_commit."
			return 1
		fi
		sleep "$release_poll_seconds"
	done

	echo "::error::Timed out waiting for CI for $release_commit."
	return 1
}

# Confirm that the release tag still names the commit that was packaged.
release_verify_tag_commit() {
	if [ "$#" -ne 3 ]; then
		echo "usage: release_verify_tag_commit <repository> <tag> <expected-commit>" >&2
		return 2
	fi

	release_repository=$1
	release_tag=$2
	release_expected_commit=$3
	if ! release_current_commit=$(gh api "repos/$release_repository/commits/refs/tags/$release_tag" --jq .sha); then
		echo "::error::Unable to resolve tag $release_tag before upload."
		return 1
	fi
	if [ "$release_current_commit" != "$release_expected_commit" ]; then
		echo "::error::Tag $release_tag moved from $release_expected_commit to $release_current_commit. Rerun the release workflow."
		return 1
	fi
}
