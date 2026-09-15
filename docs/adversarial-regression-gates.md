# Adversarial Regression Gates

This page describes the reviewed scenario index and the post-suite execution verifier. It is maintainer test accounting, not a security product, certification, or comprehensive attack catalog.

## What a passing case proves

Each indexed scenario names one existing guarantee and one exact Go package/test identity. A verifier `PASS` means that identity ran and passed in the designated lane's `go test -json` capture, and the containing package completed successfully. The test's own assertions establish the boundary (correlation, size rejection, cancellation, non-dispatch, or import preservation). The verifier does not infer protection from log text, skip messages, or test names.

A skipped required case is not evidence. Passing detection is not blocking. A successful handshake is not isolation verification. Docker results cannot satisfy a Podman-required scenario. JSON events do not identify the container engine.

## What this does not prove

- New parser rules, response limits, isolation, or credential policy
- Denial-of-service resistance, aggregate memory bounds, or fuzz coverage
- Merge enforcement (branch protection settings are independent)
- That every security-related test in the repository ran
- That feature-owned workstreams (#1220, #1221, #1222, #1223, #1224, #1226, #1228, #1229, and others) are complete

Unrelated skips, optional SSH, LiteLLM image absence on the general integration lane, and platform-inapplicable tests remain allowed outside the required set for that lane.

## Index

The reviewed source of mandatory identities is `tests/adversarial/index.yaml`. It is declarative: no commands, regex selectors, retries, or provisioning. CI still runs the existing whole suites. Rename a test in the same change as the index entry; scenario IDs stay stable.

List identities and focused reproduction commands:

```bash
task scenarios
```

Focused commands escape each `/`-separated name segment and include the lane's test tags, timeout, and runtime selection. They do not replace whole-suite acceptance.

## Verifier

`cmd/scenarioverify` is a repository test utility, not a `gridctl` subcommand. It reads an explicit lane, the index, and a completed `go test -json` capture. It does not execute tests.

Designated Gatekeeper jobs (`test`, `integration`, `podman-integration`) invoke `scripts/run-verified-tests.sh` with lanes `unit`, `integration`, and `podman-integration`. The script adds `-json -count=1 -race` while preserving coverage, tags, suite scope, and current timeouts. The general integration job sets `GRIDCTL_RUNTIME=docker` and fails a bounded `docker info` check when Docker is missing or is the wrong engine; the Podman job keeps its own engine setup. JSON events do not identify the runtime. Go, capture/tee, and verifier statuses are recorded separately; a successful verifier cannot mask a failed suite. Raw JSON captures stay on the runner; they are not uploaded as a new public artifact. LiteLLM and conformance jobs use `-count=1` but are not verifier lanes.

The verifier decodes events incrementally, discards output payloads, and rejects null or non-object records, missing actions, unfinished observed packages or tests, test events after a package terminal, parent completion before a required child runs, and Go `build-fail` events. Input, record, event, and identity counts are bounded. Unknown JSON fields remain allowed.

Reason codes: `SELECTOR_ABSENT`, `REQUIRED_SKIP`, `INCOMPLETE_EVIDENCE`, `TEST_FAILURE`, `PACKAGE_FAILURE`, `MALFORMED_INDEX`, `UNKNOWN_LANE`, `EMPTY_REQUIRED_SET`. Missing prerequisites never shrink the required set.

A failed required identity looks like:

```text
Required scenarios: FAIL
Lane: podman-integration
Go status: 0
Capture status: 0
Verifier status: 1
FAIL podman-rootless-network reason=SELECTOR_ABSENT
Expected boundary: rootless Podman runs the multi-container networking fixture
Observed: required package/test run and pass not observed
Next: inspect earlier suite failure, build tags, and selector drift
```

## Local workflow

```bash
task test
task test:integration
task test:verify
```

`task test` stays human-readable. `task test:verify` is the unit-lane capture path. Local Docker whole-suite runs with `-race` remain the general integration acceptance path; hosted Podman execution is still required for Podman-assigned cases.
