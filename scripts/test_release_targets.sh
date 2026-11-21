#!/bin/sh
set -eu

script_dir=$(dirname "$0")
script_dir=$(cd "$script_dir" && pwd)
actual=$(bash "$script_dir/build.sh" --list-targets)
expected=$(cat <<'EOF'
darwin/amd64
darwin/arm64
dragonfly/amd64
freebsd/amd64
freebsd/arm64
illumos/amd64
linux/amd64
linux/arm64
netbsd/amd64
netbsd/arm64
openbsd/amd64
openbsd/arm64
plan9/amd64
solaris/amd64
windows/amd64
windows/arm64
EOF
)

if [ "$actual" != "$expected" ]; then
	echo "release target matrix changed unexpectedly" >&2
	echo "expected:" >&2
	printf '%s\n' "$expected" >&2
	echo "actual:" >&2
	printf '%s\n' "$actual" >&2
	exit 1
fi

target_count=$(printf '%s\n' "$actual" | wc -l | tr -d ' ')
unique_count=$(printf '%s\n' "$actual" | sort -u | wc -l | tr -d ' ')
if [ "$target_count" -ne "$unique_count" ]; then
	echo "release target matrix contains duplicates" >&2
	exit 1
fi

supported=$(go tool dist list)
for target in $actual; do
	case "$target" in
		*/amd64|*/arm64) ;;
		*)
			echo "release target has unsupported architecture: $target" >&2
			exit 1
			;;
	esac
	if ! printf '%s\n' "$supported" | grep -Fqx "$target"; then
		echo "release target is not supported by this Go toolchain: $target" >&2
		exit 1
	fi
done

echo "release target matrix validated ($target_count targets)"
