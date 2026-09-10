# Security Threat Model

Gridctl runs with the operator's authority over local files, client configuration, credentials, and the container runtime. Its gateway concentrates access to downstream tools. A compromise of that gateway, an authorized management client, or a workload given broad host access can therefore affect more than a single MCP connection.

This document describes current controls and their limits. It is not a security certification or a promise that imported code or model instructions are safe. Report vulnerabilities through the [private reporting process](../../SECURITY.md#reporting-a-vulnerability).

## Evidence Baseline

Reviewed on September 10, 2026, against source revision [`569e1126c2283c4f8f9c82447439355b4dea1934`](https://github.com/gridctl/gridctl/tree/569e1126c2283c4f8f9c82447439355b4dea1934). Source and test links below identify the implementation evidence; use that revision when reproducing this snapshot. Test coverage establishes specific behavior, not the absence of other vulnerabilities.

| State | Evidence at this baseline |
|-------|---------------------------|
| Released as a prerelease | [`v1.0.0-rc.1`](https://github.com/gridctl/gridctl/releases/tag/v1.0.0-rc.1), published September 10, 2026, points to `2f6c00472c33304bcc560ecbe4be18497ec15c2e`. It contains grouped-route authentication, non-resolving stack export, reserved internal-credential filtering, scoped variable delivery, skill package completeness tracking, and authenticated binary-release tooling. The runtime source cited here is present in that tag. |
| Merged but unreleased | Changes after that tag through the reviewed revision include dependency and Actions updates, a read-only tap authentication diagnostic, and the macOS guard around the Homebrew quarantine-removal hook. Do not infer those changes are in the published archives. |
| Planned | [Gateway auth lifecycle and browser parity #1228](https://github.com/gridctl/gridctl/issues/1228), [execution hardening #1221](https://github.com/gridctl/gridctl/issues/1221), and [passive security evidence reports #1224](https://github.com/gridctl/gridctl/issues/1224) are open. Their proposed controls are not credited below. |

The published `v1.0.0-rc.1` notes explicitly identify it as the first provenance-covered production release under the release policy, while GitHub marks it as a prerelease. Its asset list includes `provenance.sigstore.json`, the inventory index, and per-archive inventories. That is release-specific evidence, not a cryptographic verification of the reader's downloaded bytes. Follow [Release Verification](../release-verification.md) before installing those bytes. Earlier releases and local builds are outside that coverage.

## Scope, Assets, and Assumptions

The intended trust model is a single operator controlling a gateway and its declared workloads. Shared-host and remote deployments introduce additional trust boundaries; a tool group or client label does not turn the process into a mutually distrustful multi-tenant service.

Assets include downstream credentials and OAuth grants, stored variables, tool arguments and results, the operator's files, client configuration, imported skill and rule content, pin and ownership records, release artifacts, and gateway availability. The ability to change a stack or projected instructions is itself an asset: those changes can cause later code execution by gridctl or an agent.

The operating system, the operator's account, the selected container runtime, and deliberately installed upstream clients are part of the trusted computing base. A malicious process running as the operator can generally read or modify the same files and invoke the same local APIs. File modes and encryption protect narrower boundaries; they do not isolate gridctl from its own user or a compromised administrator.

Threat actors considered here include a malicious website, an unauthorized network client, a compromised authorized client, a malicious or compromised downstream MCP server, a package or pack publisher, and an attacker who can change an artifact or a dependency. Downstream descriptions, schemas, results, imported documents, and scripts remain untrusted even when their source is authenticated.

## Trust Boundaries and Data Flow

```text
Upstream MCP client / browser / CLI
  -> HTTP listener and management API
  -> MCP routing, exposure policies, and call gates
  -> remote MCP/OpenAPI service, container, local process, or SSH process

Git repository / local skill source
  -> import
  -> registry and projection records
  -> files and instructions consumed by an upstream agent

Package registry / Git repository / local build source
  -> image build -> container runtime

Stored variables / OAuth grants / daemon state
  -> credential resolution
  -> explicitly configured services, local processes, and command children

Gateway activity -> logs, metrics, traces, and optional external exporters
Release workflow -> published archives and evidence -> local installation
```

| Boundary | Authority crossing it | Principal risk |
|----------|-----------------------|----------------|
| Client to gateway | Tool invocation and operational API access | Unauthorized calls, credential theft, destructive management actions, and resource exhaustion |
| Gateway to downstream server | Resolved credentials, arguments, network access, and execution privileges | Data exfiltration, poisoned instructions/results, schema mutation, and abuse of host or container privileges |
| Publisher to local registry | Documents, executable supporting files, agents, and rules | Supply-chain compromise, path traversal, unwanted instructions, and later local execution |
| Registry to client files | Persistent instructions and gateway wiring | Overwriting user changes, tool shadowing, and widening what an agent can access |
| Credential storage to runtime | Plaintext values in process memory, headers, and selected environments | Leakage through ambient inheritance, output, state files, or a compromised recipient |
| Runtime to observability and exports | Diagnostics and operational data | Disclosure through logs, traces, copied stack files, or support bundles |
| Release service to installer | Executable bytes and their provenance | Artifact substitution, compromised build dependencies, and accepting checksums as origin proof |

## Network and Client Controls

| Control | Default and enforcement boundary | Evidence | Limitations |
|---------|----------------------------------|----------|-------------|
| Listener and published ports | Gateway binds to `127.0.0.1`; container published ports also default to loopback. A non-loopback gateway bind without a nonempty configured auth token is refused unless the explicit insecure override is set. | [Gateway builder](../../pkg/controller/gateway_builder.go), [builder tests](../../pkg/controller/gateway_builder_test.go), [container creation](../../pkg/runtime/docker/container.go), and [container tests](../../pkg/runtime/docker/container_test.go) | Loopback is network reachability control, not caller authentication. Local processes can connect. Containers with published ports can also be reached directly by local clients, bypassing gateway policies. |
| Host validation | Requests arriving on loopback must use a loopback Host or an explicitly allowed host; known listener ports are checked for loopback names. The API and MCP transport share the validator. `/health` and `/ready` are exempt at the API layer. | [API assembly](../../internal/api/api.go), [Host validator](../../pkg/mcp/streamable.go), and [Host gate tests](../../internal/api/host_gate_test.go) | This is a DNS-rebinding defense for loopback arrivals. It does not validate Host on non-loopback arrivals and does not authenticate local callers. |
| Gateway authentication | Opt-in on loopback. When configured, constant-time token comparison protects `/api/`, `/groups/`, `/mcp`, `/sse`, `/message`, `/a2a/`, and `/.well-known/` paths. Bearer and raw API-key headers are supported. | [Auth middleware](../../internal/api/auth.go), [assembled route tests](../../internal/api/auth_routes_test.go), and [real grouped-auth integration tests](../../tests/integration/groups_auth_test.go) | A shared gateway credential grants access to operational APIs, not just a selected tool group. Static UI files, liveness/readiness probes, terminal CORS preflight, and the exact state-validated downstream OAuth callback remain public. A session ID is not a substitute for the configured credential. |
| Origin handling | The assembled gateway defaults to wildcard allowed origins when none are configured. REST CORS controls response headers and terminates preflight; MCP also checks Origin, allowing loopback origins and requests without Origin. | [Builder origin configuration](../../pkg/controller/gateway_builder.go), [CORS middleware/tests](../../internal/api/api_test.go), and [MCP transport](../../pkg/mcp/streamable.go) | CORS is not authorization or a general CSRF defense. REST requests with an unlisted Origin still reach handlers. An explicit origin list narrows browser response access but does not replace auth. |
| Client and group exposure | `clients:` and `groups:` are opt-in. With no client policy, clients see all tools. A configured client policy denies unmatched identities unless `default: allow`; group membership and client scope are enforced for direct calls and code-mode inner calls. | [Client scope](../../pkg/mcp/clientscope.go), [client identity extraction](../../pkg/mcp/clientid.go), [gateway dispatch](../../pkg/mcp/gateway.go), and [scope integration tests](../../tests/integration/per_client_scope_test.go) | Client identity comes from a query parameter, header, or declared client name, not a credential-bound principal. Clients choose the endpoint they connect to. These are exposure controls for cooperating clients, not tenant isolation. |

The HTTP listener does not itself establish encrypted remote transport. Use authenticated HTTPS termination or an encrypted tunnel for remote access, restrict access to the management surface, and configure origins and hosts for that deployment. The insecure unauthenticated-bind override accepts exposure explicitly; it does not add another compensating control.

### Authentication Lifecycle and Credential Custody

The CLI reads the resolved gateway token from daemon state and attaches it to local API requests. The state file is written with mode `0600`; the token is intentionally plaintext there. This avoids requiring every CLI operation to unlock the variable store, but any reader with the operator's file access has that credential. See [daemon state](../../pkg/state/state.go), [CLI API client](../../cmd/gridctl/apiclient.go), and [client tests](../../cmd/gridctl/apiclient_test.go).

The browser currently stores a bearer token under `gridctl-auth-token` in localStorage and sends `Authorization: Bearer ...`; it does not offer parity with the server's custom-header/API-key modes. Origin scripts can access that storage. See [browser API client](../../web/src/lib/api.ts). Treat the origin, its scripts, extensions with access to it, and the workstation as credential-bearing surfaces.

Listener authentication, bind, Host, and Origin settings are startup-bound. A successful config save or reload is not evidence that these controls changed on the listener. Restart and verify the running configuration when changing them. Atomic restart-required rejection and browser credential handling improvements remain in #1228; this model does not claim live rotation or in-process revocation.

### Article XII Versus Current Behavior

[Article XII](../../CONSTITUTION.md#article-xii---secure-defaults) requires opt-out authentication, a locked vault by default, and explicit CORS allowlists in production paths. At this revision, loopback authentication is opt-in, the builder defaults to wildcard origins, and the variable store supports plaintext operation unless encryption has been enabled. An existing encrypted store loads locked, but a new or plaintext store does not.

These are current behavior and governance gaps, not amendments to Article XII. The loopback default and widened-bind refusal reduce network exposure without satisfying the entire article. This documentation does not silently change either the constitution or the implementation.

## Downstream Tools, Poisoning, and Execution

| Control | Default and enforcement boundary | Evidence | Limitations |
|---------|----------------------------------|----------|-------------|
| Tool schema pins | Enabled by default with `action: warn` and scanning enabled. First contact records trust-on-first-use (TOFU) pins. `action: block` opts into stopping calls to a server after detected changes to pinned definitions. | [Pin configuration](../../pkg/controller/gateway_builder.go), [pin semantics](../../pkg/pins/types.go), [pin tests](../../pkg/controller/schema_pinning_test.go), and [pin integration tests](../../tests/integration/schema_pinning_test.go) | First contact is trusted, newly added tools are automatically pinned, and removed tools produce warnings. Default drift handling does not block. Verification errors are logged; this is not a universal fail-closed content admission system. |
| Poisoning heuristics | Tool/skill document findings are advisory; scan controls can disable or ignore rules. Server-prefixed tool names preserve routing provenance. | [Scanner](../../pkg/pins/scan.go), [scanner tests](../../pkg/pins/scan_test.go), [router](../../pkg/mcp/router.go), and [router tests](../../pkg/mcp/router_test.go) | Pattern matches do not prove maliciousness, and no finding does not prove safety. Namespacing reduces name collisions but cannot stop semantic shadowing, misleading descriptions, or a model following hostile output. Pins do not inspect a tool implementation or establish the safety of each result. |
| Local MCP processes | Explicit commands execute as child processes. Inherited and configured environments omit reserved internal-credential keys. | [Process client](../../pkg/mcp/process.go), including environment construction, and [process tests](../../pkg/mcp/process_test.go) | The rest of the daemon environment is inherited. This is a denylist, not an allowlisted minimal environment. Local commands have the operator's filesystem and network privileges; there is no general OS sandbox. |
| Containers | Image, command, environment, network, and declared mounts go to the container runtime; published ports default to loopback. Generated Python images use pinned bases and a non-root user. | [Container creation](../../pkg/runtime/docker/container.go), [Python template](../../pkg/builder/python_template.go), and [template tests](../../pkg/builder/python_template_test.go) | General container creation does not impose universal non-root execution, a read-only root filesystem, capability dropping, or CPU/memory limits. Mounted paths and the selected runtime determine host exposure. Containers share a kernel and are not a guarantee against escape. |
| Code mode | Fresh JavaScript execution context, 64 KiB input bound, a default 30-second timeout, and an allowed-tool set. Inner tool calls return through gateway enforcement. Fetch has response/time bounds and a dial-time IP blocklist. | [Sandbox](../../pkg/mcp/codemode_sandbox.go), [fetch implementation](../../pkg/mcp/codemode_fetch.go), and [code-mode tests](../../pkg/mcp/codemode_test.go) | This sandbox is not containment for downstream tools or a separate OS process. Its network checks are specific to fetch and its enumerated address ranges, not a system-wide egress policy. Tool results and console output remain untrusted. |
| Call rate and size bounds | `limits:` is opt-in and applies token buckets per configured client, server, or tool before downstream dispatch. MCP POST bodies are bounded; normal tool-result text is truncated per content item at 64 KiB by default. | [Rate policy/tests](../../pkg/limits/limits_test.go), [call-gate tests](../../pkg/mcp/callgate_test.go), [transport](../../pkg/mcp/streamable.go), and [result handling](../../pkg/mcp/gateway.go) | Buckets reset on daemon restart. These are not aggregate memory, container resource, or bandwidth quotas. Truncation occurs after receipt and is not a bound on downstream allocation. Code-mode meta-tool returns take a separate path. |

For server-side request forgery (SSRF), distinguish agent-controlled code-mode fetch from operator-configured URLs. MCP endpoints, OAuth discovery, build sources, and OpenAPI URLs can cause outbound requests by the daemon. The OpenAPI preview disables external `$ref` following, while deployment supports it; see [preview tests](../../internal/openapipreview/preview_test.go) and [OpenAPI client](../../pkg/mcp/openapi_client.go). Do not assume fetch's blocklist protects these other request paths. Restrict daemon egress and review destination URLs when operating against untrusted sources.

Generated builds inspect Python metadata without executing it on the host and verify selected package metadata against recorded hashes. Building and running a package still executes publisher-controlled code in the build/runtime environment. Content-addressed images, commit pins, and provenance labels help identify inputs; they do not prove those inputs harmless. See [Python build examples and guidance](../../examples/python-sources/README.md) and [builder tests](../../pkg/builder/plan_test.go).

## Imported Skills, Packs, and Projection

Importing a skill installs instructions and potentially executable files. Projecting that package into an agent's search path makes the content available for later execution with the agent's permissions. Pack imports extend this boundary to agents, context rules, and gateway wiring.

| Control | Default and enforcement boundary | Evidence | Limitations |
|---------|----------------------------------|----------|-------------|
| Scan before installation | Skill bodies and selected supporting text are scanned before registry writes. Body findings block by default; supporting-file danger findings block, while lower-severity findings warn. Updates forward an explicit trust choice rather than silently enabling trust. | [Importer](../../pkg/skills/importer.go), [scanner](../../pkg/skills/scanner.go), and [install regression tests](../../pkg/skills/install_test.go) | `--trust` bypasses the scan gate. Binary content is not text-scanned; pattern scanning is not code review or malware detection. Local edits outside import do not acquire an import-time safety guarantee. |
| Bounded package copy | Only managed `scripts/`, `references/`, `assets/`, and selected license/notice metadata are copied. Symlinks and nested skill directories are skipped. Limits are 5 MiB per supporting file, 500 files, and 50 MiB total. Executable modes are preserved. | [Supporting-file installer](../../pkg/skills/install.go), [symlink/path/cap tests](../../pkg/skills/install_test.go), and [real import integration tests](../../tests/integration/skills_supporting_files_test.go) | These bounds cover the selected supporting files, not every stage of cloning or building. Files manually added inside managed subtrees can be replaced on import. This is not a sandbox against a hostile process modifying local storage concurrently. |
| Skill document pins | First sight records canonical `SKILL.md` and participating supporting-file digests; refresh records drift and findings. | [Digest rules](../../pkg/skillpins/hash.go), [store tests](../../pkg/skillpins/store_test.go), and [refresh wiring](../../pkg/controller/gateway_builder.go) | Skill pins are an observation layer, not a prompt-serving or execution gate. Dotfiles, temporary files, and nested skills have defined exclusions. They are distinct from the importer's `SKILL.md`-only installed hash used for update drift. |
| Pack content gates | Pack preview scans selected skills, agents, and rule fragments; add uses the corresponding import/rule gates. Rule collisions are checked before writes. | [Pack preview](../../pkg/packops/preview.go), [pack add](../../pkg/packops/add.go), and [pack tests](../../pkg/packops/packops_test.go) | Trust overrides still apply. A pack's selected content and wiring require operator review; successful import does not authenticate its publisher or approve every future tool call. |
| Ownership and drift | Projection uses recorded ownership, hashes, locking, and backups. Reconciliation does not grant blanket authority to overwrite drift or unmanaged client files. Global skill exposure policy can restrict prompts/resources and projection. | [Projection engine](../../pkg/project/), [skill projection](../../pkg/skillsync/), [wiring](../../pkg/wiring/), and [skill policy/tests](../../pkg/mcp/skillpolicy_test.go) | Ownership prevents accidental clobbering; it is not an OS access boundary. Trusted local projection may preserve symlinks. Explicit force/adopt decisions change ownership or accept disk state; they do not establish content safety. |

### Case Study: Supporting Files Were Missing

[Issue #995](https://github.com/gridctl/gridctl/issues/995), closed July 27, 2026, reported that imports saved only rendered `SKILL.md`, leaving instructions that referenced scripts pointing at absent files. It was a packaging defect, not evidence of an observed compromise. Native projection made the failure more visible because agents consumed those incomplete directories.

Correcting the defect expanded the material crossing the publisher-to-host boundary. The fix therefore needed allowlisted tree copying, executable-mode preservation, symlink and size checks, scanning before any installation, and removal of the update path's implicit trust bypass. `TestImport_DangerousScriptGatedLeavesNoPartialInstall`, `TestImport_SkipsSymlinks`, and the real supporting-file import suite preserve those boundaries.

[PR #1225](https://github.com/gridctl/gridctl/pull/1225), included in `v1.0.0-rc.1`, subsequently recorded that supporting-file trees had been evaluated and corrected false missing-file warnings. That completeness marker does not mean every path mentioned in prose is bundled, nor that a package is safe. The importer's installed hash still covers `SKILL.md`; separate skill pins cover their defined document set. Do not describe completeness, update drift, and security pinning as interchangeable guarantees.

## Secrets, Exports, and Observability

| Control | Default and enforcement boundary | Evidence | Limitations |
|---------|----------------------------------|----------|-------------|
| Variable-store encryption | Existing encrypted stores load locked. Passphrase-based encryption uses Argon2id and XChaCha20-Poly1305; plaintext storage is also supported. | [Store](../../pkg/vault/store.go), [crypto](../../pkg/vault/crypto.go), and [crypto tests](../../pkg/vault/crypto_test.go) | Plaintext mode relies on local permissions. An unlocked daemon holds values in memory; encryption at rest does not protect against a compromised daemon or authorized secret recipient. |
| Internal-credential boundary | `GRIDCTL_*`, `OP_CONNECT_TOKEN`, and `OP_SERVICE_ACCOUNT_TOKEN` are reserved. Store mutation, store-derived resolution, export/set delivery, and local MCP process inheritance apply this boundary. | [Reserved-key policy/tests](../../pkg/vault/internal_credentials_test.go), [policy implementation](../../pkg/vault/internal_credentials.go), and [process environment](../../pkg/mcp/process.go) | Ordinary non-reserved environment values are still inherited by local MCP processes. Renaming a credential outside the reserved set changes whether the filter recognizes it. This is not complete credential discovery. |
| Scoped command delivery | `var run` requires explicit stored-variable selection and starts a direct child. Exact selected secret values are redacted on non-interactive output by default. | [Command runner](../../pkg/varrun/run.go) and [runner tests](../../pkg/varrun/run_test.go) | The child receives plaintext values and can transmit them elsewhere. Interactive output is raw; redaction is not an exfiltration barrier or a substitute for trusting the command. |
| Known-secret scan | `var scan` checks working-tree files or staged blobs for eligible exact current stored values, with redacted findings and reported skips. | [Scanner](../../pkg/varscan/scan.go) and [scanner tests](../../pkg/varscan/scan_test.go) | It is line-scoped and bounded, excludes some content, and does not find arbitrary credentials, transformed values, or secrets only in history. Review completeness and skips. |
| Downstream OAuth | PKCE/state-based authorization and an encrypted grant store are separate from inbound gateway authentication. The store uses a machine-local key in the same protected directory. | [OAuth broker/tests](../../pkg/mcpauth/broker_test.go), [store](../../pkg/mcpauth/store.go), and [key custody](../../pkg/mcpauth/machinekey.go) | Copying only the encrypted token file differs from copying the whole directory: a reader of both key and ciphertext can decrypt grants. Downstream consent does not authenticate upstream clients. |
| Stack export | CLI/API export rereads authored configuration without resolving environment or stored values. Recognized inline credentials and sensitive expansion operands are rejected; failures report field paths without values. | [Export projection/tests](../../pkg/config/export_test.go), [API tests](../../internal/api/stack_export_test.go), and [diagnostic tests](../../pkg/controller/export_diagnostic_test.go) | This is bounded field classification, not a guarantee that arbitrary literals are secret-free. Raw spec retrieval/editing and runtime resolution are separate and can contain credentials. |
| Log redaction | The logging handler redacts recognized patterns and registered exact values. Verbose apply diagnostics use bounded summaries rather than dumping resolved configuration. | [Redaction implementation/tests](../../pkg/logging/redact_test.go) and [controller diagnostic tests](../../pkg/controller/export_diagnostic_test.go) | Unknown or transformed secrets, hostile output, raw files, and external consumers remain disclosure risks. Truncation limits size, not sensitivity. Logs and traces are operational evidence, not an immutable security audit trail. |

A downstream tool can legitimately receive sensitive input and return it in another shape. No combination of pinning, token counting, and log redaction establishes end-to-end data-loss prevention. Review observability configuration, exporter destinations, and copied diagnostics as separate disclosure paths.

## Release and Dependency Boundary

The reviewed [release workflow](../../.github/workflows/release.yaml) requires exact-commit validation, draft assembly, authenticated archive/inventory subjects, independent Linux/macOS verification, and re-verification before publication. Homebrew advancement follows public-asset verification. [Release policy tests](../../scripts/test_release.py) and [scanner-wrapper tests](../../scripts/test_govulncheck.py) exercise rejection behavior; hosted acceptance and published evidence remain separate from local fixtures.

Provenance binds artifacts to expected repository, workflow, tag, and source identities. It does not prove harmless code, reproducibility, complete dependency coverage, or a SLSA level. A compromised trusted workflow or dependency remains relevant. Repository release settings and permissions must be checked for the actual publication; workflow source alone cannot establish that immutable release settings are enabled.

The installer, updater, and Homebrew checksum validate archive integrity but do not automatically enforce the provenance policy. An attacker controlling both an archive and its unauthenticated checksum is not defeated by that check. External verification is the current origin-authentication path. It would be inaccurate to label all release artifacts unsigned merely because installation does not verify attestations.

## Residual Risks and Non-Goals

The main remaining risks are broad authority within the operator's account, permissive loopback/browser defaults, shared rather than credential-bound client identity, publisher-controlled execution, and data escaping through legitimate tool access or output. Remote deployment and untrusted content increase their consequences.

Gridctl does not currently promise multi-tenant isolation, semantic prompt-injection prevention, universal outbound network restrictions, a hardened sandbox for arbitrary local commands or containers, comprehensive secret detection, or a tamper-proof audit log. Core single-user security controls remain part of the open-source project; this model does not defer them to a separate commercial boundary.

Update this document when a security-relevant PR changes defaults, enforcement points, credential custody, or release coverage. Record a new source revision, update its source/test evidence, and move planned controls into the inventory only after they actually ship or clearly label them merged-but-unreleased. Completion of this current-state model does not depend on completing every security follow-up.
