package api

import (
	"net/http"
	"strconv"
	"strings"

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
//   - min_impact: USD-per-week threshold; findings with impact below this
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
		MinImpactUSDPerWeek: parseFloatQuery(r, "min_impact"),
		SeverityFilter:      parseSeverityFilter(r),
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
		costSnap := acc.CostSnapshot()
		stats.Usage = make(map[string]optimize.ServerUsage, len(usage.PerServer))
		for name, counts := range usage.PerServer {
			cost := costSnap.PerServer[name]
			stats.Usage[name] = optimize.ServerUsage{
				InputTokens:  counts.InputTokens,
				OutputTokens: counts.OutputTokens,
				TotalTokens:  counts.TotalTokens,
				TotalCostUSD: cost.TotalUSD,
			}
		}
		if toolSnap := acc.ToolUsageSnapshot(); len(toolSnap) > 0 {
			stats.ToolUsage = make(map[string]map[string]optimize.ToolStat, len(toolSnap))
			for serverName, tools := range toolSnap {
				inner := make(map[string]optimize.ToolStat, len(tools))
				for toolName, stat := range tools {
					inner[toolName] = optimize.ToolStat{Calls: stat.Calls, LastCalledAt: stat.LastCalledAt}
				}
				stats.ToolUsage[serverName] = inner
			}
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
			})
		}
	}
	return stats
}

// parseFloatQuery returns the float64 value of a query parameter, or 0
// when the parameter is unset or unparseable.
func parseFloatQuery(r *http.Request, key string) float64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
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
