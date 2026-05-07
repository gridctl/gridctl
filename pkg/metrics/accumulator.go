// Package metrics provides token usage metrics collection and aggregation.
package metrics

import (
	"math"
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
	Session       TokenCounts                        `json:"session"`
	PerServer     map[string]TokenCounts             `json:"per_server"`
	PerReplica    map[string]map[int]TokenCounts     `json:"per_replica,omitempty"`
	// PerClient groups token usage by the originating MCP client (for example
	// "claude-code", "cursor"). The field is omitempty so consumers built
	// before per-client attribution shipped continue to see the same JSON
	// shape. Future per-user / per-team dimensions land as sibling fields
	// (per_user, per_team) under this same shape rather than reshaping
	// per_client.
	PerClient     map[string]TokenCounts             `json:"per_client,omitempty"`
	FormatSavings FormatSavings                      `json:"format_savings"`
}

// CostBreakdown is the per-call USD cost split passed to RecordCost. Cache
// fields are priced separately from input tokens to match LiteLLM's cache
// rate fields — conflating them mis-prices providers like Anthropic by
// roughly an order of magnitude.
type CostBreakdown struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// IsZero reports whether all components are zero. Used by RecordCost to
// short-circuit accumulator updates when a tool call has no priceable
// usage (unknown model, all-zero token counts).
func (c CostBreakdown) IsZero() bool {
	return c.Input == 0 && c.Output == 0 && c.CacheRead == 0 && c.CacheWrite == 0
}

// IsValid reports whether every component is finite and non-negative.
// A misconfigured Source could in theory return NaN/Inf rates or a
// negative Calculate result; recording those into atomic counters would
// permanently corrupt the snapshot. RecordCost drops invalid breakdowns.
func (c CostBreakdown) IsValid() bool {
	for _, v := range [4]float64{c.Input, c.Output, c.CacheRead, c.CacheWrite} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return false
		}
	}
	return true
}

// CostCounts is the snapshot shape for a single dimension (session,
// per-server, per-replica) of cost accumulation. All values are USD.
// Cache fields are omitempty so consumers that only care about
// input/output costs are not forced to render zeroes.
type CostCounts struct {
	InputUSD      float64 `json:"input_usd"`
	OutputUSD     float64 `json:"output_usd"`
	CacheReadUSD  float64 `json:"cache_read_usd,omitempty"`
	CacheWriteUSD float64 `json:"cache_write_usd,omitempty"`
	TotalUSD      float64 `json:"total_usd"`
}

// CostUsage is the top-level cost snapshot. The shape mirrors TokenUsage so
// API consumers can render cost charts beside token charts.
type CostUsage struct {
	Session    CostCounts                       `json:"session"`
	PerServer  map[string]CostCounts            `json:"per_server"`
	PerReplica map[string]map[int]CostCounts    `json:"per_replica,omitempty"`
	// PerClient groups USD cost by the originating MCP client. omitempty so
	// pre-attribution consumers keep their existing JSON shape.
	PerClient  map[string]CostCounts            `json:"per_client,omitempty"`
}

// CostDataPoint is the time-series shape for cost-over-time queries.
type CostDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	USD       float64   `json:"usd"`
}

// CostTimeSeriesResponse is the cost analogue of TimeSeriesResponse. The
// `Range` and `Interval` strings reuse the same vocabulary as the token
// time-series so charts can share a time-range selector.
type CostTimeSeriesResponse struct {
	Range     string                       `json:"range"`
	Interval  string                       `json:"interval"`
	Points    []CostDataPoint              `json:"data_points"`
	PerServer map[string][]CostDataPoint   `json:"per_server"`
	// PerClient groups cost over time by originating MCP client. Populated
	// only when the API caller requests per-client grouping (the
	// `per_client=true` query parameter on /api/metrics/cost) so the JSON
	// stays compact for the common per-server view.
	PerClient map[string][]CostDataPoint   `json:"per_client,omitempty"`
}

// costScale converts the public USD float64 values to the int64
// micro-USD representation used by the atomic counters and ring buffers.
// Internal accumulation uses fixed-point (1 unit = 1e-6 USD = 1 micro-USD)
// so additions are atomic int64 ops; conversion back to float64 happens
// only at snapshot time.
const costScale = 1_000_000

func usdToMicro(usd float64) int64 {
	if usd == 0 {
		return 0
	}
	return int64(usd * costScale)
}

func microToUSD(micro int64) float64 {
	if micro == 0 {
		return 0
	}
	return float64(micro) / costScale
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
	Range     string                       `json:"range"`
	Interval  string                       `json:"interval"`
	Points    []DataPoint                  `json:"data_points"`
	PerServer map[string][]DataPoint       `json:"per_server"`
}

// bucketKey returns the minute-aligned key for a timestamp.
func bucketKey(t time.Time) time.Time {
	return t.Truncate(time.Minute)
}

// bucket holds accumulated token counts and total USD cost for a single
// minute. Cost is stored as int64 micro-USD (1 unit = 1e-6 USD) so the
// addition is a plain integer write under the bucket mutex; precision below
// nano-USD is irrelevant for any realistic call.
type bucket struct {
	timestamp     time.Time
	inputTokens   int64
	outputTokens  int64
	costMicroUSD  int64
}

// serverCounters holds atomic counters for a single server.
//
// Cost components are stored as int64 micro-USD so they share the
// lock-free atomic.Int64 pattern used for tokens. The float64 USD shape
// is reconstructed at Snapshot time.
type serverCounters struct {
	inputTokens  atomic.Int64
	outputTokens atomic.Int64

	inputCostMicroUSD      atomic.Int64
	outputCostMicroUSD     atomic.Int64
	cacheReadCostMicroUSD  atomic.Int64
	cacheWriteCostMicroUSD atomic.Int64
}

// replicaCounters holds atomic counters for a single replica. Keyed by
// (serverName, replicaID) in the accumulator so existing per-server aggregates
// stay untouched.
type replicaCounters struct {
	inputTokens  atomic.Int64
	outputTokens atomic.Int64

	inputCostMicroUSD      atomic.Int64
	outputCostMicroUSD     atomic.Int64
	cacheReadCostMicroUSD  atomic.Int64
	cacheWriteCostMicroUSD atomic.Int64
}

// clientCounters holds per-client atomic counters for token + cost
// aggregates. Keyed by the normalized client ID (mcp.NormalizeClientID).
// The cardinality is bounded by the number of distinct MCP clients
// (~10s in practice), so the map fits easily under the same RWMutex
// pattern used for per-server aggregates.
type clientCounters struct {
	inputTokens  atomic.Int64
	outputTokens atomic.Int64

	inputCostMicroUSD      atomic.Int64
	outputCostMicroUSD     atomic.Int64
	cacheReadCostMicroUSD  atomic.Int64
	cacheWriteCostMicroUSD atomic.Int64
}

// ToolStat is the snapshot shape for per-(server, tool) call tracking.
// Used by pkg/optimize to detect tools that have not seen any calls
// inside a freshness window. Calls is the cumulative count since the
// accumulator was created or last cleared; LastCalledAt is the wall-clock
// time the most recent call was recorded, or the zero value when no
// calls have been recorded.
type ToolStat struct {
	Calls        int64     `json:"calls"`
	LastCalledAt time.Time `json:"last_called_at,omitempty"`
}

// toolUsage holds per-(server, tool) atomic counters. lastCalledNanos
// stores time.UnixNano so the read path can produce a time.Time without
// taking a lock. Keyed by (serverName -> toolName) in the accumulator.
type toolUsage struct {
	calls           atomic.Int64
	lastCalledNanos atomic.Int64
}

// Accumulator collects token usage metrics with thread-safe operations.
// Session totals use atomic counters. Historical data is stored in a ring buffer
// of pre-aggregated 1-minute time buckets.
type Accumulator struct {
	// startedAt is set when NewAccumulator is called and never reset by
	// Clear or ClearCost. Consumers (e.g. pkg/optimize) use it to gate
	// findings that require a minimum observation window.
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

	// Per-client totals (token + cost). Cardinality is bounded by the number
	// of distinct MCP clients seen on the gateway (~10s in practice).
	clientMu sync.RWMutex
	clients  map[string]*clientCounters

	// Per-client cost ring buffers, used to group /api/metrics/cost by the
	// originating client. Tokens are not bucketed per-client because the
	// existing TokenUsage / token time-series path is unchanged in PR 2.
	clientBufMu sync.RWMutex
	clientBufs  map[string]*serverBuffer

	// Per-(server, tool) call counters. Powers the unused_tool optimize
	// heuristic: a tool registered on a server but absent from this map
	// (or with a stale LastCalledAt) has not been called recently.
	toolUsageMu sync.RWMutex
	toolUsage   map[string]map[string]*toolUsage

	// Format savings (atomic for lock-free reads)
	savingsOriginal  atomic.Int64
	savingsFormatted atomic.Int64

	// Session-level cost totals (micro-USD, atomic). The per-component
	// breakdown is preserved separately so consumers can render
	// "input vs cache-read" without recomputing from token counts —
	// recomputation would be wrong when models drift mid-window.
	sessionInputCostMicroUSD      atomic.Int64
	sessionOutputCostMicroUSD     atomic.Int64
	sessionCacheReadCostMicroUSD  atomic.Int64
	sessionCacheWriteCostMicroUSD atomic.Int64
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
		startedAt:  time.Now(),
		servers:    make(map[string]*serverCounters),
		replicas:   make(map[string]map[int]*replicaCounters),
		buckets:    make([]bucket, maxDataPoints),
		maxSize:    maxDataPoints,
		serverBufs: make(map[string]*serverBuffer),
		clients:    make(map[string]*clientCounters),
		clientBufs: make(map[string]*serverBuffer),
		toolUsage:  make(map[string]map[string]*toolUsage),
	}
}

// StartedAt returns the wall-clock time the accumulator was created.
// Clear and ClearCost do not reset this value — the start-of-observation
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
			inner[toolName] = ToolStat{Calls: calls, LastCalledAt: lastCalled}
		}
		out[serverName] = inner
	}
	return out
}

// RecordFormatSavings records token counts before and after format conversion.
// Normal token usage tracking is handled separately by the ToolCallObserver;
// this method only tracks the format savings delta.
func (a *Accumulator) RecordFormatSavings(serverName string, originalTokens, formattedTokens int) {
	a.savingsOriginal.Add(int64(originalTokens))
	a.savingsFormatted.Add(int64(formattedTokens))
}

// RecordCost adds a per-call USD cost observation alongside the token
// observation that RecordReplica records. Pass replicaID < 0 to skip the
// per-replica update, mirroring RecordReplica.
//
// Cost MUST be computed at observation time, not derived from stored token
// totals at read time: a model change mid-window would otherwise mis-price
// earlier calls. Cache-read and cache-write components arrive as separate
// fields on CostBreakdown so the Snapshot shape can preserve the split.
func (a *Accumulator) RecordCost(serverName string, replicaID int, cost CostBreakdown) {
	a.RecordCostWithClient(serverName, replicaID, "", cost)
}

// RecordCostWithClient is the client-aware variant of RecordCost. The cost
// is added to the per-client cost aggregates and per-client cost ring
// buffer in addition to the session, per-server, and per-replica
// aggregates. An empty clientID skips the per-client update.
func (a *Accumulator) RecordCostWithClient(serverName string, replicaID int, clientID string, cost CostBreakdown) {
	if cost.IsZero() {
		return
	}
	if !cost.IsValid() {
		return
	}
	inputMicro := usdToMicro(cost.Input)
	outputMicro := usdToMicro(cost.Output)
	cacheReadMicro := usdToMicro(cost.CacheRead)
	cacheWriteMicro := usdToMicro(cost.CacheWrite)
	totalMicro := inputMicro + outputMicro + cacheReadMicro + cacheWriteMicro

	a.sessionInputCostMicroUSD.Add(inputMicro)
	a.sessionOutputCostMicroUSD.Add(outputMicro)
	a.sessionCacheReadCostMicroUSD.Add(cacheReadMicro)
	a.sessionCacheWriteCostMicroUSD.Add(cacheWriteMicro)

	sc := a.getOrCreateServerCounters(serverName)
	sc.inputCostMicroUSD.Add(inputMicro)
	sc.outputCostMicroUSD.Add(outputMicro)
	sc.cacheReadCostMicroUSD.Add(cacheReadMicro)
	sc.cacheWriteCostMicroUSD.Add(cacheWriteMicro)

	if replicaID >= 0 {
		rc := a.getOrCreateReplicaCounters(serverName, replicaID)
		rc.inputCostMicroUSD.Add(inputMicro)
		rc.outputCostMicroUSD.Add(outputMicro)
		rc.cacheReadCostMicroUSD.Add(cacheReadMicro)
		rc.cacheWriteCostMicroUSD.Add(cacheWriteMicro)
	}

	if clientID != "" {
		cc := a.getOrCreateClientCounters(clientID)
		cc.inputCostMicroUSD.Add(inputMicro)
		cc.outputCostMicroUSD.Add(outputMicro)
		cc.cacheReadCostMicroUSD.Add(cacheReadMicro)
		cc.cacheWriteCostMicroUSD.Add(cacheWriteMicro)
	}

	now := bucketKey(time.Now())
	a.addCostToBucket(now, totalMicro)
	a.addCostToServerBucket(serverName, now, totalMicro)
	if clientID != "" {
		a.addCostToClientBucket(clientID, now, totalMicro)
	}
}

// getOrCreateServerCounters returns the per-server counter bucket, creating
// it on first use. Safe for concurrent access. Used by RecordCost; the
// token RecordReplica path inlines the same double-checked-locking pattern.
func (a *Accumulator) getOrCreateServerCounters(serverName string) *serverCounters {
	a.serverMu.RLock()
	sc, ok := a.servers[serverName]
	a.serverMu.RUnlock()
	if ok {
		return sc
	}

	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	sc, ok = a.servers[serverName]
	if !ok {
		sc = &serverCounters{}
		a.servers[serverName] = sc
	}
	return sc
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

// addCostToBucket adds a USD cost (in micro-USD) to the aggregate ring
// buffer for the given minute. Mirrors addToBucket but updates the bucket's
// cost field. The bucket is created if no live slot for ts exists, even
// when token counts have not yet been recorded for that minute — cost can
// arrive on its own (e.g. a unit test pricing a fixture).
func (a *Accumulator) addCostToBucket(ts time.Time, costMicro int64) {
	a.bufMu.Lock()
	defer a.bufMu.Unlock()

	idx := a.position
	if idx > 0 || a.wrapped {
		lastIdx := idx - 1
		if lastIdx < 0 {
			lastIdx = a.maxSize - 1
		}
		if a.buckets[lastIdx].timestamp.Equal(ts) {
			a.buckets[lastIdx].costMicroUSD += costMicro
			return
		}
	}

	a.buckets[idx] = bucket{timestamp: ts, costMicroUSD: costMicro}
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

// addCostToServerBucket adds a USD cost (in micro-USD) to a per-server
// ring buffer, mirroring addToServerBucket. Creates the buffer if it does
// not exist yet so cost-only servers (rare in production but common in
// tests) still appear in time-series queries.
func (a *Accumulator) addCostToServerBucket(serverName string, ts time.Time, costMicro int64) {
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
			sb.buckets[lastIdx].costMicroUSD += costMicro
			return
		}
	}

	sb.buckets[idx] = bucket{timestamp: ts, costMicroUSD: costMicro}
	sb.position++
	if sb.position >= sb.maxSize {
		sb.position = 0
		sb.wrapped = true
	}
}

// addCostToClientBucket adds a USD cost (in micro-USD) to a per-client ring
// buffer, mirroring addCostToServerBucket. Used by RecordCostWithClient to
// power the per_client grouping on /api/metrics/cost.
func (a *Accumulator) addCostToClientBucket(clientID string, ts time.Time, costMicro int64) {
	a.clientBufMu.RLock()
	cb, ok := a.clientBufs[clientID]
	a.clientBufMu.RUnlock()

	if !ok {
		a.clientBufMu.Lock()
		cb, ok = a.clientBufs[clientID]
		if !ok {
			cb = &serverBuffer{
				buckets: make([]bucket, a.maxSize),
				maxSize: a.maxSize,
			}
			a.clientBufs[clientID] = cb
		}
		a.clientBufMu.Unlock()
	}

	a.clientBufMu.Lock()
	defer a.clientBufMu.Unlock()

	idx := cb.position
	if idx > 0 || cb.wrapped {
		lastIdx := idx - 1
		if lastIdx < 0 {
			lastIdx = cb.maxSize - 1
		}
		if cb.buckets[lastIdx].timestamp.Equal(ts) {
			cb.buckets[lastIdx].costMicroUSD += costMicro
			return
		}
	}

	cb.buckets[idx] = bucket{timestamp: ts, costMicroUSD: costMicro}
	cb.position++
	if cb.position >= cb.maxSize {
		cb.position = 0
		cb.wrapped = true
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

// CostSnapshot returns the current cost usage summary in USD. The shape
// mirrors Snapshot()'s TokenUsage so API responses can carry both side by
// side. Cache fields are non-zero only when RecordCost recorded cache
// usage; otherwise they are omitted from JSON via omitempty.
func (a *Accumulator) CostSnapshot() CostUsage {
	sessionInput := microToUSD(a.sessionInputCostMicroUSD.Load())
	sessionOutput := microToUSD(a.sessionOutputCostMicroUSD.Load())
	sessionCacheRead := microToUSD(a.sessionCacheReadCostMicroUSD.Load())
	sessionCacheWrite := microToUSD(a.sessionCacheWriteCostMicroUSD.Load())

	a.serverMu.RLock()
	perServer := make(map[string]CostCounts, len(a.servers))
	for name, sc := range a.servers {
		perServer[name] = readCostCounts(
			sc.inputCostMicroUSD.Load(),
			sc.outputCostMicroUSD.Load(),
			sc.cacheReadCostMicroUSD.Load(),
			sc.cacheWriteCostMicroUSD.Load(),
		)
	}
	a.serverMu.RUnlock()

	a.replicaMu.RLock()
	var perReplica map[string]map[int]CostCounts
	if len(a.replicas) > 0 {
		perReplica = make(map[string]map[int]CostCounts, len(a.replicas))
		for name, m := range a.replicas {
			inner := make(map[int]CostCounts, len(m))
			for id, rc := range m {
				inner[id] = readCostCounts(
					rc.inputCostMicroUSD.Load(),
					rc.outputCostMicroUSD.Load(),
					rc.cacheReadCostMicroUSD.Load(),
					rc.cacheWriteCostMicroUSD.Load(),
				)
			}
			perReplica[name] = inner
		}
	}
	a.replicaMu.RUnlock()

	a.clientMu.RLock()
	var perClient map[string]CostCounts
	if len(a.clients) > 0 {
		perClient = make(map[string]CostCounts, len(a.clients))
		for name, cc := range a.clients {
			perClient[name] = readCostCounts(
				cc.inputCostMicroUSD.Load(),
				cc.outputCostMicroUSD.Load(),
				cc.cacheReadCostMicroUSD.Load(),
				cc.cacheWriteCostMicroUSD.Load(),
			)
		}
	}
	a.clientMu.RUnlock()

	return CostUsage{
		Session: CostCounts{
			InputUSD:      sessionInput,
			OutputUSD:     sessionOutput,
			CacheReadUSD:  sessionCacheRead,
			CacheWriteUSD: sessionCacheWrite,
			TotalUSD:      sessionInput + sessionOutput + sessionCacheRead + sessionCacheWrite,
		},
		PerServer:  perServer,
		PerReplica: perReplica,
		PerClient:  perClient,
	}
}

// readCostCounts assembles a CostCounts from raw micro-USD atomic loads.
func readCostCounts(inputMicro, outputMicro, cacheReadMicro, cacheWriteMicro int64) CostCounts {
	in := microToUSD(inputMicro)
	out := microToUSD(outputMicro)
	cr := microToUSD(cacheReadMicro)
	cw := microToUSD(cacheWriteMicro)
	return CostCounts{
		InputUSD:      in,
		OutputUSD:     out,
		CacheReadUSD:  cr,
		CacheWriteUSD: cw,
		TotalUSD:      in + out + cr + cw,
	}
}

// QueryCost returns historical cost-over-time data for the given duration.
// For ranges > 6h, data points are downsampled to hourly buckets, matching
// the Query (token) behavior so charts can share the same time-range
// selector. The PerClient map on the response is left nil; call
// QueryCostByClient when caller asks for per-client grouping.
func (a *Accumulator) QueryCost(duration time.Duration) CostTimeSeriesResponse {
	return a.queryCost(duration, false)
}

// QueryCostByClient is QueryCost with per-client grouping enabled. The
// returned response has its PerClient field populated alongside PerServer
// so consumers can render either dimension off a single response.
func (a *Accumulator) QueryCostByClient(duration time.Duration) CostTimeSeriesResponse {
	return a.queryCost(duration, true)
}

func (a *Accumulator) queryCost(duration time.Duration, includeClients bool) CostTimeSeriesResponse {
	cutoff := time.Now().Add(-duration)
	downsample := duration > 6*time.Hour

	rangeName := formatRange(duration)
	interval := "1m"
	if downsample {
		interval = "1h"
	}

	points := a.queryCostBuffer(cutoff, downsample)

	a.serverBufMu.RLock()
	perServer := make(map[string][]CostDataPoint, len(a.serverBufs))
	for name, sb := range a.serverBufs {
		perServer[name] = queryServerCostBuffer(sb, cutoff, downsample)
	}
	a.serverBufMu.RUnlock()

	resp := CostTimeSeriesResponse{
		Range:     rangeName,
		Interval:  interval,
		Points:    points,
		PerServer: perServer,
	}

	if includeClients {
		a.clientBufMu.RLock()
		perClient := make(map[string][]CostDataPoint, len(a.clientBufs))
		for name, cb := range a.clientBufs {
			perClient[name] = queryServerCostBuffer(cb, cutoff, downsample)
		}
		a.clientBufMu.RUnlock()
		resp.PerClient = perClient
	}
	return resp
}

func (a *Accumulator) queryCostBuffer(cutoff time.Time, downsample bool) []CostDataPoint {
	a.bufMu.RLock()
	defer a.bufMu.RUnlock()

	raw := extractBuckets(a.buckets, a.maxSize, a.position, a.wrapped, cutoff)
	if downsample {
		return downsampleCostToHour(raw)
	}
	return toCostDataPoints(raw)
}

func queryServerCostBuffer(sb *serverBuffer, cutoff time.Time, downsample bool) []CostDataPoint {
	raw := extractBuckets(sb.buckets, sb.maxSize, sb.position, sb.wrapped, cutoff)
	if downsample {
		return downsampleCostToHour(raw)
	}
	return toCostDataPoints(raw)
}

func toCostDataPoints(buckets []bucket) []CostDataPoint {
	points := make([]CostDataPoint, len(buckets))
	for i, b := range buckets {
		points[i] = CostDataPoint{
			Timestamp: b.timestamp,
			USD:       microToUSD(b.costMicroUSD),
		}
	}
	return points
}

func downsampleCostToHour(buckets []bucket) []CostDataPoint {
	if len(buckets) == 0 {
		return nil
	}
	hourly := make(map[time.Time]*CostDataPoint)
	var order []time.Time
	for _, b := range buckets {
		hourKey := b.timestamp.Truncate(time.Hour)
		dp, ok := hourly[hourKey]
		if !ok {
			dp = &CostDataPoint{Timestamp: hourKey}
			hourly[hourKey] = dp
			order = append(order, hourKey)
		}
		dp.USD += microToUSD(b.costMicroUSD)
	}
	result := make([]CostDataPoint, len(order))
	for i, key := range order {
		result[i] = *hourly[key]
	}
	return result
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
// Cumulative counters are restored separately via Restore. Calling both with
// the same source data reproduces the on-disk state.
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

	a.clientBufMu.Lock()
	a.clientBufs = make(map[string]*serverBuffer)
	a.clientBufMu.Unlock()

	a.toolUsageMu.Lock()
	a.toolUsage = make(map[string]map[string]*toolUsage)
	a.toolUsageMu.Unlock()

	a.savingsOriginal.Store(0)
	a.savingsFormatted.Store(0)

	a.sessionInputCostMicroUSD.Store(0)
	a.sessionOutputCostMicroUSD.Store(0)
	a.sessionCacheReadCostMicroUSD.Store(0)
	a.sessionCacheWriteCostMicroUSD.Store(0)
}

// ClearCost resets cost counters and cost ring-buffer values without
// touching token counters or format-savings state. Used by the
// `DELETE /api/metrics/cost` endpoint so operators can wipe cost data
// without losing token history.
func (a *Accumulator) ClearCost() {
	a.sessionInputCostMicroUSD.Store(0)
	a.sessionOutputCostMicroUSD.Store(0)
	a.sessionCacheReadCostMicroUSD.Store(0)
	a.sessionCacheWriteCostMicroUSD.Store(0)

	a.serverMu.RLock()
	for _, sc := range a.servers {
		sc.inputCostMicroUSD.Store(0)
		sc.outputCostMicroUSD.Store(0)
		sc.cacheReadCostMicroUSD.Store(0)
		sc.cacheWriteCostMicroUSD.Store(0)
	}
	a.serverMu.RUnlock()

	a.replicaMu.RLock()
	for _, m := range a.replicas {
		for _, rc := range m {
			rc.inputCostMicroUSD.Store(0)
			rc.outputCostMicroUSD.Store(0)
			rc.cacheReadCostMicroUSD.Store(0)
			rc.cacheWriteCostMicroUSD.Store(0)
		}
	}
	a.replicaMu.RUnlock()

	a.clientMu.RLock()
	for _, cc := range a.clients {
		cc.inputCostMicroUSD.Store(0)
		cc.outputCostMicroUSD.Store(0)
		cc.cacheReadCostMicroUSD.Store(0)
		cc.cacheWriteCostMicroUSD.Store(0)
	}
	a.clientMu.RUnlock()

	a.bufMu.Lock()
	for i := range a.buckets {
		a.buckets[i].costMicroUSD = 0
	}
	a.bufMu.Unlock()

	a.serverBufMu.Lock()
	for _, sb := range a.serverBufs {
		for i := range sb.buckets {
			sb.buckets[i].costMicroUSD = 0
		}
	}
	a.serverBufMu.Unlock()

	a.clientBufMu.Lock()
	for _, cb := range a.clientBufs {
		for i := range cb.buckets {
			cb.buckets[i].costMicroUSD = 0
		}
	}
	a.clientBufMu.Unlock()
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
