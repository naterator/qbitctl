#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/qbitctl-govulncheck-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

assert_contains() {
	local needle=$1
	local file=$2
	if ! grep -Fq -- "$needle" "$file"; then
		echo "expected $file to contain: $needle" >&2
		cat "$file" >&2
		exit 1
	fi
}

assert_metadata() {
	local key=$1
	local value=$2
	local file=$3
	assert_contains "$key=$value" "$file"
}

cat >"$tmp/scanner-pass" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' '{"config":{"protocol_version":"v1.0.0"}}' '{"progress":{"message":"done"}}'
EOF
cat >"$tmp/scanner-finding" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' \
  '{"config":{"protocol_version":"v1.0.0"}}' \
  '{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.2.3","trace":[{"module":"example.com/vulnerable","version":"v1.0.0","package":"example.com/vulnerable/pkg","function":"example.com/vulnerable/pkg.Bad"}]}}' \
  '{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.2.3","trace":[{"module":"example.com/vulnerable","version":"v1.0.0","package":"example.com/vulnerable/pkg","function":"example.com/vulnerable/pkg.Bad"}]}}' \
  '{"finding":{"osv":"GO-2026-0001","trace":[{"module":"example.com/vulnerable","version":"v1.0.0","package":"example.com/vulnerable/pkg"}]}}'
EOF
cat >"$tmp/scanner-empty" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$tmp/scanner-failure" <<'EOF'
#!/usr/bin/env bash
echo 'vulnerability database unavailable' >&2
exit 42
EOF
chmod +x "$tmp/scanner-pass" "$tmp/scanner-finding" "$tmp/scanner-failure" "$tmp/scanner-empty"

pass_dir="$tmp/pass"
pass_output="$tmp/pass.output"
GOVULNCHECK_OUTPUT_DIR="$pass_dir" GITHUB_OUTPUT="$pass_output" "$script_dir/run_govulncheck.sh" "$tmp/scanner-pass"
assert_metadata has_findings false "$pass_output"
assert_metadata finding_count 0 "$pass_output"
assert_contains 'No reachable vulnerabilities were reported.' "$pass_dir/report.md"

finding_dir="$tmp/finding"
finding_output="$tmp/finding.output"
set +e
GOVULNCHECK_OUTPUT_DIR="$finding_dir" GITHUB_OUTPUT="$finding_output" "$script_dir/run_govulncheck.sh" "$tmp/scanner-finding"
finding_status=$?
set -e
if [[ "$finding_status" -eq 0 ]]; then
	echo 'finding scan unexpectedly succeeded' >&2
	exit 1
fi
assert_metadata has_findings true "$finding_output"
assert_metadata finding_count 1 "$finding_output"
assert_contains 'GO-2026-0001' "$finding_dir/report.md"

failure_dir="$tmp/failure"
failure_output="$tmp/failure.output"
set +e
GOVULNCHECK_OUTPUT_DIR="$failure_dir" GITHUB_OUTPUT="$failure_output" "$script_dir/run_govulncheck.sh" "$tmp/scanner-failure" >/dev/null
failure_status=$?
set -e
if [[ "$failure_status" -eq 0 ]]; then
	echo 'scanner failure unexpectedly succeeded' >&2
	exit 1
fi
assert_metadata has_findings false "$failure_output"
assert_metadata scanner_status 42 "$failure_output"

empty_dir="$tmp/empty"
empty_output="$tmp/empty.output"
set +e
GOVULNCHECK_OUTPUT_DIR="$empty_dir" GITHUB_OUTPUT="$empty_output" "$script_dir/run_govulncheck.sh" "$tmp/scanner-empty" >/dev/null
empty_status=$?
set -e
if [[ "$empty_status" -eq 0 ]]; then
	echo 'empty scanner output unexpectedly succeeded' >&2
	exit 1
fi
assert_metadata has_findings false "$empty_output"
assert_metadata scanner_status 0 "$empty_output"

cat >"$tmp/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$GH_LOG"
if [[ ${1:-} == api ]]; then
    printf '%s\n' "${GH_API_RESULT:-true}"
else
case "${1:-} ${2:-}" in
  'issue list')
    cat "$GH_LIST_RESULT"
    ;;
  'issue create')
    for ((index = 1; index <= $#; index++)); do
      if [[ "${!index}" == '--body-file' ]]; then
        next=$((index + 1))
        cp "${!next}" "$GH_BODY_CAPTURE"
      fi
    done
    ;;
  'issue comment')
    :
    ;;
  *)
    echo "unexpected gh invocation: $*" >&2
    exit 1
    ;;
esac
fi
EOF
chmod +x "$tmp/gh"

report_file="$finding_dir/report.md"
gh_log="$tmp/gh.log"
gh_list="$tmp/gh-list"
gh_body="$tmp/gh-body"
: >"$gh_log"
: >"$gh_list"
before=$(wc -l <"$gh_log")
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GITHUB_EVENT_NAME=schedule GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=122 GOVULNCHECK_FINDINGS=false REPORT_FILE="$report_file" "$script_dir/report_govulncheck_issue.sh"
after=$(wc -l <"$gh_log")
if [[ "$before" -ne "$after" ]]; then
	echo 'scanner failure unexpectedly attempted issue reporting' >&2
	exit 1
fi
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=true GITHUB_EVENT_NAME=schedule GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=123 GOVULNCHECK_FINDINGS=true GOVULNCHECK_FINDING_FINGERPRINT=deadbeef GOVULNCHECK_FINDING_IDS=GO-2026-0001 REPORT_FILE="$report_file" "$script_dir/report_govulncheck_issue.sh"
assert_contains 'issue create' "$gh_log"
assert_contains 'scheduled govulncheck run' "$gh_body"

printf '42\n' >"$gh_list"
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=true GITHUB_EVENT_NAME=schedule GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=124 GOVULNCHECK_FINDINGS=true GOVULNCHECK_FINDING_FINGERPRINT=deadbeef GOVULNCHECK_FINDING_IDS=GO-2026-0001 REPORT_FILE="$report_file" "$script_dir/report_govulncheck_issue.sh"
create_count=$(grep -c 'issue create' "$gh_log" || true)
if [[ "$create_count" -ne 1 ]]; then
	echo "expected one issue create, got $create_count" >&2
	cat "$gh_log" >&2
	exit 1
fi
assert_contains 'issue comment' "$gh_log"

before=$(wc -l <"$gh_log")
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=true GITHUB_EVENT_NAME=workflow_dispatch GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=125 GOVULNCHECK_FINDINGS=true GOVULNCHECK_FINDING_FINGERPRINT=other GOVULNCHECK_FINDING_IDS=GO-2026-0002 REPORT_FILE="$report_file" "$script_dir/report_govulncheck_issue.sh"
after=$(wc -l <"$gh_log")
if [[ "$before" -ne "$after" ]]; then
	echo 'manual run unexpectedly called gh' >&2
	exit 1
fi

summary="$tmp/summary.md"
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=false GITHUB_STEP_SUMMARY="$summary" GITHUB_EVENT_NAME=schedule GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=126 GOVULNCHECK_FINDINGS=true GOVULNCHECK_FINDING_FINGERPRINT=disabled GOVULNCHECK_FINDING_IDS=GO-2026-0001 REPORT_FILE="$report_file" "$script_dir/report_govulncheck_issue.sh"
assert_contains 'Issues are disabled' "$summary"
disabled_create_count=$(grep -c 'issue create' "$gh_log" || true)
if [[ "$disabled_create_count" -ne 1 ]]; then
	echo 'disabled repository attempted another issue create' >&2
	cat "$gh_log" >&2
	exit 1
fi

echo 'govulncheck reporting tests passed'
