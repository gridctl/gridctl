package scenarioverify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowDesignatedInvocations(t *testing.T) {
	root := repoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "gatekeeper.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, name := range []string{"test:", "integration:", "podman-integration:", "conformance:", "frontend:", "required:"} {
		if !strings.Contains(text, "\n  "+name) && !strings.Contains(text, "\n"+name) {
			t.Fatalf("missing job %q", name)
		}
	}
	for _, lane := range []string{"unit", "integration", "podman-integration"} {
		needle := "--lane " + lane
		if !strings.Contains(text, needle) {
			t.Fatalf("designated lane %q is not invoked: missing %q", lane, needle)
		}
	}
	if !strings.Contains(text, "scripts/run-verified-tests.sh") {
		t.Fatal("gatekeeper does not call run-verified-tests.sh")
	}
	if !strings.Contains(text, "tests/adversarial/index.yaml") {
		t.Fatal("gatekeeper does not pass the reviewed index")
	}
	if !strings.Contains(text, "-coverprofile=coverage.out") {
		t.Fatal("unit coverage profile dropped")
	}
	if !strings.Contains(text, "-timeout 20m") {
		t.Fatal("integration timeout contract dropped")
	}

	integ := workflowJob(text, "integration")
	if integ == "" {
		t.Fatal("integration job body not found")
	}
	// The job budget must exceed the go test budget so a slow runner hits
	// the package timeout (which names the running test) before the job
	// limit kills the process without a report.
	if !strings.Contains(integ, "timeout-minutes: 25") {
		t.Fatal("integration job budget dropped")
	}
	if !strings.Contains(integ, "GRIDCTL_RUNTIME: docker") {
		t.Fatal("integration job does not select Docker")
	}
	if !strings.Contains(integ, "docker info") {
		t.Fatal("integration job does not validate the Docker engine")
	}
	if strings.Contains(integ, "GRIDCTL_RUNTIME: podman") {
		t.Fatal("integration job selected Podman")
	}
	if !strings.Contains(integ, "-tags=integration") {
		t.Fatal("integration suite tag dropped")
	}
	if !strings.Contains(integ, "-timeout 20m") {
		t.Fatal("integration timeout dropped from the integration job")
	}
	if !strings.Contains(integ, "./tests/integration/...") {
		t.Fatal("integration suite scope dropped")
	}

	podman := workflowJob(text, "podman-integration")
	if podman == "" {
		t.Fatal("podman-integration job body not found")
	}
	if !strings.Contains(podman, "GRIDCTL_RUNTIME: podman") {
		t.Fatal("podman job does not select Podman")
	}
	if !strings.Contains(podman, "timeout-minutes: 25") {
		t.Fatal("podman job budget dropped")
	}
	if !strings.Contains(podman, "-tags=integration") || !strings.Contains(podman, "-timeout 20m") {
		t.Fatal("podman job dropped suite tag or timeout")
	}
}

func TestWorkflowFeedbackCycle(t *testing.T) {
	root := repoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "gatekeeper.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)

	var groupLine string
	concurrency := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "concurrency:" {
			concurrency++
			if line != trimmed {
				t.Fatalf("job-level concurrency is not allowed: %q", line)
			}
		}
		if strings.HasPrefix(trimmed, "group:") {
			if groupLine != "" {
				t.Fatalf("multiple group lines: %q and %q", groupLine, trimmed)
			}
			groupLine = trimmed
		}
	}
	if concurrency != 1 {
		t.Fatalf("expected one top-level concurrency block, found %d", concurrency)
	}
	if groupLine == "" {
		t.Fatal("concurrency group dropped")
	}
	for _, needle := range []string{"github.event.pull_request.number", "github.run_id"} {
		if !strings.Contains(groupLine, needle) {
			t.Fatalf("concurrency group missing %q: %s", needle, groupLine)
		}
	}
	for _, banned := range []string{"github.workflow", "github.ref"} {
		if strings.Contains(groupLine, banned) {
			t.Fatalf("concurrency group must not use %s: %s", banned, groupLine)
		}
	}
	if strings.Count(text, "cancel-in-progress: ${{ github.event_name == 'pull_request' }}") != 1 {
		t.Fatal("cancel-in-progress contract dropped")
	}
	if !strings.Contains(text, "bash scripts/check-coverage.sh coverage.out") {
		t.Fatal("per-package coverage step does not pass coverage.out")
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "bash scripts/check-coverage.sh" {
			t.Fatal("per-package coverage step does not pass the profile")
		}
	}
	if !strings.Contains(text, `echo "$COVERAGE < 64" | bc -l`) {
		t.Fatal("whole-suite coverage threshold changed")
	}

	required := workflowJob(text, "required")
	if required == "" {
		t.Fatal("required job body not found")
	}
	if !strings.Contains(required, "needs: [test, integration, conformance, podman-integration, frontend]") {
		t.Fatal("required job needs list changed")
	}
	if !strings.Contains(required, "if: ${{ always() }}") {
		t.Fatal("required job lost if: always()")
	}

	lanes := []struct {
		job  string
		lane string
	}{
		{"test", "unit"},
		{"integration", "integration"},
		{"podman-integration", "podman-integration"},
	}
	for _, lane := range lanes {
		body := workflowJob(text, lane.job)
		if body == "" {
			t.Fatalf("%s job body not found", lane.job)
		}
		if hasNeedsMapping(body) {
			t.Fatalf("%s job has a needs: mapping line", lane.job)
		}
		capture := `${RUNNER_TEMP}/` + lane.lane + `-capture.json`
		if !strings.Contains(body, `--capture "`+capture+`"`) {
			t.Fatalf("%s job does not pass --capture %s", lane.job, capture)
		}
		step := slowestStep(body)
		if step == "" {
			t.Fatalf("%s job has no Slowest tests step", lane.job)
		}
		if !strings.Contains(step, "if: always()") {
			t.Fatalf("%s Slowest tests step lost if: always()", lane.job)
		}
		if !strings.Contains(step, "bash scripts/slowest-tests.sh \""+capture+"\"") {
			t.Fatalf("%s Slowest tests step does not call slowest-tests.sh with %s", lane.job, capture)
		}
		if strings.Contains(step, "jq ") {
			t.Fatalf("%s Slowest tests step pipelines jq; a failing jq is invisible under bash -e", lane.job)
		}
		if !strings.Contains(step, "|| echo") {
			t.Fatalf("%s Slowest tests step does not guard a script failure", lane.job)
		}
		if strings.Contains(step, "pipefail") {
			t.Fatalf("%s Slowest tests step must not enable pipefail", lane.job)
		}
		if strings.Contains(step, ".Output") {
			t.Fatalf("%s Slowest tests step prints a test output payload", lane.job)
		}
		if !strings.HasSuffix(strings.TrimSpace(step), "exit 0") {
			t.Fatalf("%s Slowest tests step does not end with exit 0", lane.job)
		}
	}

	conformance := workflowJob(text, "conformance")
	if conformance == "" {
		t.Fatal("conformance job body not found")
	}
	if hasNeedsMapping(conformance) {
		t.Fatal("conformance job has a needs: mapping line")
	}
}

func TestWorkflowExampleStackExclusions(t *testing.T) {
	root := repoRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "gatekeeper.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	// These files live under examples/ but are not stacks. Stack validation
	// rejects them on purpose; the gatekeeper find must skip them.
	for _, path := range []string{
		"examples/stack-declaration-policy/policy.yaml",
		"examples/booster-pack/servers.yaml",
		"examples/tracing/otel-collector-config.yaml",
	} {
		if !strings.Contains(text, `-not -path "`+path+`"`) {
			t.Errorf("example stack validation does not exclude non-stack %s", path)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("excluded example path missing: %s", path)
		}
	}
}

func hasNeedsMapping(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "needs:") {
			return true
		}
	}
	return false
}

func slowestStep(body string) string {
	const marker = "- name: Slowest tests"
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	rest := body[start:]
	if next := strings.Index(rest[len(marker):], "\n      - "); next >= 0 {
		return rest[:len(marker)+next]
	}
	return rest
}

func workflowJob(text, name string) string {
	marker := "\n  " + name + ":\n"
	start := strings.Index(text, marker)
	if start < 0 {
		return ""
	}
	body := text[start+1:]
	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(lines[0])
	b.WriteByte('\n')
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		rest := strings.TrimPrefix(line, "  ")
		if rest != line && !strings.HasPrefix(rest, " ") && strings.HasSuffix(rest, ":") && !strings.Contains(strings.TrimSuffix(rest, ":"), " ") {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestScriptRequiresJSONCountRace(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "scripts", "run-verified-tests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "-json -count=1 -race") {
		t.Fatal("script does not require -json -count=1 -race")
	}
	if strings.Contains(text, "go test -list") {
		t.Fatal("script must not use go test -list as execution evidence")
	}
}

func TestLiveIndexLoads(t *testing.T) {
	root := repoRoot(t)
	idx, err := LoadIndex(t.Context(), filepath.Join(root, "tests", "adversarial", "index.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range []string{"unit", "integration", "podman-integration"} {
		required, err := idx.Required(lane)
		if err != nil {
			t.Fatalf("lane %s: %v", lane, err)
		}
		if len(required) == 0 {
			t.Fatalf("lane %s has an empty required set", lane)
		}
	}
}
