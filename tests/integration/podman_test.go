//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/gridctl/gridctl/pkg/dockerclient"
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

// networkStack is the pair of helper binaries Podman drives for rootless
// container networking, as Podman itself reports them. Each version carries its
// own diagnostic explaining why it could not be determined, empty on success.
//
// Callers MUST surface those diagnostics. An earlier version of this guard asked
// for a single Go-template field and swallowed any error, so when the field did
// not resolve it silently reported "cannot determine" and the guard never fired
// — indistinguishable from a healthy host. Parsing the whole document and
// reporting what was actually seen makes that failure mode visible instead.
type networkStack struct {
	Netavark     string
	NetavarkDiag string
	Aardvark     string
	AardvarkDiag string
}

// readNetworkStack returns the netavark and aardvark-dns versions Podman reports.
func readNetworkStack(ctx context.Context) networkStack {
	out, err := exec.CommandContext(ctx, "podman", "info", "--format", "json").Output()
	if err != nil {
		diag := "podman info failed: " + err.Error()
		return networkStack{NetavarkDiag: diag, AardvarkDiag: diag}
	}
	return parseNetworkStack(out)
}

// parseNetworkStack extracts both helper versions from `podman info --format
// json` output. Split from the exec so the JSON handling is testable without a
// live Podman — the previous guard shipped its field lookup unverified and that
// lookup is precisely what failed.
func parseNetworkStack(out []byte) networkStack {
	var info struct {
		Host struct {
			NetworkBackend     string `json:"networkBackend"`
			NetworkBackendInfo struct {
				Backend string `json:"backend"`
				Version string `json:"version"`
				Package string `json:"package"`
				DNS     struct {
					Version string `json:"version"`
					Package string `json:"package"`
				} `json:"dns"`
			} `json:"networkBackendInfo"`
		} `json:"host"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		diag := "podman info returned unparseable JSON: " + err.Error()
		return networkStack{NetavarkDiag: diag, AardvarkDiag: diag}
	}

	nb := info.Host.NetworkBackendInfo
	stack := networkStack{}

	// A version may be reported bare ("1.4.0"), prefixed ("netavark 1.4.0"), or
	// only via the package string. leadingVersion copes with all three, so take
	// the first field that yields a version.
	//
	// Every field consulted is named in the diagnostic. networkBackend is
	// included because it is known to populate, so if it parses while
	// networkBackendInfo does not, the JSON key names are what changed.
	stack.Netavark, stack.NetavarkDiag = firstVersion(
		fmt.Sprintf("no netavark version in podman info (networkBackend=%q backend=%q version=%q package=%q)",
			info.Host.NetworkBackend, nb.Backend, nb.Version, nb.Package),
		nb.Version, nb.Package)

	stack.Aardvark, stack.AardvarkDiag = firstVersion(
		fmt.Sprintf("no aardvark-dns version in podman info (networkBackendInfo.dns version=%q package=%q)",
			nb.DNS.Version, nb.DNS.Package),
		nb.DNS.Version, nb.DNS.Package)

	return stack
}

// firstVersion returns the first candidate containing a parseable version, or
// the supplied diagnostic when none does.
func firstVersion(diag string, candidates ...string) (string, string) {
	for _, candidate := range candidates {
		if _, _, ok := leadingVersion(candidate); ok {
			return strings.TrimSpace(candidate), ""
		}
	}
	return "", diag
}

// incompatibleStack reports whether Podman is too new for the netavark driving
// it. Held apart from the test so it is testable without a live Podman: CI
// runners draw from a mixed image fleet, so the skip path cannot be relied on to
// execute on any given run, and the rule would otherwise ship unverified.
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

// mismatchedHelpers reports whether netavark and aardvark-dns come from
// different release lines. netavark spawns aardvark-dns and the two share a
// protocol that is not stable across minors, so a mismatched pair resolves
// nothing while reporting no error at all.
//
// This is the case that got past the guard on ubuntu24/20260810.271: Podman
// 5.8.4 with netavark 1.17.2 is a perfectly good pairing and incompatibleStack
// correctly cleared it, but aardvark-dns was still the archive's 1.4.0, so DNS
// failed anyway and the suite blamed gridctl (#1092). Comparing Podman against
// netavark alone can never catch that — the broken relationship is between the
// two helpers.
func mismatchedHelpers(netavarkVersion, aardvarkVersion string) bool {
	nMajor, nMinor, ok := leadingVersion(netavarkVersion)
	if !ok {
		return false
	}
	aMajor, aMinor, ok := leadingVersion(aardvarkVersion)
	if !ok {
		return false
	}
	return nMajor != aMajor || nMinor != aMinor
}

// checkNetworkStack reports why the host cannot support rootless inter-container
// DNS, or an empty string when the test should run, alongside diagnostics to log
// either way.
//
// It fails open on purpose: an undetermined version runs the test, so a genuine
// gridctl regression is never masked by a probe that came back blank. Skipping
// is reserved for stacks that are provably incapable of the thing under test,
// where a failure would say nothing about gridctl. In CI this cannot hide drift
// either — the workflow pins both helpers and asserts the resolved versions
// before any test runs, so a mismatch fails the job outright rather than
// reaching this skip.
func checkNetworkStack(podmanVersion string, s networkStack) (skip string, diags []string) {
	switch {
	case s.NetavarkDiag != "":
		diags = append(diags, "netavark version undetermined, running the test anyway: "+s.NetavarkDiag)
	case incompatibleStack(podmanVersion, s.Netavark):
		return fmt.Sprintf("Podman %s needs a newer netavark than this host's %s; rootless "+
			"inter-container DNS cannot work on this stack, so the result would say "+
			"nothing about gridctl (see #1092)", podmanVersion, s.Netavark), diags
	}

	switch {
	case s.AardvarkDiag != "":
		diags = append(diags, "aardvark-dns version undetermined, running the test anyway: "+s.AardvarkDiag)
	case mismatchedHelpers(s.Netavark, s.Aardvark):
		return fmt.Sprintf("netavark %s and aardvark-dns %s are from different release "+
			"lines; the pair resolves no names and reports no error, so the result "+
			"would say nothing about gridctl (see #1092)", s.Netavark, s.Aardvark), diags
	}

	diags = append(diags, fmt.Sprintf("container stack: podman %s + netavark %s + aardvark-dns %s",
		podmanVersion, s.Netavark, s.Aardvark))
	return "", diags
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

	// A mismatched host network stack fails as silent DNS non-resolution rather
	// than a startup error, so without this check the suite reports a gridctl
	// networking bug when the real fault is the host's container stack (#1092).
	// The diagnostics are always logged because a guard that declines silently
	// is indistinguishable from a guard that found nothing wrong.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer probeCancel()
	skip, diags := checkNetworkStack(info.Version, readNetworkStack(probeCtx))
	for _, d := range diags {
		t.Log(d)
	}
	if skip != "" {
		t.Skip("skipping: " + skip)
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

	// Start container-a — resolves container-b and exits.
	//
	// The exit status is BusyBox nslookup's, preserved so the assertion below is
	// unchanged, but the command also records what the resolver actually did.
	// An exit code on its own cannot tell "the name did not resolve" apart from
	// "it resolved and the client objected to something else", and this test
	// previously reported the former without ever establishing it. getent is
	// included as a second opinion because it goes through the standard
	// resolver and, unlike nslookup, does not care about the server's answer to
	// queries the test is not asking about.
	const lookupCmd = `nslookup container-b; rc=$?; echo "nslookup_exit=$rc"; ` +
		`getent hosts container-b && echo "getent=ok" || echo "getent=fail"; ` +
		`echo "--- resolv.conf ---"; cat /etc/resolv.conf; exit $rc`

	statusA, err := rt.Runtime().Start(ctx, runtime.WorkloadConfig{
		Name:        "container-a",
		Stack:       stackName,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "alpine:latest",
		Command:     []string{"sh", "-c", lookupCmd},
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

	// Always logged, not just on failure: a passing run that took an unexpected
	// route through the resolver is worth seeing too.
	t.Logf("container-a output:\n%s", containerOutput(ctx, dockerCli, string(statusA.ID)))

	if result.State.ExitCode != 0 {
		t.Errorf("inter-container DNS lookup of 'container-b' on network %q failed: "+
			"container-a exited %d (expected 0). The output logged above shows what the "+
			"resolver returned; check it before assuming the name did not resolve",
			netName, result.State.ExitCode)
	}
}

// containerOutput returns a container's combined stdout and stderr, or a
// bracketed reason it could not be read. Diagnosing a DNS failure needs what the
// lookup printed, so this never returns an error the caller might drop — a
// missing diagnostic is worse than an ugly one.
func containerOutput(ctx context.Context, cli dockerclient.DockerClient, id string) string {
	rc, err := cli.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "<logs unavailable: " + err.Error() + ">"
	}
	defer func() { _ = rc.Close() }()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return "<logs unreadable: " + err.Error() + ">"
	}

	// Log streams are multiplexed for containers started without a TTY and raw
	// otherwise. Demultiplex, falling back to the bytes as read: which shape
	// arrives depends on how the workload was started, and guessing wrong would
	// discard the very output this exists to capture.
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, bytes.NewReader(raw)); err != nil || buf.Len() == 0 {
		return strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(buf.String())
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

func TestMismatchedHelpers(t *testing.T) {
	cases := []struct {
		name               string
		netavark, aardvark string
		want               bool
	}{
		// The pairing observed on ubuntu24/20260810.271: Podman's own netavark
		// against the Ubuntu archive's aardvark-dns. Cleared by
		// incompatibleStack, broken in practice.
		{"podman netavark with archive aardvark", "netavark 1.17.2", "aardvark-dns 1.4.0", true},
		// The pinned pair this job now provisions. Patch levels differ within
		// the minor, which is normal: upstream cuts the two projects together
		// but does not always need the same patch on both.
		{"pinned pair", "netavark 1.17.2", "aardvark-dns 1.17.1", false},
		{"identical versions", "1.4.0", "1.4.0", false},
		{"same minor across majors differs", "2.1.0", "1.1.0", true},
		{"minor drift within a major", "1.17.2", "1.10.3", true},
		// Unparseable input must not skip — running and failing is safer than
		// masking a real regression.
		{"unknown netavark", "", "aardvark-dns 1.4.0", false},
		{"unknown aardvark", "netavark 1.17.2", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mismatchedHelpers(c.netavark, c.aardvark); got != c.want {
				t.Errorf("mismatchedHelpers(%q, %q) = %v, want %v",
					c.netavark, c.aardvark, got, c.want)
			}
		})
	}
}

func TestCheckNetworkStack(t *testing.T) {
	healthy := networkStack{Netavark: "netavark 1.17.2", Aardvark: "aardvark-dns 1.17.1"}

	t.Run("pinned stack runs and logs what it saw", func(t *testing.T) {
		skip, diags := checkNetworkStack("5.8.4", healthy)
		if skip != "" {
			t.Fatalf("healthy stack must not skip, got %q", skip)
		}
		if len(diags) != 1 || !strings.Contains(diags[0], "aardvark-dns 1.17.1") {
			t.Errorf("diags = %q, want one line naming both helpers", diags)
		}
	})

	// The regression this guard exists for. Podman and netavark agree, so the
	// old podman-versus-netavark rule cleared it and the suite failed inside
	// the DNS assertion instead.
	t.Run("skips the pairing that got past the old guard", func(t *testing.T) {
		skip, _ := checkNetworkStack("5.8.4", networkStack{
			Netavark: "netavark 1.17.2",
			Aardvark: "aardvark-dns 1.4.0",
		})
		if skip == "" {
			t.Fatal("netavark 1.17.2 with aardvark-dns 1.4.0 must skip")
		}
		if !strings.Contains(skip, "different release lines") {
			t.Errorf("skip reason %q should name the mismatch", skip)
		}
	})

	t.Run("skips podman 5 on archive netavark", func(t *testing.T) {
		skip, _ := checkNetworkStack("5.8.4", networkStack{
			Netavark: "netavark 1.4.0",
			Aardvark: "aardvark-dns 1.4.0",
		})
		if !strings.Contains(skip, "needs a newer netavark") {
			t.Errorf("skip reason %q should name the podman/netavark gap", skip)
		}
	})

	// Failing open matters more than skipping cleanly: a blank probe must never
	// be able to hide a real gridctl networking regression.
	t.Run("undetermined versions run the test", func(t *testing.T) {
		for _, s := range []networkStack{
			{NetavarkDiag: "probe failed", Aardvark: "aardvark-dns 1.17.1"},
			{Netavark: "netavark 1.17.2", AardvarkDiag: "probe failed"},
		} {
			skip, diags := checkNetworkStack("5.8.4", s)
			if skip != "" {
				t.Errorf("undetermined version must not skip, got %q", skip)
			}
			if len(diags) == 0 {
				t.Error("an undetermined version must be logged, not swallowed")
			}
		}
	})
}

func TestParseNetworkStack(t *testing.T) {
	// Shape taken from `podman info --format json`, trimmed to the fields the
	// guard reads.
	const realistic = `{
	  "host": {
	    "networkBackend": "netavark",
	    "networkBackendInfo": {
	      "backend": "netavark",
	      "version": "netavark 1.4.0",
	      "package": "netavark-1.4.0-4",
	      "path": "/usr/libexec/podman/netavark",
	      "dns": {
	        "version": "aardvark-dns 1.4.0",
	        "package": "aardvark-dns-1.4.0-5",
	        "path": "/usr/libexec/podman/aardvark-dns"
	      }
	    }
	  }
	}`

	t.Run("reads both version fields", func(t *testing.T) {
		got := parseNetworkStack([]byte(realistic))
		if got.NetavarkDiag != "" || got.AardvarkDiag != "" {
			t.Fatalf("unexpected diagnostics: %q / %q", got.NetavarkDiag, got.AardvarkDiag)
		}
		if got.Netavark != "netavark 1.4.0" {
			t.Errorf("netavark = %q, want %q", got.Netavark, "netavark 1.4.0")
		}
		if got.Aardvark != "aardvark-dns 1.4.0" {
			t.Errorf("aardvark = %q, want %q", got.Aardvark, "aardvark-dns 1.4.0")
		}
		if !incompatibleStack("5.8.4", got.Netavark) {
			t.Error("podman 5.8.4 with netavark 1.4.0 must be reported incompatible")
		}
	})

	t.Run("falls back to the package fields", func(t *testing.T) {
		const versionless = `{"host":{"networkBackend":"netavark","networkBackendInfo":{"backend":"netavark","version":"","package":"netavark-1.4.0-4","dns":{"version":"","package":"aardvark-dns-1.4.0-5"}}}}`
		got := parseNetworkStack([]byte(versionless))
		if got.NetavarkDiag != "" || got.AardvarkDiag != "" {
			t.Fatalf("unexpected diagnostics: %q / %q", got.NetavarkDiag, got.AardvarkDiag)
		}
		if got.Netavark != "netavark-1.4.0-4" || got.Aardvark != "aardvark-dns-1.4.0-5" {
			t.Errorf("got %q / %q, want the package strings", got.Netavark, got.Aardvark)
		}
	})

	t.Run("bare versions", func(t *testing.T) {
		const bare = `{"host":{"networkBackendInfo":{"version":"1.17.2","dns":{"version":"1.17.1"}}}}`
		got := parseNetworkStack([]byte(bare))
		if got.NetavarkDiag != "" || got.AardvarkDiag != "" {
			t.Fatalf("unexpected diagnostics: %q / %q", got.NetavarkDiag, got.AardvarkDiag)
		}
		if incompatibleStack("5.8.4", got.Netavark) {
			t.Error("netavark 1.17.2 is new enough for podman 5.x")
		}
		if mismatchedHelpers(got.Netavark, got.Aardvark) {
			t.Error("1.17.2 and 1.17.1 are the same release line")
		}
	})

	// This is the case that silently broke the previous guard: the fields it
	// wanted were absent. The diagnostic must name what was actually seen so
	// the next failure is fixable from the log alone.
	t.Run("diagnostics name the fields consulted", func(t *testing.T) {
		const renamed = `{"host":{"networkBackend":"netavark","networkBackendInfo":{"backend":"netavark"}}}`
		got := parseNetworkStack([]byte(renamed))
		if got.Netavark != "" || got.Aardvark != "" {
			t.Errorf("got %q / %q, want empty when no version is present", got.Netavark, got.Aardvark)
		}
		if got.NetavarkDiag == "" || got.AardvarkDiag == "" {
			t.Fatal("missing versions must produce diagnostics, not silence")
		}
		for _, want := range []string{"networkBackend", "version", "package"} {
			if !strings.Contains(got.NetavarkDiag, want) {
				t.Errorf("netavark diagnostic %q does not mention %q", got.NetavarkDiag, want)
			}
		}
		// The dns subtree is the field lookup this change adds, and it is the
		// one that cannot be verified against a live Podman from a workstation.
		// If Podman ever moves or renames it, the diagnostic has to say so.
		if !strings.Contains(got.AardvarkDiag, "networkBackendInfo.dns") {
			t.Errorf("aardvark diagnostic %q does not name the dns subtree", got.AardvarkDiag)
		}
	})

	t.Run("unparseable json", func(t *testing.T) {
		got := parseNetworkStack([]byte("not json"))
		if got.NetavarkDiag == "" || got.AardvarkDiag == "" {
			t.Error("unparseable JSON must produce diagnostics")
		}
	})
}
