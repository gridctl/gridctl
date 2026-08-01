package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// fakeProvisioner is a stateful in-memory ClientProvisioner that records
// link and unlink calls; entries live in memory so the wiring ownership
// manager can read them back.
type fakeProvisioner struct {
	name     string
	entries  map[string]map[string]any
	linked   int
	unlinked int
}

func (f *fakeProvisioner) Name() string           { return f.name }
func (f *fakeProvisioner) Slug() string           { return strings.ToLower(f.name) }
func (f *fakeProvisioner) Detect() (string, bool) { return "/tmp/" + f.name + ".json", true }
func (f *fakeProvisioner) IsLinked(_, serverName string) (bool, error) {
	_, ok := f.entries[serverName]
	return ok, nil
}
func (f *fakeProvisioner) Link(_ string, opts provisioner.LinkOptions) error {
	if f.entries == nil {
		f.entries = map[string]map[string]any{}
	}
	f.entries[opts.ServerName] = f.PlannedEntry(opts)
	f.linked++
	return nil
}
func (f *fakeProvisioner) Unlink(_, serverName string) error {
	delete(f.entries, serverName)
	f.unlinked++
	return nil
}
func (f *fakeProvisioner) NeedsBridge() bool { return false }
func (f *fakeProvisioner) ListServers(string) ([]provisioner.ServerEntry, error) {
	var out []provisioner.ServerEntry
	for name, raw := range f.entries {
		out = append(out, provisioner.ServerEntry{Name: name, Raw: raw})
	}
	return out, nil
}
func (f *fakeProvisioner) PlannedEntry(opts provisioner.LinkOptions) map[string]any {
	return map[string]any{"url": opts.GatewayURL, "name": opts.ServerName}
}

// seedRecordedLink links a fake through the ownership manager so its
// entry exists AND is recorded, the precondition for unlink to remove it.
func seedRecordedLink(t *testing.T, f *fakeProvisioner) {
	t.Helper()
	mgr, err := wiring.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	path, _ := f.Detect()
	res, err := mgr.LinkClient(context.Background(), f, path, provisioner.LinkOptions{ServerName: "gridctl", GatewayURL: "http://localhost:8180/mcp", Port: 8180})
	if err != nil || res.Action != wiring.ActionLinked {
		t.Fatalf("seed link: action=%q err=%v", res.Action, err)
	}
	f.linked = 0 // the seed call is setup, not the behavior under test
}

// swapSelector replaces the interactive client selector for one test.
func swapSelector(t *testing.T, fn func(string, []provisioner.DetectedClient) ([]provisioner.DetectedClient, error)) {
	t.Helper()
	orig := clientSelector
	clientSelector = fn
	t.Cleanup(func() { clientSelector = orig })
}

func detectedFakes(names ...string) ([]provisioner.DetectedClient, []*fakeProvisioner) {
	var detected []provisioner.DetectedClient
	var fakes []*fakeProvisioner
	for _, n := range names {
		f := &fakeProvisioner{name: n}
		path, _ := f.Detect()
		detected = append(detected, provisioner.DetectedClient{Provisioner: f, ConfigPath: path})
		fakes = append(fakes, f)
	}
	return detected, fakes
}

func TestLinkSelectedLinksOnlySelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	detected, fakes := detectedFakes("Alpha", "Beta")
	swapSelector(t, func(command string, clients []provisioner.DetectedClient) ([]provisioner.DetectedClient, error) {
		if command != "link" {
			t.Errorf("selector command = %q, want link", command)
		}
		return clients[:1], nil
	})

	var buf bytes.Buffer
	printer := output.NewWithWriter(&buf)
	if err := linkSelected(context.Background(), printer, detected, provisioner.LinkOptions{ServerName: "gridctl"}); err != nil {
		t.Fatalf("linkSelected: %v", err)
	}
	if fakes[0].linked != 1 || fakes[1].linked != 0 {
		t.Errorf("expected only Alpha linked, got alpha=%d beta=%d", fakes[0].linked, fakes[1].linked)
	}
}

func TestLinkSelectedAbortWritesNothing(t *testing.T) {
	detected, fakes := detectedFakes("Alpha", "Beta")
	swapSelector(t, func(string, []provisioner.DetectedClient) ([]provisioner.DetectedClient, error) {
		return nil, errPromptCancelled
	})

	var buf bytes.Buffer
	err := linkSelected(context.Background(), output.NewWithWriter(&buf), detected, provisioner.LinkOptions{})
	if !errors.Is(err, errPromptCancelled) {
		t.Fatalf("abort should surface errPromptCancelled, got %v", err)
	}
	if fakes[0].linked != 0 || fakes[1].linked != 0 {
		t.Error("aborted form must not write any config")
	}
}

func TestUnlinkSelectedAutoSingleSkipsPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	detected, fakes := detectedFakes("Alpha")
	seedRecordedLink(t, fakes[0])
	swapSelector(t, func(string, []provisioner.DetectedClient) ([]provisioner.DetectedClient, error) {
		t.Fatal("selector must not run for a single linked client")
		return nil, nil
	})

	var buf bytes.Buffer
	if err := unlinkSelected(context.Background(), output.NewWithWriter(&buf), detected); err != nil {
		t.Fatalf("unlinkSelected: %v", err)
	}
	if fakes[0].unlinked != 1 {
		t.Errorf("expected auto-unlink of the only linked client, got %d", fakes[0].unlinked)
	}
}

func TestUnlinkSelectedMultiUsesSelector(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	detected, fakes := detectedFakes("Alpha", "Beta")
	seedRecordedLink(t, fakes[0])
	seedRecordedLink(t, fakes[1])
	swapSelector(t, func(_ string, clients []provisioner.DetectedClient) ([]provisioner.DetectedClient, error) {
		return clients[1:], nil
	})

	var buf bytes.Buffer
	if err := unlinkSelected(context.Background(), output.NewWithWriter(&buf), detected); err != nil {
		t.Fatalf("unlinkSelected: %v", err)
	}
	if fakes[0].unlinked != 0 || fakes[1].unlinked != 1 {
		t.Errorf("expected only Beta unlinked, got alpha=%d beta=%d", fakes[0].unlinked, fakes[1].unlinked)
	}
}

func TestRequireInteractiveStdinNonTTY(t *testing.T) {
	// go test runs with a non-terminal stdin, so the guard must fail and
	// name every non-interactive alternative.
	err := requireInteractiveStdin("link")
	if err == nil {
		t.Skip("stdin is a terminal in this environment")
	}
	for _, want := range []string{"--all", "gridctl link claude", "not a terminal"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("guard error missing %q: %v", want, err)
		}
	}
}

func TestHuhSelectorGuardsNonTTY(t *testing.T) {
	// The production selector itself carries the guard, so paths that
	// never prompt (zero clients, single-client auto-unlink) stay
	// script-safe while any real prompt fails fast without a terminal.
	if requireInteractiveStdin("link") == nil {
		t.Skip("stdin is a terminal in this environment")
	}
	detected, fakes := detectedFakes("Alpha")
	_, err := huhSelectClients("link", detected)
	if err == nil {
		t.Fatal("huh selector on non-TTY stdin should error before prompting")
	}
	if !strings.Contains(err.Error(), "--all") {
		t.Errorf("guard error should name --all: %v", err)
	}
	if fakes[0].linked != 0 {
		t.Error("guard must fire before any config write")
	}
}
