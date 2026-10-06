package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// HandlePromptsList returns registry skills (bare names, skill-policy
// filtered) followed by downstream prompts named server__prompt. An
// exact-name collision keeps the registry entry and drops the downstream
// one. An upstream cursor is ignored.
func (g *Gateway) HandlePromptsList(ctx context.Context) (*RawPromptsListResult, error) {
	registry, names, registryContributed := g.registryPromptEntries()
	pages := g.fanOutList(ctx, "prompts/list", wantsPrompts, g.listPromptsOf)

	entries := append([]json.RawMessage{}, registry...)
	var contributing []RawListPage
	for _, item := range pages {
		if item.err != nil {
			continue
		}
		if !g.serverVisible(ctx, item.name) {
			g.router.SetPromptCount(item.name, len(item.page.Entries))
			continue
		}
		kept := g.downstreamPromptEntries(item.name, item.page.Entries, names)
		g.router.SetPromptCount(item.name, len(kept))
		if len(kept) == 0 {
			continue
		}
		entries = append(entries, kept...)
		page := item.page
		page.Entries = kept
		contributing = append(contributing, page)
	}
	if entries == nil {
		entries = []json.RawMessage{}
	}
	result := &RawPromptsListResult{Prompts: entries}
	result.StatelessResultFields = aggregateRawListCacheMeta(registryContributed, contributing)
	return result, nil
}

func (g *Gateway) registryPromptEntries() ([]json.RawMessage, map[string]struct{}, bool) {
	names := map[string]struct{}{}
	pp := g.promptProvider()
	if pp == nil {
		return nil, names, false
	}
	policy := g.CurrentSkillPolicy()
	var entries []json.RawMessage
	for _, p := range pp.ListPromptData() {
		names[p.Name] = struct{}{}
		if !policy.Allows(p.Name) {
			continue
		}
		args := make([]PromptArgument, len(p.Arguments))
		for j, a := range p.Arguments {
			args[j] = PromptArgument{Name: a.Name, Description: a.Description, Required: a.Required}
		}
		raw, err := json.Marshal(MCPPrompt{Name: p.Name, Description: p.Description, Arguments: args})
		if err != nil {
			continue
		}
		entries = append(entries, raw)
	}
	return entries, names, len(entries) > 0
}

func (g *Gateway) downstreamPromptEntries(server string, entries []json.RawMessage, registryNames map[string]struct{}) []json.RawMessage {
	var kept []json.RawMessage
	for _, raw := range entries {
		rewritten, prefixed, ok := prefixPromptName(server, raw)
		if !ok {
			continue
		}
		if _, clash := registryNames[prefixed]; clash {
			g.logger.Warn("prompt name collision", "server", server, "prompt", prefixed, "registry", prefixed)
			continue
		}
		kept = append(kept, rewritten)
	}
	return kept
}

func prefixPromptName(server string, raw json.RawMessage) (json.RawMessage, string, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, "", false
	}
	name := jsonString(obj["name"])
	if name == "" {
		return nil, "", false
	}
	prefixed := PrefixTool(server, name)
	encoded, err := json.Marshal(prefixed)
	if err != nil {
		return nil, "", false
	}
	obj["name"] = encoded
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, "", false
	}
	return out, prefixed, true
}

// HandlePromptsGet resolves a bare name against the registry first. A name
// that parses as server__prompt routes to that server. Unknown names keep
// today's error text.
func (g *Gateway) HandlePromptsGet(ctx context.Context, params PromptsGetParams) (*PromptsGetResult, error) {
	outcome, err := g.relayPromptsGet(ctx, params, nil)
	if err != nil {
		return nil, err
	}
	var result PromptsGetResult
	if err := json.Unmarshal(outcome.raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (g *Gateway) relayPromptsGet(ctx context.Context, params PromptsGetParams, rawParams json.RawMessage) (relayResult, error) {
	if result, ok, err := g.registryPromptGet(ctx, params); ok || err != nil {
		return result, err
	}
	server, name, err := ParsePrefixedTool(params.Name)
	if err != nil || g.router.GetReplicaSet(server) == nil || !g.serverVisible(ctx, server) {
		return relayResult{}, g.unknownPrompt(params.Name)
	}
	rewritten := rewritePromptParams(rawParams, name, params)
	return g.relayToServer(ctx, server, "prompts/get", rewritten, nil)
}

func (g *Gateway) registryPromptGet(ctx context.Context, params PromptsGetParams) (relayResult, bool, error) {
	pp := g.promptProvider()
	if pp == nil {
		return relayResult{}, false, nil
	}
	p, err := pp.GetPromptData(params.Name)
	if err != nil {
		return relayResult{}, false, nil
	}
	if !g.CurrentSkillPolicy().Allows(params.Name) {
		return relayResult{}, true, fmt.Errorf("skill %q not found", params.Name)
	}
	content := p.Content
	for _, arg := range p.Arguments {
		placeholder := "{{" + arg.Name + "}}"
		value, ok := params.Arguments[arg.Name]
		if !ok {
			if arg.Default != "" {
				value = arg.Default
			} else if arg.Required {
				return relayResult{}, true, fmt.Errorf("required argument %q not provided", arg.Name)
			}
		}
		content = strings.ReplaceAll(content, placeholder, value)
	}
	g.mu.RLock()
	pObs := g.promptGetObserver
	g.mu.RUnlock()
	if pObs != nil {
		go pObs.ObservePromptGet(PromptGetObservation{
			PromptName: params.Name,
			ClientID:   ClientIDFromContext(ctx),
		})
	}
	raw, err := json.Marshal(PromptsGetResult{
		Description: p.Description,
		Messages: []PromptMessage{{
			Role:    "user",
			Content: NewTextContent(content),
		}},
	})
	if err != nil {
		return relayResult{}, true, err
	}
	return relayResult{
		raw: raw,
		fields: StatelessResultFields{
			ResultType: ResultTypeComplete,
		},
	}, true, nil
}

func (g *Gateway) unknownPrompt(name string) error {
	if g.promptProvider() == nil {
		return fmt.Errorf("registry not available")
	}
	return fmt.Errorf("skill %q not found", name)
}

func rewritePromptParams(raw json.RawMessage, name string, params PromptsGetParams) json.RawMessage {
	if len(raw) == 0 {
		encoded, err := json.Marshal(PromptsGetParams{Name: name, Arguments: params.Arguments})
		if err != nil {
			return nil
		}
		return encoded
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		encoded, _ := json.Marshal(PromptsGetParams{Name: name, Arguments: params.Arguments})
		return encoded
	}
	encoded, err := json.Marshal(name)
	if err != nil {
		return raw
	}
	obj["name"] = encoded
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}
