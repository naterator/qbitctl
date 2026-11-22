#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
	echo "usage: $0 COMMAND [ARG ...]" >&2
	exit 2
fi

output_dir=${GOVULNCHECK_OUTPUT_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/qbitctl-govulncheck}
mkdir -p "$output_dir"
raw_output="$output_dir/govulncheck.json"
stderr_output="$output_dir/govulncheck.stderr"
report="$output_dir/report.md"
metadata="$output_dir/metadata.txt"

set +e
"$@" >"$raw_output" 2>"$stderr_output"
scanner_status=$?
set -e

write_failure_metadata() {
	cat >"$metadata" <<EOF
has_findings=false
finding_count=0
finding_fingerprint=none
finding_ids=
scanner_status=$scanner_status
EOF
	if [[ -n ${GITHUB_OUTPUT:-} ]]; then
		cat "$metadata" >>"$GITHUB_OUTPUT"
	fi
	if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
		printf '### Go vulnerability scan\n\n%s\n' "$1" >>"$GITHUB_STEP_SUMMARY"
	fi
}

if (( scanner_status != 0 )); then
	write_failure_metadata "govulncheck failed to complete (status $scanner_status). See the scan log for details."
	cat "$stderr_output" >&2
	exit "$scanner_status"
fi

set +e
python3 "$(dirname "$0")/govulncheck_report.py" \
	--input "$raw_output" \
	--report "$report" \
	--outputs "$metadata"
report_status=$?
set -e

if (( report_status != 0 )); then
	write_failure_metadata 'Could not read the govulncheck report. See the scan log for details.'
	cat "$stderr_output" >&2
	exit 1
fi

printf 'scanner_status=0\n' >>"$metadata"
if [[ -n ${GITHUB_OUTPUT:-} ]]; then
	cat "$metadata" >>"$GITHUB_OUTPUT"
fi

if grep -q '^has_findings=true$' "$metadata"; then
	if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
		{
			printf '%s\n\n' '### Go vulnerability scan'
			cat "$report"
		} >>"$GITHUB_STEP_SUMMARY"
	fi
	cat "$report"
	exit 1
fi

if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
	printf '%s\n' '### Go vulnerability scan' 'No reachable vulnerabilities were reported.' >>"$GITHUB_STEP_SUMMARY"
fi
exit 0
