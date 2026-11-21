#!/bin/sh
set -eu

script_dir=$(dirname "$0")
script_dir=$(cd "$script_dir" && pwd)
helpers="$script_dir/release_helpers.sh"
fake_dir=$(mktemp -d)
trap 'rm -rf "$fake_dir"' 0 1 2 15

cat > "$fake_dir/gh" <<'EOF'
#!/bin/sh
if [ -n "${GH_FAKE_LOG:-}" ]; then
	printf '%s\n' "$*" >> "$GH_FAKE_LOG"
fi
case "${GH_FAKE_SCENARIO:-}" in
ci-success)
	printf 'completed\tsuccess\thttps://example.test/ci/1\n'
	;;
ci-failure)
	printf 'completed\tfailure\thttps://example.test/ci/2\n'
	;;
ci-missing)
	: # No output models an empty workflow_runs response after --jq.
	;;
tag-moved)
	printf '%s\n' 'new-commit'
	;;
tag-stable)
	printf '%s\n' 'expected-commit'
	;;
*)
	printf '%s\n' "unknown fake gh scenario: ${GH_FAKE_SCENARIO:-}" >&2
	exit 2
	;;
esac
EOF
chmod +x "$fake_dir/gh"

run_wait_for_ci() {
	GH_FAKE_SCENARIO=$1
	export GH_FAKE_SCENARIO
	shift
	PATH="$fake_dir:$PATH" \
	RELEASE_CI_TIMEOUT_SECONDS=30 \
	RELEASE_CI_POLL_SECONDS=0 \
	RELEASE_CI_MAX_ATTEMPTS="${RELEASE_CI_MAX_ATTEMPTS:-}" \
	sh -c '. "$1"; release_wait_for_ci "$2" "$3"' sh "$helpers" example/repo expected-commit "$@"
}

success_output=$(run_wait_for_ci ci-success)
case "$success_output" in
*"CI passed for expected-commit"*) ;;
*)
	echo "successful CI gate output was unexpected: $success_output" >&2
	exit 1
;;
esac

if run_wait_for_ci ci-failure >/dev/null 2>&1; then
	echo "failed CI gate unexpectedly succeeded" >&2
	exit 1
fi

RELEASE_CI_MAX_ATTEMPTS=1
export RELEASE_CI_MAX_ATTEMPTS
if run_wait_for_ci ci-missing >/dev/null 2>&1; then
	echo "missing CI gate unexpectedly succeeded" >&2
	exit 1
fi
unset RELEASE_CI_MAX_ATTEMPTS

stable_output=$(PATH="$fake_dir:$PATH" GH_FAKE_SCENARIO=tag-stable \
	sh -c '. "$1"; release_verify_tag_commit "$2" "$3" "$4"' sh "$helpers" example/repo v1.2.3 expected-commit)
[ -z "$stable_output" ]

if PATH="$fake_dir:$PATH" GH_FAKE_SCENARIO=tag-moved \
	sh -c '. "$1"; release_verify_tag_commit "$2" "$3" "$4"' sh "$helpers" example/repo v1.2.3 expected-commit >/dev/null 2>&1; then
	echo "moved tag verification unexpectedly succeeded" >&2
	exit 1
fi

echo "release helper regression tests passed"
