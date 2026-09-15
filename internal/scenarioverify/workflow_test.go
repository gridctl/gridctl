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
	for _, name := range []string{"test:", "integration:", "podman-integration:", "litellm-contract:", "conformance:", "frontend:", "required:"} {
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
	if !strings.Contains(text, "-timeout 15m") {
		t.Fatal("integration timeout contract dropped")
	}
	if !strings.Contains(text, "timeout-minutes: 20") {
		t.Fatal("integration job budget dropped")
	}
	if !strings.Contains(text, "timeout-minutes: 25") {
		t.Fatal("podman job budget dropped")
	}

	integ := workflowJob(text, "integration")
	if integ == "" {
		t.Fatal("integration job body not found")
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
	if !strings.Contains(integ, "-timeout 15m") {
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
	if !strings.Contains(podman, "-tags=integration") || !strings.Contains(podman, "-timeout 15m") {
		t.Fatal("podman job dropped suite tag or timeout")
	}
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
