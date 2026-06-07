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

	// Model names the model the finding refers to, when one is known. Set
	// by expensive_model_on_cheap_task from the dominant model that priced
	// the server's traffic. Empty when only an effective rate was available.
	Model string `json:"model,omitempty"`

	// Provenance describes how Model was determined: "declared" when it is
	// the model that priced the server's recorded cost; empty when the
	// finding fired on a rate inferred from observed cost÷tokens with no
	// model identity. Mirrors the effective-model provenance vocabulary.
	Provenance string `json:"provenance,omitempty"`

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

	// PinStats is per-server schema-overhead inputs. nil disables the
	// schema_overhead heuristic. Populated by callers that can read
	// schema-byte counts off the live gateway tool list (or, in the
	// future, from extended pin records); fall back to nil when no
	// data source is available so the heuristic skips silently.
	PinStats map[string]PinStat

	// FormatBaseline is the session-wide format-conversion savings rate
	// observed across servers that DO use `output_format: toon|csv`.
	// Used by the format_savings_shortfall heuristic to project savings
	// onto servers that have not adopted format conversion. The zero
	// value disables the heuristic — without a baseline we have no
	// gateway-measured evidence that conversion would help here.
	FormatBaseline FormatBaseline

	// ServerCallCount is the total tool-call count per server, summed
	// across that server's tools. Used by
	// expensive_model_on_cheap_task to compute average tokens-per-call.
	// nil means the heuristic skips per-server avg-tokens math.
	ServerCallCount map[string]int64

	// ModelStats carries per-server pricing rates when the call site
	// knows which model dominates traffic for that server. Empty/nil
	// causes expensive_model_on_cheap_task to fall back to inferring
	// the rate from observed cost÷tokens.
	ModelStats map[string]ModelStat

	// ModelHistograms carries the per-server breakdown of recorded cost by
	// the model that priced it (server -> model ID -> USD). When present it
	// names the dominant model exactly (the model that priced the most
	// cost), which resolveDominantRate prefers over the declared ModelStat
	// or the cost÷tokens fallback. nil leaves the existing behavior intact.
	ModelHistograms map[string]map[string]float64
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

// ModelStat names the dominant model for a server and carries its
// per-token rates. Used by expensive_model_on_cheap_task to detect
// "Opus-tier model on a simple lookup tool" without re-deriving rates
// from cost÷tokens (which conflates input vs output vs cache rates).
type ModelStat struct {
	// Model is the canonical model ID (e.g. "claude-opus-4-7"). Empty
	// when the gateway has no resolver wired for the server, in which
	// case the heuristic falls back to inferring rate from observed
	// cost.
	Model string
	// InputUSDPerToken is the input-token rate from pricing.Lookup.
	// Zero disables the rate-based path; the heuristic falls back to
	// inferring from observed cost÷tokens.
	InputUSDPerToken float64
	// OutputUSDPerToken is the output-token rate from pricing.Lookup.
	// Provided alongside InputUSDPerToken for completeness; the
	// heuristic itself only needs InputUSDPerToken to detect
	// Opus-tier pricing.
	OutputUSDPerToken float64
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
	// called. Used as a coarse upper-bound for unused_server impact
	// when no measured schema-token data is available (e.g. legacy
	// gateway, no live tool list).
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

	// formatShortfallProjectionWeeks is how many weeks of activity the
	// projected impact represents — set to 1 so the
	// `impact_usd_per_week` field stays comparable to the other
	// heuristics' weekly numbers.
	formatShortfallProjectionWeeks = 1.0

	// expensiveModelInputRateThreshold flags any server whose dominant
	// model has an input rate at or above this number. ~$5 per million
	// input tokens captures Opus-tier and GPT-4-class pricing without
	// catching mid-tier Sonnet/GPT-4o.
	expensiveModelInputRateThreshold = 5.0 / 1_000_000.0

	// expensiveModelMaxAvgTokensPerCall is the upper bound on the
	// per-call token average that still counts as a "simple lookup."
	// Servers with larger calls (long prompts, big result payloads)
	// are not the target of this heuristic — it's about cheap tasks
	// being executed on expensive infra.
	expensiveModelMaxAvgTokensPerCall = 200.0

	// expensiveModelMinCalls keeps the heuristic from firing on a
	// single outlier call. Five calls is enough to be a pattern; less
	// than that is anecdote.
	expensiveModelMinCalls = 5
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
	findings = append(findings, detectSchemaOverhead(stats, now)...)
	findings = append(findings, detectFormatSavingsShortfall(stats, now)...)
	findings = append(findings, detectExpensiveModelOnCheapTask(stats, now)...)

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

// detectSchemaOverhead flags servers whose tool-list payload (the
// schema gateway sends on every initialize / tools/list) is large
// relative to the value the server's tools have produced. The formula
// is the gateway-data-driven shape of the prompt's heuristic:
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
		impact := schemaOverheadImpact(pin.SchemaTokens, usage)
		out = append(out, Finding{
			ID:               "schema-overhead-" + srv.Name,
			Heuristic:        "schema_overhead",
			Severity:         SeverityWarn,
			Title:            "Schema overhead exceeds tool value: " + srv.Name,
			Summary:          summarySchemaOverhead(srv, pin, usage, ratio),
			Server:           srv.Name,
			ImpactUSDPerWeek: impact,
			Remediation:      remediationSchemaOverhead(srv),
			DetectedAt:       now,
		})
	}
	return out
}

// detectFormatSavingsShortfall flags servers that emit raw JSON output
// (no `output_format` configured) when other servers in the same
// session have already demonstrated a meaningful savings rate from
// converting to TOON or CSV. The projected impact uses the session
// baseline rate × the candidate server's measured output tokens × the
// candidate's measured per-token cost — every input is observed, not
// guessed.
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
		// Per-token rate inferred from observed cost so impact stays
		// gateway-data-driven. Falls back to the conservative default
		// when no cost has been recorded yet (rare for a server with
		// >5K output tokens but possible right after pricing data was
		// cleared via DELETE /api/metrics/cost).
		rate := estimatedInputUSDPerToken
		if usage.TotalTokens > 0 && usage.TotalCostUSD > 0 {
			rate = usage.TotalCostUSD / float64(usage.TotalTokens)
		}
		projectedSavedTokens := float64(usage.OutputTokens) * stats.FormatBaseline.SavingsPercent / 100.0
		impact := projectedSavedTokens * rate * formatShortfallProjectionWeeks
		out = append(out, Finding{
			ID:               "format-savings-shortfall-" + srv.Name,
			Heuristic:        "format_savings_shortfall",
			Severity:         SeverityWarn,
			Title:            "Output format conversion would save tokens: " + srv.Name,
			Summary:          summaryFormatShortfall(srv, usage, stats.FormatBaseline),
			Server:           srv.Name,
			ImpactUSDPerWeek: impact,
			Remediation:      remediationFormatShortfall(srv),
			DetectedAt:       now,
		})
	}
	return out
}

// detectExpensiveModelOnCheapTask flags servers where an Opus-tier
// model dominates traffic but the calls themselves are tiny — short
// prompts, short results — i.e. the simple-lookup pattern. The
// finding is informational only because the model usually lives
// client-side; gridctl can suggest the migration but cannot enforce it.
//
// The detection logic prefers explicit ModelStats when the call site
// knows which model is in use; otherwise it falls back to inferring
// an effective rate from observed cost÷tokens. When neither signal is
// available the heuristic stays silent.
func detectExpensiveModelOnCheapTask(stats Stats, now time.Time) []Finding {
	if len(stats.ServerCallCount) == 0 {
		return nil
	}
	var out []Finding
	for _, srv := range stats.Servers {
		if !srv.Initialized {
			continue
		}
		nCalls := stats.ServerCallCount[srv.Name]
		if nCalls < expensiveModelMinCalls {
			continue
		}
		usage := stats.Usage[srv.Name]
		if usage.TotalTokens == 0 {
			continue
		}
		avgTokensPerCall := float64(usage.TotalTokens) / float64(nCalls)
		if avgTokensPerCall > expensiveModelMaxAvgTokensPerCall {
			continue
		}
		rate, modelName := resolveDominantRate(stats.ModelStats[srv.Name], stats.ModelHistograms[srv.Name], usage)
		if rate < expensiveModelInputRateThreshold {
			continue
		}
		provenance := ""
		if modelName != "" {
			provenance = "declared"
		}
		out = append(out, Finding{
			ID:               "expensive-model-cheap-task-" + srv.Name,
			Heuristic:        "expensive_model_on_cheap_task",
			Severity:         SeverityInfo,
			Title:            "Expensive model on cheap task: " + srv.Name,
			Summary:          summaryExpensiveModel(srv, usage, nCalls, modelName, rate),
			Server:           srv.Name,
			ImpactUSDPerWeek: 0, // informational; client-side model swap is the action
			Remediation:      remediationExpensiveModel(srv, modelName),
			Model:            modelName,
			Provenance:       provenance,
			DetectedAt:       now,
		})
	}
	return out
}

func schemaOverheadImpact(schemaTokens int, usage ServerUsage) float64 {
	rate := estimatedInputUSDPerToken
	if usage.TotalTokens > 0 && usage.TotalCostUSD > 0 {
		rate = usage.TotalCostUSD / float64(usage.TotalTokens)
	}
	return float64(schemaTokens) * estimatedPromptsPerWeek * rate
}

func summarySchemaOverhead(srv ServerInfo, pin PinStat, usage ServerUsage, ratio float64) string {
	return "Server '" + srv.Name + "' advertises " + itoa(len(srv.Tools)) + " tools (~" + itoa(pin.SchemaTokens) + " schema tokens) but its tools have produced only " + itoa64(usage.OutputTokens) + " output tokens — a usage ratio of " + formatRatio(ratio) + ". Pruning unused tools shrinks the schema sent on every prompt."
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

func summaryExpensiveModel(srv ServerInfo, usage ServerUsage, nCalls int64, modelName string, rate float64) string {
	avg := float64(usage.TotalTokens) / float64(nCalls)
	if modelName == "" {
		return "Server '" + srv.Name + "' is priced at an Opus-tier rate (" + formatRatePerMillion(rate) + " per million input tokens) but the average call is only " + formatRatio(avg) + " tokens — a simple-lookup pattern. The model selection lives client-side; consider routing this server to a cheaper model when possible."
	}
	return "Server '" + srv.Name + "' traffic is priced as " + modelName + " (" + formatRatePerMillion(rate) + " per million input tokens) but the average call is only " + formatRatio(avg) + " tokens — a simple-lookup pattern. The model selection lives client-side; consider routing this server to a cheaper model when possible."
}

func remediationExpensiveModel(srv ServerInfo, modelName string) string {
	if modelName == "" {
		return "# Model selection is client-side; pick a smaller model (e.g. Haiku, gpt-4o-mini) for prompts that primarily call '" + srv.Name + "'."
	}
	return "# Priced as (declared): " + modelName + "\n# Model selection is client-side; pick a smaller model (e.g. Haiku, gpt-4o-mini) for prompts that primarily call '" + srv.Name + "'."
}

func usesFormatConversion(format string) bool {
	switch format {
	case "toon", "csv", "text":
		return true
	default:
		return false
	}
}

// resolveDominantRate returns the effective input-token rate and the model
// name for a server's expensive-model check. Resolution order:
//
//  1. The histogram's dominant model (the model that priced the most cost) is
//     the exact, observed attribution. When it has a known rate (ms carries
//     that model's rate), use it.
//  2. The declared ModelStat rate, when present.
//  3. The cost÷tokens fallback — an effective rate with no model identity.
//
// histogram maps model ID -> recorded USD for this server; nil leaves the
// pre-histogram behavior. ms is expected to carry the dominant model's rate
// when the call site resolved one (the API layer looks it up against the
// pricing source).
func resolveDominantRate(ms ModelStat, histogram map[string]float64, usage ServerUsage) (float64, string) {
	dominant := DominantModel(histogram)
	if dominant != "" {
		// The histogram names the model exactly. Prefer its rate when the
		// call site supplied one for this same model.
		if ms.InputUSDPerToken > 0 && ms.Model == dominant {
			return ms.InputUSDPerToken, dominant
		}
		if usage.TotalTokens > 0 && usage.TotalCostUSD > 0 {
			return usage.TotalCostUSD / float64(usage.TotalTokens), dominant
		}
	}
	if ms.InputUSDPerToken > 0 {
		return ms.InputUSDPerToken, ms.Model
	}
	if usage.TotalTokens > 0 && usage.TotalCostUSD > 0 {
		return usage.TotalCostUSD / float64(usage.TotalTokens), ms.Model
	}
	return 0, ms.Model
}

// DominantModel returns the model ID with the highest recorded cost in a
// histogram (model ID -> USD), breaking ties by model ID for determinism.
// Empty when the histogram is nil or empty. Exported so the API layer
// resolves "the dominant model" with the exact same tie-break rule this
// package uses, keeping the two in agreement.
func DominantModel(histogram map[string]float64) string {
	var best string
	var bestCost float64
	for model, cost := range histogram {
		if cost > bestCost || (cost == bestCost && (best == "" || model < best)) {
			best, bestCost = model, cost
		}
	}
	return best
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

// formatRatePerMillion renders a per-token USD rate as a "$X.XX/M"
// shape, which is the unit operators reason in when comparing models.
func formatRatePerMillion(rate float64) string {
	per := rate * 1_000_000
	whole := int(per)
	frac := int((per - float64(whole)) * 100)
	if frac < 0 {
		frac = -frac
	}
	if frac < 10 {
		return "$" + itoa(whole) + ".0" + itoa(frac)
	}
	return "$" + itoa(whole) + "." + itoa(frac)
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
