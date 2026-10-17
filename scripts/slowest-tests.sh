#!/bin/bash
# Print the 25 slowest top-level tests from a go test -json capture.
# Always exits 0. A missing capture, or a jq or sort failure, prints one notice.
# Usage: slowest-tests.sh <capture>
set -euo pipefail

# Numeric sort must treat "." as the decimal point.
export LC_ALL=C

notice() {
  echo "slowest-tests summary failed; see the capture on the runner"
}

capture="${1:-}"
if [[ -z "$capture" || ! -s "$capture" ]]; then
  echo "no capture at $capture"
  exit 0
fi

# jq and sort write to files. Under bash -e without pipefail, a pipeline's
# status is the last command, so a failing jq never reaches "||". Piping sort
# into head can also raise SIGPIPE when pipefail is on. Branch on each status.
rows=$(mktemp)
sorted=""
cleanup() {
  rm -f "$rows" "$sorted"
}
trap cleanup EXIT
sorted=$(mktemp)

filter='select(.Test != null and (.Action == "pass" or .Action == "fail")) | select(.Test | test("/") | not) | "\(.Elapsed)\t\(.Action)\t\(.Test)"'
if ! jq -r "$filter" "$capture" >"$rows"; then
  notice
  exit 0
fi
if ! sort -rn "$rows" >"$sorted"; then
  notice
  exit 0
fi
echo "Slowest top-level tests (elapsed seconds, result, name):"
head -25 "$sorted" || notice
exit 0
