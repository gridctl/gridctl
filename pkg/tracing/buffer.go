package tracing

import (
	"context"
	"strings"
	"sync"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SpanRecord is a completed OTel span stored in the trace buffer.
type SpanRecord struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time"`
	DurationMs int64             `json:"duration_ms"`
	Status     string            `json:"status"`
	IsError    bool              `json:"is_error"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

// TraceRecord groups all spans belonging to a single trace.
type TraceRecord struct {
	TraceID    string       `json:"trace_id"`
	Operation  string       `json:"operation"` // root span name
	ServerName string       `json:"server_name,omitempty"`
	MethodName string       `json:"method_name,omitempty"`
	StartTime  time.Time    `json:"start_time"`
	EndTime    time.Time    `json:"end_time"`
	DurationMs int64        `json:"duration_ms"`
	SpanCount  int          `json:"span_count"`
	IsError    bool         `json:"is_error"`
	Spans      []SpanRecord `json:"spans"`
}

// Buffer stores completed traces in a thread-safe ring buffer and implements
// sdktrace.SpanExporter so it can be registered with the OTel TracerProvider.
type Buffer struct {
	mu       sync.Mutex
	pending  map[string][]SpanRecord // traceID -> spans not yet finalised

	bufMu    sync.RWMutex
	traces   []TraceRecord
	maxSize  int
	position int
	wrapped  bool

	retention time.Duration
}

// NewBuffer creates a trace ring buffer with the given capacity and retention.
func NewBuffer(maxSize int, retention time.Duration) *Buffer {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	return &Buffer{
		pending:   make(map[string][]SpanRecord),
		traces:    make([]TraceRecord, maxSize),
		maxSize:   maxSize,
		retention: retention,
	}
}

// ExportSpans implements sdktrace.SpanExporter.
// Spans are accumulated by trace ID. When the local-root span is exported,
// all accumulated spans are finalised into a TraceRecord.
func (b *Buffer) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, s := range spans {
		rec := spanToRecord(s)
		traceID := rec.TraceID

		if isLocalRoot(s) {
			children := b.pending[traceID]
			delete(b.pending, traceID)
			allSpans := append(children, rec)
			b.addToBuffer(buildTraceRecord(rec, allSpans))
		} else {
			b.pending[traceID] = append(b.pending[traceID], rec)
		}
	}
	return nil
}

// Shutdown implements sdktrace.SpanExporter. Flushes any pending in-progress traces.
func (b *Buffer) Shutdown(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for traceID, spans := range b.pending {
		if len(spans) > 0 {
			// No root span arrived — use the earliest span as proxy root.
			root := spans[0]
			for _, s := range spans[1:] {
				if s.StartTime.Before(root.StartTime) {
					root = s
				}
			}
			tr := buildTraceRecord(root, spans)
			tr.TraceID = traceID
			b.addToBuffer(tr)
		}
	}
	b.pending = make(map[string][]SpanRecord)
	return nil
}

// GetRecent returns up to n most-recent traces, newest first.
func (b *Buffer) GetRecent(n int) []TraceRecord {
	b.bufMu.RLock()
	defer b.bufMu.RUnlock()

	count := b.count()
	if n <= 0 || n > count {
		n = count
	}
	if n == 0 {
		return nil
	}

	result := make([]TraceRecord, n)
	if b.wrapped {
		start := b.position - n
		if start < 0 {
			start += b.maxSize
		}
		for i := 0; i < n; i++ {
			result[n-1-i] = b.traces[(start+i)%b.maxSize]
		}
	} else {
		start := b.position - n
		if start < 0 {
			start = 0
			n = b.position
			result = make([]TraceRecord, n)
		}
		for i := 0; i < n; i++ {
			result[n-1-i] = b.traces[start+i]
		}
	}
	return result
}

// GetByID returns the trace with the given ID, or nil if not found.
func (b *Buffer) GetByID(traceID string) *TraceRecord {
	b.bufMu.RLock()
	defer b.bufMu.RUnlock()

	count := b.count()
	for i := 0; i < count; i++ {
		var idx int
		if b.wrapped {
			idx = (b.position - 1 - i + b.maxSize) % b.maxSize
		} else {
			idx = b.position - 1 - i
			if idx < 0 {
				break
			}
		}
		if b.traces[idx].TraceID == traceID {
			tr := b.traces[idx]
			return &tr
		}
	}
	return nil
}

// FilterOpts configures trace list filtering.
type FilterOpts struct {
	ServerName  string
	ErrorsOnly  bool
	MinDuration time.Duration
	Limit       int
}

// Filter returns traces matching the given options, newest first.
func (b *Buffer) Filter(opts FilterOpts) []TraceRecord {
	all := b.GetRecent(b.maxSize)
	cutoff := time.Now().Add(-b.retention)

	var result []TraceRecord
	for _, tr := range all {
		if tr.StartTime.Before(cutoff) {
			continue
		}
		if opts.ServerName != "" && tr.ServerName != opts.ServerName {
			continue
		}
		if opts.ErrorsOnly && !tr.IsError {
			continue
		}
		if opts.MinDuration > 0 && time.Duration(tr.DurationMs)*time.Millisecond < opts.MinDuration {
			continue
		}
		result = append(result, tr)
		if opts.Limit > 0 && len(result) >= opts.Limit {
			break
		}
	}
	return result
}

// Count returns the number of traces currently stored.
func (b *Buffer) Count() int {
	b.bufMu.RLock()
	defer b.bufMu.RUnlock()
	return b.count()
}

// addToBuffer appends a TraceRecord to the ring buffer. Thread-safe.
func (b *Buffer) addToBuffer(tr TraceRecord) {
	b.bufMu.Lock()
	defer b.bufMu.Unlock()

	b.traces[b.position] = tr
	b.position++
	if b.position >= b.maxSize {
		b.position = 0
		b.wrapped = true
	}
}

// count returns the number of traces in the buffer. Must be called with bufMu read lock.
func (b *Buffer) count() int {
	if b.wrapped {
		return b.maxSize
	}
	return b.position
}

// isLocalRoot returns true when the span has no local parent (root or remote parent).
func isLocalRoot(s sdktrace.ReadOnlySpan) bool {
	parent := s.Parent()
	return !parent.IsValid() || parent.IsRemote()
}

// spanToRecord converts an OTel span to our storage type.
func spanToRecord(s sdktrace.ReadOnlySpan) SpanRecord {
	rec := SpanRecord{
		TraceID:    s.SpanContext().TraceID().String(),
		SpanID:     s.SpanContext().SpanID().String(),
		Name:       s.Name(),
		StartTime:  s.StartTime(),
		EndTime:    s.EndTime(),
		DurationMs: s.EndTime().Sub(s.StartTime()).Milliseconds(),
		Status:     s.Status().Code.String(),
		IsError:    strings.Contains(s.Status().Code.String(), "Error"),
	}

	if s.Parent().IsValid() && !s.Parent().IsRemote() {
		rec.ParentID = s.Parent().SpanID().String()
	}

	if len(s.Attributes()) > 0 {
		rec.Attrs = make(map[string]string, len(s.Attributes()))
		for _, kv := range s.Attributes() {
			rec.Attrs[string(kv.Key)] = kv.Value.AsString()
		}
	}

	return rec
}

// buildTraceRecord constructs a TraceRecord from a root span and all its children.
func buildTraceRecord(root SpanRecord, allSpans []SpanRecord) TraceRecord {
	isError := false
	for _, s := range allSpans {
		if s.IsError {
			isError = true
			break
		}
	}

	tr := TraceRecord{
		TraceID:    root.TraceID,
		Operation:  root.Name,
		StartTime:  root.StartTime,
		EndTime:    root.EndTime,
		DurationMs: root.DurationMs,
		SpanCount:  len(allSpans),
		IsError:    isError,
		Spans:      allSpans,
	}

	// Extract well-known attributes from the root span for top-level fields.
	if root.Attrs != nil {
		if v, ok := root.Attrs["server.name"]; ok {
			tr.ServerName = v
		}
		if v, ok := root.Attrs["mcp.method.name"]; ok {
			tr.MethodName = v
		}
	}

	return tr
}
