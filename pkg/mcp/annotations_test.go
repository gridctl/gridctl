package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"go.uber.org/mock/gomock"
)

func boolPtr(b bool) *bool { return &b }

// TestToolAnnotations_UnmarshalPassthrough proves annotations survive the
// wire decode a downstream tools/list response goes through.
func TestToolAnnotations_UnmarshalPassthrough(t *testing.T) {
	raw := `{
		"name": "read_file",
		"description": "Read a file",
		"inputSchema": {"type": "object"},
		"annotations": {"title": "Read File", "readOnlyHint": true, "openWorldHint": false}
	}`
	var tool Tool
	if err := json.Unmarshal([]byte(raw), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool.Annotations == nil {
		t.Fatal("annotations dropped on decode")
	}
	if tool.Annotations.Title != "Read File" {
		t.Errorf("title = %q", tool.Annotations.Title)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("readOnlyHint lost")
	}
	if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
		t.Error("openWorldHint lost")
	}
	if tool.Annotations.DestructiveHint != nil {
		t.Error("undeclared hint must stay nil (worst-case default is the client's job)")
	}

	out, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(out) || !containsJSONKey(t, out, "annotations") {
		t.Errorf("annotations dropped on re-encode: %s", out)
	}
}

func containsJSONKey(t *testing.T, data []byte, key string) bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	_, ok := m[key]
	return ok
}

// TestRouter_AnnotationsSurviveAggregation guards the two literal-copy sites:
// every field of Tool must survive AggregatedTools and CatalogTools, so a
// future field added to Tool without updating the copies fails here.
func TestRouter_AnnotationsSurviveAggregation(t *testing.T) {
	ctrl := gomock.NewController(t)
	ann := &ToolAnnotations{ReadOnlyHint: boolPtr(true), Title: "Safe read"}
	client := setupMockAgentClient(ctrl, "alpha", []Tool{
		{Name: "read", Description: "reads", InputSchema: json.RawMessage(`{}`), Annotations: ann},
	})

	r := NewRouter()
	r.AddClient(client)
	r.RefreshTools()

	agg := r.AggregatedTools()
	if len(agg) != 1 || agg[0].Annotations == nil || agg[0].Annotations.ReadOnlyHint == nil || !*agg[0].Annotations.ReadOnlyHint {
		t.Fatalf("AggregatedTools dropped annotations: %+v", agg)
	}
	cat := r.CatalogTools()
	if len(cat) != 1 || cat[0].Annotations == nil || cat[0].Annotations.Title != "Safe read" {
		t.Fatalf("CatalogTools dropped annotations: %+v", cat)
	}

	// Field-completeness tripwire: if Tool grows a field, this count changes
	// and whoever adds it must extend both copy sites plus this test.
	if got := reflect.TypeOf(Tool{}).NumField(); got != 6 {
		t.Errorf("Tool has %d fields; update AggregatedTools, CatalogTools, and this test when adding fields", got)
	}
}

// fakeWhitelistedClient embeds ClientBase so Tools()/AllTools() behave like a
// real transport client: the whitelist filter applies on read and the full
// set is retained underneath.
type fakeWhitelistedClient struct {
	ClientBase
	name string
}

func (f *fakeWhitelistedClient) Name() string                       { return f.name }
func (f *fakeWhitelistedClient) Initialize(context.Context) error   { return nil }
func (f *fakeWhitelistedClient) RefreshTools(context.Context) error { return nil }
func (f *fakeWhitelistedClient) CallTool(context.Context, string, map[string]any) (*ToolCallResult, error) {
	return nil, nil
}

// TestRouter_AllCatalogToolsIncludesWhitelistedOut proves the ?include=all
// catalog variant surfaces whitelist-disabled tools with their annotations
// while the default catalog stays filtered.
func TestRouter_AllCatalogToolsIncludesWhitelistedOut(t *testing.T) {
	ann := &ToolAnnotations{DestructiveHint: boolPtr(true)}
	c := &fakeWhitelistedClient{name: "alpha"}
	c.SetTools([]Tool{
		{Name: "keep", InputSchema: json.RawMessage(`{}`)},
		{Name: "hidden", InputSchema: json.RawMessage(`{}`), Annotations: ann},
	})
	c.SetToolWhitelist([]string{"keep"})
	c.SetInitialized(ServerInfo{Name: "alpha", Version: "1.0.0"})

	r := NewRouter()
	r.AddClient(c)
	r.RefreshTools()

	cat := r.CatalogTools()
	if len(cat) != 1 || cat[0].Name != PrefixTool("alpha", "keep") {
		t.Fatalf("CatalogTools must stay whitelist-filtered: %+v", cat)
	}

	all := r.AllCatalogTools()
	if len(all) != 2 {
		t.Fatalf("AllCatalogTools returned %d tools, want 2: %+v", len(all), all)
	}
	var hidden *Tool
	for i := range all {
		if all[i].Name == PrefixTool("alpha", "hidden") {
			hidden = &all[i]
		}
	}
	if hidden == nil {
		t.Fatalf("AllCatalogTools missing the whitelist-disabled tool: %+v", all)
	}
	if hidden.Annotations == nil || hidden.Annotations.DestructiveHint == nil || !*hidden.Annotations.DestructiveHint {
		t.Fatalf("AllCatalogTools dropped the disabled tool's annotations: %+v", hidden)
	}
}

func TestToolAnnotations_Clone(t *testing.T) {
	var nilAnn *ToolAnnotations
	if nilAnn.Clone() != nil {
		t.Error("nil clone should be nil")
	}
	orig := &ToolAnnotations{ReadOnlyHint: boolPtr(false)}
	c := orig.Clone()
	c.ReadOnlyHint = boolPtr(true)
	c.Title = "changed"
	if *orig.ReadOnlyHint || orig.Title != "" {
		t.Error("clone mutated original")
	}
}
