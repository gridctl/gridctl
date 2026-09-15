#!/usr/bin/env bash
# Capture a go test JSON stream and verify designated scenario execution.
# This script does not choose tests, retry, or provision runtimes.
set -uo pipefail

usage() {
  echo "usage: run-verified-tests.sh --lane LANE --index INDEX [--revision REV] [--capture FILE] [--summary FILE] -- [go test args]" >&2
  exit 2
}

LANE=""
INDEX=""
REVISION=""
CAPTURE=""
SUMMARY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --lane)
      LANE="${2:-}"
      shift 2
      ;;
    --index)
      INDEX="${2:-}"
      shift 2
      ;;
    --revision)
      REVISION="${2:-}"
      shift 2
      ;;
    --capture)
      CAPTURE="${2:-}"
      shift 2
      ;;
    --summary)
      SUMMARY="${2:-}"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *)
      usage
      ;;
  esac
done

if [[ -z "$LANE" || -z "$INDEX" ]]; then
  usage
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_TEST_CMD=${GO_TEST_CMD:-go test}
VERIFIER_CMD=${VERIFIER_CMD:-go run "${ROOT}/cmd/scenarioverify"}

cleanup() {
  if [[ -n "${CAPTURE_TEMP:-}" ]]; then
    rm -f "$CAPTURE_TEMP"
  fi
  if [[ -n "${SUMMARY_TEMP:-}" ]]; then
    rm -f "$SUMMARY_TEMP"
  fi
}
trap cleanup EXIT

if [[ -z "$CAPTURE" ]]; then
  CAPTURE="$(mktemp)"
  CAPTURE_TEMP="$CAPTURE"
fi
if [[ -z "$SUMMARY" ]]; then
  SUMMARY="$(mktemp)"
  SUMMARY_TEMP="$SUMMARY"
fi

set +e
set +o pipefail
# Intentionally capture PIPESTATUS immediately after the pipeline.
# -json -count=1 -race are required for designated invocations.
# Remaining arguments preserve coverage, tags, timeouts, and suite scope.
# shellcheck disable=SC2086
${GO_TEST_CMD} -json -count=1 -race "$@" | tee "$CAPTURE"
pipeline_status=("${PIPESTATUS[@]}")
go_status=${pipeline_status[0]}
capture_status=${pipeline_status[1]}

verify_status=1
# shellcheck disable=SC2086
${VERIFIER_CMD} \
  -lane "$LANE" \
  -index "$INDEX" \
  -events "$CAPTURE" \
  -revision "$REVISION" \
  -go-status "$go_status" \
  -capture-status "$capture_status" \
  -summary "$SUMMARY"
verify_status=$?
set -e

if [[ "$go_status" -ne 0 || "$capture_status" -ne 0 || "$verify_status" -ne 0 ]]; then
  echo "go_status=${go_status} capture_status=${capture_status} verifier_status=${verify_status}" >&2
  if [[ "$go_status" -ne 0 ]]; then
    exit "$go_status"
  fi
  if [[ "$capture_status" -ne 0 ]]; then
    exit "$capture_status"
  fi
  exit "$verify_status"
fi
exit 0
