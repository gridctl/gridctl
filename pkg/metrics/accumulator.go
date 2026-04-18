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
	Session       TokenCounts                        `json:"session"`
	PerServer     map[string]TokenCounts             `json:"per_server"`
	PerReplica    map[string]map[int]TokenCounts     `json:"per_replica,omitempty"`
	FormatSavings FormatSavings                      `json:"format_savings"`
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

// bucket holds accumulated token counts for a single minute.
type bucket struct {
	timestamp    time.Time
	inputTokens  int64
	outputTokens int64
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

// Accumulator collects token usage metrics with thread-safe operations.
// Session totals use atomic counters. Historical data is stored in a ring buffer
// of pre-aggregated 1-minute time buckets.
type Accumulator struct {
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
		servers:    make(map[string]*serverCounters),
		replicas:   make(map[string]map[int]*replicaCounters),
		buckets:    make([]bucket, maxDataPoints),
		maxSize:    maxDataPoints,
		serverBufs: make(map[string]*serverBuffer),
	}
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

	// Update time-series ring buffer
	now := bucketKey(time.Now())
	a.addToBucket(now, input, output)
	a.addToServerBucket(serverName, now, input, output)
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

	a.bufMu.Lock()
	a.buckets = make([]bucket, a.maxSize)
	a.position = 0
	a.wrapped = false
	a.bufMu.Unlock()

	a.serverBufMu.Lock()
	a.serverBufs = make(map[string]*serverBuffer)
	a.serverBufMu.Unlock()

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
