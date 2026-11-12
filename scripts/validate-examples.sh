#!/usr/bin/env bash
# Validate every runnable example stack with gridctl validate.
#
# Exit handling:
#   gridctl validate <file>
#     0  valid -> pass
#     1  validation errors -> fail this check
#     2  warnings only -> pass
#     other -> fail (unexpected exit)
# The list of runnable stacks, and the exclusions for non-stack YAML, live
# in scripts/example-stacks.sh.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHECK_ROOT="${GRIDCTL_CHECK_ROOT:-$ROOT}"
BIN="${1:-"$CHECK_ROOT/gridctl"}"

if [ ! -x "$BIN" ]; then
  echo "gridctl binary not found or not executable: $BIN" >&2
  echo "Build with 'task build:go' or pass a path as the first argument." >&2
  exit 1
fi

# shellcheck source=scripts/example-stacks.sh
source "$ROOT/scripts/example-stacks.sh"
mapfile -t files < <(example_stacks "$CHECK_ROOT")

if [ "${#files[@]}" -eq 0 ]; then
  echo "No example stacks found" >&2
  exit 1
fi

failed=0
for abs in "${files[@]}"; do
  rel="${abs#"$CHECK_ROOT"/}"
  echo "Validating $rel..."
  set +e
  out="$("$BIN" validate "$abs" 2>&1)"
  rc=$?
  set -e
  if [ "$rc" -eq 0 ] || [ "$rc" -eq 2 ]; then
    continue
  fi
  if [ "$rc" -eq 1 ]; then
    echo "  FAILED: validation errors in $rel" >&2
  else
    echo "  FAILED: unexpected exit $rc for $rel" >&2
  fi
  if [ -n "$out" ]; then
    printf '%s\n' "$out" | sed -e 's/^/    /' -e 's/^ *$//' >&2
  fi
  failed=1
done

if [ "$failed" -ne 0 ]; then
  echo "Example stack validation failed." >&2
  echo "Fix the stack, or, if the file is not a stack, add an exclusion in scripts/example-stacks.sh." >&2
  exit 1
fi

echo "${#files[@]} example stacks valid."
