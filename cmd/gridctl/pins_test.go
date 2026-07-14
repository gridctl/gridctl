package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pins"
)

func testServerPins(status string, toolCount int) *pins.ServerPins {
	return &pins.ServerPins{
		ServerHash:     "h2:abc",
		PinnedAt:       time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		LastVerifiedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		ToolCount:      toolCount,
		Status:         status,
		Tools:          map[string]*pins.PinRecord{},
	}
}

func TestBuildPinsVerifyDoc_DriftFlagAndOrdering(t *testing.T) {
	servers := map[string]*pins.ServerPins{
		"zeta":  testServerPins(pins.StatusDrift, 3),
		"alpha": testServerPins(pins.StatusPinned, 2),
	}

	doc := buildPinsVerifyDoc("mystack", servers)

	if !doc.HasDrift {
		t.Error("expected HasDrift when any server has drift status")
	}
	if doc.Stack != "mystack" {
		t.Errorf("Stack: expected %q, got %q", "mystack", doc.Stack)
	}
	if doc.SchemaVersion != pinsJSONSchemaVersion {
		t.Errorf("SchemaVersion: expected %d, got %d", pinsJSONSchemaVersion, doc.SchemaVersion)
	}
	if len(doc.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(doc.Servers))
	}
	if doc.Servers[0].Name != "alpha" || doc.Servers[1].Name != "zeta" {
		t.Errorf("servers should be sorted by name, got %s, %s", doc.Servers[0].Name, doc.Servers[1].Name)
	}
}

func TestBuildPinsVerifyDoc_NoDrift(t *testing.T) {
	servers := map[string]*pins.ServerPins{
		"a": testServerPins(pins.StatusPinned, 1),
		"b": testServerPins(pins.StatusApprovedPendingRedeploy, 2),
	}

	doc := buildPinsVerifyDoc("mystack", servers)
	if doc.HasDrift {
		t.Error("HasDrift should be false when no server has drift status")
	}
}

func TestBuildPinsVerifyDoc_Empty(t *testing.T) {
	doc := buildPinsVerifyDoc("mystack", map[string]*pins.ServerPins{})
	if doc.HasDrift {
		t.Error("empty store should not report drift")
	}
	if doc.Servers == nil || len(doc.Servers) != 0 {
		t.Errorf("expected empty (non-nil) servers slice, got %#v", doc.Servers)
	}
}

func TestPinsVerifyDoc_JSONShape(t *testing.T) {
	servers := map[string]*pins.ServerPins{
		"srv": testServerPins(pins.StatusDrift, 4),
	}

	var buf bytes.Buffer
	if err := output.EncodeJSON(&buf, buildPinsVerifyDoc("mystack", servers)); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("verify JSON does not parse: %v", err)
	}
	for _, key := range []string{"schema_version", "stack", "has_drift", "servers"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("verify JSON missing key %q", key)
		}
	}
	entries, ok := decoded["servers"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("expected one server entry, got %#v", decoded["servers"])
	}
	entry := entries[0].(map[string]any)
	for _, key := range []string{"name", "status", "tool_count", "last_verified_at"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("server entry missing key %q", key)
		}
	}
}

func TestPinsListDoc_JSONShape(t *testing.T) {
	doc := pinsListDoc{
		SchemaVersion: pinsJSONSchemaVersion,
		Stack:         "mystack",
		Servers: map[string]*pins.ServerPins{
			"srv": testServerPins(pins.StatusPinned, 2),
		},
	}

	var buf bytes.Buffer
	if err := output.EncodeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("list JSON does not parse: %v", err)
	}
	for _, key := range []string{"schema_version", "stack", "servers"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("list JSON missing key %q", key)
		}
	}
	servers, ok := decoded["servers"].(map[string]any)
	if !ok {
		t.Fatalf("servers should be an object keyed by name, got %#v", decoded["servers"])
	}
	if _, ok := servers["srv"]; !ok {
		t.Error("servers object missing entry for srv")
	}
}

func TestRenderPinsVerifyText(t *testing.T) {
	servers := map[string]*pins.ServerPins{
		"clean":   testServerPins(pins.StatusPinned, 2),
		"drifted": testServerPins(pins.StatusDrift, 3),
		"pending": testServerPins(pins.StatusApprovedPendingRedeploy, 1),
	}

	var buf bytes.Buffer
	renderPinsVerifyText(&buf, buildPinsVerifyDoc("mystack", servers))
	out := buf.String()

	if !strings.Contains(out, "✓ clean") || !strings.Contains(out, "2 tools verified") {
		t.Errorf("missing clean line, got:\n%s", out)
	}
	if !strings.Contains(out, "✗ drifted") || !strings.Contains(out, "drift detected") {
		t.Errorf("missing drift line, got:\n%s", out)
	}
	if !strings.Contains(out, "~ pending") || !strings.Contains(out, "approved, pending redeploy") {
		t.Errorf("missing pending line, got:\n%s", out)
	}
}

// --- exit contract ---

func TestPinsVerifyExit_Codes(t *testing.T) {
	clean := map[string]*pins.ServerPins{"clean": testServerPins(pins.StatusPinned, 2)}
	drifted := map[string]*pins.ServerPins{
		"clean":   testServerPins(pins.StatusPinned, 2),
		"drifted": testServerPins(pins.StatusDrift, 3),
	}

	tests := []struct {
		name    string
		servers map[string]*pins.ServerPins
		server  string
		format  string
		want    int
	}{
		{name: "clean store exits 0", servers: clean, want: pinsExitOK},
		{name: "drift exits 1", servers: drifted, want: pinsExitDrift},
		{name: "drift exits 1 in json mode", servers: drifted, format: "json", want: pinsExitDrift},
		{name: "empty store is the pre-pin state, exits 0", servers: map[string]*pins.ServerPins{}, want: pinsExitOK},
		{name: "empty store exits 0 in json mode", servers: map[string]*pins.ServerPins{}, format: "json", want: pinsExitOK},
		{name: "named server verifies alone", servers: drifted, server: "clean", want: pinsExitOK},
		{name: "named drifted server exits 1", servers: drifted, server: "drifted", want: pinsExitDrift},
		{name: "unknown named server exits 2", servers: clean, server: "nosuch", want: pinsExitInfrastructure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := pinsVerifyExit(&stdout, &stderr, "mystack", tc.servers, tc.server, tc.format)
			if got != tc.want {
				t.Errorf("exit code: expected %d, got %d (stdout=%q stderr=%q)", tc.want, got, stdout.String(), stderr.String())
			}
			if tc.format == "json" && stdout.Len() > 0 {
				var decoded map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
					t.Errorf("json mode stdout is not valid JSON: %v", err)
				}
			}
		})
	}
}

func TestPinsVerifyExit_EmptyStoreJSONEmitsDocument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := pinsVerifyExit(&stdout, &stderr, "mystack", map[string]*pins.ServerPins{}, "", "json"); got != pinsExitOK {
		t.Fatalf("expected exit 0, got %d", got)
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("expected a JSON document for an empty store: %v", err)
	}
	if hasDrift, ok := decoded["has_drift"].(bool); !ok || hasDrift {
		t.Errorf("empty store must report has_drift=false, got %v", decoded["has_drift"])
	}
}
