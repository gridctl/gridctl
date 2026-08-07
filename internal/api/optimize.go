package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/optimize"
)

// handleOptimize handles GET /api/optimize and produces an
// OptimizeReport derived from the live gateway state and accumulator
// snapshot. Returns:
//
//   - 200 with the JSON report on success.
//   - 404 when stack=<name> is supplied and does not match the active
//     stack (so the CLI can surface a helpful error).
//   - 503 when the API server has no metrics accumulator wired (no
//     observation data yet).
//
// Query parameters (all optional):
//   - stack:      validate against the running stack name; mismatch is 404.
//   - min_impact: tokens-per-week threshold; findings with impact below this
//     are dropped (info findings remain so the report stays informative).
//   - severity:   comma-separated severity allowlist (info, warn, critical).
func (s *Server) handleOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if requested := r.URL.Query().Get("stack"); requested != "" && s.stackName != "" && requested != s.stackName {
		writeJSONError(w, "stack '"+requested+"' is not the active stack ('"+s.stackName+"')", http.StatusNotFound)
		return
	}

	if s.metricsAccumulator == nil {
		writeJSONError(w, "metrics accumulator not configured", http.StatusServiceUnavailable)
		return
	}

	stats := s.optimizeStats()
	opts := optimize.Options{
		MinImpactTokensPerWeek: parseIntQuery(r, "min_impact"),
		SeverityFilter:         parseSeverityFilter(r),
	}

	report := optimize.Analyze(stats, opts)

	// Ensure non-nil slice for stable JSON serialization.
	if report.Findings == nil {
		report.Findings = []optimize.Finding{}
	}
	writeJSON(w, report)
}

// optimizeStats assembles the input snapshot for optimize.Analyze from
// the gateway's registered servers and the accumulator's per-server +
// per-tool aggregates.
func (s *Server) optimizeStats() optimize.Stats {
	stats := optimize.Stats{StackName: s.stackName}
	if acc := s.metricsAccumulator; acc != nil {
		stats.ObservationStart = acc.StartedAt()
		usage := acc.Snapshot()
		stats.Usage = make(map[string]optimize.ServerUsage, len(usage.PerServer))
		for name, counts := range usage.PerServer {
			stats.Usage[name] = optimize.ServerUsage{
				InputTokens:  counts.InputTokens,
				OutputTokens: counts.OutputTokens,
				TotalTokens:  counts.TotalTokens,
			}
		}
		if toolSnap := acc.ToolUsageSnapshot(); len(toolSnap) > 0 {
			stats.ToolUsage = make(map[string]map[string]optimize.ToolStat, len(toolSnap))
			for serverName, tools := range toolSnap {
				inner := make(map[string]optimize.ToolStat, len(tools))
				for toolName, stat := range tools {
					inner[toolName] = optimize.ToolStat{
						Calls:        stat.Calls,
						LastCalledAt: stat.LastCalledAt,
						InputTokens:  stat.InputTokens,
						OutputTokens: stat.OutputTokens,
					}
				}
				stats.ToolUsage[serverName] = inner
			}
		}
		snap := acc.Snapshot()
		stats.FormatBaseline = optimize.FormatBaseline{
			OriginalTokens:  snap.FormatSavings.OriginalTokens,
			FormattedTokens: snap.FormatSavings.FormattedTokens,
			SavingsPercent:  snap.FormatSavings.SavingsPercent,
		}
	}
	if s.gateway != nil {
		gwStatus := s.gateway.Status()
		stats.Servers = make([]optimize.ServerInfo, 0, len(gwStatus))
		for _, ms := range gwStatus {
			stats.Servers = append(stats.Servers, optimize.ServerInfo{
				Name:          ms.Name,
				Tools:         ms.Tools,
				ToolWhitelist: ms.ToolWhitelist,
				Initialized:   ms.Initialized,
				OutputFormat:  ms.OutputFormat,
			})
		}
		stats.PinStats, stats.ToolSchemaTokens = computeSchemaTokens(s.gateway)
	}
	return stats
}

// computeSchemaTokens estimates schema-overhead tokens by marshaling the
// live tool list through the gateway and applying a chars-per-token
// heuristic, aggregated per server (for schema_overhead) and per tool
// (for unused_tool impact) in one pass. The pin store's PinRecord has
// SHA256 hashes only, not byte counts, so we go to the live source. The
// consuming heuristics treat this as a measurement, not a guess — every
// byte counted here is a byte the gateway actually shipped on the last
// tools/list response.
func computeSchemaTokens(gateway *mcp.Gateway) (map[string]optimize.PinStat, map[string]map[string]int) {
	if gateway == nil {
		return nil, nil
	}
	result, err := gateway.HandleToolsListUnscoped()
	if err != nil || result == nil || len(result.Tools) == 0 {
		return nil, nil
	}
	bytesPerServer := make(map[string]int, len(result.Tools))
	tokensPerTool := make(map[string]map[string]int, len(result.Tools))
	for _, tool := range result.Tools {
		serverName, toolName, ok := splitPrefixedTool(tool.Name)
		if !ok {
			continue
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		bytesPerServer[serverName] += len(raw)
		inner, ok := tokensPerTool[serverName]
		if !ok {
			inner = make(map[string]int)
			tokensPerTool[serverName] = inner
		}
		// Approximate token count via the OpenAI rule-of-thumb of ~4
		// characters per token. JSON Schemas trend slightly token-dense
		// because of curly braces and quoting, but ~4 is the right
		// order of magnitude and we don't need precision for a
		// threshold-driven heuristic.
		inner[toolName] = len(raw) / 4
	}
	if len(bytesPerServer) == 0 {
		return nil, nil
	}
	out := make(map[string]optimize.PinStat, len(bytesPerServer))
	for name, bytes := range bytesPerServer {
		out[name] = optimize.PinStat{SchemaTokens: bytes / 4}
	}
	return out, tokensPerTool
}

// splitPrefixedTool extracts the server name from the gateway's
// "<server>__<tool>" prefix shape. The mcp package owns the delimiter
// constant; we mirror the value here so this helper can stay
// stdlib-only and avoid a circular import.
func splitPrefixedTool(prefixed string) (server, tool string, ok bool) {
	const delim = "__"
	idx := strings.Index(prefixed, delim)
	if idx <= 0 || idx+len(delim) >= len(prefixed) {
		return "", "", false
	}
	return prefixed[:idx], prefixed[idx+len(delim):], true
}

// parseIntQuery returns the int64 value of a query parameter, or 0 when
// the parameter is unset or unparseable.
func parseIntQuery(r *http.Request, key string) int64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseSeverityFilter splits a comma-separated severity list, dropping
// unknown values silently — the API is permissive so a bad filter does
// not 400 the caller.
func parseSeverityFilter(r *http.Request) []optimize.Severity {
	v := r.URL.Query().Get("severity")
	if v == "" {
		return nil
	}
	var out []optimize.Severity
	for _, raw := range strings.Split(v, ",") {
		raw = strings.TrimSpace(raw)
		switch optimize.Severity(raw) {
		case optimize.SeverityInfo, optimize.SeverityWarn, optimize.SeverityCritical:
			out = append(out, optimize.Severity(raw))
		}
	}
	return out
}
