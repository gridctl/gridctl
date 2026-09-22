# External A2A verification

The experimental [outbound A2A adapter](config-schema.md#a2a) has a completed
real-agent rehearsal on September 21, 2026. Final acceptance of the phase-four
PR head remains pending. This record does not establish release readiness or
general hosted-agent compatibility. The feature remains Unreleased and off by
default.

## Evidence identity

The maintainer-authorized rehearsal ran from `2026-09-21T21:31:00Z` through
`2026-09-21T21:33:55Z` against gridctl commit
`da8073bb3c3eaf18367849d6a5d09f3bb4f6ca5d`. Both the standard daemon and a
separate observational gateway exercised genuine model-backed document reviewers.
The generic reviewer ran locally over loopback HTTP; the hosted reviewer ran on
AWS Bedrock AgentCore over TLS with Cognito CUSTOM_JWT bearer authentication.
Neither target was a canned JSON-RPC fixture.

Both executables were built with `-race -mod=readonly`:

| Executable | SHA-256 |
|------------|---------|
| Standard gridctl CLI/daemon | `c339c1eba4bf14f74bc8d3332878a1cb3248a7a3aac993557eb18a206e657f6f` |
| Observational gateway | `7d4da4f9a286e9bf055c66fb1014c4793ab1fc5996106055c7966f963d45e7c1` |

The observational executable composes the production controller, registrar,
API, gateway, and adapter with Go `net/http/httptrace` hooks. It observes
registration and request contexts without replacing the transport, modifying
requests, or bypassing policy. Session comparisons stay in memory; only boolean
outcomes and phase-scoped connection, header-write, and response counts enter
the evidence. The standard-daemon pass separately exercises normal CLI startup
and canonical tool dispatch.

### Agent and runtime versions

| Identity | Generic target | Hosted target |
|----------|----------------|---------------|
| Negotiated JSON-RPC dialect | A2A 1.0 | A2A 0.3 |
| Protocol SDK | Python `a2a-sdk==1.1.5` | Python `a2a-sdk==1.1.5` |
| Deployment runtime | Local Docker 29.7.0 | AgentCore runtime version `2`, `us-west-2` |
| Model | `nemotron3-nano-omni-30b`, operator-described Nemotron 3 Nano Omni 30B-A3B Reasoning, UD-Q6_K_XL | Bedrock `mistral.ministral-3-8b-instruct` |
| Agent source SHA-256 | `e51485acf27ba6f756b30bfa7e758185c256db02e36466f5793bce99da14f855` | `b2987ea408b7b04a3a4c088d8de76076c6eb8cb22c591759fba305264b969c12` |
| Image identity | Local image ID `sha256:5f13605eebf59ef1d919002b29d25606a8424624b539683719fe9a5b83151ba5` | Registry digest `sha256:f887715d69fbdd59ea624b9d3bab56cd7057b7a46a6d1c83ffb17b1f505121cc` |
| Raw Agent Card bytes, SHA-256 | `774ff79d6e8eeaeec80e3bb2ec1e6d6bb81c34a4ebd9da08724692ee6c768c8f` | `a8109990f654eb52acfc1b2d27e7c448058b4ce6acae0ca4db33247910cebb70` |

The generic SDK source revision recorded during setup was
`9f0f00cb0417cb59958d3186d81651be7d9b9d59` in
[a2aproject/a2a-python](https://github.com/a2aproject/a2a-python).
The approved local model weights had SHA-256
`9b6f1172d7d1a7f6c8acc44ec17f763ac7d5d7aae511d73fb9ad72954cfe11c4`;
the projection weights had SHA-256
`8e72b13d61ac5972793b94e130e69d93ebbc96bc8e281500c03e762282b1840f`.
These are artifact identities, not independent model provenance attestations.
The provider's underlying managed runtime build and model-weight revision are
Unknown. AgentCore runtime version `2` identifies this deployment version,
not a version of the AWS service.

Card digests identify fetched bytes, not the aggregate server pin hash used by
`gridctl pins approve --expect`. This record publishes no card bodies, endpoint
identifiers, tokens, capability handles, session values, raw task/context IDs,
conversation content, or hashes of those secrets.

## Observed workflows

Each result below passed for both targets in both the standard-daemon and
observational passes. These are observations of the identified rehearsal only.

| Workflow | Observed outcome |
|----------|------------------|
| Immediate result | Model-backed response returned a completed Task with the expected answer |
| Long-running poll | Immediate return exposed a live Task; subsequent authorized polling reached completion |
| Explicit cancel | Cancellation returned `canceled`, and a subsequent get retained that state |
| Input-required continuation | Approval resumed the original task with both matching capabilities and completed |
| Blocking resume/cancel overlap | Same-task cancel completed while resume was still blocked; resume failed, and a subsequent get confirmed `canceled` |
| Conversation isolation | Each of two conversations recalled its own random marker and did not return the other conversation's marker when asked; both directions had positive memory controls |
| Same-label authority rejection | Invalid get/cancel handles, mismatched context/task pairs, and task-only continuation failed despite the same caller label |
| Intentional capability transfer | Another permitted caller label used the supplied context capability and retained that conversation's memory |

The isolation conversations were created sequentially. This external evidence
does not establish concurrent fresh-root isolation or arbitrary adversarial
prompt resistance. The cancellation cases used deliberately cancellable review
windows. A remote acknowledgment does not prove that underlying model compute
stopped or that provider billing ceased.

### Session and dispatch observations

On the hosted target, every observed RPC carried a session header. Discovery was
distinct from conversation sessions, and the two conversation roots had distinct
sessions. Continuation, input approval, polling, cancellation, blocking
resume/cancel overlap, and deliberate capability transfer retained their bound
sessions. Generic RPCs carried no Bedrock session header.

For each target's invalid-authority phase, the observer recorded zero connection
attempts, zero card header writes, zero RPC header writes, and zero responses.
Positive dispatch controls recorded real HTTP writes and responses. These are
gateway-side HTTP observations, not an inference from eventually consistent
provider logs. They do not establish that no unrelated activity occurred on the
remote service.

Capabilities authorize possession within current gateway policy. Matching a
caller label is not task ownership, and successful intentional transfer is
expected bearer behavior. The observed conversation separation belongs to these
specific agents. Generic shared memory, external stores attached to Bedrock
agents, malicious agent behavior, and previously unseen foreign remote IDs
remain downstream trust boundaries. Workloads requiring tenant confidentiality
need downstream enforcement or separate deployments and credentials.

## Final acceptance and maintenance

Final PR-head evidence is pending and is separate from this rehearsal. Before PR
creation, the controller-owned preflight must verify unexpired authorization,
frozen agent/helper/model identities, live runtime and authorizer configuration,
and exact card bytes. After PR creation and CI, the maintainer-authorized external
gate must build both race-enabled executables from the clean, exact PR head,
repeat every workflow and observation above against both genuine targets, and
recheck the checkout, remote PR head, and deployment identities. It is required
before merge or manual-merge handoff, including when auto-merge is off.

The bounded deployment authorization expires at `2026-09-22T18:53:23Z`, with
teardown scheduled one second later. Completed final evidence is reusable for
at most six hours, subject to that earlier deadline and fresh identity checks.
A changed head needs new evidence. At most five candidate-head attempts are
authorized. Missing, stale, incomplete, or mismatched evidence fails closed;
rehearsal records cannot satisfy the final gate. Expiry requires a new authorized
setup, not a weakened check.

The operator retains an integrity-authenticated, metadata-only record bound to
the exact head, PR identity, executable hashes, target identities, outcomes, and
session checks. Authentication protects integrity within the trusted operator
environment; it is not independent attestation against control of that account.
A failed or interrupted attempt requires operator reconciliation of remote work
before another attempt. Do not automatically replay mutations or reset card
trust to obtain a passing result.

The external workflow uses canonical REST/CLI calls. It does not independently
establish upstream MCP negotiation, every protocol error case, or all observation
sink protections. The race-enabled real HTTP/subprocess suites provide separate
regression evidence for those adapter boundaries; CI fixtures alone cannot close
genuine-agent compatibility or isolation acceptance. Successful final acceptance
would remain scoped to its recorded targets, versions, and workflows.
