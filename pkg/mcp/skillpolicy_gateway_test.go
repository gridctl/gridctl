package mcp

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"
)

// TestGateway_SkillPolicyFiltersPromptSurfaces covers the four exposure
// boundaries: a denied skill disappears from prompts/list and
// resources/list, and prompts/get / resources/read answer as if it does
// not exist. Removing the policy restores the full surface (hot-reload).
func TestGateway_SkillPolicyFiltersPromptSurfaces(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{Name: "allowed-skill", Description: "fine", Content: "body"},
			{Name: "secret-skill", Description: "hidden", Content: "body"},
		},
	}
	g.Router().AddClient(client)

	g.SetSkillPolicy(NewSkillPolicy(&SkillPolicySpec{Deny: []string{"secret-*"}}))

	prompts, err := g.HandlePromptsList(context.Background())
	if err != nil {
		t.Fatalf("prompts/list: %v", err)
	}
	decoded := decodePrompts(t, prompts.Prompts)
	if len(decoded) != 1 || decoded[0].Name != "allowed-skill" {
		t.Fatalf("prompts/list = %+v, want only allowed-skill", decoded)
	}

	if _, err := g.HandlePromptsGet(context.Background(), PromptsGetParams{Name: "secret-skill"}); err == nil {
		t.Fatal("prompts/get served a policy-denied skill")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("denied prompts/get error = %q, want a not-found-shaped error", err)
	}
	if _, err := g.HandlePromptsGet(context.Background(), PromptsGetParams{Name: "allowed-skill"}); err != nil {
		t.Fatalf("prompts/get of allowed skill: %v", err)
	}

	resources, err := g.HandleResourcesList(context.Background())
	if err != nil {
		t.Fatalf("resources/list: %v", err)
	}
	listed := decodeResources(t, resources.Resources)
	if len(listed) != 1 || listed[0].Name != "allowed-skill" {
		t.Fatalf("resources/list = %+v, want only allowed-skill", listed)
	}

	if _, err := g.HandleResourcesRead(context.Background(), ResourcesReadParams{URI: "skills://registry/secret-skill"}); err == nil {
		t.Fatal("resources/read served a policy-denied skill")
	}
	if _, err := g.HandleResourcesRead(context.Background(), ResourcesReadParams{URI: "skills://registry/allowed-skill"}); err != nil {
		t.Fatalf("resources/read of allowed skill: %v", err)
	}

	// Removing the policy (skills: block deleted, hot reload) restores the
	// full surface on the next request.
	g.SetSkillPolicy(nil)
	prompts, err = g.HandlePromptsList(context.Background())
	if err != nil {
		t.Fatalf("prompts/list after policy removal: %v", err)
	}
	if len(prompts.Prompts) != 2 {
		t.Fatalf("prompts/list after policy removal = %d prompts, want 2", len(prompts.Prompts))
	}
}
