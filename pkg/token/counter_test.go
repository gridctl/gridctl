package token

import "testing"

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
