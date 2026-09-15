package scenarioverify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseIndex_Valid(t *testing.T) {
	idx, err := ParseIndex(context.Background(), []byte(`
version: 1
lanes: [unit]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: denied before dispatch
    nonclaims: [not a fuzz campaign]
    budgets:
      max_payload_bytes: 64
      max_duration: 2s
`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := idx.Required("unit")
	if err != nil || len(got) != 1 || got[0].ID != "sample-case" {
		t.Fatalf("required = %v err=%v", got, err)
	}
}

func TestParseIndex_Rejects(t *testing.T) {
	cases := map[string]string{
		"malformed version": `
version: 2
lanes: [unit]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: x
`,
		"duplicate id": `
version: 1
lanes: [unit]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: x
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestBar
    lanes: [unit]
    expected_boundary: x
`,
		"unknown lane on scenario": `
version: 1
lanes: [unit]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [other]
    expected_boundary: x
`,
		"empty required set": `
version: 1
lanes: [unit, integration]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: x
`,
		"command key": `
version: 1
lanes: [unit]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: x
    command: go test
`,
		"duplicate selector": `
version: 1
lanes: [unit]
scenarios:
  - id: one
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: x
  - id: two
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: x
`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseIndex(context.Background(), []byte(raw))
			if err == nil {
				t.Fatal("expected index rejection")
			}
			if !strings.Contains(err.Error(), ReasonMalformedIndex) && !strings.Contains(err.Error(), ReasonEmptyRequiredSet) {
				t.Fatalf("error %q missing malformed/empty reason", err)
			}
		})
	}
}

func TestParseIndex_AcceptsLiteralPunctuation(t *testing.T) {
	idx, err := ParseIndex(context.Background(), []byte(`
version: 1
lanes: [unit]
scenarios:
  - id: braced-route
    owner_package: internal/api
    owner_issue: "1223"
    package: github.com/gridctl/gridctl/internal/api
    test: 'TestAuthHandler_RegisteredRoutes/POST /groups/{name}/mcp'
    lanes: [unit]
    expected_boundary: grouped MCP routes require configured gateway authentication
`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := idx.Required("unit")
	if err != nil || len(got) != 1 || got[0].Test != "TestAuthHandler_RegisteredRoutes/POST /groups/{name}/mcp" {
		t.Fatalf("required = %+v err=%v", got, err)
	}
}

func TestRequired_UnknownLane(t *testing.T) {
	idx, err := ParseIndex(context.Background(), []byte(`
version: 1
lanes: [unit]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: x
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = idx.Required("podman-integration")
	if err == nil || !strings.Contains(err.Error(), ReasonUnknownLane) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadIndex_MissingPrerequisitesDoNotShrink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.yaml")
	body := []byte(`
version: 1
lanes: [podman-integration]
scenarios:
  - id: podman-only
    owner_package: tests/integration
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/tests/integration
    test: TestPodmanRootless_MultiContainerNetworking
    lanes: [podman-integration]
    prerequisites: [podman]
    expected_boundary: rootless podman networking
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadIndex(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := idx.Required("podman-integration")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "podman-only" {
		t.Fatalf("prerequisites shrank the required set: %+v", got)
	}
}
