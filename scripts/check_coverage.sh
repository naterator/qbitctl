#!/usr/bin/env bash
set -euo pipefail

coverage_file=${1:-cover.out}
floor_file=${2:-.coverage-floor}

if [[ ! -s "$coverage_file" ]]; then
  printf 'coverage profile is missing or empty: %s\n' "$coverage_file" >&2
  exit 1
fi
if [[ ! -s "$floor_file" ]]; then
  printf 'coverage floor is missing or empty: %s\n' "$floor_file" >&2
  exit 1
fi

actual=$(go tool cover -func="$coverage_file" | awk '$1 == "total:" {print $3}')
floor=$(awk 'NF {print $1; exit}' "$floor_file")

if [[ ! "$actual" =~ ^[0-9]+([.][0-9]+)?%$ ]]; then
  printf 'could not read total coverage from %s\n' "$coverage_file" >&2
  exit 1
fi
if [[ ! "$floor" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  printf 'coverage floor must be a percentage without a %% sign: %s\n' "$floor" >&2
  exit 1
fi

actual_value=${actual%%%}
if ! awk -v actual="$actual_value" -v floor="$floor" 'BEGIN { exit !(actual + 0.5 >= floor) }'; then
  printf 'coverage %s is more than 0.5 points below floor %s%%\n' "$actual" "$floor" >&2
  exit 1
fi

printf 'coverage floor passed: %s (floor %s%%)\n' "$actual" "$floor"
