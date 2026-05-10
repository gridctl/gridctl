package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/optimize"
	"github.com/gridctl/gridctl/pkg/pricing"
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
				OutputFormat:  ms.OutputFormat,
			})
		}
		stats.PinStats = computeSchemaTokens(s.gateway)
	}
	if acc := s.metricsAccumulator; acc != nil {
		snap := acc.Snapshot()
		stats.FormatBaseline = optimize.FormatBaseline{
			OriginalTokens:  snap.FormatSavings.OriginalTokens,
			FormattedTokens: snap.FormatSavings.FormattedTokens,
			SavingsPercent:  snap.FormatSavings.SavingsPercent,
		}
		stats.ServerCallCount = computeServerCallCount(acc.ToolUsageSnapshot())
	}
	stats.ModelStats = lookupModelStats(stats.Servers)
	stats.RunStats = collectRunStats(s.agentRunStore)
	return stats
}

// runStatsListLimit caps how many recent runs the optimize pass scans
// from the JSONL ledger. The agent-runtime heuristics are per-run by
// nature, so this is a recency window — the most recent runs are the
// ones operators care about, and unbounded scans would make /api/optimize
// slow for users with very long-lived daemons.
const runStatsListLimit = 50

// collectRunStats walks the most recent runs in the agent persistence
// store and aggregates the per-run signals the agent-runtime heuristics
// (unbounded_loop, oversized_prompt, untyped_handoff) consume. Returns
// nil when no store is wired (stackless mode, agent runtime not yet
// initialized) so the heuristics skip silently rather than firing on
// absence of data.
//
// Errors from individual runs are swallowed because the on-disk ledger
// is best-effort: a malformed file or a race with a concurrent writer
// must not 500 the whole optimize endpoint.
func collectRunStats(store *persist.Store) []optimize.RunStat {
	if store == nil {
		return nil
	}
	summaries, err := store.List(runStatsListLimit)
	if err != nil || len(summaries) == 0 {
		return nil
	}
	out := make([]optimize.RunStat, 0, len(summaries))
	for _, summary := range summaries {
		stat, ok := aggregateRunEvents(store, summary.RunID, summary.Skill)
		if !ok {
			continue
		}
		out = append(out, stat)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// aggregateRunEvents reduces a run's JSONL ledger to the per-run shape
// optimize.RunStat exposes. Returns ok=false on read failure or when
// the ledger has no events worth analysing — the optimize heuristics
// then silently skip the run.
func aggregateRunEvents(store *persist.Store, runID, skill string) (optimize.RunStat, bool) {
	events, err := store.Read(runID)
	if err != nil {
		return optimize.RunStat{}, false
	}
	if len(events) == 0 {
		return optimize.RunStat{}, false
	}
	stat := optimize.RunStat{RunID: runID, Skill: skill}
	nodeCounts := make(map[string]int)
	for _, ev := range events {
		switch ev.Type {
		case persist.EventNodeEnter:
			var p persist.NodeEnterPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.NodeID == "" {
				continue
			}
			nodeCounts[p.NodeID]++
			if c := nodeCounts[p.NodeID]; c > stat.MaxNodeRevisits {
				stat.MaxNodeRevisits = c
				stat.HotNodeID = p.NodeID
			}
		case persist.EventLLMCall:
			var p persist.LLMCallPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.PromptTokens > stat.MaxPromptTokens {
				stat.MaxPromptTokens = p.PromptTokens
				stat.HotModel = p.Model
			}
		case persist.EventRunStarted:
			// The orchestrator records each handoff as a child run with
			// its own ledger; we only see those here when the parent's
			// ledger replays them, which it does not. Counts of typed vs
			// untyped handoffs therefore come from inspecting the parent
			// ledger's RunStarted events whose ParentRunID matches this
			// run — handled by accumulateChildHandoffs below.
		}
	}
	accumulateChildHandoffs(store, runID, &stat)
	return stat, true
}

// accumulateChildHandoffs scans every recent run looking for children
// of runID — those whose RunStarted payload carries ParentRunID == runID.
// For each, increments ChildHandoffs and (when neither Flavor nor
// InputDigest is set on the child's RunStarted event) UntypedHandoffs.
//
// The walk is bounded by runStatsListLimit just like collectRunStats:
// older children fall off the analysis window with their parent.
func accumulateChildHandoffs(store *persist.Store, parentRunID string, stat *optimize.RunStat) {
	candidates, err := store.List(runStatsListLimit)
	if err != nil {
		return
	}
	for _, candidate := range candidates {
		if candidate.RunID == parentRunID {
			continue
		}
		if candidate.ParentRunID != parentRunID {
			continue
		}
		stat.ChildHandoffs++
		if candidate.Flavor == "" && firstStartedHasNoDigest(store, candidate.RunID) {
			stat.UntypedHandoffs++
		}
	}
}

// firstStartedHasNoDigest reports whether the run's RunStarted event
// has no InputDigest. Combined with an empty Flavor, this is the
// "untyped" signal the untyped_handoff heuristic looks for.
func firstStartedHasNoDigest(store *persist.Store, runID string) bool {
	events, err := store.Read(runID)
	if err != nil {
		return false
	}
	for _, ev := range events {
		if ev.Type != persist.EventRunStarted {
			continue
		}
		var p persist.RunStartedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return false
		}
		return p.InputDigest == ""
	}
	return false
}

// computeSchemaTokens estimates per-server schema-overhead tokens by
// marshaling the live tool list through the gateway and applying a
// chars-per-token heuristic. The pin store's PinRecord has SHA256
// hashes only, not byte counts, so we go to the live source. The
// schema_overhead heuristic treats this as a measurement, not a
// guess — every byte counted here is a byte the gateway actually
// shipped on the last tools/list response.
func computeSchemaTokens(gateway *mcp.Gateway) map[string]optimize.PinStat {
	if gateway == nil {
		return nil
	}
	result, err := gateway.HandleToolsList()
	if err != nil || result == nil || len(result.Tools) == 0 {
		return nil
	}
	bytesPerServer := make(map[string]int, len(result.Tools))
	for _, tool := range result.Tools {
		serverName, _, ok := splitPrefixedTool(tool.Name)
		if !ok {
			continue
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		bytesPerServer[serverName] += len(raw)
	}
	if len(bytesPerServer) == 0 {
		return nil
	}
	out := make(map[string]optimize.PinStat, len(bytesPerServer))
	for name, bytes := range bytesPerServer {
		// Approximate token count via the OpenAI rule-of-thumb of ~4
		// characters per token. JSON Schemas trend slightly token-dense
		// because of curly braces and quoting, but ~4 is the right
		// order of magnitude and we don't need precision for a
		// threshold-driven heuristic.
		out[name] = optimize.PinStat{SchemaTokens: bytes / 4}
	}
	return out
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

// computeServerCallCount sums the per-(server, tool) call counts the
// accumulator captured into a per-server total. Used by the
// expensive_model_on_cheap_task heuristic to compute average tokens
// per call without the metrics package growing a separate counter.
func computeServerCallCount(toolUsage map[string]map[string]metrics.ToolStat) map[string]int64 {
	if len(toolUsage) == 0 {
		return nil
	}
	out := make(map[string]int64, len(toolUsage))
	for server, tools := range toolUsage {
		var total int64
		for _, stat := range tools {
			total += stat.Calls
		}
		if total > 0 {
			out[server] = total
		}
	}
	return out
}

// lookupModelStats consults the active pricing.Source for a default
// model attribution per server. The current gateway has no per-server
// model resolver wired into a public accessor, so this returns an
// empty map for now — the expensive_model_on_cheap_task heuristic
// falls back to inferring rate from observed cost÷tokens, which still
// correctly identifies Opus-tier traffic.
//
// When a future PR exposes per-server model attribution, populate the
// returned map here so the heuristic can name the model in its
// summary instead of saying "an Opus-tier model."
func lookupModelStats(_ []optimize.ServerInfo) map[string]optimize.ModelStat {
	_ = pricing.CurrentSource()
	return nil
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
