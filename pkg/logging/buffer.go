// Package logging provides shared logging utilities for gridctl.
package logging

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// BufferedEntry represents a log entry stored in the buffer.
type BufferedEntry struct {
	Level     string         `json:"level"`
	Timestamp string         `json:"ts"`
	Message   string         `json:"msg"`
	Component string         `json:"component,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

// LogBuffer stores recent log entries in memory for retrieval via API.
type LogBuffer struct {
	mu       sync.RWMutex
	entries  []BufferedEntry
	maxSize  int
	position int // circular buffer position
	wrapped  bool
}

// NewLogBuffer creates a new log buffer with the specified maximum size.
func NewLogBuffer(maxSize int) *LogBuffer {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &LogBuffer{
		entries: make([]BufferedEntry, maxSize),
		maxSize: maxSize,
	}
}

// Add adds a new entry to the buffer.
func (b *LogBuffer) Add(entry BufferedEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries[b.position] = entry
	b.position++
	if b.position >= b.maxSize {
		b.position = 0
		b.wrapped = true
	}
}

// GetRecent returns the most recent n entries.
func (b *LogBuffer) GetRecent(n int) []BufferedEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := b.count()
	if n <= 0 || n > count {
		n = count
	}
	if n == 0 {
		return nil
	}

	result := make([]BufferedEntry, n)

	// Calculate start position for reading n entries
	if b.wrapped {
		start := b.position - n
		if start < 0 {
			start += b.maxSize
		}
		for i := 0; i < n; i++ {
			idx := (start + i) % b.maxSize
			result[i] = b.entries[idx]
		}
	} else {
		start := b.position - n
		if start < 0 {
			start = 0
			n = b.position
			result = make([]BufferedEntry, n)
		}
		copy(result, b.entries[start:b.position])
	}

	return result
}

// Clear removes all entries from the buffer.
func (b *LogBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.position = 0
	b.wrapped = false
	// No need to zero out entries, they'll be overwritten
}

// count returns the number of entries currently in the buffer.
// Must be called with lock held.
func (b *LogBuffer) count() int {
	if b.wrapped {
		return b.maxSize
	}
	return b.position
}

// Count returns the number of entries currently in the buffer.
func (b *LogBuffer) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count()
}

// BufferHandler is a slog.Handler that writes to both a LogBuffer and an underlying handler.
type BufferHandler struct {
	buffer    *LogBuffer
	inner     slog.Handler
	component string
	attrs     []slog.Attr
	group     string
}

// NewBufferHandler creates a handler that writes to both a buffer and another handler.
// If inner is nil, only the buffer receives logs.
func NewBufferHandler(buffer *LogBuffer, inner slog.Handler) *BufferHandler {
	return &BufferHandler{
		buffer: buffer,
		inner:  inner,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *BufferHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.inner != nil {
		return h.inner.Enabled(ctx, level)
	}
	return true
}

// Handle handles the record.
func (h *BufferHandler) Handle(ctx context.Context, r slog.Record) error {
	// Build buffered entry
	entry := BufferedEntry{
		Level:     r.Level.String(),
		Timestamp: r.Time.Format(time.RFC3339Nano),
		Message:   r.Message,
		Component: h.component,
		Attrs:     make(map[string]any),
	}

	// Add handler-level attrs first
	for _, attr := range h.attrs {
		if attr.Key == "component" {
			entry.Component = attr.Value.String()
		} else if attr.Key == "trace_id" {
			entry.TraceID = attr.Value.String()
		} else {
			entry.Attrs[attr.Key] = attrValue(attr.Value)
		}
	}

	// Add record-level attrs
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			entry.Component = a.Value.String()
		} else if a.Key == "trace_id" {
			entry.TraceID = a.Value.String()
		} else {
			key := a.Key
			if h.group != "" {
				key = h.group + "." + key
			}
			entry.Attrs[key] = attrValue(a.Value)
		}
		return true
	})

	// Remove empty attrs
	if len(entry.Attrs) == 0 {
		entry.Attrs = nil
	}

	h.buffer.Add(entry)

	// Also log to inner handler if present
	if h.inner != nil {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

// WithAttrs returns a new handler with the given attributes.
func (h *BufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := &BufferHandler{
		buffer:    h.buffer,
		component: h.component,
		group:     h.group,
		attrs:     make([]slog.Attr, len(h.attrs)+len(attrs)),
	}
	copy(newHandler.attrs, h.attrs)
	copy(newHandler.attrs[len(h.attrs):], attrs)

	// Check if any attrs set component
	for _, attr := range attrs {
		if attr.Key == "component" {
			newHandler.component = attr.Value.String()
		}
	}

	if h.inner != nil {
		newHandler.inner = h.inner.WithAttrs(attrs)
	}
	return newHandler
}

// WithGroup returns a new handler with the given group name.
func (h *BufferHandler) WithGroup(name string) slog.Handler {
	newGroup := name
	if h.group != "" {
		newGroup = h.group + "." + name
	}

	newHandler := &BufferHandler{
		buffer:    h.buffer,
		inner:     h.inner,
		component: h.component,
		attrs:     h.attrs,
		group:     newGroup,
	}
	if h.inner != nil {
		newHandler.inner = h.inner.WithGroup(name)
	}
	return newHandler
}

// attrValue converts a slog.Value to a Go value suitable for JSON.
func attrValue(v slog.Value) any {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339Nano)
	case slog.KindGroup:
		attrs := v.Group()
		m := make(map[string]any, len(attrs))
		for _, a := range attrs {
			m[a.Key] = attrValue(a.Value)
		}
		return m
	case slog.KindAny:
		a := v.Any()
		// Try to serialize as JSON and back for cleaner output
		if b, err := json.Marshal(a); err == nil {
			var result any
			if json.Unmarshal(b, &result) == nil {
				return result
			}
		}
		return a
	default:
		return v.Any()
	}
}
