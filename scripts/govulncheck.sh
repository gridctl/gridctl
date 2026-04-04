#!/usr/bin/env bash
# govulncheck wrapper that suppresses known vulnerabilities with no available fix.
#
# Exit codes:
#   0 = no symbol-reachable vulnerabilities (or all are suppressed)
#   1 = internal/parse error
#   3 = unsuppressed symbol-reachable vulnerabilities found
#
# To add a suppression, append to SUPPRESSED below with:
#   - The govulncheck ID (GO-YYYY-NNNN)
#   - CVE number
#   - Why it is suppressed and when to revisit
set -euo pipefail

# ---------------------------------------------------------------------------
# Suppressed vulnerabilities
# ---------------------------------------------------------------------------
# GO-2026-4887 # CVE-2026-34040 | Moby AuthZ plugin bypass via oversized request
#              bodies — daemon-only, not reachable via Docker client SDK calls.
#              Fixed in Moby v29.3.1 but v29+ not yet published to Go module
#              proxy. Remove suppression once github.com/docker/docker@v29+ lands.
#
# GO-2026-4883 # CVE-2026-33997 | Moby off-by-one in plugin privilege validation
#              during `docker plugin install` — daemon-side operation, not
#              reachable via Docker client SDK calls.
#              Fixed in Moby v29.3.1 but v29+ not yet published to Go module
#              proxy. Remove suppression once github.com/docker/docker@v29+ lands.
SUPPRESSED=(
  "GO-2026-4887"
  "GO-2026-4883"
)

# ---------------------------------------------------------------------------
# Run govulncheck (JSON format always exits 0; we parse findings ourselves)
# ---------------------------------------------------------------------------
JSON_OUTPUT=$(govulncheck -format json ./... 2>&1)

# Extract unique vuln IDs that have at least one function-level trace entry
# (symbol-reachable — not merely imported)
FOUND_IDS=$(echo "$JSON_OUTPUT" \
  | jq -r 'select(has("finding") and (.finding.trace | map(has("function")) | any)) | .finding.osv' \
  | sort -u)

if [[ -z "$FOUND_IDS" ]]; then
  echo "govulncheck: no symbol-reachable vulnerabilities found"
  exit 0
fi

# Build suppression pattern
SUPPRESSED_PATTERN=$(IFS='|'; echo "${SUPPRESSED[*]}")

UNSUPPRESSED=$(echo "$FOUND_IDS" | grep -vE "^(${SUPPRESSED_PATTERN})$" || true)

if [[ -z "$UNSUPPRESSED" ]]; then
  echo "govulncheck: all findings are suppressed pending upstream fix:"
  for id in $FOUND_IDS; do
    echo "  - ${id} (suppressed)"
  done
  exit 0
fi

# Unsuppressed vulnerabilities — print full govulncheck text output and fail
govulncheck ./... || true
echo ""
echo "govulncheck: unsuppressed vulnerabilities found: $(echo "$UNSUPPRESSED" | tr '\n' ' ')"
exit 3
