//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// conformanceVersion pins the suite so the checked-in expected-failures
// baselines describe a fixed scenario set; bump both together. Pinned to
// an exact version (never the floating `alpha` dist-tag).
//
// 0.2.0-alpha.12, published 2026-10-01. Bumped from 0.2.0-alpha.10 for
// frozen requirement sets (--requirements), per-check baseline entries,
// the wire-schema-valid synthetic check, and tools-list-deterministic-order.
const conformanceVersion = "@modelcontextprotocol/conformance@0.2.0-alpha.12"

// Measured locally on 2026-10-19 with a warm npx cache, then given margin
// for a cold fetch and a slow runner. Replace these constants, the
// Taskfile timeout, and the Gatekeeper timeout-minutes together.
//
//	requirements run: 1-4s observed, deadline 3m
//	pending scenario: about 1s observed, deadline 45s
//	daemon startup:   under 5s observed, budget 30s each
//	build:            a few seconds observed, budget 2m
//	go test -timeout: 22m (five 3m deadlines + three 45s deadlines +
//	                  two 30s startups + 2m build, rounded up)
//	job timeout:      28m (above -timeout)
const (
	conformanceRequirementsTimeout = 3 * time.Minute
	conformancePendingTimeout      = 45 * time.Second
)

// conformancePendingScenarios stay not_scored on 2026-07-28 (reason
// pending). A requirements run executes them but cannot fail on them, so
// they also run one at a time under --scenario, where the baseline is strict.
//
// Stale detection does not apply to a not_scored scenario in a
// --requirements run. The server CLI filters the baseline to scored
// scenarios before evaluateBaseline (src/index.ts, "not_scored scenarios
// are outside both the pass rate and the baseline"). A baselined
// not_scored pass is therefore not reported stale, and a baselined
// not_scored failure is not an unexpected failure. Confirmed against
// alpha.12 source and by a requirements run whose baseline named
// server-session-lifecycle (not_scored, added-after-release): the entry
// was absent from both the stale and unexpected lists. Keep not_scored
// entries out of the requirements baselines and only in the per-scenario
// loop below.
var conformancePendingScenarios = []string{
	"json-schema-2020-12",
	"http-header-validation",
	"http-custom-header-server-validation",
}

// fleetAggregationScenarios must pass on every fleet surface, including
// the tools-list-deterministic-order check inside tools-list. A wholesale
// baseline entry for any of these is a coverage hole.
var fleetAggregationScenarios = []string{
	"tools-list",
	"caching",
	"prompts-list",
	"resources-list",
	"tools-list-deterministic-order",
}

// TestConformance runs the official MCP conformance suite against a live
// gridctl daemon. Stackless runs score each generation's frozen
// requirement set. Fleet runs add two mock servers, two active registry
// skills, and a group endpoint so aggregation is under the oracle. Each
// suite invocation has its own deadline. A daemon context is never passed
// to npx.
func TestConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Skip("npx not available; conformance suite requires Node 22+")
	}
	if major := nodeMajorVersion(t); major > 0 && major < 22 {
		t.Skipf("conformance suite requires Node 22+, found major version %d", major)
	}

	// Build before redirecting HOME so `go build` resolves its real
	// module cache rather than poisoning the test's tempdir with
	// read-only mod-cache files that t.TempDir cleanup can't remove.
	bin := buildGridctlBinary(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(gridctlHomeEnv, home)

	stacklessPort := freePort(t)
	stackless := startConformanceDaemon(t, bin, home, stacklessPort, "serve", "--foreground", "--port", strconv.Itoa(stacklessPort))
	runSuite := func(t *testing.T, label, url, baseline, requirements, specVersion string, timeout time.Duration, extra ...string) {
		t.Helper()
		runConformanceSuite(t, npx, label, url, baseline, requirements, specVersion, timeout, extra...)
	}

	// Nested t.Run calls: the verifier treats each slash as an ancestor
	// that must itself have run. A single t.Run("stackless/2025-11-25")
	// does not emit that ancestor.
	t.Run("stackless", func(t *testing.T) {
		t.Run("2025-11-25", func(t *testing.T) {
			runSuite(t, "stackless/2025-11-25", stackless.url, absBaseline(t, "conformance-baseline.yml"), "2025-11-25", "", conformanceRequirementsTimeout)
		})
		t.Run("2026-07-28", func(t *testing.T) {
			runSuite(t, "stackless/2026-07-28", stackless.url, absBaseline(t, "conformance-baseline-2026-07-28.yml"), "2026-07-28", "", conformanceRequirementsTimeout)
		})
		t.Run("2026-07-28-pending", func(t *testing.T) {
			baseline := absBaseline(t, "conformance-baseline-2026-07-28-pending.yml")
			for _, scenario := range conformancePendingScenarios {
				t.Run(scenario, func(t *testing.T) {
					runSuite(t, "pending/"+scenario, stackless.url, baseline, "", "2026-07-28", conformancePendingTimeout, "--scenario", scenario)
				})
			}
		})
	})

	// Started on the parent so one daemon serves every fleet subtest and
	// t.Cleanup runs after the last of them. A focused -run that skips
	// these subtests still pays the startup; that is cheaper than sharing
	// a subtest context with the process.
	fleet := startFleetDaemon(t, bin)
	t.Run("fleet", func(t *testing.T) {
		t.Run("2025-11-25", func(t *testing.T) {
			runSuite(t, "fleet/2025-11-25", fleet.mcpURL, absBaseline(t, "conformance-baseline-fleet.yml"), "2025-11-25", "", conformanceRequirementsTimeout)
		})
		t.Run("2026-07-28", func(t *testing.T) {
			runSuite(t, "fleet/2026-07-28", fleet.mcpURL, absBaseline(t, "conformance-baseline-fleet-2026-07-28.yml"), "2026-07-28", "", conformanceRequirementsTimeout)
		})
		t.Run("2026-07-28-group", func(t *testing.T) {
			runSuite(t, "fleet/2026-07-28-group", fleet.groupURL, absBaseline(t, "conformance-baseline-fleet-group-2026-07-28.yml"), "2026-07-28", "", conformanceRequirementsTimeout)
		})
	})
}

const gridctlHomeEnv = "GRIDCTL_HOME"

type conformanceDaemon struct {
	url      string
	mcpURL   string
	groupURL string
}

func absBaseline(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	rejectFleetWholesale(t, name, path)
	return path
}

func rejectFleetWholesale(t *testing.T, name, path string) {
	t.Helper()
	if !strings.Contains(name, "fleet") {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{}
	for _, scenario := range fleetAggregationScenarios {
		forbidden[scenario] = true
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		entry := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		if i := strings.Index(entry, " #"); i >= 0 {
			entry = strings.TrimSpace(entry[:i])
		}
		if forbidden[entry] {
			t.Fatalf("%s wholesale-baselines %q; fleet aggregation scenarios must pass", name, entry)
		}
	}
}

func runConformanceSuite(t *testing.T, npx, label, url, baseline, requirements, specVersion string, timeout time.Duration, extra ...string) {
	t.Helper()
	args := []string{"--yes", conformanceVersion, "server", "--url", url, "--expected-failures", baseline}
	if requirements != "" {
		// --requirements replaces --suite, --spec-version, and --scenario.
		args = append(args, "--requirements", requirements)
	} else {
		args = append(args, "--spec-version", specVersion)
		args = append(args, extra...)
	}
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	args = append(args, "--output-dir", outDir)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	started := time.Now()
	cmd := exec.CommandContext(ctx, npx, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(started)
	t.Logf("conformance %s finished in %s (deadline %s)\n%s", label, elapsed.Round(time.Second), timeout, out)
	logFailingChecks(t, outDir)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("conformance %s exceeded %s", label, timeout)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// npx itself failed (offline, registry unreachable):
			// environment, not conformance.
			t.Skipf("conformance suite could not run: %v", err)
		}
		t.Fatalf("conformance %s failed (see %s): %v", label, baseline, err)
	}
}

func logFailingChecks(t *testing.T, outDir string) {
	t.Helper()
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(outDir, entry.Name(), "checks.json"))
		if err != nil {
			continue
		}
		var checks []struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			ErrorMessage string `json:"errorMessage"`
		}
		if err := json.Unmarshal(raw, &checks); err != nil {
			continue
		}
		scenario := scenarioFromResultDir(entry.Name())
		for _, check := range checks {
			if check.Status != "FAILURE" && check.Status != "WARNING" {
				continue
			}
			t.Logf("failing check %s:%s %s %s", scenario, check.ID, check.Status, check.ErrorMessage)
		}
	}
}

func scenarioFromResultDir(name string) string {
	// server-<scenario>-<RFC3339 timestamp>. Scenario names can contain
	// hyphens and years, so match the timestamp prefix the suite writes.
	rest := strings.TrimPrefix(name, "server-")
	if i := strings.Index(rest, "-2026-"); i > 0 {
		return rest[:i]
	}
	return rest
}

func startConformanceDaemon(t *testing.T, bin, home string, port int, args ...string) conformanceDaemon {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, bin, args...)
	logPath := filepath.Join(home, "daemon.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logFile.Close() })
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = daemonEnv(home)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", bin, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer waitCancel()
	waitForPort(t, waitCtx, port)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	return conformanceDaemon{url: base + "/mcp", mcpURL: base + "/mcp", groupURL: base + "/groups/dev/mcp"}
}

func daemonEnv(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		gridctlHomeEnv + "=" + home,
		"NO_COLOR=1",
	}
}

func startFleetDaemon(t *testing.T, bin string) conformanceDaemon {
	t.Helper()
	if mockHTTPServerBin == "" || mockStdioBin == "" {
		t.Fatal("mock server binaries were not built")
	}
	backend := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", strconv.Itoa(backend))
	ready, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	waitForPort(t, ready, backend)

	home := t.TempDir()
	seedActiveSkills(t, home)
	port := freePort(t)
	stackPath := filepath.Join(home, "stack.yaml")
	stack := fmt.Sprintf("version: \"1\"\nname: conformance-fleet\nmcp-servers:\n  - name: alpha\n    url: http://127.0.0.1:%d/mcp\n    transport: http\n  - name: zeta\n    command: [%q]\n    transport: stdio\ngroups:\n  dev:\n    description: Alpha only\n    servers: [alpha]\n", backend, mockStdioBin)
	if err := os.WriteFile(stackPath, []byte(stack), 0o600); err != nil {
		t.Fatal(err)
	}
	d := startConformanceDaemon(t, bin, home, port, "apply", stackPath, "--foreground", "--port", strconv.Itoa(port))
	waitFleetReady(t, port, home)
	return d
}

func seedActiveSkills(t *testing.T, home string) {
	t.Helper()
	for _, name := range []string{"brand-guidelines", "writing-notes"} {
		src := filepath.Join("testdata", "anthropic_skills", name, "SKILL.md")
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, "\nstate:") {
			if !strings.HasPrefix(text, "---\n") {
				t.Fatalf("%s: missing frontmatter", name)
			}
			text = "---\nstate: active\n" + text[len("---\n"):]
		}
		dstDir := filepath.Join(home, ".gridctl", "registry", "skills", name)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dstDir, "SKILL.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func waitFleetReady(t *testing.T, port int, home string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		servers, serr := getJSON(fmt.Sprintf("http://127.0.0.1:%d/api/mcp-servers", port))
		skills, kerr := getJSON(fmt.Sprintf("http://127.0.0.1:%d/api/registry/skills", port))
		last = fmt.Sprintf("servers=%s err=%v skills=%s err=%v", servers, serr, skills, kerr)
		if serr == nil && kerr == nil && fleetConnected(servers) && twoActiveSkills(skills) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	logPath := filepath.Join(home, "daemon.log")
	logs, _ := os.ReadFile(logPath)
	t.Fatalf("fleet not ready: %s\n%s", last, logs)
}

func fleetConnected(raw []byte) bool {
	var servers []struct {
		Name               string `json:"name"`
		Initialized        bool   `json:"initialized"`
		ToolCount          int    `json:"toolCount"`
		RegistrationFailed bool   `json:"registrationFailed"`
	}
	if json.Unmarshal(raw, &servers) != nil {
		return false
	}
	seen := map[string]bool{}
	for _, server := range servers {
		if server.RegistrationFailed || !server.Initialized || server.ToolCount < 2 {
			continue
		}
		seen[server.Name] = true
	}
	return seen["alpha"] && seen["zeta"]
}

func twoActiveSkills(raw []byte) bool {
	var skills []struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if json.Unmarshal(raw, &skills) != nil {
		return false
	}
	active := 0
	for _, skill := range skills {
		if skill.State == "active" && skill.Name != "" {
			active++
		}
	}
	return active >= 2
}

func getJSON(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("status %d", resp.StatusCode)
	}
	return body, nil
}

// nodeMajorVersion parses `node --version` (e.g. "v22.23.1"); 0 when
// unknown, which lets the run proceed and fail with real output.
func nodeMajorVersion(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return 0
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}
