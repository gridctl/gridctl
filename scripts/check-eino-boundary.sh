#!/usr/bin/env bash
# Boundary lint for the cloudwego/eino adapter.
#
# pkg/agent/internal/eino/ is the only place in the gridctl tree allowed
# to reference github.com/cloudwego/eino. The constraint is what keeps
# an eino swap a 1–2 week project rather than a multi-month rewrite —
# treat it as load-bearing.
#
# This script greps every file under pkg/agent/ for cloudwego/eino
# references and excludes files inside the adapter directory. Any other
# match fails the build.
#
# Exit codes:
#   0 = boundary intact (no eino references outside the adapter)
#   1 = at least one violation found

set -euo pipefail

ROOT="${1:-pkg/agent}"
ADAPTER_DIR="pkg/agent/internal/eino/"

if [[ ! -d "$ROOT" ]]; then
  echo "check-eino-boundary: $ROOT does not exist (skipping)"
  exit 0
fi

# grep -l prints matching filenames; null on no match. The `|| true`
# prevents set -e from tripping on grep's exit code 1 (no match).
MATCHES="$(grep -rl 'github.com/cloudwego/eino' "$ROOT" 2>/dev/null || true)"

if [[ -z "$MATCHES" ]]; then
  echo "check-eino-boundary: no eino references in $ROOT (clean tree)"
  exit 0
fi

VIOLATIONS=""
while IFS= read -r f; do
  case "$f" in
    "$ADAPTER_DIR"*) ;;  # allowed: inside the adapter
    *) VIOLATIONS+="$f"$'\n' ;;
  esac
done <<< "$MATCHES"

if [[ -z "$VIOLATIONS" ]]; then
  echo "check-eino-boundary: adapter boundary intact (all eino references inside $ADAPTER_DIR)"
  exit 0
fi

echo "check-eino-boundary: FAIL — eino references found outside $ADAPTER_DIR:"
printf '%s' "$VIOLATIONS" | sed 's/^/  /'
echo ""
echo "Move the eino touch behind the adapter in $ADAPTER_DIR and expose a"
echo "gridctl-shaped wrapper to the rest of pkg/agent."
exit 1
