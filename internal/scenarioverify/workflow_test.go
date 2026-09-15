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
