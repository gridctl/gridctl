# Troubleshooting Guide

Common issues and resolutions for gridctl.

Start with `gridctl doctor`: it runs most of the environment checks below automatically (runtime detection, socket reachability, version floor, gateway port, `npx` availability, state hygiene, and vault status) and prints a verdict with a remediation hint for each.

---

## Container Runtime

### Docker socket not found

**Symptoms:**

```
docker runtime requested but Docker socket not found or not responding

Checked:
  - /var/run/docker.sock

Install Docker: https://docs.docker.com/get-docker/
```

**Causes:**

- Docker Desktop or the Docker daemon is not running
- The socket file is missing or has wrong permissions
- Your user is not in the `docker` group

**Resolution:**

1. Start Docker:
   ```bash
   # Linux (systemd)
   sudo systemctl start docker

   # macOS
   open -a Docker
   ```

2. Verify the socket exists:
   ```bash
   ls -la /var/run/docker.sock
   ```

3. If permission denied, add your user to the `docker` group:
   ```bash
   sudo usermod -aG docker $USER
   # Log out and back in for group changes to take effect
   ```

### Podman socket not found

**Symptoms:**

```
podman runtime requested but Podman socket not found or not responding

Checked:
  - /run/podman/podman.sock
  - /run/user/<uid>/podman/podman.sock
```

**Causes:**

- Podman socket service is not running
- Rootless Podman socket is at a non-standard path

**Resolution:**

1. Enable and start the Podman socket:
   ```bash
   # Rootless (recommended)
   systemctl --user enable --now podman.socket

   # Rootful
   sudo systemctl enable --now podman.socket
   ```

2. Verify the socket is active:
   ```bash
   podman info --format '{{.Host.RemoteSocket.Path}}'
   ```

3. If using a custom socket path, set `DOCKER_HOST`:
   ```bash
   export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
   ```

### No container runtime available

**Symptoms:**

```
no container runtime available

Sockets checked:
  - /var/run/docker.sock
  - /run/podman/podman.sock
```

The error also lists which workloads need a container runtime and which can run without one (external URL, local process, SSH, OpenAPI servers).

**Resolution:**

Install Docker or Podman. If your stack only uses external URL or local process servers, no container runtime is needed - check your `stack.yaml` for servers that require containers (those with `image:` or `source:`).

---

## Port Conflicts

### Address already in use

**Symptoms:**

```
failed to start server on port 9000: listen tcp :9000: bind: address already in use
```

**Causes:**

- Another process is using the port
- A previous gridctl instance is still running
- The OS has the port in a TIME_WAIT state

**Resolution:**

1. Find what is using the port:
   ```bash
   # macOS
   lsof -i :9000

   # Linux
   ss -tlnp | grep 9000
   ```

2. If a previous gridctl instance is running, stop it first (`destroy` takes the stack file or the name shown in `gridctl status`; a stackless daemon is stopped with `gridctl stop`):
   ```bash
   gridctl destroy stack.yaml
   ```

3. Start on a different port: `--port` sets the gateway/web UI port (default `8180`), `--base-port` sets the MCP server host-port allocation base (default `9000`):
   ```bash
   gridctl apply stack.yaml --port 8181 --base-port 9100
   ```

4. If the port is in TIME_WAIT, wait a few seconds or use a different port.

---

## Skills, Agents, and Packs

### ssh agent not available

**Symptoms:**

```
ssh agent not available: SSH_AUTH_SOCK is unset in this gridctl process, which
inherits an agent only from the shell that started it
```

Over REST this arrives as HTTP 422 with `"code": "ssh_agent_unavailable"` and, for an SSH URL, an `httpsEquivalent` field naming the HTTPS URL for the same repository.

**Causes:**

Importing a skill, agent, or pack from an SSH URL when the gridctl process has no reachable ssh-agent socket. Note the wording: your shell almost certainly does have an agent. The process doing the clone may not.

- `gridctl apply` and `gridctl serve` daemonize by re-spawning with the launching shell's environment, so a daemon started from a shell without an agent never had one, and a long-running daemon can outlive the agent it did inherit.
- Every import from the web UI or the REST API runs in the daemon's environment, not in your terminal's.
- gridctl does not read `~/.ssh/config`, so a per-host `IdentityFile` entry that makes plain `git clone` work does nothing here.

**Resolution:**

1. Use the HTTPS URL with a credential, which needs no agent at all:
   ```bash
   gridctl pack add https://github.com/acme/pack --vault-key GIT_TOKEN
   ```

2. Or start an agent and restart the daemon so it inherits the socket. Restarting the agent alone is not enough; the daemon captured its environment at start:
   ```bash
   eval "$(ssh-agent -s)" && ssh-add
   gridctl serve --foreground
   ```

3. Or name the key directly, which bypasses the agent:
   ```bash
   gridctl pack add git@github.com:acme/pack.git --ssh-key ~/.ssh/id_ed25519
   ```

---

## Container Startup

### Generated Python source fails before build

**Symptoms:**

```text
No Dockerfile or Python project metadata was found in <path>. Add a Dockerfile, pyproject.toml, or setup.py.
This package installs no commands. Set the server command explicitly.
This package provides commands: <candidates>. Set the server command to one of them.
Package requires Python <range>, which is incompatible with image selection <selection>. Set source.python to a compatible version from 3.10 through 3.13, or use a custom Dockerfile.
No PyPI project named <package>.
<version> is not a published version of <package>. Latest is <latest>.
<version> of <package> is yanked on PyPI. Choose a non-yanked version.
```

**Resolution:**

1. For git or local generation, set `source.runtime: python`, omit
   `source.dockerfile`, and point `source.path` (git) or
   `source.project_path` (local) at a packaged project containing a static
   `pyproject.toml` or `setup.py`.
2. Set the MCP server's top-level `command` when metadata has no console script
   or has several possible scripts. Candidate names in the error are sorted.
3. For PyPI, use an exact public release in `source.ref`; `latest`, version
   ranges, yanked releases, and private indexes are rejected.
4. If package metadata requires an unsupported interpreter, choose a compatible
   `source.python` from 3.10 through 3.13, or provide a custom Dockerfile.

Generated contexts are temporary and do not modify the source tree. Build logs
come from the existing server log path: `gridctl logs --server <name>`. When
`source.dockerfile` is non-empty, the INFO log
`Found configured Dockerfile; building from it.` confirms that gridctl selected
the custom Dockerfile instead of generation; a missing configured file fails
before the image build.

### Image pull failures

**Symptoms:**

```
pulling image nginx:latest: manifest not found
pulling image gcr.io/private/image:tag: unauthorized
```

**Causes:**

- Image name or tag is incorrect
- Private registry requires authentication
- Network connectivity issues

**Resolution:**

1. Verify the image exists:
   ```bash
   docker pull <image>:<tag>
   ```

2. For private registries, authenticate first:
   ```bash
   docker login <registry>
   ```

3. Check your `stack.yaml` for typos in image names.

### Container fails to start

**Symptoms:**

Container is created but immediately stops or shows `error` status in `gridctl status`.

**Causes:**

- Missing required environment variables
- Insufficient disk space
- The container's entrypoint crashes on startup

**Resolution:**

1. Check container logs:
   ```bash
   docker logs <container-name>
   ```

2. Verify environment variables in your `stack.yaml`:
   ```yaml
   mcp-servers:
     - name: my-server
       image: my-image:latest
       env:
         REQUIRED_VAR: "value"
   ```

3. Check available disk space:
   ```bash
   df -h
   docker system df
   ```

---

## MCP Connections

### Protocol generation mismatch

**Symptoms:**

```
server negotiated protocol version "...", which this gridctl supports in neither the handshake nor the stateless generation
protocol_generation is pinned to stateless but the server did not answer server/discover as a stateless-era peer
```

**Causes:**

- The server speaks only a protocol revision gridctl does not support
- A `protocol_generation:` pin in stack.yaml disagrees with what the server actually speaks
- A redeployed server changed generations and the pin was never updated
- A legacy server answers the `server/discover` probe with an auth challenge or a 5xx; the gateway rejects registration rather than guessing at the generation (pin `protocol_generation: handshake` for such servers)

**Resolution:**

1. Inspect per-server negotiated generations:
   ```bash
   gridctl doctor
   gridctl status --json
   ```
2. Remove any `protocol_generation:` pin so the gateway auto-negotiates (probe `server/discover`, fall back to `initialize`).
3. If a lax server misbehaves under the probe, pin it explicitly:
   ```yaml
   mcp-servers:
     - name: quirky
       url: https://example.com/mcp
       protocol_generation: handshake
   ```

The gateway serves both generations concurrently and bridges mixed fleets; a mixed-generation stack is normal and reported by `gridctl doctor`, not an error. The one deliberate gap is cross-generation MRTR: a handshake-era client calling a stateless-era tool that needs additional input receives a clear error instead of an interim result.

### Timeout waiting for MCP server

**Symptoms:**

```
timeout waiting for MCP server
timeout waiting for response from container
```

**Causes:**

- The MCP server inside the container is slow to initialize
- The server crashed after starting
- Network misconfiguration preventing the gateway from reaching the container

**Resolution:**

1. Check if the container is running:
   ```bash
   gridctl status
   ```

2. Check container logs for startup errors:
   ```bash
   docker logs gridctl-<stack>-<server-name>
   ```

3. Verify the server's port matches the `port` field in `stack.yaml`.

4. For stdio transport servers, ensure the container's entrypoint writes valid JSON-RPC to stdout.

### Connection lost

**Symptoms:**

Tool calls fail with `connection lost` after working initially.

**Causes:**

- The container was killed (OOMKilled, manual stop)
- The container process crashed
- Docker/Podman daemon restarted

**Resolution:**

1. Check container status:
   ```bash
   docker ps -a | grep gridctl
   ```

2. If OOMKilled, increase the container's memory limit.

3. Wait one health-check cycle (30 seconds by default): the gateway's health monitor detects the lost connection and reconnects automatically with exponential backoff. If the server needs a manual kick, restart just that server:
   ```bash
   curl -X POST http://localhost:8180/api/mcp-servers/<name>/restart
   ```
   (`gridctl reload` only applies stack.yaml changes; with an unchanged file it is a no-op.)

### Server was not running when gridctl started

**Symptoms:**

`gridctl status` shows a server as failed with `ready timeout after 30s`, and its tools are missing from the gateway, even though the server is running now. Typical with external URL servers started by hand after `gridctl apply`, or after a reboot where gridctl came up before its backends.

**Causes:**

The server was unreachable during gridctl's registration attempt. Registration is retried automatically: the failed server enters a retry loop driven by the health monitor, with exponential backoff capped at 30 seconds, and the status message shows when the next attempt is due (`retrying in 8s`).

**Resolution:**

1. Start the backend server. The gateway registers it automatically within one retry cycle; no gridctl restart and no config change is needed.
2. To force an attempt immediately, use the Restart button in the web UI or:
   ```bash
   curl -X POST http://localhost:8180/api/mcp-servers/<name>/restart
   ```
3. Servers whose failure cannot heal on its own are not retried: authorization failures (for a broker-managed OAuth server, fix with `gridctl auth login <name>`; for a raw 401 from a server with no `auth:` block, add `auth: {type: oauth}` or the correct static credentials to that server in stack.yaml), configuration errors (fix stack.yaml and reload), and container HTTP/SSE servers whose container was removed after the readiness timeout (fix with `gridctl reload` after a config change, or re-apply).

### Client shows "gridctl-gateway" instead of my config entry name

**Symptoms:**

The tool list in VS Code / GitHub Copilot labels a gridctl connection `gridctl-gateway` even though the entry in the client's config file has a different name (for example `gridctl-local`). With several gridctl entries linked, all of them show the same label.

**Causes:**

The entry key written by `gridctl link --name` / `--group` is a client-local alias and never reaches the gateway. Some clients instead display the identity the gateway reports in its MCP `initialize` response (`serverInfo.name`), which defaults to `gridctl-gateway` for every gridctl endpoint.

**Resolution:**

1. Set a distinct announced name per gateway in `stack.yaml`:
   ```yaml
   gateway:
     name: acme-stack
   ```

2. Group endpoints (`/groups/<name>/mcp`) automatically announce a suffixed identity such as `acme-stack/<group>`, so linked groups are distinguishable without configuration.

3. Restart the stack (`gridctl apply`); connected clients pick up the new name on their next initialize.

---

## Hot Reload

### Network configuration changed

**Symptoms:**

```
network configuration changed - full restart required (run gridctl destroy && gridctl apply)
```

**Causes:**

Network changes cannot be applied via hot reload because containers must be recreated with new network settings.

**Resolution:**

Perform a full restart (`destroy` takes the stack file or the name shown in `gridctl status`):

```bash
gridctl destroy stack.yaml
gridctl apply stack.yaml
```

### Partial reload failure

**Symptoms:**

Some servers reload successfully while others fail. The reload result shows errors for specific servers.

**Causes:**

- One server's image pull failed
- Port conflict on a new server
- Invalid configuration for the changed server

**Resolution:**

1. If the failure was transient (the backend was briefly unreachable), no action is needed: failed registrations are retried automatically by the health monitor and the server joins once it is reachable.
2. If source preparation or an image build failed before replacement, resolve the cause and run `gridctl reload` again. The failed declaration remains pending, so an unchanged file is retried while the old workload stays active.
3. For other configuration problems, fix the server's configuration in `stack.yaml` and run `gridctl reload`.
4. Servers that reloaded successfully are unaffected.

---

## Variables (vault)

### Vault is locked

**Symptoms:**

```
vault is locked. Set GRIDCTL_VAULT_PASSPHRASE or run 'gridctl var unlock'
```

Or via the API:

```json
{"error": "vault is locked"}
```

**Resolution:**

Unlock the store before accessing secrets:

```bash
gridctl var unlock
```

Or set the passphrase as an environment variable for non-interactive use:

```bash
export GRIDCTL_VAULT_PASSPHRASE="your-passphrase"
```

### Wrong passphrase

**Symptoms:**

```
wrong passphrase or corrupted vault
```

**Causes:**

- Incorrect passphrase entered
- The store file was corrupted (rare - disk error or interrupted write)

**Resolution:**

1. Try the correct passphrase. The store uses Argon2id key derivation - there is no way to recover a forgotten passphrase.

2. If the store file is corrupted, check for a backup:
   ```bash
   ls -la ~/.gridctl/vault/
   ```

3. As a last resort, delete the store and recreate variables:
   ```bash
   rm -rf ~/.gridctl/vault
   gridctl var set <KEY>   # the store is recreated on first write
   ```

   If more than the vault is wedged, prefer `gridctl reset` over hand-run
   `rm -rf`: it removes only what gridctl created (projections, wiring
   entries, containers), writes a backup first, and is safe to re-run.

---

## Podman-Specific Issues

### Rootless networking

**Symptoms:**

Containers cannot resolve each other by name in rootless Podman mode (DNS resolution fails, `nslookup` exits non-zero).

**Causes:**

Rootless Podman inter-container DNS requires `netavark` (the network backend) and `aardvark-dns` (the DNS resolver). These are separate from `pasta`/`slirp4netns`, which are egress transports used only for container-to-internet traffic, not container-to-container communication.

Gridctl automatically creates named netavark bridge networks (`gridctl apply` calls `EnsureNetwork` before starting containers). If `netavark` or `aardvark-dns` is missing, container name resolution will fail even though containers start successfully.

**Resolution:**

Install netavark and aardvark-dns:

```bash
# Fedora/RHEL
sudo dnf install netavark aardvark-dns

# Debian/Ubuntu
sudo apt install netavark aardvark-dns
```

Then verify Podman is using the netavark backend:

```bash
podman info --format 'network_backend={{.Host.NetworkBackend}}'
# Expected: network_backend=netavark
```

If the backend shows `cni`, configure Podman to use netavark by editing `/etc/containers/containers.conf`:

```ini
[network]
network_backend = "netavark"
```

> **Note:** `pasta` and `slirp4netns` provide container-to-host (egress) connectivity only. Inter-container networking uses netavark bridge networks - these are separate concerns.

### SELinux volume mount errors

**Symptoms:**

```
Permission denied
```

when a container tries to read mounted volumes on SELinux-enabled systems.

**Causes:**

SELinux labels on mounted files prevent container access.

**Resolution:**

Gridctl auto-detects SELinux and appends the `:Z` label to volume mounts. If you still see errors:

1. Check SELinux status:
   ```bash
   getenforce
   ```

2. Verify the file context:
   ```bash
   ls -Z /path/to/mounted/file
   ```

3. If needed, relabel manually:
   ```bash
   chcon -Rt svirt_sandbox_file_t /path/to/mounted/dir
   ```

### Host alias differences

Podman uses `host.containers.internal` (Podman 4.7+) instead of Docker's `host.docker.internal`. Gridctl handles this automatically - no action needed. If you see connection errors between agents and the gateway, ensure you are on Podman 4.7 or later:

```bash
podman --version
```

---

## Web UI

### UI not loading

**Symptoms:**

Browser shows a blank page or connection refused when accessing the gateway URL.

**Resolution:**

1. Verify the gateway is running:
   ```bash
   gridctl status
   ```

2. Check that you're using the correct port (default: 8180):
   ```
   http://localhost:8180
   ```

3. The web UI requires a modern browser - Chrome, Firefox, Safari, or Edge.

### Authentication prompt loop

**Symptoms:**

The UI keeps showing the authentication prompt after entering a valid token.

**Causes:**

- Token is incorrect or expired
- Auth configuration mismatch between `stack.yaml` and the token being used

**Resolution:**

1. Verify your auth configuration in `stack.yaml`:
   ```yaml
   gateway:
     auth:
       type: bearer
       token: ${AUTH_TOKEN}
   ```

2. Ensure the environment variable is set:
   ```bash
   echo $AUTH_TOKEN
   ```

3. Try clearing browser storage and re-entering the token.

---

## Downstream OAuth

### Login keeps failing after provider-side app rotation

**Symptoms:**

`gridctl auth login <name>` (or the UI's Authorize button) fails repeatedly for a server that used to authorize fine, often with an invalid-client or unauthorized-client error from the provider.

**Causes:**

- The provider rotated, deleted, or re-created the OAuth app that gridctl dynamically registered. gridctl still presents the cached client registration, which the provider no longer recognizes.

**Resolution:**

Reset the server's authorization state, which deletes both the stored grant and the cached client registration, then log in again:

```bash
gridctl auth reset <name>
gridctl auth login <name>
```

The next login re-discovers the authorization server and registers a fresh client. Plain `gridctl auth logout <name>` only removes the grant and keeps the (stale) client, so `reset` is the right tool here.

### Where OAuth tokens live, and what protects them

Tokens are stored encrypted at rest under `~/.gridctl/oauth/`, keyed by server URL so one login serves every connected client. The encryption key is a per-machine key stored adjacent to the ciphertext, not the passphrase-protected variable vault: it protects against casual file exposure (backups, copied home directories) but not against an attacker with code execution as your user, who could read the key just as gridctl does. Treat the directory's contents as credentials: keep it out of shared volumes and dotfile repositories, and use `gridctl auth logout` or `reset` to revoke and remove grants you no longer need.

---

## Pins and Poisoning Scan

### A legitimate tool is flagged with scan findings

The poisoning scan is a set of local heuristics, and some legitimate tools trip them. Common cases: a shell or database tool whose description honestly says it executes commands or drops tables fires `P003` (which is why P003 is info-tier), a security tool that documents attack phrases fires `P001` in downgraded form (quoted matches drop to info severity with low confidence), and a workflow tool that names another server's distinctively named tool fires `P006`. Generic tool names (`search`, `fetch`, `query`, and similar) in ordinary prose do not fire `P006` unless the description also names the owning server, and a bare mention of another server's name is reported at info severity only, so it never lights the findings chip or toast.

Findings never block anything: drift still requires the same approve decision, exit codes are unchanged unless you pass `--fail-on-findings`, and the Approve button stays enabled. If a specific code keeps firing on a legitimate stack, suppress it:

```yaml
gateway:
  security:
    schema_pinning:
      scan_ignore: [P004]
```

Set `scan: false` to disable the scanner: stack-time findings, API decoration, and add-server wizard probe findings all honor it, as does `scan_ignore`. (If schema pinning itself is disabled, the wizard probe still scans candidate servers with default settings, since no pin store exists to carry the configuration.) Both settings are advisory-only knobs; they never affect fingerprinting or drift detection.

### A finding reports "hidden characters" I cannot see

That is the point of the finding: zero-width characters, bidi controls, and Unicode Tags-block sequences render invisibly in most UIs but are read by the model. Every gridctl surface escapes them as visible sequences (`\u200b`, `\u202e`) so they become visible, and when a Tags-block sequence decodes to ASCII the smuggled message is shown as evidence. Treat a decoded hidden message in a tool description as hostile until proven otherwise; reset or remove the server rather than approving.

### What the scan cannot catch

Static heuristics are one layer. Published benchmarks put signature-only detection near two thirds recall, and attacks carried in runtime tool output (a tool that returns a fake error asking the model to read a credential file) are invisible to any pin-time check by construction. The scan makes the approve decision informed; it does not make a malicious server safe.

---

## Skills

### Where imported skill sources are cached

`gridctl skill add` clones each source repo into `~/.gridctl/cache/repos/<hash>/`, where `<hash>` is derived from the repo URL. The cache holds only clones; everything user-facing lives elsewhere (installed skills in `~/.gridctl/registry/skills/`, tracking in `~/.gridctl/skills.lock.yaml`). Deleting the cache directory is always safe: the next `skill add` or `skill update` that needs a repo re-clones it.

Git-sourced MCP server image builds use a separate builder namespace. Each active build plan gets an isolated checkout under `~/.gridctl/cache/builder/worktrees/`, resolved to the fetched commit, and removes that checkout when the plan closes. Deleting leftover builder worktrees is safe when no build is running; the next apply creates a fresh checkout.

### `skill update` does not pick up an upstream change

`skill update` fetches the source and installs whatever the pinned ref (or the default branch, for unpinned sources) now points at. Sources pinned to a version tag or full commit SHA are deliberately skipped by a bulk `gridctl skill update`; update them explicitly by name, or re-pin. A skill with local edits (drift) is also skipped so your changes are not overwritten; resolve the drift or pass `--force` to discard local edits and reinstall upstream (a backup of the edited `SKILL.md` is kept beside the skill). If a drifted skill was previously skipped during a web UI sync, its reviewed upstream version was recorded as seen, so a plain update reports up to date; `gridctl skill update --force <name>` installs it. When the network is unreachable, updates degrade to the cached content with a warning rather than failing.

### "written by a newer gridctl version" on ctx, skill project, or project commands

The unified projection lockfile (`~/.gridctl/project.lock.yaml`) carries a schema version, and an older gridctl refuses to touch a file written by a newer one rather than risk corrupting it. Upgrade gridctl on this machine, or restore the pre-migration state from `~/.gridctl/project-migration-backup/` if you need to stay on the older version.

---

## Recovering from reset

`gridctl reset` writes a tar.gz backup before deleting anything: under
`~/.gridctl/backups/` for the default tier, or beside the removed
directory (`~/.gridctl-backup-reset-<timestamp>.tar.gz`) for `--purge`.
The backup is a safety copy, not an undo; gridctl has no restore
command, and copying projection files back by hand leaves the lockfile
out of sync (everything shows as drifted).

The supported recovery path is forward: re-run the imports that created
the state.

```bash
gridctl apply <stack.yaml>      # stacks, containers, daemons
gridctl pack add <repo>         # skills, agents, rules, wiring from a pack
gridctl skill add <repo>        # individually imported skills
gridctl link <client>           # gateway entries in client configs
```

Manual fallback: extract the archive from the home directory it was
made in (`tar -xzf <backup> -C ~`). Paths inside are relative to that
home. After a manual extract of projection files, run
`gridctl project status` and adopt or re-sync anything reported as
drifted. The purge-tier archive deliberately excludes oauth tokens and
daemon state (re-authorize with `gridctl auth login`) and
cache/logs/telemetry (recreated as needed); it does include the vault,
pins, registry, context store, saved stacks, and lockfiles.

Reset does not remove built container images or named Docker volumes;
they are not gridctl-owned the way labeled containers are. If leftover
`gridctl-*` images or volumes bother you, clear them with
`docker image prune` and `docker volume prune`.

## General

### Getting help

If your issue isn't covered here:

1. Run `gridctl doctor` for automated environment checks with remediation hints
2. Check `gridctl status` for the current state of your stack
3. Tail the gateway daemon log: `gridctl logs [stack] -f`
4. Review an MCP server's container logs: `gridctl logs --server <name>` (or `docker logs gridctl-<stack>-<name>`)
5. Run with verbose logging: `gridctl apply <stack.yaml> --verbose`
6. Open an issue at [github.com/gridctl/gridctl/issues](https://github.com/gridctl/gridctl/issues)
