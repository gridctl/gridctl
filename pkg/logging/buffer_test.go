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
