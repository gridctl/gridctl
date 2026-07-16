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

func testDiffDoc(hasDrift bool) pinsDiffDoc {
	sv := pinsDiffServer{
		Name:          "myserver",
		Status:        pins.VerifyStatusVerified,
		ModifiedTools: []pinsToolDiff{},
		NewTools:      []string{},
		RemovedTools:  []string{},
	}
	if hasDrift {
		sv.Status = pins.VerifyStatusDrift
		sv.ModifiedTools = []pinsToolDiff{{
			Name:           "poisoned",
			OldHash:        "h2:947cd68fbf83c18ca75435e6730174418b91fd0e",
			NewHash:        "h2:267032e068c7ee40310b8cea8e12f1248a974166",
			OldDescription: "original description",
			NewDescription: "new\u202edescription\nwith hidden characters",
		}}
		sv.NewTools = []string{"added"}
		sv.RemovedTools = []string{"retired"}
	}
	return pinsDiffDoc{
		SchemaVersion: pinsJSONSchemaVersion,
		Stack:         "mystack",
		HasDrift:      hasDrift,
		Servers:       []pinsDiffServer{sv},
	}
}

func TestPinsDiffExit_Codes(t *testing.T) {
	cases := []struct {
		name     string
		doc      pinsDiffDoc
		warnings []string
		format   string
		want     int
	}{
		{"clean text", testDiffDoc(false), nil, "", pinsExitOK},
		{"clean json", testDiffDoc(false), nil, "json", pinsExitOK},
		{"drift text", testDiffDoc(true), nil, "", pinsExitDrift},
		{"drift json", testDiffDoc(true), nil, "json", pinsExitDrift},
		// Skipped servers must not read as clean: warnings raise exit 2.
		{"clean with warnings", testDiffDoc(false), []string{"skipping gone: not in gateway"}, "", pinsExitInfrastructure},
		// Drift outranks warnings; it is the actionable signal.
		{"drift with warnings", testDiffDoc(true), []string{"skipping gone: not in gateway"}, "", pinsExitDrift},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := pinsDiffExit(&stdout, &stderr, tc.doc, tc.warnings, tc.format)
			if got != tc.want {
				t.Errorf("exit code: expected %d, got %d (stdout=%q stderr=%q)", tc.want, got, stdout.String(), stderr.String())
			}
			if len(tc.warnings) > 0 && !strings.Contains(stderr.String(), "warning: skipping gone") {
				t.Errorf("warnings not printed to stderr, got %q", stderr.String())
			}
			if tc.format == "json" {
				var decoded map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
					t.Errorf("json mode stdout is not valid JSON: %v", err)
				}
				if hasDrift, ok := decoded["has_drift"].(bool); !ok || hasDrift != tc.doc.HasDrift {
					t.Errorf("has_drift = %v, want %v", decoded["has_drift"], tc.doc.HasDrift)
				}
			}
		})
	}
}

func TestRenderPinsDiffText_EscapesAndShortensHashes(t *testing.T) {
	var buf bytes.Buffer
	renderPinsDiffText(&buf, testDiffDoc(true))
	out := buf.String()

	if !strings.Contains(out, "myserver (drift)") {
		t.Errorf("missing server header, got:\n%s", out)
	}
	if !strings.Contains(out, "old h2:947cd68fbf83") || !strings.Contains(out, "new h2:267032e068c7") {
		t.Errorf("expected shortened old/new hashes, got:\n%s", out)
	}
	// The RTL-override and newline in the poisoned description must render as
	// visible escapes, never as the raw control characters.
	if strings.ContainsAny(out, "\u202e") {
		t.Error("raw U+202E leaked into text output")
	}
	if !strings.Contains(out, `new\u202edescription\nwith hidden characters`) {
		t.Errorf("expected escaped description, got:\n%s", out)
	}
	if !strings.Contains(out, "+ added") || !strings.Contains(out, "- retired") {
		t.Errorf("expected new/removed tool lines, got:\n%s", out)
	}
}

func TestRenderPinsDiffText_CleanAndEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderPinsDiffText(&buf, testDiffDoc(false))
	if !strings.Contains(buf.String(), "no changes") {
		t.Errorf("expected 'no changes' for a clean server, got:\n%s", buf.String())
	}

	buf.Reset()
	renderPinsDiffText(&buf, pinsDiffDoc{Servers: []pinsDiffServer{}})
	if !strings.Contains(buf.String(), "No drift detected.") {
		t.Errorf("expected empty-doc message, got:\n%s", buf.String())
	}
}

func TestEscapeNonPrintable(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text stays", "plain text stays"},
		{"tab\there", `tab\there`},
		{"line\nbreak", `line\nbreak`},
		{"rtl\u202eoverride", `rtl\u202eoverride`},
		{"zero\u200bwidth", `zero\u200bwidth`},
		{"bell\x07", `bell\a`},
		{"back\\slash", `back\\slash`},
		{"isolate\u2066wrap\u2069", `isolate\u2066wrap\u2069`},
	}
	for _, tc := range cases {
		if got := escapeNonPrintable(tc.in); got != tc.want {
			t.Errorf("escapeNonPrintable(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShortPinHash(t *testing.T) {
	cases := []struct{ in, want string }{
		{"h2:947cd68fbf83c18ca75435e6730174418b91fd0e", "h2:947cd68fbf83"},
		{"947cd68fbf83c18ca75435e6730174418b91fd0e", "947cd68fbf83"},
		{"h2:short", "h2:short"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := shortPinHash(tc.in); got != tc.want {
			t.Errorf("shortPinHash(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
