//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // register factory
)

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// leadingVersion pulls the major and minor out of a version string such as
// "5.8.4" or "netavark 1.4.0".
func leadingVersion(s string) (major, minor int, ok bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

// netavarkVersion returns the netavark version Podman reports. The second
// return is a diagnostic explaining why the version could not be determined,
// and is empty on success.
//
// Callers MUST surface that diagnostic. An earlier version of this guard asked
// for a single Go-template field and swallowed any error, so when the field did
// not resolve it silently reported "cannot determine" and the guard never fired
// — indistinguishable from a healthy host. Parsing the whole document and
// reporting what was actually seen makes that failure mode visible instead.
func netavarkVersion(ctx context.Context) (string, string) {
	out, err := exec.CommandContext(ctx, "podman", "info", "--format", "json").Output()
	if err != nil {
		return "", "podman info failed: " + err.Error()
	}
	return parseNetavarkVersion(out)
}

// parseNetavarkVersion extracts the netavark version from `podman info --format
// json` output. Split from the exec so the JSON handling is testable without a
// live Podman — the previous guard shipped its field lookup unverified and that
// lookup is precisely what failed.
func parseNetavarkVersion(out []byte) (string, string) {
	var info struct {
		Host struct {
			NetworkBackend     string `json:"networkBackend"`
			NetworkBackendInfo struct {
				Backend string `json:"backend"`
				Version string `json:"version"`
				Package string `json:"package"`
			} `json:"networkBackendInfo"`
		} `json:"host"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return "", "podman info returned unparseable JSON: " + err.Error()
	}

	// The version may be reported bare ("1.4.0"), prefixed ("netavark 1.4.0"),
	// or only via the package string. leadingVersion copes with all three, so
	// take the first field that yields a version.
	nb := info.Host.NetworkBackendInfo
	for _, candidate := range []string{nb.Version, nb.Package} {
		if _, _, ok := leadingVersion(candidate); ok {
			return strings.TrimSpace(candidate), ""
		}
	}

	// Report every field consulted. networkBackend is included because it is
	// known to populate, so if it parses while networkBackendInfo does not, the
	// JSON key names are what changed.
	return "", fmt.Sprintf(
		"no netavark version in podman info (networkBackend=%q backend=%q version=%q package=%q)",
		info.Host.NetworkBackend, nb.Backend, nb.Version, nb.Package)
}

// incompatibleStack holds the version rule on its own so it is testable without
// a live Podman. Keeping it pure matters here: CI runners draw from a mixed
// image fleet, so the skip path above cannot be relied on to execute on any
// given run, and the rule would otherwise ship unverified.
func incompatibleStack(podmanVersion, netavarkVersion string) bool {
	pMajor, _, ok := leadingVersion(podmanVersion)
	if !ok || pMajor < 5 {
		return false
	}
	nMajor, nMinor, ok := leadingVersion(netavarkVersion)
	if !ok {
		return false
	}
	return nMajor == 1 && nMinor < 10
}

// TestPodmanRootless_MultiContainerNetworking is the graduation gate for stable Podman
// support. It verifies that two containers on a shared named bridge network can resolve
// each other by DNS alias — the mechanism used for inter-container communication in
// gridctl stacks running under rootless Podman with netavark + aardvark-dns.
func TestPodmanRootless_MultiContainerNetworking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}
	if info.Type != runtime.RuntimePodman {
		t.Skip("skipping: requires Podman runtime (Docker detected)")
	}
	t.Logf("Podman version=%s rootless=%v socket=%s", info.Version, info.IsRootless(), info.SocketPath)

	// Podman 5.x drives a netavark interface that 1.4.x does not implement, and
	// the pairing fails as silent DNS non-resolution rather than a startup
	// error. Without this check the suite reports a gridctl networking bug when
	// the real fault is the host's container stack (#1092).
	//
	// Fails open on purpose: an undetermined netavark version runs the test, so
	// a genuine regression is never masked. The diagnostic is always logged
	// because a guard that declines silently is indistinguishable from a guard
	// that found nothing wrong.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer probeCancel()
	netavark, diag := netavarkVersion(probeCtx)
	switch {
	case diag != "":
		t.Logf("netavark version undetermined, running the test anyway: %s", diag)
	case incompatibleStack(info.Version, netavark):
		t.Skipf("skipping: Podman %s needs a newer netavark than this host's %s; "+
			"rootless inter-container DNS cannot work on this stack, so the result "+
			"would say nothing about gridctl (see #1092)", info.Version, netavark)
	default:
		t.Logf("container stack: podman %s + netavark %s", info.Version, netavark)
	}

	rt, err := runtime.NewWithInfo(info)
	if err != nil {
		t.Fatalf("NewWithInfo() error: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const stackName = "integration-mc-net"
	const netName = stackName + "-net"

	// Ensure clean state before and after.
	_ = rt.Down(ctx, stackName)
	defer func() { _ = rt.Down(ctx, stackName) }()

	// Create a named bridge network. In Podman 4.0+ this uses netavark,
	// which enables inter-container DNS via aardvark-dns.
	if err := rt.Runtime().EnsureNetwork(ctx, netName, runtime.NetworkOptions{
		Driver: "bridge",
		Stack:  stackName,
	}); err != nil {
		t.Fatalf("EnsureNetwork() error: %v", err)
	}

	if err := rt.Runtime().EnsureImage(ctx, "alpine:latest"); err != nil {
		t.Fatalf("EnsureImage() error: %v", err)
	}

	managedLabels := func(name string) map[string]string {
		return map[string]string{
			"gridctl.managed":    "true",
			"gridctl.stack":      stackName,
			"gridctl.mcp-server": name,
		}
	}

	// Start container-b — the DNS lookup target.
	// Its DNS alias on the network will be "container-b" (WorkloadConfig.Name).
	statusB, err := rt.Runtime().Start(ctx, runtime.WorkloadConfig{
		Name:        "container-b",
		Stack:       stackName,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "alpine:latest",
		Command:     []string{"sh", "-c", "sleep 30"},
		NetworkName: netName,
		Labels:      managedLabels("container-b"),
	})
	if err != nil {
		t.Fatalf("Start(container-b) error: %v", err)
	}
	t.Logf("container-b started: id=%s state=%s", statusB.ID, statusB.State)

	// Allow container-b to register with the network's DNS before container-a queries it.
	time.Sleep(2 * time.Second)

	// Start container-a — runs nslookup and exits.
	// BusyBox nslookup (included in alpine:latest) resolves via the container network's DNS.
	statusA, err := rt.Runtime().Start(ctx, runtime.WorkloadConfig{
		Name:        "container-a",
		Stack:       stackName,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "alpine:latest",
		Command:     []string{"sh", "-c", "nslookup container-b"},
		NetworkName: netName,
		Labels:      managedLabels("container-a"),
	})
	if err != nil {
		t.Fatalf("Start(container-a) error: %v", err)
	}
	t.Logf("container-a started: id=%s state=%s", statusA.ID, statusA.State)

	// Poll until container-a completes (nslookup exits when it gets a response or times out).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err := rt.Runtime().Status(ctx, statusA.ID)
		if err != nil {
			t.Fatalf("Status(container-a): %v", err)
		}
		if st.State == runtime.WorkloadStateStopped || st.State == runtime.WorkloadStateFailed {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Verify exit code 0: nslookup exits 0 only on successful resolution.
	dockerCli := rt.DockerClient()
	if dockerCli == nil {
		t.Fatal("DockerClient() returned nil — cannot verify exit code")
	}
	result, err := dockerCli.ContainerInspect(ctx, string(statusA.ID))
	if err != nil {
		t.Fatalf("ContainerInspect(container-a): %v", err)
	}
	t.Logf("container-a exit_code=%d status=%s", result.State.ExitCode, result.State.Status)

	if result.State.ExitCode != 0 {
		t.Errorf("inter-container DNS resolution failed: container-a exited %d (expected 0)\n"+
			"container-a could not resolve 'container-b' by DNS alias on network %q\n"+
			"ensure netavark and aardvark-dns are installed for rootless Podman networking",
			result.State.ExitCode, netName)
	}
}

// TestRuntimeDetection_AutoDetect verifies auto-detection finds the available runtime.
func TestRuntimeDetection_AutoDetect(t *testing.T) {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}

	if info.Type != runtime.RuntimeDocker && info.Type != runtime.RuntimePodman {
		t.Errorf("unexpected runtime type: %s", info.Type)
	}
	if info.SocketPath == "" {
		t.Error("expected non-empty socket path")
	}
	if info.HostAlias == "" {
		t.Error("expected non-empty host alias")
	}
	t.Logf("Detected runtime: %s (socket: %s, version: %s, host: %s)", info.DisplayName(), info.SocketPath, info.Version, info.HostAlias)
}

// TestRuntimeDetection_ExplicitInvalid verifies explicit selection with invalid runtime errors.
func TestRuntimeDetection_ExplicitInvalid(t *testing.T) {
	_, err := runtime.DetectRuntime(runtime.DetectOptions{Explicit: "invalid"})
	if err == nil {
		t.Error("expected error for invalid runtime")
	}
}

// TestRuntimeDetection_EnvVar verifies GRIDCTL_RUNTIME env var selection.
func TestRuntimeDetection_EnvVar(t *testing.T) {
	// Detect what's available first
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}

	// Set env var to the detected runtime type
	origEnv := os.Getenv("GRIDCTL_RUNTIME")
	os.Setenv("GRIDCTL_RUNTIME", string(info.Type))
	defer os.Setenv("GRIDCTL_RUNTIME", origEnv)

	envInfo, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Fatalf("DetectRuntime with GRIDCTL_RUNTIME=%s failed: %v", info.Type, err)
	}
	if envInfo.Type != info.Type {
		t.Errorf("expected type %s via env var, got %s", info.Type, envInfo.Type)
	}
}

// TestNewWithInfo_CreateOrchestrator verifies NewWithInfo creates a working orchestrator.
func TestNewWithInfo_CreateOrchestrator(t *testing.T) {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("No container runtime available: %v", err)
	}

	orch, err := runtime.NewWithInfo(info)
	if err != nil {
		t.Fatalf("NewWithInfo() error: %v", err)
	}
	defer orch.Close()

	// Verify runtime info is stored
	if orch.RuntimeInfo() == nil {
		t.Error("expected RuntimeInfo to be set")
	}
	if orch.RuntimeInfo().Type != info.Type {
		t.Errorf("expected type %s, got %s", info.Type, orch.RuntimeInfo().Type)
	}

	// Verify the orchestrator can ping the runtime
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := orch.Runtime().Ping(ctx); err != nil {
		t.Errorf("Ping() failed: %v", err)
	}
}

// TestContainerCleanup_CreatedNeverStarted verifies that containers in "created"
// state (never started) are correctly cleaned up by Down().
func TestContainerCleanup_CreatedNeverStarted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Container runtime not available: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stackName := "integration-cleanup"

	// Ensure clean state
	_ = rt.Down(ctx, stackName)

	// Create network
	if err := rt.Runtime().EnsureNetwork(ctx, stackName+"-net", runtime.NetworkOptions{
		Driver: "bridge",
		Stack:  stackName,
	}); err != nil {
		t.Fatalf("EnsureNetwork() error: %v", err)
	}

	// Start a container that will exit immediately (simulating "created" state)
	cfg := runtime.WorkloadConfig{
		Name:        "cleanup-test",
		Stack:       stackName,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "alpine:latest",
		Command:     []string{"true"}, // exits immediately
		NetworkName: stackName + "-net",
		Labels: map[string]string{
			"gridctl.managed":    "true",
			"gridctl.stack":      stackName,
			"gridctl.mcp-server": "cleanup-test",
		},
	}

	// Ensure image is available
	if err := rt.Runtime().EnsureImage(ctx, "alpine:latest"); err != nil {
		t.Fatalf("EnsureImage() error: %v", err)
	}

	_, err = rt.Runtime().Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Brief pause to let container exit
	time.Sleep(2 * time.Second)

	// Verify container exists (in stopped/exited state)
	statuses, err := rt.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("expected at least 1 container in status before cleanup")
	}

	// Down() should clean up even exited/stopped containers
	if err := rt.Down(ctx, stackName); err != nil {
		t.Fatalf("Down() error: %v", err)
	}

	// Verify everything is cleaned up
	statuses, err = rt.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("Status() after Down() error: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected 0 containers after cleanup, got %d", len(statuses))
	}
}

func TestLeadingVersion(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"5.8.4", 5, 8, true},
		{"1.4.0", 1, 4, true},
		{"netavark 1.4.0", 1, 4, true},
		{"4.9.3+ds1-1ubuntu0.2", 4, 9, true},
		{"1.10.3", 1, 10, true},
		{"", 0, 0, false},
		{"unknown", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := leadingVersion(c.in)
		if ok != c.ok || major != c.major || minor != c.minor {
			t.Errorf("leadingVersion(%q) = (%d, %d, %v), want (%d, %d, %v)",
				c.in, major, minor, ok, c.major, c.minor, c.ok)
		}
	}
}

func TestIncompatibleStack(t *testing.T) {
	cases := []struct {
		name     string
		podman   string
		netavark string
		want     bool
	}{
		// The pairing observed on ubuntu24/20260810.271, which fails with
		// silent DNS non-resolution (#1092).
		{"podman 5 with archive netavark", "5.8.4", "1.4.0", true},
		// The pairing on ubuntu24/20260720.247, which works.
		{"podman 4 with archive netavark", "4.9.3", "1.4.0", false},
		// Podman 5 with a netavark new enough to drive it.
		{"podman 5 with modern netavark", "5.8.4", "1.10.3", false},
		{"podman 5 with much newer netavark", "5.8.4", "2.0.0", false},
		// Unparseable input must not skip — running and failing is safer than
		// masking a real regression.
		{"unknown podman", "", "1.4.0", false},
		{"unknown netavark", "5.8.4", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := incompatibleStack(c.podman, c.netavark); got != c.want {
				t.Errorf("incompatibleStack(%q, %q) = %v, want %v", c.podman, c.netavark, got, c.want)
			}
		})
	}
}

func TestParseNetavarkVersion(t *testing.T) {
	// Shape taken from `podman info --format json`, trimmed to the fields the
	// guard reads.
	const realistic = `{
	  "host": {
	    "networkBackend": "netavark",
	    "networkBackendInfo": {
	      "backend": "netavark",
	      "version": "netavark 1.4.0",
	      "package": "netavark-1.4.0-4",
	      "path": "/usr/libexec/podman/netavark"
	    }
	  }
	}`

	t.Run("reads the version field", func(t *testing.T) {
		got, diag := parseNetavarkVersion([]byte(realistic))
		if diag != "" {
			t.Fatalf("unexpected diagnostic: %s", diag)
		}
		if got != "netavark 1.4.0" {
			t.Errorf("got %q, want %q", got, "netavark 1.4.0")
		}
		if !incompatibleStack("5.8.4", got) {
			t.Error("podman 5.8.4 with netavark 1.4.0 must be reported incompatible")
		}
	})

	t.Run("falls back to the package field", func(t *testing.T) {
		const versionless = `{"host":{"networkBackend":"netavark","networkBackendInfo":{"backend":"netavark","version":"","package":"netavark-1.4.0-4"}}}`
		got, diag := parseNetavarkVersion([]byte(versionless))
		if diag != "" {
			t.Fatalf("unexpected diagnostic: %s", diag)
		}
		if got != "netavark-1.4.0-4" {
			t.Errorf("got %q, want the package string", got)
		}
	})

	t.Run("bare version", func(t *testing.T) {
		const bare = `{"host":{"networkBackendInfo":{"version":"1.10.3"}}}`
		got, diag := parseNetavarkVersion([]byte(bare))
		if diag != "" {
			t.Fatalf("unexpected diagnostic: %s", diag)
		}
		if incompatibleStack("5.8.4", got) {
			t.Error("netavark 1.10.3 is new enough for podman 5.x")
		}
	})

	// This is the case that silently broke the previous guard: the fields it
	// wanted were absent. The diagnostic must name what was actually seen so
	// the next failure is fixable from the log alone.
	t.Run("diagnostic names the fields consulted", func(t *testing.T) {
		const renamed = `{"host":{"networkBackend":"netavark","networkBackendInfo":{"backend":"netavark"}}}`
		got, diag := parseNetavarkVersion([]byte(renamed))
		if got != "" {
			t.Errorf("got %q, want empty when no version is present", got)
		}
		if diag == "" {
			t.Fatal("a missing version must produce a diagnostic, not silence")
		}
		for _, want := range []string{"networkBackend", "version", "package"} {
			if !strings.Contains(diag, want) {
				t.Errorf("diagnostic %q does not mention %q", diag, want)
			}
		}
	})

	t.Run("unparseable json", func(t *testing.T) {
		_, diag := parseNetavarkVersion([]byte("not json"))
		if diag == "" {
			t.Error("unparseable JSON must produce a diagnostic")
		}
	})
}
