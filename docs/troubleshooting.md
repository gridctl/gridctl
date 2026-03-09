# Troubleshooting Guide

Common issues and resolutions for gridctl.

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

Install Docker or Podman. If your stack only uses external URL or local process servers, no container runtime is needed — check your `stack.yaml` for servers that require containers (those with `image:` or `source:`).

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

3. Change the gateway port in your `stack.yaml`:
   ```yaml
   gateway:
     port: 9001  # Use a different port
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

---

## Hot Reload

### Network configuration changed

**Symptoms:**

```
network configuration changed - full restart required (run gridctl destroy && gridctl deploy)
```

**Causes:**

Network changes cannot be applied via hot reload because containers must be recreated with new network settings.

**Resolution:**

Perform a full restart:

```bash
gridctl destroy
gridctl deploy
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
2. Run `gridctl reload` again — only the failed changes will be retried.
3. Servers that reloaded successfully are unaffected.

---

## Vault

### Vault is locked

**Symptoms:**

```
vault is locked. Set GRIDCTL_VAULT_PASSPHRASE or run 'gridctl vault unlock'
```

Or via the API:

```json
{"error": "vault is locked"}
```

**Resolution:**

Unlock the vault before accessing secrets:

```bash
gridctl vault unlock
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
- The vault file was corrupted (rare — disk error or interrupted write)

**Resolution:**

1. Try the correct passphrase. The vault uses PBKDF2 key derivation — there is no way to recover a forgotten passphrase.

2. If the vault file is corrupted, check for a backup:
   ```bash
   ls -la ~/.config/gridctl/vault*
   ```

3. As a last resort, delete the vault and recreate secrets:
   ```bash
   rm ~/.config/gridctl/vault.enc
   gridctl vault init
   ```

---

## Podman-Specific Issues

### Rootless networking

**Symptoms:**

Containers cannot communicate with each other in rootless Podman mode.

**Causes:**

Rootless Podman requires `slirp4netns` or `pasta` for inter-container networking.

**Resolution:**

1. Install the networking helper:
   ```bash
   # Fedora/RHEL
   sudo dnf install slirp4netns

   # Debian/Ubuntu
   sudo apt install slirp4netns
   ```

2. Alternatively, use `pasta` (newer, better performance):
   ```bash
   sudo dnf install passt    # Fedora
   sudo apt install passt    # Debian/Ubuntu
   ```

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

Podman uses `host.containers.internal` (Podman 4.7+) instead of Docker's `host.docker.internal`. Gridctl handles this automatically — no action needed. If you see connection errors between agents and the gateway, ensure you are on Podman 4.7 or later:

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

2. Check that you're using the correct port (default: 9000):
   ```
   http://localhost:9000
   ```

3. The web UI requires a modern browser — Chrome, Firefox, Safari, or Edge.

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

## General

### Getting help

If your issue isn't covered here:

1. Check `gridctl status` for the current state of your stack
2. Review container logs: `docker logs gridctl-<stack>-<name>`
3. Run with verbose logging: `gridctl deploy --log-level debug`
4. Open an issue at [github.com/gridctl/gridctl/issues](https://github.com/gridctl/gridctl/issues)
