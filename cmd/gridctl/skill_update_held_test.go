package main

import (
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
)

func TestUpdateWarnings_DropsHeldHookWarning(t *testing.T) {
	result := &skills.ImportResult{
		HeldHooks: []skills.HeldHook{{Name: "block", CompositeHash: "sha256:abc"}},
		Warnings:  []string{"other warning", skills.HeldHookWarning("block", "sha256:abc")},
	}
	got := updateWarnings(result)
	if len(got) != 1 || got[0] != "other warning" {
		t.Fatalf("warnings = %v", got)
	}
	if !result.IsHeldHook("block") || result.IsHeldHook("other") {
		t.Fatal("IsHeldHook mismatch")
	}
}

func TestHeldHookLine_NamesApproveCommand(t *testing.T) {
	line := heldHookLine(skills.HeldHook{Name: "block", CompositeHash: "sha256:abc"})
	want := "Held hook block: content changed since it was pinned; approve with gridctl skill pins approve block --kind hook --expect sha256:abc\n"
	if line != want {
		t.Fatalf("line = %q", line)
	}
	if strings.Contains(line, "Updated hook") {
		t.Fatal("held line says updated")
	}
}
