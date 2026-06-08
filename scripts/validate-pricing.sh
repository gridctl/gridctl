#!/usr/bin/env bash
# Validate a candidate LiteLLM pricing snapshot before it replaces the committed
# one. Guards `make update-pricing` (and the scheduled refresh workflow) against a
# poisoned, empty, or truncated upstream response landing silently — the failure
# mode behind LiteLLM's 2026-01-27 model-cost-map incident, where a malformed but
# HTTP-200 `main` shipped invalid JSON.
#
# Usage:
#   validate-pricing.sh <candidate.json> [<reference.json>]
#
#   <candidate.json>   File to validate (required).
#   <reference.json>   Committed snapshot to compare entry counts against
#                      (optional). When omitted or absent, the shrink check is
#                      skipped (bootstrap case).
#
# Checks (any failure exits non-zero and touches nothing):
#   a. Candidate parses as JSON.
#   b. Top level is a non-empty object.
#   c. Priced-model count >= MIN_MODELS.
#   d. Priced-model count has not shrunk below SHRINK_PCT% of the reference.
#
# A "priced model" is counted the way pkg/pricing/litellm.go parses: a top-level
# object entry (excluding the `sample_spec` sentinel) carrying at least one
# nonzero numeric cost field. Non-model top-level keys are not assumed to be
# priced.
#
# Tunables (env):
#   MIN_MODELS   Minimum priced-model floor (default 1000).
#   SHRINK_PCT   Minimum percent of the reference count to accept (default 80).
#
# Exit codes:
#   0 = candidate is valid
#   1 = candidate failed a check, or arguments/tools are missing
set -euo pipefail

MIN_MODELS="${MIN_MODELS:-1000}"
SHRINK_PCT="${SHRINK_PCT:-80}"

candidate="${1:-}"
reference="${2:-}"

if [[ -z "$candidate" ]]; then
  echo "validate-pricing: usage: $0 <candidate.json> [<reference.json>]" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "validate-pricing: jq is required but not installed" >&2
  exit 1
fi

if [[ ! -f "$candidate" ]]; then
  echo "validate-pricing: candidate file not found: $candidate" >&2
  exit 1
fi

# (a) Parses as JSON.
if ! jq empty "$candidate" >/dev/null 2>&1; then
  echo "validate-pricing: FAIL — $candidate is not valid JSON" >&2
  exit 1
fi

# (b) Non-empty top-level object.
if ! jq -e 'type == "object" and (length > 0)' "$candidate" >/dev/null 2>&1; then
  echo "validate-pricing: FAIL — $candidate is not a non-empty JSON object" >&2
  exit 1
fi

# Count priced models the way the Go parser does: object entries (minus the
# sample_spec sentinel) with at least one nonzero numeric cost field.
count_models() {
  jq '[ to_entries[]
        | select(.key != "sample_spec")
        | select(.value | type == "object")
        # Mirror the Go parser (pkg/pricing/litellm.go): it unmarshals each entry
        # into float64 cost fields, so an entry where ANY cost field is a string
        # (the sample_spec-mirroring shape behind the 2026-01-27 poisoning) fails
        # to decode and is dropped whole. Reject such entries here too rather than
        # counting a sibling numeric field.
        | select(
            [ .value.input_cost_per_token,
              .value.output_cost_per_token,
              .value.cache_read_input_token_cost,
              .value.cache_creation_input_token_cost ]
            | all(. == null or type == "number")
          )
        | select(
            ( [ .value.input_cost_per_token,
                .value.output_cost_per_token,
                .value.cache_read_input_token_cost,
                .value.cache_creation_input_token_cost ]
              | map(select(type == "number" and . != 0))
              | length ) > 0
          )
      ] | length' "$1"
}

candidate_count="$(count_models "$candidate")"

# (c) Floor.
if (( candidate_count < MIN_MODELS )); then
  echo "validate-pricing: FAIL — $candidate has $candidate_count priced models, below floor of $MIN_MODELS" >&2
  exit 1
fi

# (d) Shrink guard (only when a valid reference is available).
if [[ -n "$reference" && -f "$reference" ]] && jq empty "$reference" >/dev/null 2>&1; then
  reference_count="$(count_models "$reference")"
  if (( reference_count > 0 )); then
    # candidate_count >= SHRINK_PCT% of reference_count, in integer math.
    if (( candidate_count * 100 < reference_count * SHRINK_PCT )); then
      echo "validate-pricing: FAIL — $candidate has $candidate_count priced models, below ${SHRINK_PCT}% of the committed $reference_count" >&2
      exit 1
    fi
  fi
fi

echo "validate-pricing: OK — $candidate has $candidate_count priced models (floor $MIN_MODELS)"
