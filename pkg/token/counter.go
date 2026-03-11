// Package token provides token counting for MCP tool call content.
package token

import "encoding/json"

// Counter estimates token counts for text content.
// Implementations may vary in accuracy — the interface allows swapping
// a heuristic counter for a tiktoken-based one without changing consumers.
type Counter interface {
	// Count returns the estimated number of tokens in the given text.
	Count(text string) int
}

// HeuristicCounter estimates tokens using a simple bytes-per-token ratio.
// This is fast and zero-dependency but approximate. Suitable for visibility
// purposes where exact counts are not required.
type HeuristicCounter struct {
	bytesPerToken int
}

// NewHeuristicCounter creates a counter that estimates tokens at the given
// bytes-per-token ratio. A ratio of 4 is a reasonable default for English text.
func NewHeuristicCounter(bytesPerToken int) *HeuristicCounter {
	if bytesPerToken <= 0 {
		bytesPerToken = 4
	}
	return &HeuristicCounter{bytesPerToken: bytesPerToken}
}

// Count returns the estimated token count for the given text.
func (c *HeuristicCounter) Count(text string) int {
	n := len(text)
	if n == 0 {
		return 0
	}
	// Round up to avoid underestimating
	return (n + c.bytesPerToken - 1) / c.bytesPerToken
}

// CountJSON estimates the token count for a value by marshaling it to JSON first.
// This is useful for counting tool call arguments (map[string]any).
func CountJSON(c Counter, v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return c.Count(string(data))
}
