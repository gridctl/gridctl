# MCP execution controls

Execution controls are opt-in per MCP server. Omit `execution` to retain compatibility behavior. Supporting resource containers are not covered. External URL and OpenAPI servers are externally managed; SSH does not provide locally verifiable remote confinement.

## Container profile

```yaml
execution:
  mode: hardened
  uid: 1000
  gid: 1000
```

Choose a nonzero numeric UID and GID that can read the image's code and dependencies. A username or image label is not identity evidence. This configuration does not change daemon rootlessness, host ownership, or user namespace mappings.

The profile requires nonprivileged operation, a private PID namespace, a nonzero container identity, engine-default seccomp, and finite per-replica resource limits.

Preflight checks the actual daemon's capabilities. When Docker-compatible resource flags are incomplete, Gridctl queries native Podman info over the same local Unix socket and requires cgroup v2 with CPU, memory, and PID controllers. Missing native evidence is refused. Capability evidence does not establish workload enforcement: engine inspection and instance-bound kernel checks, including disabled swap, remain mandatory before routing.

Podman may expand an `ALL` capability drop into individual names. In that case, Gridctl requires native inspection of the same full container ID to report empty effective and bounding capability sets. Missing sets remain unknown, and capability additions remain refused. Tmpfs inspection compares required flags, size, and mode rather than option order or size spelling; conflicting and unknown options are refused. Podman's `tmpcopyup` option may initialize declared scratch with image contents; its exact size and nonexecutable, nodev, and nosuid restrictions remain required. Inspection failures name safe control fields and mount subconditions, such as `data_mounts.scratch_inventory` or `data_mounts.scratch_options`, without returning raw engine values. These engine checks do not replace the started workload's kernel observations.

Hardened workloads on a selected Podman runtime use native container creation because its compatibility API automatically adds writable tmpfs mounts to read-only containers. Gridctl explicitly disables automatic scratch and systemd mount injection, then supplies only the declared scratch and named volumes. An empty scratch list remains empty. Native creation failures do not fall back to compatibility creation. Engine-default seccomp/LSM protections, image-volume inspection, and instance-bound kernel admission remain required.

| Field | Default | Meaning |
|---|---|---|
| `read_only` | `true` | Read-only root filesystem |
| `no_new_privileges` | `true` | Prevent privilege gains through executable transitions |
| `drop_capabilities` | `[ALL]` | Drop all Linux capabilities; no additions |
| `memory_bytes` | `268435456` | 256 MiB per container, with swap disabled |
| `cpu_millis` | `1000` | One CPU quota ceiling, not shares or a reservation |
| `pids` | `128` | Per-container processes and threads |
| `network` | `none` | Stdio only, without published ports or managed endpoints |
| `seccomp` | `engine-default` | Keep engine-default seccomp protection |
| `tmpfs` | `/tmp`, 64 MiB | Nonexecutable, nodev, nosuid scratch |
| `mounts` | Empty | No additional data volumes |

The default envelope is exercised with third-party Python and Node images, MCP initialization, tool discovery, and repeated bounded JSON tool calls. These are small MCP workloads, not capacity guarantees for browsers, model weights, or every package. Override finite budgets explicitly for larger workloads. Memory accounts for charged tmpfs usage. Limits are per replica, not a fleet budget; CPU quota is not a latency guarantee.

Keys inside `execution` are strict. Unknown fields, invalid enums, and transport conflicts are rejected. YAML and JSON reject null fields, including fields inside mount and tmpfs entries; omit optional fields instead. Empty lists and explicit false values are retained. `read_only: false`, `no_new_privileges: false`, a smaller capability-drop list, and `network: connected` are visible exceptions. There is no privileged, unconfined seccomp, or LSM-disable fallback after a failure.

Capability names are case-insensitive and accept an optional `CAP_` prefix. Supported drops are `ALL`, `CHOWN`, `DAC_OVERRIDE`, `FOWNER`, `FSETID`, `KILL`, `SETGID`, `SETUID`, `SETPCAP`, `NET_BIND_SERVICE`, `NET_RAW`, `SYS_CHROOT`, `MKNOD`, `AUDIT_WRITE`, and `SETFCAP`. `drop_capabilities: []` drops none; `tmpfs: []` removes the default scratch mount. These are deliberate changes to the baseline.

Memory must be between 6 MiB and 1 PiB, CPU between 10 and 1000000 milliseconds, and PID limits between one and 1048576. Memory and scratch sizes must be aligned to 4 KiB; scratch must be at least 4 KiB and no greater than memory. Schema ceilings do not promise available daemon capacity. Kernels with different page-size requirements must establish the exact requested limits or refuse admission.

### Writable state

```yaml
execution:
  mode: hardened
  uid: 1000
  gid: 1000
  tmpfs:
    - target: /tmp
      size_bytes: 33554432
  mounts:
    - source: tool-data
      target: /data
      read_only: false
```

Data mounts use explicit plain engine-local volume names, two through 128 characters matching `[A-Za-z0-9][A-Za-z0-9_.-]{1,127}`. Mount `read_only` defaults to `true`; set `false` for writable data. Host binds, engine sockets, host device grants, remote/plugin volumes, and local-driver volumes with host-mount options are rejected. Migrate existing `volumes` entries deliberately when selecting this profile. The volume must have permissions suitable for the chosen UID/GID; Gridctl does not fix ownership automatically.

Writable targets are `/tmp`, `/data`, `/state`, or descendants of `/data` and `/state`. Keep code and installed dependencies on the read-only root filesystem. Data volumes are persistent and not storage-bounded. A read-only volume can still expose secrets. Shared volumes can expose files or Unix sockets created by another workload; network settings do not revoke that authority.

Admission inventories image-declared volumes, engine mounts, and the kernel mount table. Undeclared image volumes are rejected before start. The inventory distinguishes declared data from engine virtual filesystems under `/proc` and `/dev` and engine-managed hostname/DNS metadata. Read-only root does not mean every virtual filesystem is read-only.

### Networking

`none` requires stdio, no server port, and no explicitly selected server network. It removes Gridctl's automatic host-gateway alias and managed endpoint attachment. Stdio still uses the engine attachment API.

`connected` explicitly permits the normal managed network. It is not destination filtering or host isolation. HTTP/SSE requires this exception and a server port; admission verifies the MCP endpoint against the replica's inspected `127.0.0.1` publication. Host networking and sharing another container's network namespace are refused. In advanced network mode, select a declared server network for connected operation; network-none servers omit that selection.

Removing an alias does not deny direct IPs, IPv6, runtime-generated aliases, peers, or mounted Unix sockets. Destination allowlists and an internal-network security contract are not implemented. Runtime restrictions do not constrain image pulls, Dockerfile build downloads, or information returned through MCP.

## Evidence and lifecycle

Required controls are checked against the actual daemon, engine inspection, and instance-bound kernel observations. Create success, desired YAML, image names, and revision labels are not enforcement evidence. Kernel observations bind the daemon's full container ID to its PID's cgroup v2 membership and verify resource limits, identity, capabilities, seccomp, and relevant mount state. No workload-controlled helper is trusted for admission.

The current observation path requires a Linux client with safe access to the actual daemon workload's `/proc` and cgroup v2 state. Local Docker acceptance exercises it. Rootless Podman uses the same evidence contract and requires successful hosted real-runtime acceptance. A compatible API alone does not establish support. Remote and VM-backed engines without locally instance-bound observations are unsupported for this profile. Non-Linux clients reject it before creation. Gridctl does not mount control sockets into observers or request extra privileges for evidence.

Some observations require a started process. That process may act before verification completes. It cannot become MCP-ready until admission succeeds; rejected instances are stopped, and cleanup failures are reported. This does not retroactively prevent actions during that window.

Initial starts, reuse, restart, and replica growth carry the same desired contract. Reuse checks engine controls and collects fresh evidence. MCP clients check eligibility at registration, health checks, and dispatch, including raw relay. Detected mismatches reject dispatch and stop the container. These are snapshots, not continuous monitoring or protection against an administrator changing the runtime between checks. In-flight work may have started before detection.

Reload performs capability preflight before accepting an execution transition. Once accepted, superseded routes are withdrawn, and old replicas are stopped. Failed preparation or replacement does not restore weaker execution. Accepted desired intent remains visible separately from runtime evidence. Relaxation requires explicit configuration. Execution-only recreation preserves schema pins; combined autoscale and execution edits cannot take the autoscale-only update path.

`extends` still replaces whole server declarations by name. Redefining a server does not deep-merge its execution fields.

## Local environment hygiene

Retirement withdraws routing before waiting for cleanup, cancels obsolete autoscaler operations, and reaps late provisioning results. Blocked container stdin writes cannot hold the connection-state lock during shutdown. Successful gateway-owned processes outlive the request that initialized them; canceled initialization still terminates them. Closing an unregistered process permanently prevents stale health recovery from reviving it.

```yaml
execution:
  mode: local
  inherit: [HOME, LANG]
  lookup: absolute
```

Local processes remain unsandboxed. Selecting `local` defaults to no ambient inheritance and an absolute executable path. `inherit: []` inherits nothing. Explicit and scoped server delivery keeps its existing precedence. The internal-credential denylist, including the reserved `GRIDCTL_` prefix, always wins. Reports contain names, never environment values.

Lookup is separate from the child's environment. `absolute` does not authenticate the executable or make it immutable. `ambient_path` explicitly permits the gateway's PATH, even if the child does not inherit PATH. Current-directory executable protection is retained. Commands remain argv execution with the existing working directory.

Selected local execution uses Unix process groups for ordinary descendant signaling. Cancellation, signal escalation, waiting, and pipe cleanup are bounded. Direct-child exit is reaped promptly, and reported PID follows exit. Groups are not containment: detached descendants and descendants surviving their parent can remain. Windows uses direct-child termination; no native sandbox or process-tree confinement is claimed.

SSH keeps its connection and argument semantics. Local SSH-client exit does not prove remote descendant termination. SSH execution declarations are rejected. `npx` and `uvx` bootstrap run under the same selected local contract. Cache or network failures never grant extra inheritance or access automatically.

## Inspect and edit

Existing server details show Execution evidence separately from MCP health. Per-replica reports include revision, instance, timestamps, evidence source, and control outcomes. Mixed replicas are not uniformly eligible. Idle-to-zero has no current active evidence. `gridctl status --json` retains reports; `gridctl status --replicas` shows execution outcomes beside MCP state.

The wizard's Execution section selects compatibility, hardened containers, or local hygiene. Container forms expose UID/GID and networking; local forms expose inherited names and lookup. Use YAML mode for container budgets, mounts, and exceptions. The entire execution mapping survives unrelated form edits, including explicit empty lists and false values. Lossy whole-stack transitions are blocked in YAML mode. Review and save use authoritative proposed YAML. A saved draft and syntax validation are not runtime enforcement.

The [configuration reference](config-schema.md#execution) inventories the fields, and the [API reference](api-reference.md#execution-reports) defines report shapes and outcome semantics. Unreleased status metadata and human replica output changes require maintainer-owned major-release scheduling under Article VIII. Migrate text-parsing consumers to `status --json`; inspect per-replica evidence rather than treating MCP health as protection. No release version or approval is assigned here.
