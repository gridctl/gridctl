package token

import (
	"testing"
)

func TestHeuristicCounter_Count(t *testing.T) {
	c := NewHeuristicCounter(4)

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty", "", 0},
		{"single byte", "a", 1},
		{"exactly 4 bytes", "abcd", 1},
		{"5 bytes rounds up", "abcde", 2},
		{"8 bytes", "abcdefgh", 2},
		{"12 bytes", "hello world!", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Count(tt.input)
			if got != tt.expected {
				t.Errorf("Count(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHeuristicCounter_DefaultBytesPerToken(t *testing.T) {
	c := NewHeuristicCounter(0)
	if c.bytesPerToken != 4 {
		t.Errorf("expected default bytesPerToken=4, got %d", c.bytesPerToken)
	}

	c = NewHeuristicCounter(-1)
	if c.bytesPerToken != 4 {
		t.Errorf("expected default bytesPerToken=4 for negative, got %d", c.bytesPerToken)
	}
}

func TestCountJSON(t *testing.T) {
	c := NewHeuristicCounter(4)

	args := map[string]any{
		"key": "value",
	}
	got := CountJSON(c, args)
	// {"key":"value"} = 15 bytes -> 4 tokens
	if got != 4 {
		t.Errorf("CountJSON = %d, want 4", got)
	}

	// Nil value
	got = CountJSON(c, nil)
	// "null" = 4 bytes -> 1 token
	if got != 1 {
		t.Errorf("CountJSON(nil) = %d, want 1", got)
	}
}

func TestCountJSON_UnmarshalableValue(t *testing.T) {
	c := NewHeuristicCounter(4)
	// Channels can't be marshaled to JSON
	ch := make(chan int)
	got := CountJSON(c, ch)
	if got != 0 {
		t.Errorf("CountJSON(channel) = %d, want 0", got)
	}
}

func TestTiktokenCounter_Count(t *testing.T) {
	c, err := NewTiktokenCounter()
	if err != nil {
		t.Fatalf("NewTiktokenCounter() error: %v", err)
	}

	// Reference counts verified against the cl100k_base vocabulary.
	// These values are fixed by the BPE encoding, not derived from the heuristic.
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty string", "", 0},
		{"single byte", "a", 1},
		{"single word", "hello", 1},
		{"short sentence", "Hello, World!", 4},
		{"pangram", "The quick brown fox jumps over the lazy dog", 9},
		{"json object", `{"key":"value"}`, 5},
		{"japanese cjk", "こんにちは世界", 4},
		{"sql query", "SELECT * FROM users WHERE id = 1;", 10},
		{"go code", "// Package token provides token counting\npackage token", 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Count(tt.input)
			if got != tt.expected {
				t.Errorf("Count(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTiktokenCounter_ImplementsInterface(t *testing.T) {
	c, err := NewTiktokenCounter()
	if err != nil {
		t.Fatalf("NewTiktokenCounter() error: %v", err)
	}
	// Verify TiktokenCounter satisfies the Counter interface.
	var _ Counter = c
}

func TestTiktokenCounter_CountJSON(t *testing.T) {
	c, err := NewTiktokenCounter()
	if err != nil {
		t.Fatalf("NewTiktokenCounter() error: %v", err)
	}

	args := map[string]any{"key": "value"}
	got := CountJSON(c, args)
	// {"key":"value"} encodes to 5 cl100k_base tokens (verified reference value).
	if got != 5 {
		t.Errorf("CountJSON = %d, want 5", got)
	}

	// Unmarshalable value returns 0.
	ch := make(chan int)
	got = CountJSON(c, ch)
	if got != 0 {
		t.Errorf("CountJSON(channel) = %d, want 0", got)
	}
}
