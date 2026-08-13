package mcp

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// specWithMixedOperations covers the three states EnumerateOperations must
// distinguish: a normal operation, one with no operationId, and one whose
// operationId sanitizes to an unusable tool name.
const specWithMixedOperations = `
openapi: 3.0.0
info:
  title: Mixed
  version: "1.2.3"
paths:
  /pets:
    get:
      operationId: listPets
      summary: List pets
      tags: [pet]
    post:
      summary: Create a pet with no operationId
  /pets/{id}:
    get:
      operationId: pets.get.byId
      summary: Dotted id
      tags: [pet, detail]
      deprecated: true
  /weird:
    get:
      operationId: "!!!"
      summary: Unusable name
`

func parseSpec(t *testing.T, data string) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.Context = context.Background()
	doc, err := loader.LoadFromData([]byte(data))
	if err != nil {
		t.Fatalf("loading spec: %v", err)
	}
	return doc
}

func findOp(ops []OperationSummary, method, path string) *OperationSummary {
	for i := range ops {
		if ops[i].Method == method && ops[i].Path == path {
			return &ops[i]
		}
	}
	return nil
}

func TestEnumerateOperations_ReportsSkipReasons(t *testing.T) {
	ops := EnumerateOperations(parseSpec(t, specWithMixedOperations))

	if len(ops) != 4 {
		t.Fatalf("expected 4 operations (including skipped), got %d", len(ops))
	}

	noID := findOp(ops, "POST", "/pets")
	if noID == nil {
		t.Fatal("POST /pets missing — operations without an operationId must be reported, not dropped")
	}
	if !noID.Skipped || noID.SkipReason != SkipReasonNoOperationID {
		t.Errorf("POST /pets: got skipped=%v reason=%q, want true/%s", noID.Skipped, noID.SkipReason, SkipReasonNoOperationID)
	}

	unusable := findOp(ops, "GET", "/weird")
	if unusable == nil {
		t.Fatal("GET /weird missing")
	}
	if !unusable.Skipped || unusable.SkipReason != SkipReasonUnusableName {
		t.Errorf("GET /weird: got skipped=%v reason=%q, want true/%s", unusable.Skipped, unusable.SkipReason, SkipReasonUnusableName)
	}

	ok := findOp(ops, "GET", "/pets")
	if ok == nil {
		t.Fatal("GET /pets missing")
	}
	if ok.Skipped {
		t.Errorf("GET /pets should not be skipped, reason=%q", ok.SkipReason)
	}
	if ok.OperationID != "listPets" || ok.ToolName != "listPets" {
		t.Errorf("GET /pets: got id=%q tool=%q, want listPets/listPets", ok.OperationID, ok.ToolName)
	}
	if len(ok.Tags) != 1 || ok.Tags[0] != "pet" {
		t.Errorf("GET /pets tags = %v, want [pet]", ok.Tags)
	}
}

// The raw operationId is what openapi.operations.include matches; the tool name
// is the sanitized form. A caller that conflated them would write a filter that
// silently matches nothing, so the two must be reported separately.
func TestEnumerateOperations_KeepsRawAndSanitizedIDs(t *testing.T) {
	ops := EnumerateOperations(parseSpec(t, specWithMixedOperations))

	dotted := findOp(ops, "GET", "/pets/{id}")
	if dotted == nil {
		t.Fatal("GET /pets/{id} missing")
	}
	if dotted.OperationID != "pets.get.byId" {
		t.Errorf("OperationID = %q, want the raw spec value pets.get.byId", dotted.OperationID)
	}
	if dotted.ToolName != "pets_get_byId" {
		t.Errorf("ToolName = %q, want the sanitized pets_get_byId", dotted.ToolName)
	}
	if dotted.OperationID == dotted.ToolName {
		t.Error("raw and sanitized IDs must stay distinct for this spec")
	}
	if !dotted.Deprecated {
		t.Error("deprecated flag not carried through")
	}
}

func TestEnumerateOperations_DeterministicOrder(t *testing.T) {
	doc := parseSpec(t, specWithMixedOperations)
	first := EnumerateOperations(doc)
	for i := 0; i < 20; i++ {
		next := EnumerateOperations(doc)
		if len(next) != len(first) {
			t.Fatalf("length changed between calls: %d vs %d", len(next), len(first))
		}
		for j := range first {
			if next[j].Method != first[j].Method || next[j].Path != first[j].Path {
				t.Fatalf("order changed at %d: %s %s vs %s %s",
					j, next[j].Method, next[j].Path, first[j].Method, first[j].Path)
			}
		}
	}
}

func TestEnumerateOperations_NilSafe(t *testing.T) {
	if got := EnumerateOperations(nil); got != nil {
		t.Errorf("EnumerateOperations(nil) = %v, want nil", got)
	}
	if got := EnumerateOperations(&openapi3.T{}); got != nil {
		t.Errorf("EnumerateOperations(empty) = %v, want nil", got)
	}
}

// TestEnumerateOperations_ParityWithRefreshTools is the guard that matters: the
// wizard preview and the deployed tool builder must agree on which operations
// are usable. If RefreshTools stops consuming EnumerateOperations, or either
// side changes its skip rules independently, this fails.
func TestEnumerateOperations_ParityWithRefreshTools(t *testing.T) {
	doc := parseSpec(t, specWithMixedOperations)

	c, err := NewOpenAPIClient("parity", &OpenAPIClientConfig{
		Spec:    "http://example.invalid/spec.json",
		BaseURL: "http://example.invalid",
	})
	if err != nil {
		t.Fatalf("NewOpenAPIClient: %v", err)
	}
	c.cachedDoc = doc

	if err := c.RefreshTools(context.Background()); err != nil {
		t.Fatalf("RefreshTools: %v", err)
	}

	// Every non-skipped enumeration row must have produced exactly one tool,
	// under the sanitized name.
	wantTools := map[string]bool{}
	for _, op := range EnumerateOperations(doc) {
		if !op.Skipped {
			wantTools[op.ToolName] = true
		}
	}

	gotTools := map[string]bool{}
	for _, tool := range c.allTools {
		gotTools[tool.Name] = true
	}

	if len(gotTools) != len(wantTools) {
		t.Fatalf("tool count %d does not match usable operation count %d\n got: %v\nwant: %v",
			len(gotTools), len(wantTools), gotTools, wantTools)
	}
	for name := range wantTools {
		if !gotTools[name] {
			t.Errorf("operation %q is usable per EnumerateOperations but produced no tool", name)
		}
	}
}

// The include filter matches raw operationIds, so a dotted ID must be
// selectable even though its tool name differs.
func TestRefreshTools_IncludeMatchesRawOperationID(t *testing.T) {
	doc := parseSpec(t, specWithMixedOperations)

	c, err := NewOpenAPIClient("filtered", &OpenAPIClientConfig{
		Spec:    "http://example.invalid/spec.json",
		BaseURL: "http://example.invalid",
		Include: []string{"pets.get.byId"},
	})
	if err != nil {
		t.Fatalf("NewOpenAPIClient: %v", err)
	}
	c.cachedDoc = doc

	if err := c.RefreshTools(context.Background()); err != nil {
		t.Fatalf("RefreshTools: %v", err)
	}

	if len(c.allTools) != 1 {
		t.Fatalf("expected exactly 1 tool from the include list, got %d", len(c.allTools))
	}
	if c.allTools[0].Name != "pets_get_byId" {
		t.Errorf("tool name = %q, want the sanitized pets_get_byId", c.allTools[0].Name)
	}
}
