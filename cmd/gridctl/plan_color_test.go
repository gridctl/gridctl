package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestPrintPlanDiffSymbolsNoColorWhenPiped(t *testing.T) {
	diff := &config.PlanDiff{
		HasChanges: true,
		Summary:    "1 to add, 1 to change, 1 to destroy",
		Items: []config.DiffItem{
			{Action: config.DiffAdd, Kind: "mcp-server", Name: "foo"},
			{Action: config.DiffChange, Kind: "mcp-server", Name: "bar", Details: []string{"image: a -> b"}},
			{Action: config.DiffRemove, Kind: "mcp-server", Name: "baz"},
		},
	}

	var buf bytes.Buffer
	printPlanDiff(&buf, diff)
	out := buf.String()

	for _, want := range []string{`+ mcp-server "foo" (add)`, `~ mcp-server "bar" (update)`, `- mcp-server "baz" (destroy)`, "image: a -> b"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "\033") {
		t.Errorf("plan output must be colorless on a non-TTY writer, got %q", out)
	}
}

func TestPrintPlanDiffNoChanges(t *testing.T) {
	var buf bytes.Buffer
	printPlanDiff(&buf, &config.PlanDiff{HasChanges: false})
	if !strings.Contains(buf.String(), "No changes") {
		t.Errorf("expected no-changes message, got %q", buf.String())
	}
}
