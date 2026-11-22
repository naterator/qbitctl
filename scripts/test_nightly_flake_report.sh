#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
reporter="$script_dir/report_nightly_flake.sh"
tick='`'
fake_dir=$(mktemp -d)
work_dir=$(mktemp -d)
trap 'rm -rf "$fake_dir" "$work_dir"' EXIT

cat > "$fake_dir/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >> "${GH_FAKE_LOG:?}"
if [[ "$1" == api ]]; then
	printf '%s\n' "${GH_FAKE_ISSUES:-true}"
	exit 0
fi
if [[ "$1 $2" == 'issue list' ]]; then
	if [[ "${GH_FAKE_EXISTING:-}" == 1 ]]; then
		printf '42\n'
	else
		printf '\n'
	fi
	exit 0
fi
if [[ "$1 $2" == 'issue create' || "$1 $2" == 'issue comment' ]]; then
	body_file=''
	while (( $# > 0 )); do
		if [[ "$1" == --body-file ]]; then
			body_file=$2
			shift 2
		else
			shift
		fi
	done
	if [[ -n "$body_file" && -n "${GH_BODY_COPY:-}" ]]; then
		cp "$body_file" "$GH_BODY_COPY"
	fi
	exit 0
fi
printf 'unexpected fake gh call: %s\n' "$*" >&2
exit 2
EOF
chmod +x "$fake_dir/gh"

cat > "$work_dir/nightly-race-ubuntu-26.04.txt" <<'EOF'
--- FAIL: TestRace (0.00s)
-test.shuffle 123456789
FAIL	github.com/naterator/qbitctl/pkg/client	0.01s
FAIL	github.com/naterator/qbitctl/internal/releaseattest	[build failed]
-test.shuffle 739872321
FAIL	github.com/naterator/qbitctl/internal/configfile	0.01s
FAIL
EOF
cat > "$work_dir/nightly-race-macos-26.txt" <<'EOF'
--- FAIL: TestRace (0.00s)
-test.shuffle 987654321
FAIL github.com/naterator/qbitctl/internal/tui
FAIL
EOF
run_reporter() {
	GH_FAKE_ISSUES=${GH_FAKE_ISSUES:-true} \
	GH_FAKE_EXISTING=${GH_FAKE_EXISTING:-} \
	GH_FAKE_LOG="$work_dir/gh.log" \
	GH_BODY_COPY="${GH_BODY_COPY:-}" \
	GITHUB_EVENT_NAME=schedule \
	GITHUB_REPOSITORY=example/qbitctl \
	GITHUB_SERVER_URL=https://github.example \
	GITHUB_RUN_ID=9876 \
	GITHUB_STEP_SUMMARY="$work_dir/summary.md" \
	PATH="$fake_dir:$PATH" \
	"$reporter" "$work_dir"
}

: > "$work_dir/gh.log"
: > "$work_dir/summary.md"
GH_FAKE_ISSUES=true GH_FAKE_EXISTING='' GH_BODY_COPY="$work_dir/body.md" run_reporter >/dev/null
grep -qF 'issue create --repo example/qbitctl' "$work_dir/gh.log"
grep -qF 'https://github.example/example/qbitctl/actions/runs/9876' "$work_dir/body.md"
grep -qF "${tick}ubuntu-26.04${tick} (Linux; reproduce with: ${tick}go test -race -count=3 -shuffle=123456789 github.com/naterator/qbitctl/pkg/client${tick})" "$work_dir/body.md"
grep -qF "${tick}macos-26${tick} (macOS; reproduce with: ${tick}go test -race -count=3 -shuffle=987654321 github.com/naterator/qbitctl/internal/tui${tick})" "$work_dir/body.md"
grep -qF 'go test -race -count=3 -shuffle=739872321 github.com/naterator/qbitctl/internal/configfile' "$work_dir/body.md"
grep -qF 'github.com/naterator/qbitctl/internal/releaseattest; no shuffle seed in test output' "$work_dir/body.md"
if grep -qF 'go test -race -count=3 -shuffle=123456789 github.com/naterator/qbitctl/internal/configfile' "$work_dir/body.md"; then
	echo 'shuffle seed was attributed to the wrong package' >&2
	exit 1
fi

: > "$work_dir/gh.log"
GH_FAKE_ISSUES=true GH_FAKE_EXISTING=1 GH_BODY_COPY="$work_dir/comment.md" run_reporter >/dev/null
grep -qF 'issue comment 42 --repo example/qbitctl' "$work_dir/gh.log"
if grep -qF 'issue create' "$work_dir/gh.log"; then
	echo 'existing issue scenario unexpectedly created a new issue' >&2
	exit 1
fi

: > "$work_dir/gh.log"
: > "$work_dir/summary.md"
GH_FAKE_ISSUES=false GH_FAKE_EXISTING='' run_reporter >/dev/null
grep -qF 'https://github.example/example/qbitctl/actions/runs/9876' "$work_dir/summary.md"
if grep -qF 'issue ' "$work_dir/gh.log"; then
	echo 'issues-disabled scenario called an issue API' >&2
	exit 1
fi

: > "$work_dir/gh.log"
GITHUB_EVENT_NAME=workflow_dispatch GITHUB_REPOSITORY=example/qbitctl \
	GITHUB_RUN_ID=9876 PATH="$fake_dir:$PATH" "$reporter" "$work_dir" >/dev/null
if [[ -s "$work_dir/gh.log" ]]; then
	echo 'manual run unexpectedly contacted gh' >&2
	exit 1
fi

echo 'nightly flake reporter tests passed'
