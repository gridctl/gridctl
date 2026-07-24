package logging

import (
	"log/slog"
	"testing"
)

func TestLogBuffer_AddAndGetRecent(t *testing.T) {
	buffer := NewLogBuffer(5)

	// Add some entries
	for i := 0; i < 3; i++ {
		buffer.Add(BufferedEntry{
			Level:   "INFO",
			Message: "test message",
		})
	}

	if buffer.Count() != 3 {
		t.Errorf("expected count 3, got %d", buffer.Count())
	}

	recent := buffer.GetRecent(2)
	if len(recent) != 2 {
		t.Errorf("expected 2 entries, got %d", len(recent))
	}

	// Get more than available
	recent = buffer.GetRecent(10)
	if len(recent) != 3 {
		t.Errorf("expected 3 entries, got %d", len(recent))
	}
}

func TestLogBuffer_CircularWrap(t *testing.T) {
	buffer := NewLogBuffer(3)

	// Add more entries than buffer size
	for i := 0; i < 5; i++ {
		buffer.Add(BufferedEntry{
			Level:   "INFO",
			Message: "message",
			Attrs:   map[string]any{"index": i},
		})
	}

	if buffer.Count() != 3 {
		t.Errorf("expected count 3 after wrap, got %d", buffer.Count())
	}

	recent := buffer.GetRecent(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 entries, got %d", len(recent))
	}

	// Verify we have the most recent entries (indices 2, 3, 4)
	for i, entry := range recent {
		expectedIndex := i + 2
		if idx, ok := entry.Attrs["index"].(int); !ok || idx != expectedIndex {
			t.Errorf("entry %d: expected index %d, got %v", i, expectedIndex, entry.Attrs["index"])
		}
	}
}

func TestLogBuffer_Clear(t *testing.T) {
	buffer := NewLogBuffer(5)

	buffer.Add(BufferedEntry{Level: "INFO", Message: "test"})
	buffer.Add(BufferedEntry{Level: "ERROR", Message: "error"})

	if buffer.Count() != 2 {
		t.Errorf("expected count 2, got %d", buffer.Count())
	}

	buffer.Clear()

	if buffer.Count() != 0 {
		t.Errorf("expected count 0 after clear, got %d", buffer.Count())
	}

	recent := buffer.GetRecent(10)
	if len(recent) != 0 {
		t.Errorf("expected empty after clear, got %d entries", len(recent))
	}
}

func TestLogBuffer_EmptyBuffer(t *testing.T) {
	buffer := NewLogBuffer(5)

	recent := buffer.GetRecent(5)
	if len(recent) > 0 {
		t.Errorf("expected empty for empty buffer, got %v", recent)
	}
}

func TestLogBuffer_ZeroOrNegativeN(t *testing.T) {
	buffer := NewLogBuffer(5)

	buffer.Add(BufferedEntry{Level: "INFO", Message: "test"})
	buffer.Add(BufferedEntry{Level: "INFO", Message: "test2"})

	// Zero n should return all
	recent := buffer.GetRecent(0)
	if len(recent) != 2 {
		t.Errorf("expected 2 entries for n=0, got %d", len(recent))
	}

	// Negative n should return all
	recent = buffer.GetRecent(-1)
	if len(recent) != 2 {
		t.Errorf("expected 2 entries for n=-1, got %d", len(recent))
	}
}

func TestLogBuffer_GetRecentMatching_SparseMatches(t *testing.T) {
	buffer := NewLogBuffer(300)

	// Sparse matches followed by many non-matches: a slice-then-filter over
	// the last n raw entries would return nothing.
	for i := 0; i < 5; i++ {
		buffer.Add(BufferedEntry{Level: "ERROR", Message: "boom", Attrs: map[string]any{"index": i}})
	}
	for i := 0; i < 200; i++ {
		buffer.Add(BufferedEntry{Level: "INFO", Message: "tick"})
	}

	matches := buffer.GetRecentMatching(50, func(e BufferedEntry) bool { return e.Level == "ERROR" })
	if len(matches) != 5 {
		t.Fatalf("expected 5 sparse matches, got %d", len(matches))
	}
	// Chronological order preserved.
	for i, entry := range matches {
		if idx, ok := entry.Attrs["index"].(int); !ok || idx != i {
			t.Errorf("entry %d: expected index %d, got %v", i, i, entry.Attrs["index"])
		}
	}
}

func TestLogBuffer_GetRecentMatching_AcrossWrap(t *testing.T) {
	buffer := NewLogBuffer(10)

	// 15 adds into a 10-slot ring: entries 5..14 survive, wrap engaged.
	for i := 0; i < 15; i++ {
		level := "INFO"
		if i%2 == 0 {
			level = "ERROR"
		}
		buffer.Add(BufferedEntry{Level: level, Attrs: map[string]any{"index": i}})
	}

	matches := buffer.GetRecentMatching(3, func(e BufferedEntry) bool { return e.Level == "ERROR" })
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}
	// The three most recent even indices among 5..14 are 10, 12, 14.
	want := []int{10, 12, 14}
	for i, entry := range matches {
		if idx, ok := entry.Attrs["index"].(int); !ok || idx != want[i] {
			t.Errorf("entry %d: expected index %d, got %v", i, want[i], entry.Attrs["index"])
		}
	}
}

func TestLogBuffer_GetRecentMatching_Limits(t *testing.T) {
	buffer := NewLogBuffer(10)

	if got := buffer.GetRecentMatching(5, func(BufferedEntry) bool { return true }); got != nil {
		t.Errorf("expected nil on empty buffer, got %d entries", len(got))
	}

	buffer.Add(BufferedEntry{Level: "ERROR"})
	buffer.Add(BufferedEntry{Level: "INFO"})
	buffer.Add(BufferedEntry{Level: "ERROR"})

	if got := buffer.GetRecentMatching(10, func(e BufferedEntry) bool { return e.Level == "WARN" }); len(got) != 0 {
		t.Errorf("expected no matches, got %d", len(got))
	}
	// n <= 0 returns every match, mirroring GetRecent.
	if got := buffer.GetRecentMatching(0, func(e BufferedEntry) bool { return e.Level == "ERROR" }); len(got) != 2 {
		t.Errorf("expected 2 matches for n=0, got %d", len(got))
	}
	if got := buffer.GetRecentMatching(1, func(e BufferedEntry) bool { return e.Level == "ERROR" }); len(got) != 1 {
		t.Errorf("expected 1 match for n=1, got %d", len(got))
	}
}

func TestBufferHandler_BasicLogging(t *testing.T) {
	buffer := NewLogBuffer(10)
	handler := NewBufferHandler(buffer, nil)
	logger := slog.New(handler)

	logger.Info("test message", "key", "value")

	entries := buffer.GetRecent(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Level != "INFO" {
		t.Errorf("expected level INFO, got %s", entry.Level)
	}
	if entry.Message != "test message" {
		t.Errorf("expected message 'test message', got %s", entry.Message)
	}
	if entry.Attrs["key"] != "value" {
		t.Errorf("expected key=value, got %v", entry.Attrs["key"])
	}
}

func TestBufferHandler_WithAttrs(t *testing.T) {
	buffer := NewLogBuffer(10)
	handler := NewBufferHandler(buffer, nil)
	logger := slog.New(handler).With("component", "test-component")

	logger.Info("test message")

	entries := buffer.GetRecent(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Component != "test-component" {
		t.Errorf("expected component 'test-component', got %s", entry.Component)
	}
}

func TestBufferHandler_TraceID(t *testing.T) {
	buffer := NewLogBuffer(10)
	handler := NewBufferHandler(buffer, nil)
	logger := slog.New(handler).With("trace_id", "abc123")

	logger.Info("test message")

	entries := buffer.GetRecent(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.TraceID != "abc123" {
		t.Errorf("expected trace_id 'abc123', got %s", entry.TraceID)
	}
}

func TestBufferHandler_MultipleLevels(t *testing.T) {
	buffer := NewLogBuffer(10)
	handler := NewBufferHandler(buffer, nil)
	logger := slog.New(handler)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	entries := buffer.GetRecent(10)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	expectedLevels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for i, entry := range entries {
		if entry.Level != expectedLevels[i] {
			t.Errorf("entry %d: expected level %s, got %s", i, expectedLevels[i], entry.Level)
		}
	}
}
