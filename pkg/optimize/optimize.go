// Package optimize produces actionable findings from gateway-observed
// data — server registrations, per-server token totals, and per-(server,
// tool) call counts — to help platform engineers shrink the token
// footprint of a running gridctl stack.
//
// The package is read-only: it never mutates accumulator state or the
// running gateway. Callers assemble a Stats snapshot, hand it to
// Analyze, and render the resulting OptimizeReport (CLI table, JSON, or
// Web UI).
//
// Heuristics:
//   - unused_server: a registered server has seen zero tool calls in
//     the freshness window. Remediation: drop the server from the
//     stack YAML.
//   - unused_tool:   a registered tool on an active server has not been
//     called in the freshness window. Remediation: add it to the
//     server's tools: exclusion list.
//   - schema_overhead: a server's tool-list schema outweighs the output
//     its tools have produced.
//   - format_savings_shortfall: a server emits raw JSON while siblings
//     demonstrate measured TOON/CSV savings.
//
// Impact is denominated in tokens per week: schema tokens the finding
// would free, projected at an assumed ~500 prompts/week (deliberately
// conservative). Tokens are what the gateway actually measures; dollar
// conversion belongs to systems that can see real spend.
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

	// ImpactTokensPerWeek is the projected weekly token savings from
	// applying the remediation, computed from measured schema or output
	// tokens times the assumed ~500 prompts/week. Findings that cannot
	// prove an impact set this to zero.
	ImpactTokensPerWeek int64 `json:"impact_tokens_per_week"`

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

	// OutputFormat is the configured `output_format` from the stack
	// YAML — "json" (or empty) for the default, "toon" / "csv" / "text"
	// for the format-conversion variants. Powers the
	// format_savings_shortfall heuristic.
	OutputFormat string
}

// ServerUsage carries per-server token totals as observed by the
// accumulator. The fields mirror metrics.TokenCounts so call sites can
// populate them directly.
type ServerUsage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// CallCount returns a coarse "calls happened" indicator for a server.
// True when any token activity has been recorded. Used by
// unused_server: a server with zero observed traffic is unused.
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

	// Usage is the per-server token totals keyed by server name.
	// Servers absent from this map are treated as zero-traffic.
	Usage map[string]ServerUsage

	// ToolUsage is per-(server, tool) call counts and last-call
	// timestamps. nil means "no per-tool data captured", which causes
	// Analyze to skip the unused_tool heuristic entirely (rather than
	// flagging every tool as unused).
	ToolUsage map[string]map[string]ToolStat

	// PinStats is per-server schema-overhead inputs. nil disables the
	// schema_overhead heuristic. Populated by callers that can read
	// schema-byte counts off the live gateway tool list (or, in the
	// future, from extended pin records); fall back to nil when no
	// data source is available so the heuristic skips silently.
	PinStats map[string]PinStat

	// ToolSchemaTokens is the estimated schema-token cost of each
	// individual tool definition, keyed server -> tool. Populated from
	// the same live tools/list measurement as PinStats. Used by
	// unused_tool to size the context tax an unused tool's definition
	// adds to every prompt. nil (or a missing tool) falls back to a
	// conservative per-tool estimate.
	ToolSchemaTokens map[string]map[string]int

	// FormatBaseline is the session-wide format-conversion savings rate
	// observed across servers that DO use `output_format: toon|csv`.
	// Used by the format_savings_shortfall heuristic to project savings
	// onto servers that have not adopted format conversion. The zero
	// value disables the heuristic — without a baseline we have no
	// gateway-measured evidence that conversion would help here.
	FormatBaseline FormatBaseline
}

// PinStat carries the schema-overhead inputs for a single server. The
// pin shape evolves over time, so callers populate this from whichever
// source is most authoritative on their build (live tool list, pin
// records, etc.). Zero values cause the heuristic to skip the server.
type PinStat struct {
	// SchemaTokens is the estimated total token cost of the server's
	// tool definitions when serialized into a tools/list response.
	// Populated by counting bytes of marshaled JSON and applying a
	// chars-per-token heuristic.
	SchemaTokens int
}

// FormatBaseline summarizes the session-wide token savings achieved by
// servers already using `output_format: toon|csv` conversion. The
// format_savings_shortfall heuristic projects this rate onto candidate
// servers to derive a measured impact estimate.
type FormatBaseline struct {
	// OriginalTokens is the pre-conversion token count summed across
	// every conversion the gateway has observed.
	OriginalTokens int64
	// FormattedTokens is the post-conversion token count for the same
	// observations.
	FormattedTokens int64
	// SavingsPercent is the percentage of tokens saved by conversion
	// (0–100). Computed by the caller; zero means "no demonstrated
	// savings yet" and the heuristic skips.
	SavingsPercent float64
}

// ToolStat mirrors metrics.ToolStat so pkg/optimize stays free of an
// import on pkg/metrics. Callers can populate it directly from
// metrics.Accumulator.ToolUsageSnapshot(). Zero values simply mean
// "nothing recorded" and no heuristic treats them as measured.
type ToolStat struct {
	Calls        int64
	LastCalledAt time.Time
	InputTokens  int64
	OutputTokens int64
}

// Options tunes the Analyze pass. All fields are optional.
type Options struct {
	// MinImpactTokensPerWeek filters findings whose projected weekly
	// token savings fall below the threshold. Zero disables the filter.
	MinImpactTokensPerWeek int64

	// SeverityFilter, when non-empty, drops findings whose severity is
	// not in the set. Use to render only warn/critical in CI.
	SeverityFilter []Severity
}

const (
	defaultFreshnessWindow      = 7 * 24 * time.Hour
	defaultMinObservationWindow = 24 * time.Hour

	// estimatedSchemaOverheadTokens is the rough JSON Schema cost a
	// server adds to every prompt regardless of whether its tools are
	// called. Used as a coarse upper-bound for unused_server impact
	// when no measured schema-token data is available (e.g. legacy
	// gateway, no live tool list).
	estimatedSchemaOverheadTokens = 1500

	// estimatedPromptsPerWeek is a conservative upper-bound on how
	// many prompts a stack sees in a week. We deliberately understate
	// it (a busy team easily hits >1000/day) so impact numbers do not
	// over-promise — this is gateway-data-driven inference, not a
	// guess about the user's workflow. Summaries name the assumption
	// so projected figures never read as measured.
	estimatedPromptsPerWeek = 500

	// estimatedToolSchemaTokens is the fallback schema cost of a single
	// tool definition when no measured per-tool count is available.
	// Published measurements put real tools at roughly 100-1,000 tokens;
	// 300 deliberately understates the median so unused_tool impact
	// never over-promises on unmeasured tools.
	estimatedToolSchemaTokens = 300

	// schemaOverheadMinSchemaTokens is the floor on schema size below
	// which schema_overhead never fires — small servers never push a
	// meaningful prompt-tax even if they're idle.
	schemaOverheadMinSchemaTokens = 2000

	// schemaOverheadRatioFloor is the minimum ratio of (output tokens
	// observed) to (schema tokens) that a server must achieve to avoid
	// firing. A server that has produced fewer output tokens than its
	// schema costs to advertise is paying more for the schema than
	// it's getting back from the calls.
	schemaOverheadRatioFloor = 5.0

	// formatShortfallMinSavingsPercent is the floor on the session
	// FormatBaseline.SavingsPercent below which format_savings_shortfall
	// is silent. Without demonstrated savings, projecting onto a
	// candidate server would be a guess, not a measurement.
	formatShortfallMinSavingsPercent = 10.0

	// formatShortfallMinOutputTokens is the floor on a candidate
	// server's output tokens below which the heuristic skips. Below
	// this threshold the projected savings rounds to zero and the
	// finding adds noise without value.
	formatShortfallMinOutputTokens = 5_000
)

// Analyze runs the heuristic pass over the supplied Stats and returns a
// fully-populated OptimizeReport. The report's Findings slice is sorted
// by severity (critical → warn → info) then by impact descending so
// renderers can stream the most actionable finding first without
// re-sorting.
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
	findings = append(findings, detectSchemaOverhead(stats, now)...)
	findings = append(findings, detectFormatSavingsShortfall(stats, now)...)

	if opts.MinImpactTokensPerWeek > 0 {
		findings = filterByImpact(findings, opts.MinImpactTokensPerWeek)
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
// the server adds to every prompt × estimated weekly prompts.
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
		out = append(out, Finding{
			ID:                  "unused-server-" + srv.Name,
			Heuristic:           "unused_server",
			Severity:            SeverityWarn,
			Title:               "Unused server: " + srv.Name,
			Summary:             summaryUnusedServer(srv),
			Server:              srv.Name,
			ImpactTokensPerWeek: unusedServerImpact(srv),
			Remediation:         remediationUnusedServer(srv),
			DetectedAt:          now,
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
// it returns no findings rather than flagging every tool as unused.
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
				ID:                  "unused-tool-" + srv.Name + "-" + tool,
				Heuristic:           "unused_tool",
				Severity:            SeverityInfo,
				Title:               "Unused tool: " + srv.Name + "/" + tool,
				Summary:             summaryUnusedTool(srv.Name, tool),
				Server:              srv.Name,
				Tool:                tool,
				ImpactTokensPerWeek: unusedToolImpact(stats.ToolSchemaTokens[srv.Name][tool]),
				Remediation:         remediationUnusedTool(srv, tool),
				DetectedAt:          now,
			})
		}
	}
	return out
}

// unusedToolImpact sizes the context tax of one unused tool's schema:
// the tokens its definition adds to every prompt × estimated weekly
// prompts. Capped so a single oversized schema never over-promises.
func unusedToolImpact(schemaTokens int) int64 {
	tokens := schemaTokens
	if tokens <= 0 {
		tokens = estimatedToolSchemaTokens
	}
	if tokens > estimatedSchemaOverheadTokens {
		tokens = estimatedSchemaOverheadTokens // cap at the per-server estimate to stay conservative
	}
	return int64(tokens) * estimatedPromptsPerWeek
}

// unusedServerImpact sizes the schema overhead an unused server adds to
// every prompt: per-tool schema estimate × tool count (capped at 5×) ×
// estimated weekly prompts. An unused server has no measured traffic by
// definition, so this is a conservative projection, not a measurement.
func unusedServerImpact(srv ServerInfo) int64 {
	tools := len(srv.Tools)
	if tools <= 0 {
		tools = 1
	}
	overhead := estimatedSchemaOverheadTokens * tools
	if overhead > 5*estimatedSchemaOverheadTokens {
		overhead = 5 * estimatedSchemaOverheadTokens // cap at 5× to stay conservative
	}
	return int64(overhead) * estimatedPromptsPerWeek
}

func summaryUnusedServer(srv ServerInfo) string {
	count := len(srv.Tools)
	plural := "s"
	if count == 1 {
		plural = ""
	}
	return "Server '" + srv.Name + "' has registered " + itoa(count) + " tool" + plural + " but no calls have been observed. Removing it (or excluding all its tools) frees the schema overhead it adds to every prompt (impact projected assuming ~500 prompts/week)."
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

// detectSchemaOverhead flags servers whose tool-list payload (the
// schema gateway sends on every initialize / tools/list) is large
// relative to the value the server's tools have produced:
//
//	ratio = output_tokens / schema_tokens
//
// If schema_tokens crosses the floor and ratio falls below
// schemaOverheadRatioFloor, the server's schema is paying more than
// the calls have delivered back — typically because few of the
// advertised tools are exercised. The remediation pushes the user to
// trim the tool list via `tools:` so the schema shrinks.
//
// Skipped silently when PinStats is empty for the server: we never
// fabricate schema-token counts, and without them the heuristic has
// no measurement to anchor to.
func detectSchemaOverhead(stats Stats, now time.Time) []Finding {
	if len(stats.PinStats) == 0 {
		return nil
	}
	var out []Finding
	for _, srv := range stats.Servers {
		if !srv.Initialized {
			continue
		}
		pin, ok := stats.PinStats[srv.Name]
		if !ok || pin.SchemaTokens < schemaOverheadMinSchemaTokens {
			continue
		}
		usage := stats.Usage[srv.Name]
		// A server with zero output tokens is unused — covered by
		// detectUnusedServers; skip here so we don't emit two warnings
		// for the same root cause.
		if usage.OutputTokens == 0 {
			continue
		}
		ratio := float64(usage.OutputTokens) / float64(pin.SchemaTokens)
		if ratio >= schemaOverheadRatioFloor {
			continue
		}
		out = append(out, Finding{
			ID:                  "schema-overhead-" + srv.Name,
			Heuristic:           "schema_overhead",
			Severity:            SeverityWarn,
			Title:               "Schema overhead exceeds tool value: " + srv.Name,
			Summary:             summarySchemaOverhead(srv, pin, usage, ratio),
			Server:              srv.Name,
			ImpactTokensPerWeek: schemaOverheadImpact(pin.SchemaTokens),
			Remediation:         remediationSchemaOverhead(srv),
			DetectedAt:          now,
		})
	}
	return out
}

// detectFormatSavingsShortfall flags servers that emit raw JSON output
// (no `output_format` configured) when other servers in the same
// session have already demonstrated a meaningful savings rate from
// converting to TOON or CSV. The projected impact is the session
// baseline rate × the candidate server's measured output tokens —
// every input is observed, not guessed.
//
// Skipped silently when:
//   - FormatBaseline.SavingsPercent is below the demonstration floor
//     (the gateway has no measured savings to project).
//   - The candidate server already has output_format set to a
//     conversion variant (toon, csv, text).
//   - The candidate's output tokens are below formatShortfallMinOutputTokens.
func detectFormatSavingsShortfall(stats Stats, now time.Time) []Finding {
	if stats.FormatBaseline.SavingsPercent < formatShortfallMinSavingsPercent {
		return nil
	}
	var out []Finding
	for _, srv := range stats.Servers {
		if !srv.Initialized {
			continue
		}
		if usesFormatConversion(srv.OutputFormat) {
			continue
		}
		usage := stats.Usage[srv.Name]
		if usage.OutputTokens < formatShortfallMinOutputTokens {
			continue
		}
		projectedSavedTokens := int64(float64(usage.OutputTokens) * stats.FormatBaseline.SavingsPercent / 100.0)
		out = append(out, Finding{
			ID:                  "format-savings-shortfall-" + srv.Name,
			Heuristic:           "format_savings_shortfall",
			Severity:            SeverityWarn,
			Title:               "Output format conversion would save tokens: " + srv.Name,
			Summary:             summaryFormatShortfall(srv, usage, stats.FormatBaseline),
			Server:              srv.Name,
			ImpactTokensPerWeek: projectedSavedTokens,
			Remediation:         remediationFormatShortfall(srv),
			DetectedAt:          now,
		})
	}
	return out
}

func schemaOverheadImpact(schemaTokens int) int64 {
	return int64(schemaTokens) * estimatedPromptsPerWeek
}

func summarySchemaOverhead(srv ServerInfo, pin PinStat, usage ServerUsage, ratio float64) string {
	return "Server '" + srv.Name + "' advertises " + itoa(len(srv.Tools)) + " tools (~" + itoa(pin.SchemaTokens) + " schema tokens) but its tools have produced only " + itoa64(usage.OutputTokens) + " output tokens — a usage ratio of " + formatRatio(ratio) + ". Pruning unused tools shrinks the schema sent on every prompt (impact projected assuming ~500 prompts/week)."
}

func remediationSchemaOverhead(srv ServerInfo) string {
	return "# Trim the tool surface to the tools that actually get called:\nmcp-servers:\n  - name: " + srv.Name + "\n    tools:\n      # only list the tools you use, e.g.:\n      # - one_tool_you_actually_call"
}

func summaryFormatShortfall(srv ServerInfo, usage ServerUsage, baseline FormatBaseline) string {
	return "Server '" + srv.Name + "' emitted " + itoa64(usage.OutputTokens) + " output tokens with no `output_format` configured. Servers in this stack that use TOON or CSV conversion saved " + formatPercent(baseline.SavingsPercent) + "% on average — applying the same conversion to '" + srv.Name + "' would project a similar reduction."
}

func remediationFormatShortfall(srv ServerInfo) string {
	return "# Switch the server to a token-efficient output format\nmcp-servers:\n  - name: " + srv.Name + "\n    output_format: toon  # or csv when the result is tabular"
}

func usesFormatConversion(format string) bool {
	switch format {
	case "toon", "csv", "text":
		return true
	default:
		return false
	}
}

func filterByImpact(in []Finding, min int64) []Finding {
	out := in[:0]
	for _, f := range in {
		// info findings are kept regardless of impact — they exist to
		// communicate state, not savings.
		if f.Severity == SeverityInfo || f.ImpactTokensPerWeek >= min {
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
		if in[i].ImpactTokensPerWeek != in[j].ImpactTokensPerWeek {
			return in[i].ImpactTokensPerWeek > in[j].ImpactTokensPerWeek
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

// formatRatio formats a float ratio with one decimal of precision.
// Used in summaries where exact precision adds noise but the rough
// magnitude carries the message.
func formatRatio(r float64) string {
	if r >= 100 {
		return itoa(int(r))
	}
	whole := int(r)
	frac := int((r - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac)
}

// formatPercent renders a 0-100 percentage with no decimals.
func formatPercent(p float64) string {
	return itoa(int(p + 0.5))
}

// itoa64 is the int64 sibling of itoa.
func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [21]byte
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
