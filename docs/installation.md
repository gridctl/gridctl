# Installation

Install gridctl on macOS, Linux, or WSL2. The recommended path is the one-liner installer; package managers and pre-built binaries are documented below for users who prefer them.

## Quick install (macOS, Linux, WSL2)

```bash
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh
```

Installs the latest release to `~/.local/bin/gridctl`. The script verifies the release checksum and prints the install path and next steps.

The script can be inspected before running:

```bash
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | less
```

> **Windows**: install [WSL2](https://learn.microsoft.com/en-us/windows/wsl/install), then run the command above inside your Linux distribution.

![Install Gridctl](../assets/install.gif)

## Package managers

<details>
<summary><strong>Homebrew</strong> (macOS, Linux)</summary>

```bash
brew install gridctl/tap/gridctl
```

Update with `brew upgrade gridctl/tap/gridctl`.

</details>

## Other options

<details>
<summary><strong>Pre-built binaries</strong></summary>

Download the tarball for your platform from the [releases page](https://github.com/gridctl/gridctl/releases), verify it against `checksums.txt`, extract, and place `gridctl` on your `PATH`.

</details>

<details>
<summary><strong>Build from source</strong></summary>

Requires Go 1.26+ and Node 20+.

```bash
git clone https://github.com/gridctl/gridctl
cd gridctl && make build
./gridctl --help
```

</details>

## Updating

```bash
gridctl upgrade            # check + prompt + upgrade (standalone install)
gridctl upgrade --check    # only check; do not install
gridctl upgrade --yes      # non-interactive (CI)
gridctl upgrade --version v0.1.0-beta.6   # install a specific version
```

If gridctl was installed via Homebrew, `gridctl upgrade` detects that and recommends `brew upgrade gridctl/tap/gridctl` instead.

## Uninstalling

```bash
# Standalone install
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh -s -- --uninstall

# Also remove the config directory at ~/.gridctl
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh -s -- --uninstall --purge

# Homebrew install
brew uninstall gridctl/tap/gridctl
```

## Container runtime

Gridctl requires a container runtime for workloads that run in containers (MCP servers with `image` and resources). Docker is detected by default; [Podman](https://podman.io) is also fully supported.

### Runtime detection

Gridctl auto-detects your runtime by probing sockets in this order:

1. `$DOCKER_HOST` (if set)
2. `/var/run/docker.sock` (Docker)
3. `/run/podman/podman.sock` (Podman rootful)
4. `$XDG_RUNTIME_DIR/podman/podman.sock` (Podman rootless)

Override detection with the `--runtime` flag or `GRIDCTL_RUNTIME` environment variable:

```bash
gridctl apply stack.yaml --runtime podman
# or
GRIDCTL_RUNTIME=podman gridctl apply stack.yaml
```

### Using Podman

```bash
# Install Podman (macOS)
brew install podman
podman machine init
podman machine start

# Install Podman (Linux)
sudo apt install podman        # Debian/Ubuntu
sudo dnf install podman        # Fedora/RHEL

# Enable the Podman socket (Linux rootless)
systemctl --user enable --now podman.socket

# Verify gridctl detects Podman
gridctl info
```

Podman 4.0+ is required for rootless multi-container networking (netavark + aardvark-dns). Podman 4.7+ is recommended for full `host.containers.internal` support. Older versions fall back to the Docker-compatible `host.docker.internal` alias. SELinux volume labels (`:Z`) are applied automatically when Podman is running on an SELinux-enforcing system.

---

Back to the [docs index](README.md) or the [project README](../README.md).
