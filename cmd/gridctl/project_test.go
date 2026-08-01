package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// wiringTestManager builds a wiring manager over a temp home and one
// stateful fake client.
func wiringTestManager(t *testing.T) (*wiring.Manager, *fakeProvisioner) {
	t.Helper()
	fake := &fakeProvisioner{name: "Alpha"}
	return wiring.NewManagerWith(t.TempDir(), provisioner.NewRegistryWith(fake)), fake
}

func wiringSyncOpts() wiring.SyncOptions {
	return wiring.SyncOptions{
		ServerName: "gridctl",
		GatewayURL: provisioner.GatewayHTTPURL(8180),
		Port:       8180,
	}
}

func TestRunWiringSync_JSONAndExitCodes(t *testing.T) {
	mgr, fake := wiringTestManager(t)
	var stdout, stderr bytes.Buffer

	exit := runWiringSync(context.Background(), &stdout, &stderr, mgr, wiringSyncOpts(), "json", false)
	if exit != ctxExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr.String())
	}
	var doc struct {
		SchemaVersion int  `json:"schema_version"`
		HasFailures   bool `json:"has_failures"`
		Results       []struct {
			Client string `json:"client"`
			Action string `json:"action"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if doc.SchemaVersion != projectJSONSchemaVersion || doc.HasFailures || len(doc.Results) != 1 || doc.Results[0].Action != wiring.ActionLinked {
		t.Errorf("doc = %+v", doc)
	}
	if fake.linked != 1 {
		t.Errorf("fake linked %d times, want 1", fake.linked)
	}
}

func TestRunWiringSync_ForeignEntryExitsAttention(t *testing.T) {
	mgr, fake := wiringTestManager(t)
	fake.entries = map[string]map[string]any{"gridctl": {"url": "https://example.com/mine"}}
	var stdout, stderr bytes.Buffer

	exit := runWiringSync(context.Background(), &stdout, &stderr, mgr, wiringSyncOpts(), "json", false)
	if exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1", exit)
	}
	var doc struct {
		Results []struct {
			Action      string `json:"action"`
			Detail      string `json:"detail"`
			Remediation string `json:"remediation"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	r := doc.Results[0]
	if r.Action != wiring.ActionSkippedForeign || r.Detail == "" || r.Remediation == "" {
		t.Errorf("structured fields missing: %+v", r)
	}
}

func TestRunWiringStatus_TableAndExitCodes(t *testing.T) {
	mgr, _ := wiringTestManager(t)
	var stdout, stderr bytes.Buffer

	// Detected but never linked: advisory missing row, exit 0.
	exit := runWiringStatus(context.Background(), &stdout, &stderr, mgr, wiring.StatusOptions{Port: 8180}, "text", true)
	if exit != ctxExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "missing") {
		t.Errorf("expected a missing row:\n%s", stdout.String())
	}

	// Link, then hand-edit: drifted row, exit 1.
	if exit := runWiringSync(context.Background(), &bytes.Buffer{}, &stderr, mgr, wiringSyncOpts(), "text", true); exit != ctxExitOK {
		t.Fatalf("sync exit = %d", exit)
	}
	fakeRegistry := mgr.Registry()
	prov, _ := fakeRegistry.FindBySlug("alpha")
	prov.(*fakeProvisioner).entries["gridctl"] = map[string]any{"url": "http://localhost:8180/mcp", "edited": true}

	stdout.Reset()
	exit = runWiringStatus(context.Background(), &stdout, &stderr, mgr, wiring.StatusOptions{Port: 8180}, "text", true)
	if exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1\n%s", exit, stdout.String())
	}
	if !strings.Contains(stdout.String(), "drifted") || !strings.Contains(stdout.String(), "adopt") {
		t.Errorf("expected drifted row with adopt remediation:\n%s", stdout.String())
	}
}

func TestRunWiringUnsyncAndAdopt(t *testing.T) {
	mgr, fake := wiringTestManager(t)
	var stdout, stderr bytes.Buffer
	if exit := runWiringSync(context.Background(), &stdout, &stderr, mgr, wiringSyncOpts(), "text", true); exit != ctxExitOK {
		t.Fatal("seed sync failed")
	}

	// Hand-edit, adopt, then unsync succeeds without force.
	fake.entries["gridctl"] = map[string]any{"url": "http://localhost:9999/mcp"}
	stdout.Reset()
	if exit := runWiringAdopt(context.Background(), &stdout, &stderr, mgr, "alpha", "gridctl", "text"); exit != ctxExitOK {
		t.Fatalf("adopt exit != 0: %s", stderr.String())
	}
	stdout.Reset()
	if exit := runWiringUnsync(context.Background(), &stdout, &stderr, mgr, "alpha", "gridctl", false, false, "json"); exit != ctxExitOK {
		t.Fatalf("unsync exit != 0: %s\n%s", stderr.String(), stdout.String())
	}
	if fake.unlinked != 1 {
		t.Errorf("fake unlinked %d times, want 1", fake.unlinked)
	}
}

func TestValidTopProjectKind(t *testing.T) {
	if err := validTopProjectKind("wiring"); err != nil {
		t.Errorf("wiring must be valid: %v", err)
	}
	if err := validTopProjectKind("skill"); err == nil {
		t.Error("skill must be rejected at the top-level verb for now")
	}
}
