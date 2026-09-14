#!/usr/bin/env bash
# Fail when runnable example stacks have unpinned image or package selectors.
#
# Exit handling:
#   gridctl validate --check-mutable-refs --format json
#     0  valid (info-only findings allowed)
#     1  validation errors -> fail this check
#     2  warnings only -> fail only for mutable-image-reference or
#        mutable-package-reference findings that are not excepted
#     other -> fail (unexpected exit)
# Unrelated existing example warnings stay non-fatal.
# Informational reference-not-assessed findings never fail this check.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${1:-"$ROOT/gridctl"}"
EXCEPTIONS_FILE="${ROOT}/examples/reference-exceptions.txt"

if [ ! -x "$BIN" ]; then
  echo "gridctl binary not found or not executable: $BIN" >&2
  echo "Build with 'task build:go' or pass a path as the first argument." >&2
  exit 1
fi

if [ ! -f "$EXCEPTIONS_FILE" ]; then
  echo "missing $EXCEPTIONS_FILE" >&2
  exit 1
fi

declare -A EXCEPT_ALL
declare -A EXCEPT_FIELD

while IFS= read -r line || [ -n "$line" ]; do
  line="${line%%$'\r'}"
  case "$line" in
    ''|'#'*) continue ;;
  esac
  path="${line%% *}"
  rest="${line#"$path"}"
  rest="${rest#"${rest%%[![:space:]]*}"}"
  if [ -z "$rest" ]; then
    EXCEPT_ALL["$path"]=1
  else
    EXCEPT_FIELD["$path $rest"]=1
  fi
done < "$EXCEPTIONS_FILE"

is_excepted() {
  local file="$1" field="$2"
  if [ -n "${EXCEPT_ALL[$file]+x}" ]; then
    return 0
  fi
  if [ -n "${EXCEPT_FIELD[$file $field]+x}" ]; then
    return 0
  fi
  return 1
}

mapfile -t files < <(find "$ROOT/examples" -name "*.yaml" \
  -not -name "skills.yaml" \
  -not -name "gridctl-pack.yaml" \
  -not -name "gateway-remote.yaml" \
  -not -path "*/model-policy/*" | sort)

if [ "${#files[@]}" -eq 0 ]; then
  echo "No example stacks found" >&2
  exit 1
fi

failed=0
for abs in "${files[@]}"; do
  rel="${abs#"$ROOT"/}"
  echo "Checking $rel..."
  out="$(mktemp)"
  set +e
  "$BIN" validate --check-mutable-refs --format json "$abs" >"$out" 2>/dev/null
  rc=$?
  set -e
  if [ "$rc" -eq 1 ]; then
    echo "  FAILED: validation errors in $rel" >&2
    failed=1
    rm -f "$out"
    continue
  fi
  if [ "$rc" -ne 0 ] && [ "$rc" -ne 2 ]; then
    echo "  FAILED: unexpected exit $rc for $rel" >&2
    failed=1
    rm -f "$out"
    continue
  fi
  fields="$(python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
except Exception as exc:
    print("JSON_ERROR:" + str(exc), file=sys.stderr)
    sys.exit(3)
for issue in data.get("issues") or []:
    msg = issue.get("message") or ""
    if msg.startswith("mutable-image-reference:") or msg.startswith("mutable-package-reference:"):
        print(issue.get("field") or "")
' <"$out")" || py_rc=$?
  py_rc="${py_rc:-0}"
  rm -f "$out"
  if [ "$py_rc" -ne 0 ]; then
    echo "  FAILED: could not parse JSON from $rel" >&2
    failed=1
    continue
  fi
  while IFS= read -r field || [ -n "$field" ]; do
    [ -z "$field" ] && continue
    if is_excepted "$rel" "$field"; then
      echo "  excepted $field"
      continue
    fi
    echo "  FAILED: unpinned selector at $field" >&2
    failed=1
  done <<<"$fields"
done

if [ "$failed" -ne 0 ]; then
  echo "Example dependency-reference check failed." >&2
  echo "Pin public images to a version tag plus index digest and npx/uvx selectors to an exact version," >&2
  echo "or add a documented placeholder exception in examples/reference-exceptions.txt." >&2
  exit 1
fi

echo "Example dependency references are pinned or excepted."
