#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/qbitctl-qbittorrent-watch.XXXXXX")
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

gh_log="$tmp/gh.log"
gh_list="$tmp/gh-list"
gh_body="$tmp/gh-body"
: >"$gh_log"
: >"$gh_list"

# A detected tag alone, and a successful integration run, never report.
INTEGRATION_OUTCOME=success "$script_dir/report_qbittorrent_compatibility.sh"
if [[ -s "$gh_log" ]]; then
	echo 'successful compatibility run unexpectedly called gh' >&2
	exit 1
fi

digest="sha256:$(printf 'a%.0s' {1..64})"
image="qbittorrentofficial/qbittorrent-nox:5.4.0@$digest"
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=true INTEGRATION_OUTCOME=failure GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=301 NEW_TAG=5.4.0 NEW_IMAGE="$image" NEW_DIGEST="$digest" "$script_dir/report_qbittorrent_compatibility.sh"
grep -Fq 'issue create' "$gh_log"
assert_contains '5.4.0' "$gh_body"
assert_contains 'sha256:' "$gh_body"
assert_contains 'actions/runs/301' "$gh_body"

# The same tag and digest update one existing issue rather than creating a duplicate.
printf '17\n' >"$gh_list"
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=true INTEGRATION_OUTCOME=failure GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=302 NEW_TAG=5.4.0 NEW_IMAGE="$image" NEW_DIGEST="$digest" "$script_dir/report_qbittorrent_compatibility.sh"
create_count=$(grep -c 'issue create' "$gh_log" || true)
if [[ "$create_count" -ne 1 ]]; then
	echo "expected one issue create, got $create_count" >&2
	cat "$gh_log" >&2
	exit 1
fi
grep -Fq 'issue comment' "$gh_log"

# A disabled Issues setting leaves the finding in the run summary without a write.
summary="$tmp/summary.md"
before=$(grep -Ec 'issue (create|comment|list)' "$gh_log" || true)
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=false GITHUB_STEP_SUMMARY="$summary" INTEGRATION_OUTCOME=failure GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=303 NEW_TAG=5.4.0 NEW_IMAGE="$image" NEW_DIGEST="$digest" "$script_dir/report_qbittorrent_compatibility.sh"
after=$(grep -Ec 'issue (create|comment|list)' "$gh_log" || true)
if [[ "$before" -ne "$after" ]]; then
	echo 'disabled Issues path attempted an issue command' >&2
	exit 1
fi
assert_contains 'Issues are disabled' "$summary"

# Reject a mismatched digest before making any GitHub API call.
set +e
PATH="$tmp:$PATH" GH_LOG="$gh_log" GH_LIST_RESULT="$gh_list" GH_BODY_CAPTURE="$gh_body" GH_API_RESULT=true INTEGRATION_OUTCOME=failure GITHUB_REPOSITORY=example/qbitctl GITHUB_SERVER_URL=https://github.com GITHUB_RUN_ID=304 NEW_TAG=5.4.0 NEW_IMAGE="$image" NEW_DIGEST=sha256:bad "$script_dir/report_qbittorrent_compatibility.sh" >/dev/null 2>&1
invalid_status=$?
set -e
if [[ "$invalid_status" -eq 0 ]]; then
	echo 'invalid digest unexpectedly succeeded' >&2
	exit 1
fi

# Exercise detector output and its exact digest validation without Docker Hub.
python3 - "$script_dir/check_qbittorrent_releases.py" <<'PY'
import contextlib
import importlib.util
import io
import sys

path = sys.argv[1]
spec = importlib.util.spec_from_file_location("check_qbittorrent_releases", path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

module.image_tags = lambda _: [{"name": "5.4.0", "digest": "sha256:" + "a" * 64}]
sys.argv = [path, "--current", "5.3.0", "--github-output"]
out = io.StringIO()
with contextlib.redirect_stdout(out):
    assert module.main() == 0
assert "newer=true" in out.getvalue()
assert "digest=sha256:" + "a" * 64 in out.getvalue()

module.image_tags = lambda _: [{"name": "5.4.0", "digest": "sha256:" + "a" * 63}]
sys.argv = [path, "--current", "5.3.0", "--github-output"]
with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
    assert module.main() == 1
PY

echo 'qBittorrent compatibility reporting tests passed'
