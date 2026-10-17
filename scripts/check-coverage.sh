#!/bin/bash
# Per-package coverage threshold enforcement.
# Exits non-zero if any critical package is below its minimum threshold.

set -euo pipefail

# Minimum coverage thresholds for critical packages.
declare -A THRESHOLDS=(
  ["pkg/controller"]=50
  ["pkg/runtime"]=60
  ["pkg/mcp"]=75
  ["pkg/reload"]=60
  ["pkg/config"]=70
)

FAILED=0

for PKG in "${!THRESHOLDS[@]}"; do
  THRESHOLD=${THRESHOLDS[$PKG]}
  PROFILE=$(mktemp)
  OUTPUT=$(mktemp)

  # Run tests and capture coverage for this package. Test output is kept
  # and printed only on failure, so a failing or flaky test is named in the
  # CI log instead of appearing as a bare "tests failed".
  if ! go test -coverprofile="$PROFILE" "./$PKG/..." > "$OUTPUT" 2>&1; then
    echo "FAIL  $PKG — tests failed"
    echo "----- go test output for $PKG -----"
    cat "$OUTPUT"
    echo "----- end of output for $PKG -----"
    FAILED=1
    rm -f "$PROFILE" "$OUTPUT"
    continue
  fi
  rm -f "$OUTPUT"

  # Extract total coverage percentage
  COVERAGE=$(go tool cover -func="$PROFILE" | tail -1 | awk '{print $3}' | sed 's/%//')
  rm -f "$PROFILE"

  if (( $(echo "$COVERAGE < $THRESHOLD" | bc -l) )); then
    echo "FAIL  $PKG — ${COVERAGE}% < ${THRESHOLD}% threshold"
    FAILED=1
  else
    echo "OK    $PKG — ${COVERAGE}% (threshold: ${THRESHOLD}%)"
  fi
done

if [ "$FAILED" -ne 0 ]; then
  echo ""
  echo "Per-package coverage check failed."
  exit 1
fi

echo ""
echo "All packages meet coverage thresholds."
