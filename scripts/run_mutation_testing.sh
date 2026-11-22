#!/usr/bin/env bash
set -euo pipefail

# Manual quarterly mutation check. Gremlins edits source files while it tests
# each mutant, so every run starts from a disposable archive of HEAD.
tool_module='github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0'
repo_root=$(git rev-parse --show-toplevel)
output_dir="$repo_root/mutation-results"
mkdir -p "$output_dir"
output_file="$output_dir/gremlins-v0.6.0.json"
log_file="$output_dir/gremlins-v0.6.0.log"

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/qbitctl-mutation.XXXXXX")
cleanup() {
	rm -rf -- "$work_dir"
}
trap cleanup EXIT

# A git archive gives Gremlins a clean, disposable copy and makes the tested
# revision explicit. Run this script after committing the code to be checked.
git archive --format=tar HEAD | tar -xf - -C "$work_dir"

exclude_args=()
while IFS= read -r file; do
	case "$file" in
		pkg/client/torrent_setters.go|pkg/client/validation.go|internal/releaseattest/verify.go)
			continue
		;;
	esac
	escaped=${file//./\\.}
	exclude_args+=(--exclude-files "^${escaped}$")
done < <(git ls-tree -r --name-only HEAD | while IFS= read -r file; do
	case "$file" in
		*.go) printf '%s\n' "$file" ;;
	esac
done)

gremlins_args=(
	go run "$tool_module" unleash
	--coverpkg './pkg/...,./internal/releaseattest'
	--workers "${MUTATION_WORKERS:-2}"
	--output-statuses 'lctv'
	--output "$output_file"
)
gremlins_args+=("${exclude_args[@]}")

(
	cd "$work_dir"
	"${gremlins_args[@]}"
) 2>&1 | tee "$log_file"

python3 - "$output_file" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
report = json.loads(path.read_text())
print(
    "mutation summary: "
    f"total={report.get('mutants_total', 0)} "
    f"killed={report.get('mutants_killed', 0)} "
    f"lived={report.get('mutants_lived', 0)} "
    f"not-covered={report.get('mutants_not_covered', 0)} "
    f"not-viable={report.get('mutants_not_viable', 0)} "
    f"efficacy={report.get('test_efficacy', 0):.2f}%"
)
PY
