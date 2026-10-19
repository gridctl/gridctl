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

Unrelated skips, optional SSH, and platform-inapplicable tests remain allowed outside the required set for that lane.

## Index

The reviewed source of mandatory identities is `tests/adversarial/index.yaml`. It is declarative: no commands, regex selectors, retries, or provisioning. CI still runs the existing whole suites. Rename a test in the same change as the index entry; scenario IDs stay stable.

List identities and focused reproduction commands:

```bash
task scenarios
```

Focused commands escape each `/`-separated name segment and include the lane's test tags, timeout, and runtime selection. They do not replace whole-suite acceptance.

## Verifier

`cmd/scenarioverify` is a repository test utility, not a `gridctl` subcommand. It reads an explicit lane, the index, and a completed `go test -json` capture. It does not execute tests.

The test, integration, conformance, podman-integration, and frontend jobs start together. The integration and podman-integration jobs do not wait on the unit job: those lanes consume nothing from it, and the required job still aggregates every gate. Each designated job invokes `scripts/run-verified-tests.sh` with lanes `unit`, `integration`, `podman-integration`, and `conformance`. The script adds `-json -count=1 -race` while preserving coverage, tags, suite scope, and current timeouts. The general integration job sets `GRIDCTL_RUNTIME=docker` and fails a bounded `docker info` check when Docker is missing or is the wrong engine; the Podman job keeps its own engine setup. JSON events do not identify the runtime. Go, capture/tee, and verifier statuses are recorded separately; a successful verifier cannot mask a failed suite. After the suite, each designated lane prints its 25 slowest top-level tests (elapsed seconds, result, and name). A missing capture, a jq failure, or a sort failure prints a one-line notice instead. That summary never fails the job. Raw JSON captures stay on the runner; they are not uploaded as a new public artifact. The conformance lane is one of those designated jobs. It runs `TestConformance` through the same wrapper, with Node 22 and no Docker. A skip of a required leaf is `REQUIRED_SKIP` and fails the job.

The verifier decodes events incrementally, discards output payloads, and rejects null or non-object records, missing actions, unfinished observed packages or tests, test events after a package terminal, repeated or conflicting results for the same package/test identity, and Go `build-fail` events. Slash characters in a test name are not capture-time parent/child nesting; a parent pass does not credit a missing required child. Input, record, event, and identity counts are bounded. Unknown JSON fields remain allowed.

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

## Updating a conformance baseline

Run `task test:conformance`. Copy the failing check id from the runner output and add a per-check entry with a reason. Write it as `<scenario>:<check-id>` with no space after the colon. Never add a scenario that passes on the fleet run. Bump the suite pin and the baselines together. A listed pass fails the run, so remove an entry that starts passing. A baselined failure is still a failure against the requirement set.

## Local workflow

```bash
task verify
task test
task test:integration
```

`task verify` is the local gate. It runs the unit-lane capture and scenario verification, backend and frontend lint, frontend tests, and the Python policy tests (Python 3.11+, jsonschema 4.23.0, PyYAML 6.0.3), each once, and stops at the first failure. `task test` is the human-readable loop and is not needed before `task verify`. `task test:verify` remains the unit-lane capture path and is included in `task verify`. Local Docker whole-suite runs with `-race` remain the general integration acceptance path; hosted Podman execution is still required for Podman-assigned cases.
