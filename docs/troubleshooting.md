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

2. If a previous gridctl instance is running, stop it first:
   ```bash
   gridctl destroy
   ```

3. Start on a different port: `--port` sets the gateway/web UI port (default `8180`), `--base-port` sets the MCP server host-port allocation base (default `9000`):
   ```bash
   gridctl apply stack.yaml --port 8181 --base-port 9100
   ```

4. If the port is in TIME_WAIT, wait a few seconds or use a different port.

---

## Container Startup

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

3. Use hot reload to restart the affected server:
   ```bash
   # Touch the config to trigger reload
   gridctl reload
   ```

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

Perform a full restart:

```bash
gridctl destroy
gridctl apply
```

### Partial reload failure

**Symptoms:**

Some servers reload successfully while others fail. The reload result shows errors for specific servers.

**Causes:**

- One server's image pull failed
- Port conflict on a new server
- Invalid configuration for the changed server

**Resolution:**

1. Fix the specific server's configuration in `stack.yaml`.
2. Run `gridctl reload` again - only the failed changes will be retried.
3. Servers that reloaded successfully are unaffected.

---

## Vault

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

The poisoning scan is a set of local heuristics, and some legitimate tools trip them. Common cases: a shell or database tool whose description honestly says it executes commands or drops tables fires `P003` (which is why P003 is info-tier), a security tool that documents attack phrases fires `P001` in downgraded form (quoted matches drop to info severity with low confidence), and a workflow tool that mentions another server's tool by name fires `P006`.

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

## General

### Getting help

If your issue isn't covered here:

1. Run `gridctl doctor` for automated environment checks with remediation hints
2. Check `gridctl status` for the current state of your stack
3. Tail the gateway daemon log: `gridctl logs [stack] -f`
4. Review an MCP server's container logs: `gridctl logs --server <name>` (or `docker logs gridctl-<stack>-<name>`)
5. Run with verbose logging: `gridctl apply <stack.yaml> --verbose`
6. Open an issue at [github.com/gridctl/gridctl/issues](https://github.com/gridctl/gridctl/issues)
