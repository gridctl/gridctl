# Practical Security Guide

Start with a private gateway, enable authentication, and give each downstream server only the credentials and host access its work requires. Gridctl operates with your authority over local files and the container runtime. Treat an imported server, skill, or pack as software you are choosing to run.

This guide is for a single operator. A shared gateway token, tool group, or client selector does not provide isolation between mutually distrustful users. The [threat model](threat-model.md) records the trust boundaries and source/test evidence; [SECURITY.md](../../SECURITY.md) explains private vulnerability reporting.

## Baseline and availability

Initial guide revision: September 15, 2026, reviewed against source commit [`a146ddb877f145ab98a32a2b90b052a5548b39af`](https://github.com/gridctl/gridctl/tree/a146ddb877f145ab98a32a2b90b052a5548b39af).

The finite baseline covers installation, listener access, workload authority, secrets, content review, diagnostic privacy, and declaration checks implemented at that revision. It does not wait for every roadmap feature. Check `gridctl version`, the [changelog](../../CHANGELOG.md), and your release's documentation before using a command below.

| Availability at this baseline | Capabilities |
|---|---|
| Included in `v1.0.0-rc.1` | Grouped-route authentication, non-resolving stack export, scoped variable delivery, internal-credential filtering, and authenticated binary-release tooling |
| Implemented after that tag; marked Unreleased | Restart-required authentication preflight and browser credential verification, opt-in execution profiles, passive security evidence reports, mutable-reference diagnostics, offline declaration policy, and opt-in persisted run records |
| Outside this baseline | A published Gridctl Python MCP runtime base. This guide provides no commands or image tags for that proposal. |

Implementation in a checkout does not establish release publication or runtime support. Follow the [execution support requirements](../execution.md#evidence-and-lifecycle) for your actual host and daemon.

## Install bytes you have verified

Use the [release verification procedure](../release-verification.md#verify-before-installing) before extracting or executing a provenance-covered archive. Obtain the verifier independently, choose the expected tag and full source commit independently of the downloaded bundle, and require successful verification of the repository, workflow, tag, source digest, and artifact bytes. Install those same verified local bytes.

The installer, updater, and Homebrew flow check archive checksums but do not automatically enforce that origin policy. A checksum can detect changed bytes against a trusted expected digest; an attacker who replaces both the archive and its unauthenticated checksum defeats that check. Authenticated provenance establishes the expected build origin, not harmless code or complete dependency coverage. Earlier releases and local builds are outside the release policy's guarantee.

For downstream servers, review publisher and dependency choices separately. Prefer reviewed image digests and locked build inputs. A digest identifies content; it does not authenticate its publisher. An exact `npx` or `uvx` package version does not lock all transitive dependencies. Use the repository's [pinned examples](../../examples/) as starting points, then review each update rather than replacing pins with floating tags.

## Keep the listener private and authenticate clients

The gateway binds to `127.0.0.1` by default. Authentication is opt-in on loopback, so another process on the same host can otherwise call its operational API. Configure authentication even for a local gateway when access should require a credential.

Merge this fragment into the existing `gateway` block. Supply `GRIDCTL_GATEWAY_TOKEN` through the launching process's environment, using your secret-management workflow; do not put its value in the file or shell history. The reserved `GRIDCTL_` prefix keeps this environment credential out of downstream local-process inheritance.

```yaml
gateway:
  bind: 127.0.0.1
  auth:
    type: bearer
    token: "${GRIDCTL_GATEWAY_TOKEN}"
```

Supply the credential separately through each client's authentication settings. Linking a client selects an endpoint but does not provision credentials. Bearer auth uses `Authorization: Bearer <token>`; API-key auth sends the raw value in the configured header. See the [authentication schema](../config-schema.md#auth).

With auth configured, a request to `/api/status` without a credential should return `401`. Run this read-only check against your selected local port:

```bash
curl --silent --output /dev/null --write-out '%{http_code}\n' \
  http://127.0.0.1:8180/api/status
```

Verify authenticated access through your configured client as well. A successful `/health`, `/ready`, or UI-shell request does not test authentication: those surfaces are public. Grouped MCP routes require the same gateway credential. Possession of that shared credential also grants operational API access; selecting a group is not a management-access restriction.

For remote use, keep the backend private to a TLS-terminating proxy or encrypted tunnel. Restrict who can reach the proxy and management surface, and configure the appropriate allowed Hosts and Origins for that deployment. Plain remote HTTP exposes the shared token in transit. The non-loopback unauthenticated-bind refusal is a guardrail, not TLS. `--insecure-allow-unauthenticated` explicitly bypasses that refusal and should not be used to resolve an authentication setup failure.

Host validation helps defend loopback arrivals against DNS rebinding. CORS controls browser response access, not authorization: default origins are wildcard, and an unlisted REST Origin does not prevent handler execution. Neither replaces a gateway credential or a private listener. Downstream OAuth login is a separate authorization flow and does not authenticate upstream clients.

### Change credentials through a process restart

In the Unreleased lifecycle implementation, authentication, bind, Host/Origin allowlists, and the insecure override are fixed at startup. A `restart_required` response rejects the entire reload, including ordinary edits submitted with the security change. Saved YAML can differ from live settings; repeated reloads do not activate the new token.

Follow [restart recovery](../troubleshooting.md#gateway-security-requires-a-restart) with the original startup options. Verify the new credential works and the old credential is rejected after restarting. Applying while the old daemon is still running is not a process restart.

The CLI's daemon state contains the resolved token in a `0600` file. Browser credentials are verified before persistence and stored in same-origin localStorage; origin scripts can read them. Protect the state directory, workstation, browser profile, and scripts served on that origin. See [browser credentials](../config-schema.md#browser-credentials) for custom-header and storage limits.

## Restrict downstream authority

Review the server's executable or image, credentials, writable state, network destinations, and exposed tools before applying a stack. Per-server tool lists, groups, and client policies reduce the tool surface. Client names and selectors are self-declared, so they are not authenticated user identities. Rate limits are opt-in call-rate controls, not workload memory or CPU limits.

Execution profiles are Unreleased at this baseline. Omitting `execution` preserves compatibility behavior. Read the [execution guide](../execution.md) before selecting a profile:

| Server type | Operator choice | What to verify |
|---|---|---|
| Container | Opt into `execution.mode: hardened`; choose nonzero numeric UID/GID appropriate to the image | Actual per-replica admission evidence, including read-only state, capabilities, seccomp, finite resource limits, and networking. Linux instance-bound observations of the actual daemon workload are required. |
| Local command | Opt into `execution.mode: local`; inherit only needed environment names and use absolute executable lookup | The executable and any bootstrap code remain trusted local software with your filesystem and network authority. Environment hygiene is not a sandbox. |
| External URL, OpenAPI, or SSH | Review the remote service and its deployment controls separately | Local gateway checks cannot establish remote confinement or prove remote descendants stopped. |

Hardened containers default to network none, read-only root, all capabilities dropped, and bounded scratch. HTTP/SSE needs the explicit connected-network exception. Connected networking is not destination filtering or host isolation. Read-only mounts can disclose secrets, persistent volumes are not storage-bounded, and mounted sockets can carry authority even without IP networking. Runtime restrictions do not constrain image pulls or build downloads. Supporting resource containers are outside these profiles.

A saved declaration, successful container creation, or healthy MCP connection does not prove the requested controls are effective. Check per-replica execution reports through `gridctl status --json` or server details. Missing required evidence refuses routing. Observations are snapshots; a process can act before post-start verification, and checks cannot undo those actions. Do not remove restrictions merely to turn a refused or unknown state green.

## Handle secrets and shared configuration deliberately

The variable store supports plaintext operation. Enable encryption using the [variable commands](../cli-reference.md#variables) when you need encryption at rest, and check its lock state. An encrypted store loads locked; an unlocked daemon and an authorized recipient still hold plaintext values. Downstream OAuth grants use separate storage with a machine-local key; copying both key and ciphertext exposes the grants.

Use references such as `${var:GITHUB_TOKEN}` in authored stack files. Select only necessary variables or sets for each workload. `gridctl var explain GITHUB_TOKEN` helps inspect resolution and consumers without printing its value. Variable declarations document prerequisites; they do not themselves supply values or enforce deployment admission.

For an arbitrary command, `var run` requires explicit selection. Prefer `--only` or a scoped set to broad delivery. Its default exact-secret redaction applies to non-interactive output; interactive output is raw, and transformed values or network transmissions are not prevented. Trust the recipient before delivering a secret.

Before committing, `gridctl var scan --staged` checks staged blobs for eligible exact current stored secrets. Inspect skips and completeness. It does not replace a general secret scanner or a history review, and it cannot discover arbitrary or transformed credentials.

For sharing, use [stack export](../cli-reference.md#export-semantics) or the Stack view's Export YAML action. Export preserves references without resolving environment or stored values and rejects recognized inline credentials. Review remaining authored literals manually: bounded field classification cannot guarantee arbitrary text is secret-free. Raw stack files, spec retrieval/editing, `var export`, and a stack export have different disclosure boundaries. Do not substitute a raw spec download when export refuses a credential.

## Review pins, findings, and imported content

Schema pins record trust on first use. Drift handling defaults to `warn`; select `gateway.security.schema_pinning.action: block` if detected changes should stop calls until review. Check global and per-server enablement rather than assuming a local setting installs a verifier.

Use `gridctl pins verify` to inspect continuity and `gridctl pins diff <server>` to review changes before approval. Compare against the intended upstream update. Resetting or approving a pin accepts a new baseline; it does not establish publisher identity, implementation safety, or trustworthy tool results. First contact is trusted, new tools are automatically pinned, and removed tools warn. See [schema pinning](../config-schema.md#security) for exact behavior.

Poisoning findings are advisory heuristics. Review suspicious descriptions and cross-server references, but treat clean scans and unchanged schemas as limited observations. They cannot prevent a model from following hostile runtime output.

Review a skill or pack's supporting scripts, agents, rules, and wiring before import and projection. Import-time scan gates differ from advisory skill-pin observations. `--trust` bypasses the import scan gate; it is not a remedy for an unexplained finding. Projection ownership and drift records protect against accidental overwrites, not hostile code. Force and adopt operations are explicit ownership decisions. The [skills](../skills.md) and [packs](../packs.md) guides describe these workflows.

## Use diagnostics without overstating their evidence

The following checks are implemented but Unreleased at this baseline. Use the matching local build or a release that includes them.

| Check | Useful result | Boundary |
|---|---|---|
| `gridctl validate stack.yaml --check-mutable-refs` | Literal image and package selector diagnostics | No registry lookup or rewriting. Unassessed references remain visible; exit zero is not complete coverage. |
| `gridctl validate stack.yaml --policy policy.yaml --format json` | Captured declarations evaluated against explicitly selected rules | No runtime admission, secret resolution, or enforcement on later apply/API/reload. Keep the checker, policy, and CI workflow independently trusted from candidate input. |
| `gridctl doctor --security --source file:stack.yaml --json` | Passive authored-configuration evidence | No expansion, probes, scans, or pin mutation. Runtime producers can be unknown. |
| `gridctl doctor --security --source gateway:http://localhost:8180 --json` | Passive evidence already held by the selected gateway | No silent fallback and no fresh container inspection. Recorded credentials attach only to loopback origins. |

Read the [declaration policy](../stack-declaration-policy.md) and [security evidence report](../security-evidence.md) contracts before automating exit handling. Policy exits are `0` accepted without warnings, `1` errors/rejection/indeterminate, and `2` accepted with warnings. Security-report exits are `0` no established failure among named predicates, `1` an established fail predicate, and `2` source/report failure. Security exit zero permits warnings and unknowns; it is not a secure verdict. An imported snapshot is historical supplied evidence, not a fresh authenticated observation.

## Protect operational evidence

Logs, traces, metrics, run records, exports, and screenshots can disclose operational data. Review tracing configuration and exporter destinations before enabling external collection. Apply access and retention controls at those destinations. Redaction of recognized patterns or registered values does not cover every transformed or unknown secret, and truncation limits size rather than sensitivity. Run records omit argument and result values, but names and caller-declared labels may still be sensitive.

Security reports exclude raw credentials and payloads, but operator-authored identifiers may themselves contain sensitive text. Review reports before sharing. Logs, traces, and run records are not a complete or tamper-proof security audit trail; do not use them to claim every attempt was recorded or that a downstream real-world effect succeeded.

For a suspected compromise, restrict access to the affected gateway, revoke exposed credentials with their issuers, and review the server or imported content before resuming. Preserve only the diagnostic evidence needed for investigation under appropriate access controls. Send vulnerability details through [private reporting](../../SECURITY.md#reporting-a-vulnerability), not a public issue containing raw logs or secrets.

## Keep the guide current

Security-relevant PRs should update this guide or its linked canonical documentation in the same PR. Record changes to release availability, defaults, credential custody, enforcement, and known limits. Completing this initial baseline does not imply completion of the remaining roadmap or certify a deployment.
