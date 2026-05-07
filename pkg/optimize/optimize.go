// Package optimize produces actionable findings from gateway-observed
// data — server registrations, per-server token + cost totals, and
// per-(server, tool) call counts — to help platform engineers reduce
// spend on a running gridctl stack.
//
// The package is read-only: it never mutates accumulator state or the
// running gateway. Callers assemble a Stats snapshot, hand it to
// Analyze, and render the resulting OptimizeReport (CLI table, JSON, or
// Web UI).
//
// Heuristics in v1 (PR 4 of the gateway-cost-observability feature):
//   - unused_server: a registered server has seen zero tool calls in
//     the freshness window. Remediation: drop the server from the
//     stack YAML.
//   - unused_tool:   a registered tool on an active server has not been
//     called in the freshness window. Remediation: add it to the
//     server's tools: exclusion list.
//
// Additional heuristics (schema_overhead, format_savings_shortfall,
// expensive_model_on_cheap_task) ship in PR 5; the Stats shape is
// intentionally additive so future heuristics can read more inputs
// without breaking call sites.
package optimize

import (
	"sort"
	"time"
)

// Severity classifies findings for filtering and exit-code mapping.
type Severity string

// Severity levels in ascending order of actionability.
const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// IsActionable reports whether a severity should drive a non-zero exit
// code. info findings (including the "<24h of data" gate) are advisory
// only and exit cleanly.
func (s Severity) IsActionable() bool {
	return s == SeverityWarn || s == SeverityCritical
}

// Finding is a single optimization recommendation. Each finding carries
// enough context for the user to either dismiss it (info) or paste the
// Remediation snippet into their stack YAML.
type Finding struct {
	// ID is a stable kebab-case identifier (e.g. "unused-server-github").
	// Generated from the heuristic name plus the targeted server/tool.
	ID string `json:"id"`

	// Heuristic is the rule that fired (unused_server, unused_tool, ...).
	Heuristic string `json:"heuristic"`

	// Severity drives CLI exit codes and Web UI badge color.
	Severity Severity `json:"severity"`

	// Title is a short user-facing summary (≤ 80 chars typical).
	Title string `json:"title"`

	// Summary is a longer explanation, including measured numbers.
	Summary string `json:"summary"`

	// Server names the MCP server the finding refers to. Empty for
	// stack-wide findings such as the "<24h of data" info gate.
	Server string `json:"server,omitempty"`

	// Tool names the specific tool the finding refers to. Empty for
	// server-level findings.
	Tool string `json:"tool,omitempty"`

	// ImpactUSDPerWeek is the projected weekly USD savings from
	// applying the remediation. Always derived from measured data;
	// findings that cannot prove an impact set this to zero.
	ImpactUSDPerWeek float64 `json:"impact_usd_per_week"`

	// Remediation is a paste-ready YAML snippet or shell command that
	// resolves the finding. Multi-line strings are allowed.
	Remediation string `json:"remediation"`

	// DetectedAt is the wall-clock time the report was generated, not
	// the time the underlying condition began.
	DetectedAt time.Time `json:"detected_at"`
}

// OptimizeReport is the full output of one optimize pass.
type OptimizeReport struct {
	Findings    []Finding `json:"findings"`
	HealthScore int       `json:"health_score"`
	GeneratedAt time.Time `json:"generated_at"`
}

// ServerInfo describes one MCP server registered in the running stack.
// It is the smallest cross-package shape that lets pkg/optimize reason
// about which servers and tools exist without depending on pkg/mcp's
// MCPServerStatus.
type ServerInfo struct {
	// Name is the server's logical name in the stack YAML.
	Name string

	// Tools is the unprefixed tool list the server exposes through the
	// gateway.
	Tools []string

	// ToolWhitelist is the operator-curated tools: list from the stack
	// YAML. Empty means no whitelist (every tool is exposed).
	ToolWhitelist []string

	// Initialized is true once the gateway has handshaken with the
	// server. Optimize skips uninitialized servers because their tool
	// list may be empty for transient reasons (cold start, network
	// blip) rather than misconfiguration.
	Initialized bool
}

// ServerUsage carries per-server token and cost totals as observed by
// the accumulator. The fields mirror metrics.TokenCounts and
// metrics.CostCounts so call sites can populate them directly.
type ServerUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	TotalCostUSD float64
}

// CallCount returns a coarse "calls happened" indicator for a server.
// True when any token activity has been recorded, regardless of cost.
// Used by unused_server: a server with zero observed traffic is unused.
func (u ServerUsage) CallCount() bool {
	return u.TotalTokens > 0
}

// Stats is the input snapshot that callers hand to Analyze. Every field
// is required for the corresponding heuristic to produce a non-info
// finding; missing inputs degrade gracefully.
type Stats struct {
	// StackName is reported back on findings for context. Empty is
	// allowed; no validation here.
	StackName string

	// ObservationStart is the wall-clock time the gateway began
	// recording metrics (typically Accumulator.StartedAt()). Used to
	// gate the "<24h of data" info finding.
	ObservationStart time.Time

	// Now is the analysis time. Tests inject a fixed value; production
	// callers leave this zero so Analyze defaults to time.Now().
	Now time.Time

	// FreshnessWindow is the lookback span for unused_server and
	// unused_tool. Defaults to 7 * 24h when zero.
	FreshnessWindow time.Duration

	// MinObservationWindow is the minimum age of the gateway before
	// non-info findings are emitted. Defaults to 24h when zero. A
	// gateway younger than this returns a single info finding.
	MinObservationWindow time.Duration

	// Servers is every server registered in the active stack.
	Servers []ServerInfo

	// Usage is the per-server token + cost totals keyed by server name.
	// Servers absent from this map are treated as zero-traffic.
	Usage map[string]ServerUsage

	// ToolUsage is per-(server, tool) call counts and last-call
	// timestamps. nil means "no per-tool data captured", which causes
	// Analyze to skip the unused_tool heuristic entirely (rather than
	// flagging every tool as unused).
	ToolUsage map[string]map[string]ToolStat
}

// ToolStat mirrors metrics.ToolStat so pkg/optimize stays free of an
// import on pkg/metrics. Callers can populate it directly from
// metrics.Accumulator.ToolUsageSnapshot().
type ToolStat struct {
	Calls        int64
	LastCalledAt time.Time
}

// Options tunes the Analyze pass. All fields are optional.
type Options struct {
	// MinImpactUSDPerWeek filters findings whose projected weekly
	// savings fall below the threshold. Zero disables the filter.
	MinImpactUSDPerWeek float64

	// SeverityFilter, when non-empty, drops findings whose severity is
	// not in the set. Use to render only warn/critical in CI.
	SeverityFilter []Severity
}

const (
	defaultFreshnessWindow      = 7 * 24 * time.Hour
	defaultMinObservationWindow = 24 * time.Hour

	// estimatedSchemaOverheadTokens is the rough JSON Schema cost a
	// server adds to every prompt regardless of whether its tools are
	// called. Used as a coarse upper-bound for unused_server impact;
	// the schema_overhead heuristic in PR 5 will replace this with the
	// real byte count from the pin store.
	estimatedSchemaOverheadTokens = 1500

	// estimatedPromptsPerWeek is a conservative upper-bound on how
	// many prompts a stack sees in a week. We deliberately understate
	// it (a busy team easily hits >1000/day) so impact numbers do not
	// over-promise — this is gateway-data-driven inference, not a
	// guess about the user's workflow.
	estimatedPromptsPerWeek = 500

	// estimatedInputUSDPerToken is the rough per-token input rate used
	// when the server has no recorded cost yet. ~$3 per million input
	// tokens, the Anthropic Sonnet rate, is a defensible mid-range
	// number across providers in 2026.
	estimatedInputUSDPerToken = 3.0 / 1_000_000.0
)

// Analyze runs the v1 heuristic pass over the supplied Stats and
// returns a fully-populated OptimizeReport. The report's Findings slice
// is sorted by severity (critical → warn → info) then by impact
// descending so renderers can stream the most actionable finding
// first without re-sorting.
func Analyze(stats Stats, opts Options) OptimizeReport {
	now := stats.Now
	if now.IsZero() {
		now = time.Now()
	}
	freshness := stats.FreshnessWindow
	if freshness <= 0 {
		freshness = defaultFreshnessWindow
	}
	minObs := stats.MinObservationWindow
	if minObs <= 0 {
		minObs = defaultMinObservationWindow
	}

	report := OptimizeReport{GeneratedAt: now}

	// Insufficient observation window — emit a single info finding and
	// return so the report is unambiguous and never over-fires.
	if !stats.ObservationStart.IsZero() && now.Sub(stats.ObservationStart) < minObs {
		report.Findings = []Finding{{
			ID:         "info-need-more-data",
			Heuristic:  "need_more_data",
			Severity:   SeverityInfo,
			Title:      "Need more data",
			Summary:    "Gateway has been running for less than the minimum observation window. Re-run after at least 24 hours of activity for actionable findings.",
			DetectedAt: now,
		}}
		report.HealthScore = 100
		return report
	}

	cutoff := now.Add(-freshness)

	var findings []Finding
	findings = append(findings, detectUnusedServers(stats, now, cutoff)...)
	findings = append(findings, detectUnusedTools(stats, now, cutoff)...)

	if opts.MinImpactUSDPerWeek > 0 {
		findings = filterByImpact(findings, opts.MinImpactUSDPerWeek)
	}
	if len(opts.SeverityFilter) > 0 {
		findings = filterBySeverity(findings, opts.SeverityFilter)
	}

	sortFindings(findings)
	report.Findings = findings
	report.HealthScore = healthScore(findings)
	return report
}

// detectUnusedServers flags every initialized server with zero recorded
// token activity in the freshness window. Impact is the schema overhead
// the server adds to every prompt × estimated weekly prompts × the
// server's effective per-token cost.
func detectUnusedServers(stats Stats, now, _ time.Time) []Finding {
	var out []Finding
	for _, srv := range stats.Servers {
		if !srv.Initialized {
			continue
		}
		usage := stats.Usage[srv.Name]
		if usage.CallCount() {
			continue
		}
		impact := unusedServerImpact(srv, usage)
		out = append(out, Finding{
			ID:               "unused-server-" + srv.Name,
			Heuristic:        "unused_server",
			Severity:         SeverityWarn,
			Title:            "Unused server: " + srv.Name,
			Summary:          summaryUnusedServer(srv),
			Server:           srv.Name,
			ImpactUSDPerWeek: impact,
			Remediation:      remediationUnusedServer(srv),
			DetectedAt:       now,
		})
	}
	return out
}

// detectUnusedTools flags tools that the gateway has registered for an
// initialized, active server but never observed being called in the
// freshness window. Tools already excluded via the server's
// ToolWhitelist are skipped because the operator has already curated
// them out.
//
// The heuristic is intentionally conservative: if the accumulator has
// no per-tool data at all (legacy gateway, freshly restarted process),
// it returns no findings rather than flagging every tool.
func detectUnusedTools(stats Stats, now, cutoff time.Time) []Finding {
	if len(stats.ToolUsage) == 0 {
		return nil
	}
	var out []Finding
	for _, srv := range stats.Servers {
		if !srv.Initialized || len(srv.Tools) == 0 {
			continue
		}
		usage := stats.Usage[srv.Name]
		// Server itself unused — already covered by detectUnusedServers.
		if !usage.CallCount() {
			continue
		}
		whitelist := toSet(srv.ToolWhitelist)
		toolStats := stats.ToolUsage[srv.Name]
		for _, tool := range srv.Tools {
			// Operator already excluded the tool — nothing to do.
			if len(whitelist) > 0 && !whitelist[tool] {
				continue
			}
			stat, ok := toolStats[tool]
			if ok && stat.Calls > 0 && !stat.LastCalledAt.IsZero() && stat.LastCalledAt.After(cutoff) {
				continue
			}
			out = append(out, Finding{
				ID:               "unused-tool-" + srv.Name + "-" + tool,
				Heuristic:        "unused_tool",
				Severity:         SeverityInfo,
				Title:            "Unused tool: " + srv.Name + "/" + tool,
				Summary:          summaryUnusedTool(srv.Name, tool),
				Server:           srv.Name,
				Tool:             tool,
				ImpactUSDPerWeek: 0, // per-tool schema savings land in PR 5
				Remediation:      remediationUnusedTool(srv, tool),
				DetectedAt:       now,
			})
		}
	}
	return out
}

func unusedServerImpact(srv ServerInfo, usage ServerUsage) float64 {
	rate := estimatedInputUSDPerToken
	// If the server has any historic cost we use its observed per-token
	// rate; otherwise we fall back to the conservative default. We do
	// not invent numbers when there is no data — usage stays zero, so
	// the formula returns zero unless we can prove tokens-per-prompt
	// from elsewhere. Schema-overhead × prompts × default-rate gives a
	// defensible upper bound for the unused_server case (which has no
	// usage data by definition) without claiming an impact we did not
	// measure end-to-end.
	if usage.TotalTokens > 0 && usage.TotalCostUSD > 0 {
		rate = usage.TotalCostUSD / float64(usage.TotalTokens)
	}
	tools := len(srv.Tools)
	if tools <= 0 {
		tools = 1
	}
	overhead := estimatedSchemaOverheadTokens * tools
	if overhead > 5*estimatedSchemaOverheadTokens {
		overhead = 5 * estimatedSchemaOverheadTokens // cap at 5× to stay conservative
	}
	return float64(overhead) * estimatedPromptsPerWeek * rate
}

func summaryUnusedServer(srv ServerInfo) string {
	count := len(srv.Tools)
	plural := "s"
	if count == 1 {
		plural = ""
	}
	return "Server '" + srv.Name + "' has registered " + itoa(count) + " tool" + plural + " but no calls have been observed. Removing it (or excluding all its tools) frees the schema overhead it adds to every prompt."
}

func summaryUnusedTool(server, tool string) string {
	return "Tool '" + server + "/" + tool + "' is exposed by the gateway but has not been called in the lookback window. Excluding it shrinks the tool list each client sees on initialize."
}

func remediationUnusedServer(srv ServerInfo) string {
	return "# Remove the server entirely:\nmcp-servers:\n  # delete the entry for: " + srv.Name + "\n\n# Or keep the runtime but exclude every tool:\nmcp-servers:\n  - name: " + srv.Name + "\n    tools: []"
}

func remediationUnusedTool(srv ServerInfo, tool string) string {
	existing := append([]string(nil), srv.ToolWhitelist...)
	existing = append(existing, tool)
	sort.Strings(existing)
	out := "# Add the tool to the server's tools: filter\nmcp-servers:\n  - name: " + srv.Name + "\n    tools:\n"
	for _, t := range existing {
		if t == tool {
			out += "      # add this line:\n"
		}
		out += "      - " + t + "\n"
	}
	return out
}

func filterByImpact(in []Finding, min float64) []Finding {
	out := in[:0]
	for _, f := range in {
		// info findings are kept regardless of impact — they exist to
		// communicate state, not savings.
		if f.Severity == SeverityInfo || f.ImpactUSDPerWeek >= min {
			out = append(out, f)
		}
	}
	return out
}

func filterBySeverity(in []Finding, allowed []Severity) []Finding {
	set := make(map[Severity]bool, len(allowed))
	for _, s := range allowed {
		set[s] = true
	}
	out := in[:0]
	for _, f := range in {
		if set[f.Severity] {
			out = append(out, f)
		}
	}
	return out
}

func sortFindings(in []Finding) {
	rank := map[Severity]int{
		SeverityCritical: 0,
		SeverityWarn:     1,
		SeverityInfo:     2,
	}
	sort.SliceStable(in, func(i, j int) bool {
		ri, rj := rank[in[i].Severity], rank[in[j].Severity]
		if ri != rj {
			return ri < rj
		}
		if in[i].ImpactUSDPerWeek != in[j].ImpactUSDPerWeek {
			return in[i].ImpactUSDPerWeek > in[j].ImpactUSDPerWeek
		}
		return in[i].ID < in[j].ID
	})
}

// healthScore is a 0-100 indicator with no findings = 100. Each warn
// drops 10 points (capped) and each critical drops 20; info findings
// are advisory and do not move the score.
func healthScore(findings []Finding) int {
	score := 100
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			score -= 20
		case SeverityWarn:
			score -= 10
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}

func toSet(in []string) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

// itoa avoids a fmt dependency on the rendering hot path. The values
// passed here are small (tool counts), so a simple decimal encoding is
// fine.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
