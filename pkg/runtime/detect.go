package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/client"
)

// RuntimeType identifies the container runtime.
type RuntimeType string

const (
	RuntimeDocker RuntimeType = "docker"
	RuntimePodman RuntimeType = "podman"
)

// RuntimeInfo describes the detected container runtime.
type RuntimeInfo struct {
	Type       RuntimeType
	SocketPath string
	HostAlias  string // "host.docker.internal:host-gateway" or "host.containers.internal:host-gateway"
	Version    string // Runtime version (e.g., "5.8.0")
	SELinux    bool   // Whether SELinux is enforcing
}

// DetectOptions controls runtime detection behavior.
type DetectOptions struct {
	// Explicit is the user-specified runtime ("docker" or "podman") from --runtime flag.
	Explicit string
}

// DetectRuntime probes for an available container runtime.
// Resolution priority: explicit flag > GRIDCTL_RUNTIME env > auto-detect (DOCKER_HOST first).
func DetectRuntime(ctx context.Context, opts DetectOptions) (*RuntimeInfo, error) {
	// 1. Explicit flag
	if opts.Explicit != "" {
		return resolveExplicit(ctx, RuntimeType(opts.Explicit))
	}

	// 2. GRIDCTL_RUNTIME env var
	if envRT := os.Getenv("GRIDCTL_RUNTIME"); envRT != "" {
		return resolveExplicit(ctx, RuntimeType(envRT))
	}

	// 3. Auto-detect by socket probing
	return autoDetect(ctx)
}

// resolveExplicit validates and connects to a user-specified runtime.
func resolveExplicit(ctx context.Context, rt RuntimeType) (*RuntimeInfo, error) {
	switch rt {
	case RuntimeDocker:
		sockets := dockerSockets()
		for _, sock := range sockets {
			if info, err := probeSocket(ctx, sock, RuntimeDocker); err == nil {
				return info, nil
			}
		}
		return nil, fmt.Errorf("docker runtime explicitly selected but not available\n  Checked: %s\n\n  Install Docker: https://docs.docker.com/get-docker/", strings.Join(sockets, ", "))

	case RuntimePodman:
		sockets := podmanSockets()
		for _, sock := range sockets {
			if info, err := probeSocket(ctx, sock, RuntimePodman); err == nil {
				return info, nil
			}
		}
		return nil, fmt.Errorf("podman runtime explicitly selected but not available\n  Checked: %s\n\n  Install Podman: https://podman.io/docs/installation\n  Start socket: systemctl start podman.socket", strings.Join(sockets, ", "))

	default:
		return nil, fmt.Errorf("unknown runtime %q: valid options are 'docker' or 'podman'", rt)
	}
}

// autoDetect probes sockets in priority order.
func autoDetect(ctx context.Context) (*RuntimeInfo, error) {
	var checked []string

	// DOCKER_HOST always takes precedence in auto-detect
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		checked = append(checked, dockerHost+" ($DOCKER_HOST)")
		// Determine runtime type by probing
		if info, err := probeSocket(ctx, dockerHost, ""); err == nil {
			return info, nil
		}
	}

	// Docker default socket
	for _, sock := range dockerSockets() {
		checked = append(checked, sock)
		if info, err := probeSocket(ctx, sock, RuntimeDocker); err == nil {
			return info, nil
		}
	}

	// Podman sockets
	for _, sock := range podmanSockets() {
		checked = append(checked, sock)
		if info, err := probeSocket(ctx, sock, RuntimePodman); err == nil {
			return info, nil
		}
	}

	return nil, buildNoRuntimeError(checked)
}

// probeSocket attempts to connect to a socket and identify the runtime.
func probeSocket(ctx context.Context, socketPath string, expectedType RuntimeType) (*RuntimeInfo, error) {
	// Normalize socket path for Docker SDK
	host := socketPath
	if !strings.Contains(host, "://") {
		// Check if socket file exists
		if _, err := os.Stat(socketPath); err != nil {
			return nil, fmt.Errorf("socket not found: %s", socketPath)
		}
		host = "unix://" + socketPath
	}

	cli, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating client for %s: %w", socketPath, err)
	}
	defer cli.Close()

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ping, err := cli.Ping(probeCtx)
	if err != nil {
		return nil, fmt.Errorf("ping %s: %w", socketPath, err)
	}

	// Determine runtime type
	runtimeType := expectedType
	if runtimeType == "" {
		// Detect from server response
		runtimeType = RuntimeDocker
	}

	// Check if this is actually Podman by looking at component names
	version, verErr := cli.ServerVersion(probeCtx)
	runtimeVersion := ping.APIVersion
	if verErr == nil {
		for _, component := range version.Components {
			if strings.EqualFold(component.Name, "Podman Engine") {
				runtimeType = RuntimePodman
				runtimeVersion = component.Version
				break
			}
		}
		if runtimeType == RuntimeDocker {
			// Use Docker server version
			runtimeVersion = version.Version
		}
	}

	info := &RuntimeInfo{
		Type:       runtimeType,
		SocketPath: socketPath,
		Version:    runtimeVersion,
	}

	// Set host alias based on runtime
	info.HostAlias = resolveHostAlias(info)

	// Detect SELinux on Linux
	if runtime.GOOS == "linux" {
		info.SELinux = detectSELinux()
	}

	return info, nil
}

// resolveHostAlias returns the appropriate host alias for the runtime.
func resolveHostAlias(info *RuntimeInfo) string {
	if info.Type == RuntimePodman && compareSemver(info.Version, "4.7.0") >= 0 {
		return "host.containers.internal:host-gateway"
	}
	// Docker and older Podman both support host.docker.internal
	return "host.docker.internal:host-gateway"
}

// dockerSockets returns Docker socket paths to probe.
func dockerSockets() []string {
	return []string{"/var/run/docker.sock"}
}

// podmanSockets returns Podman socket paths to probe in priority order.
func podmanSockets() []string {
	var sockets []string

	// Rootful socket first (default for system-wide Podman)
	sockets = append(sockets, "/run/podman/podman.sock")

	// User socket (rootless)
	if xdgDir := os.Getenv("XDG_RUNTIME_DIR"); xdgDir != "" {
		sockets = append(sockets, filepath.Join(xdgDir, "podman", "podman.sock"))
	}

	return sockets
}

// detectSELinux checks if SELinux is in enforcing mode.
func detectSELinux() bool {
	// Try reading sysfs first (faster, no exec)
	data, err := os.ReadFile("/sys/fs/selinux/enforce")
	if err == nil {
		return strings.TrimSpace(string(data)) == "1"
	}

	// Fallback to getenforce
	out, err := exec.Command("getenforce").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "Enforcing"
}

// detectRootlessPodman checks if a Podman socket is running in rootless mode.
func detectRootlessPodman(socketPath string) bool {
	// Rootful socket is at /run/podman/podman.sock
	// Rootless sockets are at /run/user/<uid>/podman/podman.sock or $XDG_RUNTIME_DIR/...
	return !strings.HasPrefix(socketPath, "/run/podman/")
}

// binaryInPath checks if a binary exists in PATH.
func binaryInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// buildNoRuntimeError constructs an actionable error message when no runtime is found.
func buildNoRuntimeError(checked []string) error {
	msg := "no container runtime available\n"

	for _, s := range checked {
		msg += fmt.Sprintf("  Checked: %s (not available)\n", s)
	}

	// Check if binaries are installed for better guidance
	hasDocker := binaryInPath("docker")
	hasPodman := binaryInPath("podman")

	if hasDocker {
		msg += "\n  Docker is installed but the daemon is not running."
		msg += "\n  Start it: sudo systemctl start docker"
	}
	if hasPodman {
		msg += "\n  Podman is installed but the socket is not active."
		msg += "\n  Start it: systemctl start podman.socket"
	}

	if !hasDocker && !hasPodman {
		msg += "\n  Install a container runtime:"
		msg += "\n    Docker: https://docs.docker.com/get-docker/"
		msg += "\n    Podman: https://podman.io/docs/installation"
	}

	msg += "\n\n  Set manually: --runtime docker or --runtime podman"
	msg += "\n  Or set: GRIDCTL_RUNTIME=docker or GRIDCTL_RUNTIME=podman"

	return fmt.Errorf("%s", msg)
}

// IsExperimental returns true if the runtime is experimental.
func (r *RuntimeInfo) IsExperimental() bool {
	return r.Type == RuntimePodman
}

// IsRootless returns true if this appears to be a rootless Podman instance.
func (r *RuntimeInfo) IsRootless() bool {
	return r.Type == RuntimePodman && detectRootlessPodman(r.SocketPath)
}

// DisplayName returns a human-readable runtime name with experimental label if applicable.
func (r *RuntimeInfo) DisplayName() string {
	name := string(r.Type)
	if r.IsExperimental() {
		name += " (experimental)"
	}
	return name
}

// CLIName returns the CLI binary name for this runtime (used in user-facing messages).
func (r *RuntimeInfo) CLIName() string {
	if r.Type == RuntimePodman {
		return "podman"
	}
	return "docker"
}

// ApplyVolumeLabels applies SELinux volume labels when needed.
// On Podman with SELinux enforcing, appends :Z to volume binds that don't already have a label.
func (r *RuntimeInfo) ApplyVolumeLabels(volumes []string) []string {
	if r.Type != RuntimePodman || !r.SELinux {
		return volumes
	}

	result := make([]string, len(volumes))
	for i, v := range volumes {
		parts := strings.Split(v, ":")
		// Format: "host:container" or "host:container:mode"
		if len(parts) == 2 {
			// No mode specified, append :Z
			result[i] = v + ":Z"
		} else if len(parts) == 3 {
			// Check if already has SELinux label
			mode := strings.ToLower(parts[2])
			if !strings.Contains(mode, "z") {
				result[i] = v + ",Z"
			} else {
				result[i] = v
			}
		} else {
			result[i] = v
		}
	}
	return result
}

// compareSemver compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareSemver(a, b string) int {
	aParts := parseSemver(a)
	bParts := parseSemver(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

var semverRe = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// parseSemver extracts major.minor.patch from a version string.
func parseSemver(v string) [3]int {
	matches := semverRe.FindStringSubmatch(v)
	if matches == nil {
		return [3]int{}
	}

	var parts [3]int
	for i := 1; i < len(matches) && i <= 3; i++ {
		if matches[i] != "" {
			n, _ := strconv.Atoi(matches[i])
			parts[i-1] = n
		}
	}
	return parts
}

// DockerHost returns the DOCKER_HOST value to use for the Docker SDK client.
// Returns empty string if the default socket should be used.
func (r *RuntimeInfo) DockerHost() string {
	if r.Type == RuntimePodman {
		return "unix://" + r.SocketPath
	}
	// For Docker, if the socket is the default, let the SDK use its own defaults
	if r.SocketPath == "/var/run/docker.sock" {
		return ""
	}
	return "unix://" + r.SocketPath
}

// HostAliasHostname returns just the hostname part of the host alias (without :host-gateway).
func (r *RuntimeInfo) HostAliasHostname() string {
	parts := strings.SplitN(r.HostAlias, ":", 2)
	return parts[0]
}

