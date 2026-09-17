package mcp

import (
	"context"
	"errors"
	"sort"
)

// ErrDiscoverNotFound is returned when an exact server or canonical name is
// missing from the scoped live inventory.
var ErrDiscoverNotFound = errors.New("not found")

// DiscoverOptions selects a scoped live-tool inventory. Limit defaults to 20.
type DiscoverOptions struct {
	Server string
	Name   string
	Query  string
	Limit  int
}

// ToolDiscoverResult is the versioned live-tool discovery envelope.
type ToolDiscoverResult struct {
	SchemaVersion int    `json:"schema_version"`
	Client        string `json:"client"`
	Tools         []Tool `json:"tools"`
	TotalVisible  int    `json:"total_visible"`
	Matched       int    `json:"matched"`
	Returned      int    `json:"returned"`
	Truncated     bool   `json:"truncated"`
}

const (
	defaultDiscoverLimit = 20
	maxDiscoverLimit     = 200
)

// DiscoverTools returns the whitelist-filtered, client-scoped live tool
// inventory. Code mode does not change membership or search semantics.
// Search uses AggregatedTools descriptions so matching stays aligned with
// code-mode search, including the generated "MCP server:" prefix.
func (g *Gateway) DiscoverTools(ctx context.Context, opts DiscoverOptions) (*ToolDiscoverResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultDiscoverLimit
	}
	if limit > maxDiscoverLimit {
		limit = maxDiscoverLimit
	}

	tools := g.scopeToolsForContext(ctx, g.router.AggregatedTools())
	client := ClientAccessIDFromContext(ctx)
	if client == "" {
		client = ClientIDFromContext(ctx)
	}

	if opts.Name != "" {
		var found []Tool
		for _, t := range tools {
			if t.Name == opts.Name {
				found = append(found, t)
			}
		}
		if len(found) == 0 {
			return nil, ErrDiscoverNotFound
		}
		return &ToolDiscoverResult{
			SchemaVersion: 1,
			Client:        client,
			Tools:         found,
			TotalVisible:  1,
			Matched:       1,
			Returned:      1,
			Truncated:     false,
		}, nil
	}

	if opts.Server != "" {
		filtered := make([]Tool, 0, len(tools))
		for _, t := range tools {
			server, _, err := ParsePrefixedTool(t.Name)
			if err == nil && server == opts.Server {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			return nil, ErrDiscoverNotFound
		}
		tools = filtered
	}

	totalVisible := len(tools)
	matched := NewSearchIndex(tools).Search(opts.Query)
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Name < matched[j].Name
	})
	nMatched := len(matched)
	returned := matched
	if len(returned) > limit {
		returned = returned[:limit]
	}
	if returned == nil {
		returned = []Tool{}
	}
	return &ToolDiscoverResult{
		SchemaVersion: 1,
		Client:        client,
		Tools:         returned,
		TotalVisible:  totalVisible,
		Matched:       nMatched,
		Returned:      len(returned),
		Truncated:     len(returned) < nMatched,
	}, nil
}

func isMetaToolName(name string) bool {
	return name == MetaToolSearch || name == MetaToolExecute
}

func canonicalHalves(name string) (server, tool string, ok bool) {
	server, tool, err := ParsePrefixedTool(name)
	if err != nil || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

func (g *Gateway) canonicalInventoryHas(name string) bool {
	if isMetaToolName(name) {
		return false
	}
	if _, _, ok := canonicalHalves(name); !ok {
		return false
	}
	for _, t := range g.router.AggregatedTools() {
		if t.Name == name {
			return true
		}
	}
	return false
}

func replicaAdvertisesTool(replica *Replica, toolName string) bool {
	if replica == nil || replica.Client() == nil {
		return false
	}
	for _, t := range replica.Client().Tools() {
		if t.Name == toolName {
			return true
		}
	}
	return false
}
