#!/bin/bash
# Per-package coverage threshold enforcement from an existing profile.
# Usage: check-coverage.sh <coverage profile>
set -euo pipefail

# Percentages and bc comparisons must stay dot-decimal regardless of locale.
export LC_ALL=C

PROFILE="${1:-}"
if [[ -z "$PROFILE" || ! -r "$PROFILE" ]]; then
  echo "usage: check-coverage.sh <coverage profile>" >&2
  exit 2
fi

MODULE="github.com/gridctl/gridctl"
MODE=$(head -n1 "$PROFILE" | sed -n 's/^mode: //p')
if [[ "$MODE" != "set" && "$MODE" != "count" && "$MODE" != "atomic" ]]; then
  echo "FAIL  $PROFILE has no recognized mode header" >&2
  exit 1
fi

# Minimum coverage thresholds for critical packages.
declare -A THRESHOLDS=(
  ["pkg/controller"]=50
  ["pkg/runtime"]=60
  ["pkg/mcp"]=75
  ["pkg/reload"]=60
  ["pkg/config"]=70
)

# Stable order so CI logs can be compared. The trailing slash on the profile
# prefix makes pkg/mcp include pkg/mcp/... and exclude pkg/mcpauth, and makes
# pkg/runtime include pkg/runtime/docker.
ORDER=(pkg/controller pkg/runtime pkg/mcp pkg/reload pkg/config)

# A key added to only one list would otherwise pass silently: the loop
# follows ORDER and looks up THRESHOLDS.
if (( ${#ORDER[@]} != ${#THRESHOLDS[@]} )); then
  echo "check-coverage.sh: ORDER and THRESHOLDS differ" >&2
  exit 1
fi

FAILED=0

for PKG in "${ORDER[@]}"; do
  if [[ ! -v "THRESHOLDS[$PKG]" ]]; then
    echo "check-coverage.sh: ORDER and THRESHOLDS differ" >&2
    exit 1
  fi
  THRESHOLD=${THRESHOLDS[$PKG]}
  # Duplicate blocks (same file and range) merge before counting. set takes
  # the maximum count; count and atomic sum counts. A block is covered when
  # the merged count is greater than zero. Statement counts are not summed.
  COVERAGE=$(awk -v prefix="$MODULE/$PKG/" -v mode="$MODE" '
    NR == 1 { next }
    index($1, prefix) != 1 { next }
    {
      key = $1
      if (mode == "set") { if ($3 > count[key]) count[key] = $3 }
      else { count[key] += $3 }
      stmts[key] = $2
    }
    END {
      for (k in stmts) { total += stmts[k]; if (count[k] > 0) covered += stmts[k] }
      if (total == 0) { print "none"; exit }
      printf "%.1f", 100 * covered / total
    }' "$PROFILE")
  if [[ "$COVERAGE" == "none" ]]; then
    echo "FAIL  $PKG: no coverage blocks under $MODULE/$PKG/ in $PROFILE"
    FAILED=1
    continue
  fi
  if (( $(echo "$COVERAGE < $THRESHOLD" | bc -l) )); then
    echo "FAIL  $PKG — ${COVERAGE}% < ${THRESHOLD}% threshold"
    FAILED=1
  else
    echo "OK    $PKG — ${COVERAGE}% (threshold: ${THRESHOLD}%)"
  fi
done

if [[ "$FAILED" -ne 0 ]]; then
  echo ""
  echo "Per-package coverage check failed."
  exit 1
fi

echo ""
echo "All packages meet coverage thresholds."
