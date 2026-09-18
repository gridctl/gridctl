# Gateway capability and sensitive-call primitives

The gateway owns an in-memory capability store and an internal sensitive-call
classification. The experimental [A2A adapter](config-schema.md#a2a) uses these
primitives for every dispatch. The store has no tuning fields or new listeners.
Existing sources keep their observer, token
counter, result formatting, and routing behavior unless trusted server
construction classifies their calls as sensitive.

## Authority and lifetime

Each store belongs to one gateway. Adapter generations share its accounting
lock, and replacement cannot allocate a separate global budget. A generation
binds logical server identity, card URL, full RPC endpoint, dialect, profile,
and approved card digest. Each conversation has an independent random session.
Caller labels are never inputs to capability authority.

Context and task handles contain 32 cryptographically random bytes encoded as
unpadded base64url, with the respective public prefixes `gca2a_c1_` and
`gca2a_t1_`. Lookup maps store SHA-256 digests rather than usable handles.
Completed operations clear their transient handle strings. There is no recovery
index, persistence, or store-dump endpoint.

A task authorizes reads and explicit cancellation. Continuation also requires
its matching context, parent relationship, and interrupted task state. A task
returned without context cannot acquire context authority from a later reply.
Validated top-level routing IDs alone introduce records. Nested references must
agree, and arbitrary application content never enters the store.

Generic conversations share remote-ID collision protection within the adapter
generation. Independent Bedrock session UUIDs partition the remote namespace.
Local random root identities do not provide remote isolation. Known-ID checks
cannot establish the provenance of a previously unseen ID or prevent a remote
agent from exposing its own shared memory.

The fixed bounds are:

| Resource | Per adapter generation | Per gateway |
|----------|------------------------|-------------|
| Live roots | 1024 | 4096 |
| Live tasks | 8192 | 32768 |
| Remote-ID collision entries and reservations | 65536 | 262144 |

Root lifetime is 24 hours, inherited by tasks without sliding renewal. Remote
IDs are limited to 1024 UTF-8 bytes. Admission reserves root, task, and collision
protection capacity atomically before dispatch, including potential handles.
Unused reservations are returned for direct-message responses. Exhaustion
refuses work instead of evicting authority or collision protection. Invalid
capability attempts have a per-generation aggregate budget of 100 per second
with burst 100, independent of caller labels. All invalid, expired, mismatched, or exhausted
lookup attempts return `capability_unavailable` without revealing membership.

Expiry is checked at admission, immediately before dispatch, and at commit.
An admission-time sweep reclaims expired live capacity across every active
generation, including idle adapters. Tombstones remain until generation
retirement. Retirement rejects new admission and cancels local request contexts;
pending callbacks retain their accounting charges until all callbacks drain.
Unregister retires the named generation, and gateway shutdown closes the store.
An untouched generation retains its authority.

## Concurrency and ambiguous outcomes

One send may be active per root. A single independent cancel can overlap only
with a resume of that same known task. Other overlaps fail immediately with
`operation_in_progress`. No store lock is held during network work.

Overlapping cancel admission advances a task revision. The older send must still
validate response identities, but its result becomes `operation_superseded` and
cannot publish state or authority. Once both operations drain, an authorized get
must observe interrupted or terminal state before another new/resume send is
permitted. Explicit cancellation remains available after the slots drain. A late
result cannot revive terminal work.

A mutation that may have reached the remote but has an unknown outcome marks
the task or root uncertain. Reads and explicit cancellation of known tasks remain
possible after active operations drain. A working/submitted read does not clear
uncertainty. An ambiguous context-only new turn cannot be reconciled by reading a
different known task, so new/resume mutations remain blocked until root expiry.

An undelivered fresh root keeps its reservation for up to 10 minutes, bounded by
root expiry. That lease reclaims capacity only; it never reauthorizes mutation on
a delivered uncertain root. Local timeout, expiry, replacement, disconnect, or
shutdown does not prove remote cancellation. Cleanup sends no remote cancellation
request, performs no automatic retry, and makes no exactly-once guarantee.

## Observations and diagnostics

Trusted construction metadata opts a call into sensitive handling. Caller
arguments and tool annotations cannot disable or enable that classification.
Sensitivity propagates through the shared context of a code-mode execution.

For sensitive calls, the gateway skips both `ToolCallObserver` and
`ClientObserver` payload callbacks. An observer can implement
`SensitiveToolCallObserver` to receive a value snapshot containing a sanitized
server name, replica number, local operation category, failure flag, duration,
and numeric token usage. It receives no argument, result, error, context, caller
label, session, handle, or digest reference. Failures also count as observations.
The metrics observer supports this interface. Operation is `send`, `task_get`,
or `task_cancel` for those exact local names, and `skill` otherwise. Usage is
attributed to the server, replica, and operation category, without client or
individual skill attribution.

Counting occurs locally before observation using the heuristic of four bytes
per token. The configured counter is not invoked because implementations may
send content to external services. Input estimates count serialized arguments;
output estimates count result content text before ordinary truncation. A Go
error contributes input usage with zero output tokens. Usage is approximate.
Sensitive calls also skip format conversion, whose accounting path can use that
configured counter.

Gateway instrumentation receives locally authored error categories and sanitized
name/label copies. Shared diagnostic sanitation recognizes typed capability
strings inside messages, keys, and nested JSON-compatible attributes. It does
not register each minted handle in a growing secret list. Code-mode logs contain
failure categories and timing, never parser source excerpts or thrown values;
caller-delivered errors and console results remain available.

Sensitive downstream Go errors are projected to a safe local category, preserving
context cancellation/deadline identity without retaining an untrusted error
chain. Ordinary downstream Go errors retain their caller-facing text. Shared log
redaction can turn a nested structured attribute into a sanitized JSON string
when a recognized secret occurs within it; log consumers must accept that shape.

This changes diagnostic output, including recognizable secret-bearing names and
labels in usage and run metadata. Consumers should correlate by generated
attempt/trace IDs and outcome categories instead of raw code errors or secret
identifiers. Ordinary caller-delivered source errors remain unchanged. The
diagnostic-output change is recorded under Unreleased for maintainer-owned
major-release scheduling under Article VIII.

Recognition is defense in depth, not a general information-flow sandbox. It
cannot identify arbitrary encoded or split secrets. Sensitive payload exclusion
protects those values at observer and diagnostic boundaries. A caller can still
deliberately disclose a possessed value through application output or sandbox
fetch. Transcripts, stdout capture, and browser developer tools are outside the
gateway's metadata confidentiality boundary.

Tests cover shared accounting and rollback, controlled-clock expiry, random
source failures, remote-ID conflicts, slot races, retirement, and real HTTP
cancel overlap. Disclosure checks use real file and memory log sinks, run
records, traces, and an OTLP HTTP receiver while preserving caller delivery.
