package api

import (
	"net/http"
	"time"
)

// toolUsageStat is the wire shape for one tool's usage in
// GET /api/tools/usage. LastCalledAt is a pointer so a tool that has a
// recorded call count but a zero timestamp (or never-called tools the UI
// cross-references from the status list) renders as null rather than the
// Go zero time. CostUSD is a pointer for the same honesty rule every cost
// surface follows: absent means "no priced calls", never $0.
type toolUsageStat struct {
	Calls        int64      `json:"calls"`
	LastCalledAt *time.Time `json:"lastCalledAt,omitempty"`
	InputTokens  int64      `json:"inputTokens,omitempty"`
	OutputTokens int64      `json:"outputTokens,omitempty"`
	CostUSD      *float64   `json:"costUsd,omitempty"`
}

// toolUsageResponse is the GET /api/tools/usage envelope. Servers maps each
// MCP server name to its per-tool usage. ObservedSince is when this gateway
// process began recording calls; with metrics persistence enabled the
// restored counts may predate it. The Audit Mode UI uses ObservedSince to
// stay honest: a tool with no recorded calls may simply predate the tracking
// horizon rather than being genuinely unused.
type toolUsageResponse struct {
	ObservedSince *time.Time                          `json:"observedSince,omitempty"`
	Servers       map[string]map[string]toolUsageStat `json:"servers"`
}

// handleToolsUsage serves GET /api/tools/usage: per-(server, tool) cumulative
// call counts, last-called timestamps, token counts, and estimated cost from
// the metrics accumulator. The
// data is seeded from disk on startup (telemetry.MetricsFlusher.SeedFromFile)
// so it survives gateway restarts for servers with metrics persistence
// enabled. Returns 503 when no accumulator is wired (no observation data
// yet), mirroring GET /api/optimize.
//
// Servers is always a non-nil object so the JSON body is "{}" rather than
// null when nothing has been called.
func (s *Server) handleToolsUsage(w http.ResponseWriter, _ *http.Request) {
	if s.metricsAccumulator == nil {
		writeJSONError(w, "metrics accumulator not configured", http.StatusServiceUnavailable)
		return
	}

	resp := toolUsageResponse{Servers: map[string]map[string]toolUsageStat{}}
	if started := s.metricsAccumulator.StartedAt(); !started.IsZero() {
		t := started.UTC()
		resp.ObservedSince = &t
	}
	for serverName, tools := range s.metricsAccumulator.ToolUsageSnapshot() {
		inner := make(map[string]toolUsageStat, len(tools))
		for toolName, stat := range tools {
			entry := toolUsageStat{
				Calls:        stat.Calls,
				InputTokens:  stat.InputTokens,
				OutputTokens: stat.OutputTokens,
			}
			if !stat.LastCalledAt.IsZero() {
				t := stat.LastCalledAt.UTC()
				entry.LastCalledAt = &t
			}
			if stat.CostMicroUSD > 0 {
				usd := stat.CostUSD()
				entry.CostUSD = &usd
			}
			inner[toolName] = entry
		}
		resp.Servers[serverName] = inner
	}
	writeJSON(w, resp)
}
