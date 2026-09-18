package mcp

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/a2aclient"
)

func a2aToolTestCard() *a2aclient.Card {
	return &a2aclient.Card{
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"image/png", "application/json", "text/plain"},
		Skills:             []a2aclient.Skill{{ID: "Ask / Expert", Description: "Preserve this text.\n  Including spacing."}},
	}
}

func TestA2ATools_SchemasAndAdvisory(t *testing.T) {
	set, err := buildA2ATools("agent", A2AClientConfig{}, a2aToolTestCard(), "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.tools) != 4 || set.tools[3].Name != "skill-Ask-Expert" || set.tools[3].Description != a2aToolTestCard().Skills[0].Description+a2aSkillAdvisory {
		t.Fatalf("unexpected tools: %+v", set.tools)
	}
	for _, tool := range set.tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if schema["additionalProperties"] != false || schema["type"] != "object" {
			t.Fatalf("schema is not closed: %s", tool.InputSchema)
		}
		properties := schema["properties"].(map[string]any)
		_, skillID := properties["skill_id"]
		if skillID != (tool.Name == "send") {
			t.Fatal("skill selector exposed on wrong tool")
		}
	}
}

func TestA2ATools_CompatibilityAndNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*a2aclient.Card, *A2AClientConfig, *string)
		bad   bool
		count int
	}{
		{"missing included", func(_ *a2aclient.Card, c *A2AClientConfig, _ *string) { c.Include = []string{"missing"} }, true, 0},
		{"agent lacks text", func(c *a2aclient.Card, _ *A2AClientConfig, _ *string) {
			c.DefaultInputModes = []string{"application/json"}
		}, true, 0},
		{"empty id", func(c *a2aclient.Card, _ *A2AClientConfig, _ *string) { c.Skills[0].ID = "" }, true, 0},
		{"duplicate id", func(c *a2aclient.Card, _ *A2AClientConfig, _ *string) { c.Skills = append(c.Skills, c.Skills[0]) }, true, 0},
		{"sanitized collision", func(c *a2aclient.Card, _ *A2AClientConfig, _ *string) {
			c.Skills = append(c.Skills, a2aclient.Skill{ID: "Ask-Expert"})
		}, true, 0},
		{"long skill", func(c *a2aclient.Card, _ *A2AClientConfig, _ *string) { c.Skills[0].ID = strings.Repeat("a", 52) }, true, 0},
		{"long server", func(_ *a2aclient.Card, _ *A2AClientConfig, s *string) { *s = strings.Repeat("a", 52) }, true, 0},
		{"invalid server", func(_ *a2aclient.Card, _ *A2AClientConfig, s *string) { *s = "agent.dot" }, true, 0},
		{"omit incompatible", func(c *a2aclient.Card, _ *A2AClientConfig, _ *string) { c.Skills[0].InputModes = []string{"image/png"} }, false, 3},
		{"included incompatible", func(c *a2aclient.Card, cfg *A2AClientConfig, _ *string) {
			c.Skills[0].InputModes = []string{"image/png"}
			cfg.Include = []string{c.Skills[0].ID}
		}, true, 0},
		{"empty skills", func(c *a2aclient.Card, _ *A2AClientConfig, _ *string) { c.Skills = nil }, false, 3},
		{"include preserves send", func(c *a2aclient.Card, cfg *A2AClientConfig, _ *string) {
			cfg.Include = []string{c.Skills[0].ID}
			c.Skills = append(c.Skills, a2aclient.Skill{ID: "other"})
		}, false, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, cfg, server := a2aToolTestCard(), A2AClientConfig{}, "agent"
			tc.edit(card, &cfg, &server)
			set, err := buildA2ATools(server, cfg, card, "0.3")
			if (err != nil) != tc.bad {
				t.Fatalf("error = %v", err)
			}
			if !tc.bad && len(set.tools) != tc.count {
				t.Fatalf("tool count = %d", len(set.tools))
			}
			if tc.name == "omit incompatible" && len(set.omitted) != 1 {
				t.Fatal("missing omission reason")
			}
		})
	}
}

func TestA2ATools_Arguments(t *testing.T) {
	set, err := buildA2ATools("agent", A2AClientConfig{}, a2aToolTestCard(), "1.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tool string
		args       map[string]any
		bad        bool
	}{
		{"empty message allowed", "send", map[string]any{"message": ""}, false},
		{"missing message", "send", nil, true},
		{"wrong message", "send", map[string]any{"message": 1}, true},
		{"raw id", "send", map[string]any{"message": "x", "context_id": "x"}, true},
		{"session override", "send", map[string]any{"message": "x", "session_id": "x"}, true},
		{"wire object", "send", map[string]any{"message": "x", "parts": []any{}}, true},
		{"empty handle", "send", map[string]any{"message": "x", "context_handle": ""}, true},
		{"null handle", "send", map[string]any{"message": "x", "context_handle": nil}, true},
		{"task only", "send", map[string]any{"message": "x", "task_handle": "x"}, true},
		{"data array", "send", map[string]any{"message": "x", "data": []any{}}, true},
		{"null data", "send", map[string]any{"message": "x", "data": nil}, true},
		{"empty data", "send", map[string]any{"message": "x", "data": map[string]any{}}, false},
		{"oversize input", "send", map[string]any{"message": strings.Repeat("a", 256<<10), "data": map[string]any{}}, true},
		{"exact input limit", "send", map[string]any{"message": strings.Repeat("a", 256<<10)}, false},
		{"immediate", "send", map[string]any{"message": "x", "return_immediately": true}, false},
		{"wrong immediate", "send", map[string]any{"message": "x", "return_immediately": "true"}, true},
		{"unknown skill", "send", map[string]any{"message": "x", "skill_id": "missing"}, true},
		{"selected skill", "send", map[string]any{"message": "x", "skill_id": "Ask / Expert"}, false},
		{"advisory skill", "skill-Ask-Expert", map[string]any{"message": "x"}, false},
		{"skill override", "skill-Ask-Expert", map[string]any{"message": "x", "skill_id": "Ask / Expert"}, true},
		{"missing task", "task_get", nil, true},
		{"get context", "task_get", map[string]any{"task_handle": "x", "context_handle": "y"}, true},
		{"get history", "task_get", map[string]any{"task_handle": "x", "history_length": float64(100)}, false},
		{"get fractional history", "task_get", map[string]any{"task_handle": "x", "history_length": 1.5}, true},
		{"get excessive history", "task_get", map[string]any{"task_handle": "x", "history_length": 101}, true},
		{"cancel history", "task_cancel", map[string]any{"task_handle": "x", "history_length": 0}, true},
		{"cancel", "task_cancel", map[string]any{"task_handle": "x"}, false},
		{"unknown tool", "_agent_card", map[string]any{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := set.arguments(tc.tool, tc.args)
			if (err != nil) != tc.bad {
				t.Fatalf("error = %v", err)
			}
			if !tc.bad && parsed.operation == capabilitySend && len(parsed.request.OutputModes) != 2 {
				t.Fatal("output intersection lost")
			}
		})
	}
}

func TestA2ATools_IncludeAndDataRestrictions(t *testing.T) {
	card := a2aToolTestCard()
	card.Skills = append(card.Skills, a2aclient.Skill{ID: "other"})
	card.Skills[0].InputModes = []string{"text/plain"}
	set, err := buildA2ATools("agent", A2AClientConfig{Include: []string{card.Skills[0].ID}}, card, "0.3")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{
		{"message": "x", "skill_id": "other"},
		{"message": "x", "skill_id": card.Skills[0].ID, "data": map[string]any{}},
	} {
		if _, err := set.arguments("send", args); err == nil {
			t.Fatal("accepted incompatible selection")
		}
	}
	if _, err := set.arguments("send", map[string]any{"message": "x", "data": map[string]any{}}); err != nil {
		t.Fatal("generic agent compatibility should still allow data", err)
	}
	_, err = set.arguments("send", map[string]any{"message": "x", "task_handle": "x"})
	if !errors.Is(err, errCapabilityUnavailable) {
		t.Fatalf("task-only error = %v", err)
	}
}

func TestA2AHistoryLength(t *testing.T) {
	for _, v := range []any{float64(0), float64(100), 42, json.Number("10")} {
		if _, ok := a2aHistoryLength(v); !ok {
			t.Fatalf("rejected %v", v)
		}
	}
	for _, v := range []any{-1, 101, 1.5, math.NaN(), math.Inf(1), "1", nil, json.Number("1.5"), json.Number("bad")} {
		if _, ok := a2aHistoryLength(v); ok {
			t.Fatalf("accepted %v", v)
		}
	}
}
