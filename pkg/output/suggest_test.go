package output

import "testing"

func TestSuggest(t *testing.T) {
	clients := []string{"claude", "claude-code", "cursor", "windsurf", "zed", "goose"}

	tests := []struct {
		name       string
		input      string
		candidates []string
		want       string
	}{
		{name: "one deletion", input: "claud", candidates: clients, want: "claude"},
		{name: "one substitution", input: "claude", candidates: clients, want: "claude"},
		{name: "transposition-ish", input: "cursro", candidates: clients, want: "cursor"},
		{name: "exact match", input: "zed", candidates: clients, want: "zed"},
		{name: "case-insensitive", input: "CLAUD", candidates: clients, want: "claude"},
		{name: "too far", input: "totally-unrelated", candidates: clients, want: ""},
		{name: "no candidates", input: "claud", candidates: nil, want: ""},
		{name: "empty input", input: "", candidates: clients, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Suggest(tt.input, tt.candidates); got != tt.want {
				t.Errorf("Suggest(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"claud", "claude", 1},
		{"status", "status", 0},
	}
	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
