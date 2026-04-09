package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// RuntimeType identifies the container runtime.
type RuntimeType string

const (
	RuntimeDocker RuntimeType = "docker"
	RuntimePodman RuntimeType = "podman"
)

// RuntimeInfo holds detected runtime configuration.
type RuntimeInfo struct {
	Type           RuntimeType
	SocketPath     string
	HostAlias      string // "host.docker.internal" or "host.containers.internal"
	Version        string // Runtime version for feature gating
	SELinux        bool   // Whether SELinux is enforcing
	HasNetavark    bool   // Whether netavark is available (rootless inter-container networking)
	HasAardvarkDNS bool   // Whether aardvark-dns is available (inter-container DNS resolution)
}

// DetectOptions configures runtime detection.
type DetectOptions struct {
	Explicit string // Value from --runtime flag or GRIDCTL_RUNTIME env var
}

// DetectRuntime probes for an available container runtime.
// Priority: explicit selection > DOCKER_HOST > Docker socket > Podman sockets.
func DetectRuntime(opts DetectOptions) (*RuntimeInfo, error) {
	// Explicit selection via --runtime flag or GRIDCTL_RUNTIME env var
	if opts.Explicit != "" {
		return resolveExplicit(opts.Explicit)
	}

	// Check GRIDCTL_RUNTIME env var
	if envRT := os.Getenv("GRIDCTL_RUNTIME"); envRT != "" {
		return resolveExplicit(envRT)
	}

	// Auto-detect
	return autoDetect()
}

// resolveExplicit resolves an explicitly requested runtime.
func resolveExplicit(requested string) (*RuntimeInfo, error) {
	rt := RuntimeType(strings.ToLower(requested))
	switch rt {
	case RuntimeDocker:
		// Try DOCKER_HOST first, then default socket
		if host := os.Getenv("DOCKER_HOST"); host != "" {
			socketPath := extractSocketPath(host)
			if socketPath != "" && probeSocket(socketPath) {
				return buildRuntimeInfo(RuntimeDocker, socketPath)
			}
		}
		if probeSocket("/var/run/docker.sock") {
			return buildRuntimeInfo(RuntimeDocker, "/var/run/docker.sock")
		}
		return nil, fmt.Errorf("docker runtime requested but Docker socket not found or not responding\n\nChecked:\n  - /var/run/docker.sock\n\nInstall Docker: https://docs.docker.com/get-docker/")

	case RuntimePodman:
		sockets := podmanSockets()
		for _, s := range sockets {
			if probeSocket(s) {
				return buildRuntimeInfo(RuntimePodman, s)
			}
		}
		return nil, fmt.Errorf("podman runtime requested but Podman socket not found or not responding\n\nChecked:\n%s\n\nInstall Podman: https://podman.io/getting-started/installation", formatCheckedSockets(sockets))

	default:
		return nil, fmt.Errorf("unknown runtime %q: must be 'docker' or 'podman'", requested)
	}
}

// autoDetect probes sockets in priority order.
func autoDetect() (*RuntimeInfo, error) {
	// 1. DOCKER_HOST (if set)
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		socketPath := extractSocketPath(host)
		if socketPath != "" && probeSocket(socketPath) {
			// Determine type by querying version
			rt := detectTypeFromSocket(socketPath)
			return buildRuntimeInfo(rt, socketPath)
		}
	}

	// 2. Docker default socket
	if probeSocket("/var/run/docker.sock") {
		return buildRuntimeInfo(RuntimeDocker, "/var/run/docker.sock")
	}

	// 3. Podman sockets
	for _, s := range podmanSockets() {
		if probeSocket(s) {
			return buildRuntimeInfo(RuntimePodman, s)
		}
	}

	return nil, buildNoRuntimeError()
}

// podmanSockets returns Podman socket paths to check.
func podmanSockets() []string {
	var sockets []string
	// Rootful Podman
	sockets = append(sockets, "/run/podman/podman.sock")
	// Rootless Podman (user socket)
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		sockets = append(sockets, xdg+"/podman/podman.sock")
	} else {
		// Common default for rootless
		sockets = append(sockets, fmt.Sprintf("/run/user/%d/podman/podman.sock", os.Getuid()))
	}
	return sockets
}

// probeSocket checks if a Unix socket exists and responds to HTTP pings.
func probeSocket(socketPath string) bool {
	// Check socket file exists
	fi, err := os.Stat(socketPath)
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return false
	}

	// Ping the API
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", socketPath, 2*time.Second)
			},
		},
	}
	resp, err := client.Get("http://localhost/_ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// extractSocketPath extracts a Unix socket path from a DOCKER_HOST value.
func extractSocketPath(host string) string {
	if strings.HasPrefix(host, "unix://") {
		return strings.TrimPrefix(host, "unix://")
	}
	return ""
}

// detectTypeFromSocket queries the version endpoint to determine runtime type.
func detectTypeFromSocket(socketPath string) RuntimeType {
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", socketPath, 2*time.Second)
			},
		},
	}
	resp, err := client.Get("http://localhost/version")
	if err != nil {
		return RuntimeDocker // Default assumption
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	if strings.Contains(strings.ToLower(body), "podman") {
		return RuntimePodman
	}
	return RuntimeDocker
}

// buildRuntimeInfo constructs RuntimeInfo with version and platform details.
func buildRuntimeInfo(rt RuntimeType, socketPath string) (*RuntimeInfo, error) {
	info := &RuntimeInfo{
		Type:       rt,
		SocketPath: socketPath,
	}

	// Query version
	info.Version = queryVersion(socketPath)

	// Resolve host alias
	info.HostAlias = resolveHostAlias(rt, info.Version)

	// Detect SELinux (Linux only)
	if runtime.GOOS == "linux" {
		info.SELinux = detectSELinux()
	}

	// Detect netavark/aardvark-dns for rootless Podman
	if rt == RuntimePodman && info.IsRootless() {
		info.HasNetavark = detectNetavark()
		info.HasAardvarkDNS = detectAardvarkDNS()
	}

	return info, nil
}

// queryVersion gets the runtime version from the API.
func queryVersion(socketPath string) string {
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", socketPath, 2*time.Second)
			},
		},
	}
	resp, err := client.Get("http://localhost/version")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// Extract version from JSON response
	re := regexp.MustCompile(`"Version"\s*:\s*"([^"]+)"`)
	if matches := re.FindStringSubmatch(body); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// resolveHostAlias determines the correct host alias for container-to-host communication.
func resolveHostAlias(rt RuntimeType, version string) string {
	if rt == RuntimeDocker {
		return "host.docker.internal"
	}
	// Podman 4.7+ supports host.containers.internal
	if compareSemver(version, "4.7.0") >= 0 {
		return "host.containers.internal"
	}
	// Older Podman: use Docker-compatible alias (supported as compatibility shim)
	return "host.docker.internal"
}

// detectNetavark checks if netavark is available for rootless bridge networking.
func detectNetavark() bool {
	if _, err := exec.LookPath("netavark"); err == nil {
		return true
	}
	for _, p := range []string{"/usr/libexec/podman/netavark", "/usr/lib/podman/netavark"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// detectAardvarkDNS checks if aardvark-dns is available for inter-container DNS.
func detectAardvarkDNS() bool {
	if _, err := exec.LookPath("aardvark-dns"); err == nil {
		return true
	}
	for _, p := range []string{"/usr/libexec/podman/aardvark-dns", "/usr/lib/podman/aardvark-dns"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// detectSELinux checks if SELinux is in enforcing mode.
func detectSELinux() bool {
	// Check /sys/fs/selinux/enforce
	data, err := os.ReadFile("/sys/fs/selinux/enforce")
	if err == nil {
		return strings.TrimSpace(string(data)) == "1"
	}
	// Fallback to getenforce command
	out, err := exec.Command("getenforce").Output()
	if err == nil {
		return strings.TrimSpace(string(out)) == "Enforcing"
	}
	return false
}

// ApplyVolumeLabels appends :Z to volume mounts when Podman + SELinux.
func ApplyVolumeLabels(volumes []string, info *RuntimeInfo) []string {
	if info == nil || info.Type != RuntimePodman || !info.SELinux {
		return volumes
	}
	result := make([]string, len(volumes))
	for i, v := range volumes {
		parts := strings.Split(v, ":")
		// Only add :Z if no SELinux label already present
		if len(parts) == 2 {
			result[i] = v + ":Z"
		} else if len(parts) >= 3 {
			lastPart := strings.ToLower(parts[len(parts)-1])
			if lastPart == "z" || lastPart == "Z" {
				result[i] = v // Already has label
			} else {
				result[i] = v + ",Z"
			}
		} else {
			result[i] = v
		}
	}
	return result
}

// compareSemver compares two semver strings. Returns -1, 0, or 1.
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

// parseSemver extracts major.minor.patch from a version string.
func parseSemver(v string) [3]int {
	var parts [3]int
	re := regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(v)
	if len(matches) < 2 {
		return parts
	}
	for i := 1; i < len(matches) && i <= 3; i++ {
		if matches[i] != "" {
			n, _ := strconv.Atoi(matches[i])
			parts[i-1] = n
		}
	}
	return parts
}

// DockerHost returns the DOCKER_HOST value for connecting to this runtime.
func (info *RuntimeInfo) DockerHost() string {
	return "unix://" + info.SocketPath
}

// HostAliasHostname returns just the hostname part of the host alias.
func (info *RuntimeInfo) HostAliasHostname() string {
	return info.HostAlias
}

// DisplayName returns a human-readable name for the runtime.
func (info *RuntimeInfo) DisplayName() string {
	switch info.Type {
	case RuntimePodman:
		return "podman"
	default:
		return "docker"
	}
}

// CLIName returns the CLI binary name for user-facing messages.
func (info *RuntimeInfo) CLIName() string {
	return string(info.Type)
}

// IsExperimental returns true if this runtime is experimental.
func (info *RuntimeInfo) IsExperimental() bool {
	return false
}

// IsSupportedPodmanVersion returns true if the Podman version supports netavark (4.0+).
func (info *RuntimeInfo) IsSupportedPodmanVersion() bool {
	if info.Type != RuntimePodman {
		return true
	}
	return compareSemver(info.Version, "4.0.0") >= 0
}

// IsRootless returns true if this appears to be a rootless Podman socket.
func (info *RuntimeInfo) IsRootless() bool {
	if info.Type != RuntimePodman {
		return false
	}
	// Rootful socket is at /run/podman/podman.sock
	return !strings.HasPrefix(info.SocketPath, "/run/podman/")
}

// buildNoRuntimeError creates an actionable error when no runtime is found.
func buildNoRuntimeError() error {
	var checked []string
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		checked = append(checked, "  - "+host+" (from DOCKER_HOST)")
	}
	checked = append(checked, "  - /var/run/docker.sock")
	for _, s := range podmanSockets() {
		checked = append(checked, "  - "+s)
	}

	// Check if binaries are in PATH
	var hints []string
	if _, err := exec.LookPath("docker"); err != nil {
		hints = append(hints, "  - 'docker' not found in PATH")
	} else {
		hints = append(hints, "  - 'docker' found in PATH but socket not responding")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		hints = append(hints, "  - 'podman' not found in PATH")
	} else {
		hints = append(hints, "  - 'podman' found in PATH but socket not responding")
	}

	msg := "no container runtime available\n\n"
	msg += "Sockets checked:\n" + strings.Join(checked, "\n") + "\n\n"
	msg += "Diagnostics:\n" + strings.Join(hints, "\n") + "\n\n"
	msg += "Install Docker: https://docs.docker.com/get-docker/\n"
	msg += "Install Podman: https://podman.io/getting-started/installation"

	return fmt.Errorf("%s", msg)
}

// formatCheckedSockets formats socket paths for error messages.
func formatCheckedSockets(sockets []string) string {
	var lines []string
	for _, s := range sockets {
		lines = append(lines, "  - "+s)
	}
	return strings.Join(lines, "\n")
}
