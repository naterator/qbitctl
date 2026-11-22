#!/usr/bin/env bash
set -euo pipefail

# actionlint validates workflow files. Composite actions contain shell in
# YAML, so extract each literal run block and pass it through ShellCheck too.
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

if ! command -v shellcheck >/dev/null 2>&1; then
	echo "lint_actions: shellcheck is required" >&2
	exit 1
fi

for action in "$root"/.github/actions/*/action.yml; do
	[ -f "$action" ] || continue
	tmp=$(mktemp)
	trap 'rm -f "$tmp"' EXIT
	printf '%s\n' '#!/usr/bin/env bash' > "$tmp"
	awk '
		/^[[:space:]]+run: \|[[:space:]]*$/ {match($0, /^[ ]*/); base=RLENGTH; in_run=1; next}
		in_run && length($0) == 0 {print ""; next}
		in_run {
			match($0, /^[ ]*/)
			if (RLENGTH <= base) {in_run=0; next}
			print substr($0, base + 3)
		}
	' "$action" >> "$tmp"
	cleaned=$(mktemp)
	sed -E 's/\$\{\{[^}]*\}\}/qbitctl_expr/g' "$tmp" > "$cleaned"
	mv "$cleaned" "$tmp"
	if [ -s "$tmp" ]; then
		shellcheck "$tmp"
	fi
	rm -f "$tmp"
	trap - EXIT
done

echo "composite action shell checks passed"
