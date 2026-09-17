package main

import (
	"strings"
	"testing"
)

func TestSummarizeSchema_EnumsAndComposition(t *testing.T) {
	t.Run("integer enum", func(t *testing.T) {
		props, partial := summarizeSchema([]byte(`{"type":"object","properties":{"n":{"type":"integer","enum":[1,2]}}}`))
		if partial || len(props) != 1 {
			t.Fatalf("props=%v partial=%v", props, partial)
		}
		if got := strings.Join(props[0].enum, ","); got != "1,2" {
			t.Fatalf("enum = %q", got)
		}
	})
	t.Run("boolean and null enum", func(t *testing.T) {
		props, partial := summarizeSchema([]byte(`{"type":"object","properties":{"v":{"enum":[true,null]}}}`))
		if partial || len(props) != 1 {
			t.Fatalf("props=%v partial=%v", props, partial)
		}
		if got := strings.Join(props[0].enum, ","); got != "true,null" {
			t.Fatalf("enum = %q", got)
		}
	})
	t.Run("union type", func(t *testing.T) {
		props, partial := summarizeSchema([]byte(`{"type":"object","properties":{"v":{"type":["string","null"]}}}`))
		if partial || len(props) != 1 || props[0].typ != "string|null" {
			t.Fatalf("props=%v partial=%v", props, partial)
		}
	})
	t.Run("root allOf is partial", func(t *testing.T) {
		props, partial := summarizeSchema([]byte(`{"allOf":[{"properties":{"a":{"type":"string"}}}]}`))
		if !partial {
			t.Fatal("expected partial")
		}
		if len(props) != 0 {
			t.Fatalf("did not invent properties from allOf: %v", props)
		}
	})
	t.Run("root oneOf is partial", func(t *testing.T) {
		_, partial := summarizeSchema([]byte(`{"oneOf":[{"type":"string"},{"type":"number"}]}`))
		if !partial {
			t.Fatal("expected partial")
		}
	})
	t.Run("property ref is partial", func(t *testing.T) {
		props, partial := summarizeSchema([]byte(`{"type":"object","properties":{"a":{"$ref":"#/defs/a"}}}`))
		if !partial || len(props) != 1 || props[0].name != "a" {
			t.Fatalf("props=%v partial=%v", props, partial)
		}
	})
}

func TestCallHelpPositionals(t *testing.T) {
	args := []string{"call", "--format", "json", "echo__echo", "--help"}
	got := collectFlagAwarePositionals(callCmd, args)
	if len(got) != 1 || got[0] != "echo__echo" {
		t.Fatalf("got %v", got)
	}
	got = collectFlagAwarePositionals(callCmd, []string{"call", "--format", "json", "--help"})
	if len(got) != 0 {
		t.Fatalf("zero-target got %v", got)
	}
	got = collectFlagAwarePositionals(callCmd, []string{"call", "--home", "/tmp", "echo", "--help"})
	if len(got) != 1 || got[0] != "echo" {
		t.Fatalf("home value treated as target: %v", got)
	}
	got = collectFlagAwarePositionals(callCmd, []string{"call", "call", "--help"})
	if len(got) != 1 || got[0] != "call" {
		t.Fatalf("server named call: %v", got)
	}
}
