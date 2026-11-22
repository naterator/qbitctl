#!/usr/bin/env bash
set -euo pipefail

# Each test supplies its own event data. Start with no Actions context so a
# real CI push cannot turn a local test case into a strict signature check.
unset GITHUB_EVENT_NAME GITHUB_REF GITHUB_SHA GITHUB_BASE_SHA GITHUB_HEAD_SHA \
	GITHUB_BEFORE GITHUB_REPOSITORY GH_TOKEN

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/qbitctl-hygiene.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "commit hygiene test: $*" >&2
	exit 1
}

new_repo() {
	local repo=$1
	mkdir -p "$repo"
	git -C "$repo" init -q -b main
	git -C "$repo" config user.email test@example.com
	git -C "$repo" config user.name "Test User"
	git -C "$repo" config commit.gpgsign false
	git -C "$repo" commit --allow-empty -q -m "initial commit"
}

new_commit() {
	local repo=$1
	local message=$2
	git -C "$repo" commit --allow-empty -q -m "$message"
	git -C "$repo" rev-parse HEAD
}

repo=$tmp/local
new_repo "$repo"
new_commit "$repo" $'ordinary contributor\n\nCo-Authored-By: Example <person@gmail.com>' >/dev/null
(cd "$repo" && "$script_dir/check_commit_hygiene.sh" --range HEAD)

if (cd "$repo" && "$script_dir/check_commit_hygiene.sh" --range does-not-exist); then
	fail "an invalid commit range was accepted"
fi

new_commit "$repo" $'generated change\n\nGenerated with Codex' >/dev/null
if (cd "$repo" && "$script_dir/check_commit_hygiene.sh" --range HEAD); then
	fail "AI attribution was accepted"
fi

message=$tmp/commit-message
printf '%s\n' 'ordinary contributor' >"$message"
"$script_dir/hooks/commit-msg" "$message"
printf '%s\n' 'Generated with Codex' >"$message"
if "$script_dir/hooks/commit-msg" "$message"; then
	fail "the commit-msg hook accepted AI attribution"
fi

fake_bin=$tmp/bin
mkdir -p "$fake_bin"
cat >"$fake_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >>"${GH_CALLS:?}"
count=0
if [ -n "${GH_COUNT_FILE:-}" ] && [ -f "$GH_COUNT_FILE" ]; then
	count=$(<"$GH_COUNT_FILE")
fi
count=$((count + 1))
if [ -n "${GH_COUNT_FILE:-}" ]; then
	printf '%s\n' "$count" >"$GH_COUNT_FILE"
fi

case "${GH_SCENARIO:-verified}" in
	verified)
		printf '%s\n' true
		;;
	unverified)
		printf '%s\n' false
		;;
	retry)
		if [ "$count" -lt 3 ]; then
			echo 'transient API failure' >&2
			exit 1
		fi
		printf '%s\n' true
		;;
	failure)
		echo 'permanent API failure' >&2
		exit 1
		;;
	*)
		echo "unknown fake gh scenario: ${GH_SCENARIO:-}" >&2
		exit 2
		;;
esac
EOF
chmod +x "$fake_bin/gh"

calls=$tmp/gh-calls
count_file=$tmp/gh-count
before=$(git -C "$repo" rev-parse HEAD)
head=$(new_commit "$repo" 'signed main push')
local_only=$(new_commit "$repo" 'local checkout only')
: >"$calls"
rm -f "$count_file"

(
	cd "$repo"
	env \
		GITHUB_EVENT_NAME=push \
		GITHUB_REF=refs/heads/main \
		GITHUB_BEFORE="$before" \
		GITHUB_SHA="$head" \
		GITHUB_REPOSITORY=example/repo \
		GH_TOKEN=test-token \
		GH_CALLS="$calls" \
		GH_COUNT_FILE="$count_file" \
		GH_SCENARIO=verified \
		COMMIT_HYGIENE_RETRY_DELAY=0 \
		PATH="$fake_bin:$PATH" \
		"$script_dir/check_commit_hygiene.sh"
)
grep -F "$head" "$calls" >/dev/null || fail "main push did not check GITHUB_SHA"
if grep -F "$local_only" "$calls" >/dev/null; then
	fail "main push checked the local checkout instead of GITHUB_SHA"
fi

: >"$calls"
rm -f "$count_file"
if (
	cd "$repo"
	env GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main GITHUB_BEFORE="$before" \
		GITHUB_SHA="$head" GITHUB_REPOSITORY=example/repo GH_TOKEN=test-token \
		GH_CALLS="$calls" GH_COUNT_FILE="$count_file" GH_SCENARIO=unverified \
		COMMIT_HYGIENE_RETRY_DELAY=0 PATH="$fake_bin:$PATH" \
		"$script_dir/check_commit_hygiene.sh"
); then
	fail "an unverified main push was accepted"
fi

: >"$calls"
rm -f "$count_file"
(
	cd "$repo"
	env GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main GITHUB_BEFORE="$before" \
		GITHUB_SHA="$head" GITHUB_REPOSITORY=example/repo GH_TOKEN=test-token \
		GH_CALLS="$calls" GH_COUNT_FILE="$count_file" GH_SCENARIO=retry \
		COMMIT_HYGIENE_RETRY_DELAY=0 PATH="$fake_bin:$PATH" \
		"$script_dir/check_commit_hygiene.sh"
)
[ "$(<"$count_file")" = 3 ] || fail "signature lookup did not retry exactly three times"

: >"$calls"
rm -f "$count_file"
if (
	cd "$repo"
	env GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main GITHUB_BEFORE="$before" \
		GITHUB_SHA="$head" GITHUB_REPOSITORY=example/repo GH_TOKEN=test-token \
		GH_CALLS="$calls" GH_COUNT_FILE="$count_file" GH_SCENARIO=failure \
		COMMIT_HYGIENE_RETRY_DELAY=0 PATH="$fake_bin:$PATH" \
		"$script_dir/check_commit_hygiene.sh"
); then
	fail "a permanent signature API failure was accepted on main"
fi
[ "$(<"$count_file")" = 3 ] || fail "signature API failure did not stop after three attempts"

if (
	cd "$repo"
	env GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main GITHUB_SHA="$head" \
		GITHUB_REPOSITORY=example/repo GH_TOKEN=test-token GH_CALLS="$calls" \
		PATH="$fake_bin:$PATH" "$script_dir/check_commit_hygiene.sh"
); then
	fail "a main push with no before SHA was accepted"
fi

# A local explicit verification uses normal gh authentication and therefore does
# not require GH_TOKEN, unlike a GitHub Actions invocation.
auth_repo=$tmp/auth
new_repo "$auth_repo"
new_commit "$auth_repo" 'locally verified commit' >/dev/null
(
	cd "$auth_repo"
	env GITHUB_REPOSITORY=example/repo GH_CALLS="$calls" GH_COUNT_FILE="$count_file" \
		GH_SCENARIO=verified COMMIT_HYGIENE_RETRY_DELAY=0 PATH="$fake_bin:$PATH" \
		"$script_dir/check_commit_hygiene.sh" --range HEAD --require-verified
)

pr_repo=$tmp/pr
new_repo "$pr_repo"
base=$(git -C "$pr_repo" rev-parse HEAD)
pr_head=$(new_commit "$pr_repo" 'pull request head')
merge=$(new_commit "$pr_repo" 'merge checkout only')
: >"$calls"
rm -f "$count_file"
pr_output=$(
	cd "$pr_repo"
	env GITHUB_EVENT_NAME=pull_request GITHUB_BASE_SHA="$base" \
		GITHUB_HEAD_SHA="$pr_head" GITHUB_SHA="$merge" GITHUB_REPOSITORY=example/repo \
		GH_TOKEN=test-token GH_CALLS="$calls" GH_COUNT_FILE="$count_file" \
		GH_SCENARIO=unverified COMMIT_HYGIENE_RETRY_DELAY=0 PATH="$fake_bin:$PATH" \
		"$script_dir/check_commit_hygiene.sh" 2>&1
)
printf '%s\n' "$pr_output"
grep -F '::warning::commit hygiene:' <<<"$pr_output" >/dev/null || fail "PR verification did not warn"
grep -F "$pr_head" "$calls" >/dev/null || fail "PR did not check GITHUB_HEAD_SHA"
if grep -F "$merge" "$calls" >/dev/null; then
	fail "PR checked the merge SHA instead of GITHUB_HEAD_SHA"
fi

echo "commit hygiene tests passed"
