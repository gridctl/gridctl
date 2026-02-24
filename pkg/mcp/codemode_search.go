package mcp

import (
	"encoding/json"
	"strings"
)

// SearchIndex builds and queries a searchable catalog of MCP tools.
type SearchIndex struct {
	tools []Tool
}

// NewSearchIndex creates a search index from the given tools.
func NewSearchIndex(tools []Tool) *SearchIndex {
	return &SearchIndex{tools: tools}
}

// Search returns tools matching the query string. It searches tool names,
// descriptions, and parameter names. Results are returned with full schemas.
func (idx *SearchIndex) Search(query string) []Tool {
	if query == "" {
		return idx.tools
	}

	q := strings.ToLower(query)
	var matches []Tool

	for _, tool := range idx.tools {
		if idx.matchTool(tool, q) {
			matches = append(matches, tool)
		}
	}

	return matches
}

// matchTool checks if a tool matches the search query.
func (idx *SearchIndex) matchTool(tool Tool, query string) bool {
	if strings.Contains(strings.ToLower(tool.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(tool.Description), query) {
		return true
	}

	// Match against parameter names in input schema
	if len(tool.InputSchema) > 0 {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(tool.InputSchema, &schema) == nil {
			for paramName := range schema.Properties {
				if strings.Contains(strings.ToLower(paramName), query) {
					return true
				}
			}
		}
	}

	return false
}

// ToolCount returns the number of indexed tools.
func (idx *SearchIndex) ToolCount() int {
	return len(idx.tools)
}
