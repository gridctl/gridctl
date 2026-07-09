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
# NOTE on the docker/docker suppressions below: the github.com/docker/docker
# Go module is deprecated and permanently frozen at v28.5.2+incompatible
# (moby/moby discussion #52404); daemon-side fixes ship in Moby 29.x engine
# releases that will never be published to this module path. "A fixed
# docker/docker release" therefore cannot land. The real removal condition
# for every suppression below is migrating gridctl to the successor modules
# github.com/moby/moby/client and github.com/moby/moby/api. Until then,
# each entry documents why the finding is unreachable from gridctl's
# client-only usage.
#
# GO-2026-4887 # CVE-2026-34040 | Moby AuthZ plugin bypass via oversized request
#              bodies — daemon-only, not reachable via Docker client SDK calls.
#              Fixed in Moby engine 29.3.1 (never in this module path).
#
# GO-2026-4883 # CVE-2026-33997 | Moby off-by-one in plugin privilege validation
#              during `docker plugin install` — daemon-side operation, not
#              reachable via Docker client SDK calls.
#              Fixed in Moby engine 29.3.1 (never in this module path).
#
# GO-2026-5746 # CVE-2026-41567 | Moby `PUT /containers/{id}/archive` executes a
#              container binary on the host (`docker cp` into a container). Not
#              reachable: gridctl never calls CopyToContainer/CopyFromContainer/
#              ContainerArchive; flagged at import/init level only.
#
# GO-2026-5668 # CVE-2026-41568 | Moby `docker cp` race condition allows creating
#              arbitrary empty files on the host via symlink swap. Not reachable:
#              gridctl uses no container copy/archive APIs.
#
# GO-2026-5617 # CVE-2026-42306 | Moby `docker cp` race condition allows bind
#              mount redirection to a host path. Not reachable: gridctl uses no
#              container copy/archive APIs.
SUPPRESSED=(
  "GO-2026-4887"
  "GO-2026-4883"
  "GO-2026-5746"
  "GO-2026-5668"
  "GO-2026-5617"
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
