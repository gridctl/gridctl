// Package metrics provides token usage metrics collection and aggregation.
package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// TokenCounts holds input/output/total token counts.
type TokenCounts struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// FormatSavings tracks token savings from output formatting.
type FormatSavings struct {
	OriginalTokens  int64   `json:"original_tokens"`
	FormattedTokens int64   `json:"formatted_tokens"`
	SavedTokens     int64   `json:"saved_tokens"`
	SavingsPercent  float64 `json:"savings_percent"`
}

// TokenUsage is the top-level token usage snapshot returned by the API.
type TokenUsage struct {
	Session    TokenCounts                    `json:"session"`
	PerServer  map[string]TokenCounts         `json:"per_server"`
	PerReplica map[string]map[int]TokenCounts `json:"per_replica,omitempty"`
	// PerClient groups token usage by the originating MCP client (for example
	// "claude-code", "cursor"). The field is omitempty so consumers built
	// before per-client attribution shipped continue to see the same JSON
	// shape. Future per-user / per-team dimensions land as sibling fields
	// (per_user, per_team) under this same shape rather than reshaping
	// per_client.
	PerClient     map[string]TokenCounts `json:"per_client,omitempty"`
	FormatSavings FormatSavings          `json:"format_savings"`
}

// DataPoint is a single time-series data point with token counts.
type DataPoint struct {
	Timestamp    time.Time `json:"timestamp"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	TotalTokens  int64     `json:"total_tokens"`
}

// TimeSeriesResponse is returned by the historical metrics endpoint.
type TimeSeriesResponse struct {
	Range     string                 `json:"range"`
	Interval  string                 `json:"interval"`
	Points    []DataPoint            `json:"data_points"`
	PerServer map[string][]DataPoint `json:"per_server"`
}

// bucket holds accumulated token counts for a single minute.
type bucket struct {
	timestamp    time.Time
	inputTokens  int64
	outputTokens int64
}

// bucketKey returns the minute-aligned key for a timestamp.
func bucketKey(t time.Time) time.Time {
	return t.Truncate(time.Minute)
}

// serverCounters holds atomic counters for a single server.
type serverCounters struct {
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
}

// replicaCounters holds atomic counters for a single replica. Keyed by
// (serverName, replicaID) in the accumulator so existing per-server aggregates
// stay untouched.
type replicaCounters struct {
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
}

// clientCounters holds per-client atomic counters for token aggregates.
// Keyed by the normalized client ID (mcp.NormalizeClientID). The
// cardinality is bounded by the number of distinct MCP clients (~10s in
// practice), so the map fits easily under the same RWMutex pattern used
// for per-server aggregates.
type clientCounters struct {
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
}

// ToolStat is the snapshot shape for per-(server, tool) call tracking.
// Used by pkg/optimize to detect tools that have not seen any calls
// inside a freshness window and to attribute observed spend per tool.
// Calls is the cumulative count since the accumulator was created or
// last cleared; LastCalledAt is the wall-clock time the most recent call
// was recorded, or the zero value when no calls have been recorded.
// InputTokens/OutputTokens are the cumulative token counts of the tool's
// own calls. The token fields are omitempty so persisted lines written
// before per-tool attribution stay byte-identical.
type ToolStat struct {
	Calls        int64     `json:"calls"`
	LastCalledAt time.Time `json:"last_called_at,omitempty"`
	InputTokens  int64     `json:"input_tokens,omitempty"`
	OutputTokens int64     `json:"output_tokens,omitempty"`
}

// toolUsage holds per-(server, tool) atomic counters. lastCalledNanos
// stores time.UnixNano so the read path can produce a time.Time without
// taking a lock. Keyed by (serverName -> toolName) in the accumulator.
type toolUsage struct {
	calls           atomic.Int64
	lastCalledNanos atomic.Int64
	inputTokens     atomic.Int64
	outputTokens    atomic.Int64
}

// promptUsage holds per-skill atomic counters for prompts/get serving. Same
// shape as toolUsage but keyed by a single skill (prompt) name in the
// accumulator. Kept in a separate namespace from toolUsage so prompt serving
// never appears in Tools Audit Mode. lastCalledNanos stores time.UnixNano so
// the read path can produce a time.Time without taking a lock.
type promptUsage struct {
	calls           atomic.Int64
	lastCalledNanos atomic.Int64
}

// Accumulator collects token usage metrics with thread-safe operations.
// Session totals use atomic counters. Historical data is stored in a ring buffer
// of pre-aggregated 1-minute time buckets.
type Accumulator struct {
	// startedAt is set when NewAccumulator is called and never reset by
	// Clear. Consumers (e.g. pkg/optimize) use it to gate findings that
	// require a minimum observation window.
	startedAt time.Time

	// Session totals (atomic for lock-free reads)
	sessionInput  atomic.Int64
	sessionOutput atomic.Int64

	// Per-server totals
	serverMu sync.RWMutex
	servers  map[string]*serverCounters

	// Per-replica totals. Cardinality is bounded by the server replica limit
	// (config validation caps replicas at 32) so the outer map scales with
	// server count and the inner map with replica count — a small product.
	replicaMu sync.RWMutex
	replicas  map[string]map[int]*replicaCounters

	// Ring buffer of 1-minute buckets
	bufMu    sync.RWMutex
	buckets  []bucket
	maxSize  int
	position int
	wrapped  bool

	// Per-server ring buffers
	serverBufMu sync.RWMutex
	serverBufs  map[string]*serverBuffer

	// Per-client token totals. Cardinality is bounded by the number of
	// distinct MCP clients seen on the gateway (~10s in practice).
	clientMu sync.RWMutex
	clients  map[string]*clientCounters

	// Per-(server, tool) call counters. Powers the unused_tool optimize
	// heuristic: a tool registered on a server but absent from this map
	// (or with a stale LastCalledAt) has not been called recently.
	toolUsageMu sync.RWMutex
	toolUsage   map[string]map[string]*toolUsage

	// Per-skill prompts/get call counters. Powers the Skills Library
	// "Never used" facet. Kept in a separate namespace from toolUsage so
	// prompt serving never pollutes Tools Audit Mode.
	promptUsageMu sync.RWMutex
	promptUsage   map[string]*promptUsage

	// Format savings (atomic for lock-free reads)
	savingsOriginal  atomic.Int64
	savingsFormatted atomic.Int64
}

// serverBuffer is a per-server ring buffer of minute buckets.
type serverBuffer struct {
	buckets  []bucket
	maxSize  int
	position int
	wrapped  bool
}

// NewAccumulator creates a metrics accumulator with the given ring buffer capacity.
// Each slot holds one minute of aggregated data, so 10000 slots ≈ ~7 days.
func NewAccumulator(maxDataPoints int) *Accumulator {
	if maxDataPoints <= 0 {
		maxDataPoints = 10000
	}
	return &Accumulator{
		startedAt:    time.Now(),
		servers:      make(map[string]*serverCounters),
		replicas:     make(map[string]map[int]*replicaCounters),
		buckets:      make([]bucket, maxDataPoints),
		maxSize:      maxDataPoints,
		serverBufs:  make(map[string]*serverBuffer),
		clients:     make(map[string]*clientCounters),
		toolUsage:   make(map[string]map[string]*toolUsage),
		promptUsage: make(map[string]*promptUsage),
	}
}

// StartedAt returns the wall-clock time the accumulator was created.
// Clear does not reset this value — the start-of-observation
// window stays anchored to the gateway lifetime, which is what
// pkg/optimize uses to gate "<24h of data" findings.
func (a *Accumulator) StartedAt() time.Time {
	return a.startedAt
}

// Record adds a token usage observation from a tool call. Equivalent to
// RecordReplica with replicaID=-1 (i.e. do not attribute to a replica).
func (a *Accumulator) Record(serverName string, inputTokens, outputTokens int) {
	a.RecordReplica(serverName, -1, inputTokens, outputTokens)
}

// RecordReplica adds a token usage observation attributed to a specific
// replica. Per-server aggregates are updated in all cases. Pass replicaID < 0
// to skip the per-replica update (used for servers that are not part of a
// replica set).
func (a *Accumulator) RecordReplica(serverName string, replicaID, inputTokens, outputTokens int) {
	a.RecordReplicaWithClient(serverName, replicaID, "", inputTokens, outputTokens)
}

// RecordReplicaWithClient is the client-aware variant of RecordReplica. It
// updates the per-client token counters in addition to session, per-server,
// and per-replica aggregates. An empty clientID skips the per-client update,
// matching the replicaID < 0 convention so callers without attribution can
// continue to use the same code path.
func (a *Accumulator) RecordReplicaWithClient(serverName string, replicaID int, clientID string, inputTokens, outputTokens int) {
	input := int64(inputTokens)
	output := int64(outputTokens)

	// Update session totals
	a.sessionInput.Add(input)
	a.sessionOutput.Add(output)

	// Update per-server totals
	a.serverMu.RLock()
	sc, ok := a.servers[serverName]
	a.serverMu.RUnlock()

	if !ok {
		a.serverMu.Lock()
		sc, ok = a.servers[serverName]
		if !ok {
			sc = &serverCounters{}
			a.servers[serverName] = sc
		}
		a.serverMu.Unlock()
	}
	sc.inputTokens.Add(input)
	sc.outputTokens.Add(output)

	if replicaID >= 0 {
		rc := a.getOrCreateReplicaCounters(serverName, replicaID)
		rc.inputTokens.Add(input)
		rc.outputTokens.Add(output)
	}

	if clientID != "" {
		cc := a.getOrCreateClientCounters(clientID)
		cc.inputTokens.Add(input)
		cc.outputTokens.Add(output)
	}

	// Update time-series ring buffer
	now := bucketKey(time.Now())
	a.addToBucket(now, input, output)
	a.addToServerBucket(serverName, now, input, output)
}

// getOrCreateClientCounters returns the per-client counter bucket, creating
// it on first use. Safe for concurrent access; uses the same
// double-checked-locking pattern as the per-server map.
func (a *Accumulator) getOrCreateClientCounters(clientID string) *clientCounters {
	a.clientMu.RLock()
	cc, ok := a.clients[clientID]
	a.clientMu.RUnlock()
	if ok {
		return cc
	}

	a.clientMu.Lock()
	defer a.clientMu.Unlock()
	cc, ok = a.clients[clientID]
	if !ok {
		cc = &clientCounters{}
		a.clients[clientID] = cc
	}
	return cc
}

// getOrCreateReplicaCounters returns the per-replica counter bucket, creating
// it on first use. Safe for concurrent access.
func (a *Accumulator) getOrCreateReplicaCounters(serverName string, replicaID int) *replicaCounters {
	a.replicaMu.RLock()
	if m, ok := a.replicas[serverName]; ok {
		if rc, ok := m[replicaID]; ok {
			a.replicaMu.RUnlock()
			return rc
		}
	}
	a.replicaMu.RUnlock()

	a.replicaMu.Lock()
	defer a.replicaMu.Unlock()
	m, ok := a.replicas[serverName]
	if !ok {
		m = make(map[int]*replicaCounters)
		a.replicas[serverName] = m
	}
	rc, ok := m[replicaID]
	if !ok {
		rc = &replicaCounters{}
		m[replicaID] = rc
	}
	return rc
}

// RecordToolCall increments per-(server, tool) call counters and stamps
// the last-called timestamp. Used by pkg/optimize's unused_tool heuristic.
//
// An empty serverName or toolName is a no-op so callers without per-tool
// attribution (legacy ToolCallObserver path) can invoke unconditionally.
func (a *Accumulator) RecordToolCall(serverName, toolName string) {
	if serverName == "" || toolName == "" {
		return
	}
	tu := a.getOrCreateToolUsage(serverName, toolName)
	tu.calls.Add(1)
	tu.lastCalledNanos.Store(time.Now().UnixNano())
}

// RecordToolCallUsage is RecordToolCall plus the call's token counts: one
// bucket lookup increments the call counter, stamps the last-called
// timestamp, and adds input/output tokens, so the observer's hot path
// touches the tool-usage map once per call.
//
// An empty serverName or toolName is a no-op so callers without per-tool
// attribution (legacy ToolCallObserver path) can invoke unconditionally.
func (a *Accumulator) RecordToolCallUsage(serverName, toolName string, inputTokens, outputTokens int) {
	if serverName == "" || toolName == "" {
		return
	}
	tu := a.getOrCreateToolUsage(serverName, toolName)
	tu.calls.Add(1)
	tu.lastCalledNanos.Store(time.Now().UnixNano())
	tu.inputTokens.Add(int64(inputTokens))
	tu.outputTokens.Add(int64(outputTokens))
}

func (a *Accumulator) getOrCreateToolUsage(serverName, toolName string) *toolUsage {
	a.toolUsageMu.RLock()
	if m, ok := a.toolUsage[serverName]; ok {
		if tu, ok := m[toolName]; ok {
			a.toolUsageMu.RUnlock()
			return tu
		}
	}
	a.toolUsageMu.RUnlock()

	a.toolUsageMu.Lock()
	defer a.toolUsageMu.Unlock()
	m, ok := a.toolUsage[serverName]
	if !ok {
		m = make(map[string]*toolUsage)
		a.toolUsage[serverName] = m
	}
	tu, ok := m[toolName]
	if !ok {
		tu = &toolUsage{}
		m[toolName] = tu
	}
	return tu
}

// ToolUsageSnapshot returns a deep copy of the per-(server, tool) call
// counters. Empty when no per-tool calls have been recorded (typical for
// gateways still on the legacy ToolCallObserver path).
func (a *Accumulator) ToolUsageSnapshot() map[string]map[string]ToolStat {
	a.toolUsageMu.RLock()
	defer a.toolUsageMu.RUnlock()
	if len(a.toolUsage) == 0 {
		return nil
	}
	out := make(map[string]map[string]ToolStat, len(a.toolUsage))
	for serverName, tools := range a.toolUsage {
		inner := make(map[string]ToolStat, len(tools))
		for toolName, tu := range tools {
			calls := tu.calls.Load()
			var lastCalled time.Time
			if nanos := tu.lastCalledNanos.Load(); nanos > 0 {
				lastCalled = time.Unix(0, nanos)
			}
			inner[toolName] = ToolStat{
				Calls:        calls,
				LastCalledAt: lastCalled,
				InputTokens:  tu.inputTokens.Load(),
				OutputTokens: tu.outputTokens.Load(),
			}
		}
		out[serverName] = inner
	}
	return out
}

// RestoreToolUsage seeds per-(server, tool) call counters from a persisted
// snapshot so Audit Mode's usage history survives a gateway restart. Called
// on startup by telemetry.MetricsFlusher.SeedFromFile before the gateway
// serves traffic, so the counters it re-creates are the same *toolUsage
// buckets RecordToolCall increments afterward — live calls continue from the
// restored count rather than starting at zero.
//
// Tool-call attribution flows through the same observer for direct and
// code-mode calls (Gateway.CallTool → HandleToolsCall → Observer →
// RecordToolCall), so a restored snapshot reflects both equally.
//
// Restore is max-wins per counter — calls and tokens alike: an existing
// in-memory value is kept when it already exceeds the restored one
// (defensive against a seed racing late initialization; the counters are
// monotonic between resets, so max-wins never double-counts). Entries with no
// recorded calls are skipped so the snapshot stays sparse. An empty map is a
// no-op.
func (a *Accumulator) RestoreToolUsage(perServer map[string]map[string]ToolStat) {
	if len(perServer) == 0 {
		return
	}
	a.toolUsageMu.Lock()
	defer a.toolUsageMu.Unlock()
	for serverName, tools := range perServer {
		if serverName == "" {
			continue
		}
		for toolName, stat := range tools {
			if toolName == "" || stat.Calls <= 0 {
				continue
			}
			m, ok := a.toolUsage[serverName]
			if !ok {
				m = make(map[string]*toolUsage)
				a.toolUsage[serverName] = m
			}
			tu, ok := m[toolName]
			if !ok {
				tu = &toolUsage{}
				m[toolName] = tu
			}
			if stat.Calls > tu.calls.Load() {
				tu.calls.Store(stat.Calls)
			}
			if !stat.LastCalledAt.IsZero() {
				if nanos := stat.LastCalledAt.UnixNano(); nanos > tu.lastCalledNanos.Load() {
					tu.lastCalledNanos.Store(nanos)
				}
			}
			if stat.InputTokens > tu.inputTokens.Load() {
				tu.inputTokens.Store(stat.InputTokens)
			}
			if stat.OutputTokens > tu.outputTokens.Load() {
				tu.outputTokens.Store(stat.OutputTokens)
			}
		}
	}
}

// RecordPromptGet increments the call counter for a single skill (prompt)
// served via prompts/get and stamps the last-called timestamp. Powers the
// Skills Library "Never used" facet. An empty name is a no-op so callers
// without attribution can invoke unconditionally.
//
// Kept parallel to RecordToolCall rather than reusing it: routing prompt
// serving through the tool-usage map would surface synthetic entries in
// Tools Audit Mode.
func (a *Accumulator) RecordPromptGet(name string) {
	if name == "" {
		return
	}
	pu := a.getOrCreatePromptUsage(name)
	pu.calls.Add(1)
	pu.lastCalledNanos.Store(time.Now().UnixNano())
}

func (a *Accumulator) getOrCreatePromptUsage(name string) *promptUsage {
	a.promptUsageMu.RLock()
	if pu, ok := a.promptUsage[name]; ok {
		a.promptUsageMu.RUnlock()
		return pu
	}
	a.promptUsageMu.RUnlock()

	a.promptUsageMu.Lock()
	defer a.promptUsageMu.Unlock()
	pu, ok := a.promptUsage[name]
	if !ok {
		pu = &promptUsage{}
		a.promptUsage[name] = pu
	}
	return pu
}

// PromptUsageSnapshot returns a deep copy of the per-skill prompts/get call
// counters. Empty (nil) when no prompt has been served yet. Reuses the
// ToolStat value shape so the persistence and API layers share one type.
func (a *Accumulator) PromptUsageSnapshot() map[string]ToolStat {
	a.promptUsageMu.RLock()
	defer a.promptUsageMu.RUnlock()
	if len(a.promptUsage) == 0 {
		return nil
	}
	out := make(map[string]ToolStat, len(a.promptUsage))
	for name, pu := range a.promptUsage {
		calls := pu.calls.Load()
		var lastCalled time.Time
		if nanos := pu.lastCalledNanos.Load(); nanos > 0 {
			lastCalled = time.Unix(0, nanos)
		}
		out[name] = ToolStat{Calls: calls, LastCalledAt: lastCalled}
	}
	return out
}

// RestorePromptUsage seeds per-skill prompts/get counters from a persisted
// snapshot so usage history survives a gateway restart. Mirrors
// RestoreToolUsage: max-wins per counter (an existing in-memory value is kept
// when it already exceeds the restored one), entries with no recorded calls
// are skipped, and an empty map is a no-op.
func (a *Accumulator) RestorePromptUsage(perSkill map[string]ToolStat) {
	if len(perSkill) == 0 {
		return
	}
	a.promptUsageMu.Lock()
	defer a.promptUsageMu.Unlock()
	for name, stat := range perSkill {
		if name == "" || stat.Calls <= 0 {
			continue
		}
		pu, ok := a.promptUsage[name]
		if !ok {
			pu = &promptUsage{}
			a.promptUsage[name] = pu
		}
		if stat.Calls > pu.calls.Load() {
			pu.calls.Store(stat.Calls)
		}
		if !stat.LastCalledAt.IsZero() {
			if nanos := stat.LastCalledAt.UnixNano(); nanos > pu.lastCalledNanos.Load() {
				pu.lastCalledNanos.Store(nanos)
			}
		}
	}
}

// RecordFormatSavings records token counts before and after format conversion.
// Normal token usage tracking is handled separately by the ToolCallObserver;
// this method only tracks the format savings delta.
func (a *Accumulator) RecordFormatSavings(serverName string, originalTokens, formattedTokens int) {
	a.savingsOriginal.Add(int64(originalTokens))
	a.savingsFormatted.Add(int64(formattedTokens))
}

// addToBucket adds tokens to the aggregate ring buffer for the given minute.
func (a *Accumulator) addToBucket(ts time.Time, input, output int64) {
	a.bufMu.Lock()
	defer a.bufMu.Unlock()

	// Check if the current position's bucket matches the timestamp
	idx := a.position
	if idx > 0 || a.wrapped {
		// Look at the last written position
		lastIdx := idx - 1
		if lastIdx < 0 {
			lastIdx = a.maxSize - 1
		}
		if a.buckets[lastIdx].timestamp.Equal(ts) {
			a.buckets[lastIdx].inputTokens += input
			a.buckets[lastIdx].outputTokens += output
			return
		}
	}

	// New minute bucket
	a.buckets[idx] = bucket{
		timestamp:    ts,
		inputTokens:  input,
		outputTokens: output,
	}
	a.position++
	if a.position >= a.maxSize {
		a.position = 0
		a.wrapped = true
	}
}

// addToServerBucket adds tokens to a per-server ring buffer.
func (a *Accumulator) addToServerBucket(serverName string, ts time.Time, input, output int64) {
	a.serverBufMu.RLock()
	sb, ok := a.serverBufs[serverName]
	a.serverBufMu.RUnlock()

	if !ok {
		a.serverBufMu.Lock()
		sb, ok = a.serverBufs[serverName]
		if !ok {
			sb = &serverBuffer{
				buckets: make([]bucket, a.maxSize),
				maxSize: a.maxSize,
			}
			a.serverBufs[serverName] = sb
		}
		a.serverBufMu.Unlock()
	}

	a.serverBufMu.Lock()
	defer a.serverBufMu.Unlock()

	idx := sb.position
	if idx > 0 || sb.wrapped {
		lastIdx := idx - 1
		if lastIdx < 0 {
			lastIdx = sb.maxSize - 1
		}
		if sb.buckets[lastIdx].timestamp.Equal(ts) {
			sb.buckets[lastIdx].inputTokens += input
			sb.buckets[lastIdx].outputTokens += output
			return
		}
	}

	sb.buckets[idx] = bucket{
		timestamp:    ts,
		inputTokens:  input,
		outputTokens: output,
	}
	sb.position++
	if sb.position >= sb.maxSize {
		sb.position = 0
		sb.wrapped = true
	}
}

// Snapshot returns the current token usage summary.
func (a *Accumulator) Snapshot() TokenUsage {
	input := a.sessionInput.Load()
	output := a.sessionOutput.Load()

	a.serverMu.RLock()
	perServer := make(map[string]TokenCounts, len(a.servers))
	for name, sc := range a.servers {
		si := sc.inputTokens.Load()
		so := sc.outputTokens.Load()
		perServer[name] = TokenCounts{
			InputTokens:  si,
			OutputTokens: so,
			TotalTokens:  si + so,
		}
	}
	a.serverMu.RUnlock()

	a.replicaMu.RLock()
	var perReplica map[string]map[int]TokenCounts
	if len(a.replicas) > 0 {
		perReplica = make(map[string]map[int]TokenCounts, len(a.replicas))
		for name, m := range a.replicas {
			inner := make(map[int]TokenCounts, len(m))
			for id, rc := range m {
				ri := rc.inputTokens.Load()
				ro := rc.outputTokens.Load()
				inner[id] = TokenCounts{
					InputTokens:  ri,
					OutputTokens: ro,
					TotalTokens:  ri + ro,
				}
			}
			perReplica[name] = inner
		}
	}
	a.replicaMu.RUnlock()

	a.clientMu.RLock()
	var perClient map[string]TokenCounts
	if len(a.clients) > 0 {
		perClient = make(map[string]TokenCounts, len(a.clients))
		for name, cc := range a.clients {
			ci := cc.inputTokens.Load()
			co := cc.outputTokens.Load()
			perClient[name] = TokenCounts{
				InputTokens:  ci,
				OutputTokens: co,
				TotalTokens:  ci + co,
			}
		}
	}
	a.clientMu.RUnlock()

	// Compute format savings
	origTokens := a.savingsOriginal.Load()
	fmtTokens := a.savingsFormatted.Load()
	savedTokens := origTokens - fmtTokens
	var savingsPct float64
	if origTokens > 0 {
		savingsPct = float64(savedTokens) / float64(origTokens) * 100
	}

	return TokenUsage{
		Session: TokenCounts{
			InputTokens:  input,
			OutputTokens: output,
			TotalTokens:  input + output,
		},
		PerServer:  perServer,
		PerReplica: perReplica,
		PerClient:  perClient,
		FormatSavings: FormatSavings{
			OriginalTokens:  origTokens,
			FormattedTokens: fmtTokens,
			SavedTokens:     savedTokens,
			SavingsPercent:  savingsPct,
		},
	}
}

// Query returns historical time-series data for the given duration.
// For ranges > 6h, data points are downsampled to hourly buckets.
func (a *Accumulator) Query(duration time.Duration) TimeSeriesResponse {
	cutoff := time.Now().Add(-duration)
	downsample := duration > 6*time.Hour

	rangeName := formatRange(duration)
	interval := "1m"
	if downsample {
		interval = "1h"
	}

	points := a.queryBuffer(cutoff, downsample)

	a.serverBufMu.RLock()
	perServer := make(map[string][]DataPoint, len(a.serverBufs))
	for name, sb := range a.serverBufs {
		perServer[name] = queryServerBuffer(sb, cutoff, downsample)
	}
	a.serverBufMu.RUnlock()

	return TimeSeriesResponse{
		Range:     rangeName,
		Interval:  interval,
		Points:    points,
		PerServer: perServer,
	}
}

// queryBuffer reads from the aggregate ring buffer, optionally downsampling.
func (a *Accumulator) queryBuffer(cutoff time.Time, downsample bool) []DataPoint {
	a.bufMu.RLock()
	defer a.bufMu.RUnlock()

	raw := extractBuckets(a.buckets, a.maxSize, a.position, a.wrapped, cutoff)
	if downsample {
		return downsampleToHour(raw)
	}
	return toDataPoints(raw)
}

// queryServerBuffer reads from a per-server ring buffer.
func queryServerBuffer(sb *serverBuffer, cutoff time.Time, downsample bool) []DataPoint {
	raw := extractBuckets(sb.buckets, sb.maxSize, sb.position, sb.wrapped, cutoff)
	if downsample {
		return downsampleToHour(raw)
	}
	return toDataPoints(raw)
}

// extractBuckets reads all buckets after cutoff from a ring buffer.
func extractBuckets(buckets []bucket, maxSize, position int, wrapped bool, cutoff time.Time) []bucket {
	count := position
	if wrapped {
		count = maxSize
	}

	var result []bucket
	start := 0
	if wrapped {
		start = position
	}

	for i := 0; i < count; i++ {
		idx := (start + i) % maxSize
		b := buckets[idx]
		if b.timestamp.IsZero() {
			continue
		}
		if b.timestamp.Before(cutoff) {
			continue
		}
		result = append(result, b)
	}
	return result
}

// toDataPoints converts raw buckets to API data points.
func toDataPoints(buckets []bucket) []DataPoint {
	points := make([]DataPoint, len(buckets))
	for i, b := range buckets {
		points[i] = DataPoint{
			Timestamp:    b.timestamp,
			InputTokens:  b.inputTokens,
			OutputTokens: b.outputTokens,
			TotalTokens:  b.inputTokens + b.outputTokens,
		}
	}
	return points
}

// downsampleToHour aggregates minute-level buckets into hourly buckets.
func downsampleToHour(buckets []bucket) []DataPoint {
	if len(buckets) == 0 {
		return nil
	}

	hourly := make(map[time.Time]*DataPoint)
	var order []time.Time

	for _, b := range buckets {
		hourKey := b.timestamp.Truncate(time.Hour)
		dp, ok := hourly[hourKey]
		if !ok {
			dp = &DataPoint{Timestamp: hourKey}
			hourly[hourKey] = dp
			order = append(order, hourKey)
		}
		dp.InputTokens += b.inputTokens
		dp.OutputTokens += b.outputTokens
		dp.TotalTokens += b.inputTokens + b.outputTokens
	}

	result := make([]DataPoint, len(order))
	for i, key := range order {
		result[i] = *hourly[key]
	}
	return result
}

// Restore replaces per-server token totals with the supplied map and
// recomputes session totals as the sum across all servers (matching the
// invariant Record/RecordReplica maintains). Used on daemon startup to
// repopulate cumulative counters from a persisted metrics.jsonl file.
//
// Existing per-server counters are overwritten for any server present in the
// map; servers absent from the map retain their current state. Replicas and
// format-savings counters are not restored — those carry no on-disk
// equivalent in the snapshot format. Time-series ring buckets are populated
// separately via ReplaySnapshot.
func (a *Accumulator) Restore(perServer map[string]TokenCounts) {
	if len(perServer) == 0 {
		return
	}

	a.serverMu.Lock()
	defer a.serverMu.Unlock()

	for name, counts := range perServer {
		sc, ok := a.servers[name]
		if !ok {
			sc = &serverCounters{}
			a.servers[name] = sc
		}
		sc.inputTokens.Store(counts.InputTokens)
		sc.outputTokens.Store(counts.OutputTokens)
	}

	var sessionIn, sessionOut int64
	for _, sc := range a.servers {
		sessionIn += sc.inputTokens.Load()
		sessionOut += sc.outputTokens.Load()
	}
	a.sessionInput.Store(sessionIn)
	a.sessionOutput.Store(sessionOut)
}

// ReplaySnapshot adds a historical observation to the time-series ring
// buffers (aggregate + per-server) without touching cumulative counters.
// Used by telemetry.MetricsFlusher.SeedFromFile to rehydrate per-minute
// bucket history from each persisted Diff line — the chart shows pre-restart
// activity continuously alongside live data instead of resetting to a single
// post-restart point.
//
// Cumulative counters are restored separately via Restore. Calling both
// with the same source data reproduces the on-disk state.
//
// ts is bucketed to the minute via the same key the live Record path uses,
// so chronological replay produces one bucket per flush minute and live
// observations after replay continue advancing the same ring naturally.
func (a *Accumulator) ReplaySnapshot(serverName string, ts time.Time, inputTokens, outputTokens int64) {
	if inputTokens == 0 && outputTokens == 0 {
		return
	}
	bucket := bucketKey(ts)
	a.addToBucket(bucket, inputTokens, outputTokens)
	if serverName != "" {
		a.addToServerBucket(serverName, bucket, inputTokens, outputTokens)
	}
}

// Clear resets all metrics — session totals, per-server totals, and history.
func (a *Accumulator) Clear() {
	a.sessionInput.Store(0)
	a.sessionOutput.Store(0)

	a.serverMu.Lock()
	a.servers = make(map[string]*serverCounters)
	a.serverMu.Unlock()

	a.replicaMu.Lock()
	a.replicas = make(map[string]map[int]*replicaCounters)
	a.replicaMu.Unlock()

	a.clientMu.Lock()
	a.clients = make(map[string]*clientCounters)
	a.clientMu.Unlock()

	a.bufMu.Lock()
	a.buckets = make([]bucket, a.maxSize)
	a.position = 0
	a.wrapped = false
	a.bufMu.Unlock()

	a.serverBufMu.Lock()
	a.serverBufs = make(map[string]*serverBuffer)
	a.serverBufMu.Unlock()

	a.toolUsageMu.Lock()
	a.toolUsage = make(map[string]map[string]*toolUsage)
	a.toolUsageMu.Unlock()

	a.promptUsageMu.Lock()
	a.promptUsage = make(map[string]*promptUsage)
	a.promptUsageMu.Unlock()

	a.savingsOriginal.Store(0)
	a.savingsFormatted.Store(0)
}

// formatRange returns a human-readable range string for a duration.
func formatRange(d time.Duration) string {
	switch {
	case d <= 30*time.Minute:
		return "30m"
	case d <= time.Hour:
		return "1h"
	case d <= 6*time.Hour:
		return "6h"
	case d <= 24*time.Hour:
		return "24h"
	default:
		return "7d"
	}
}
