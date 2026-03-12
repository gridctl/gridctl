package format

import (
	"strings"
	"testing"
)

func TestFormat_Dispatch(t *testing.T) {
	data := map[string]any{"name": "Alice", "age": float64(30)}

	tests := []struct {
		name     string
		format   string
		contains string
		wantErr  bool
	}{
		{
			name:     "json format",
			format:   "json",
			contains: `"name"`,
		},
		{
			name:     "toon format",
			format:   "toon",
			contains: "name: Alice",
		},
		{
			name:    "csv format errors on non-array",
			format:  "csv",
			wantErr: true,
		},
		{
			name:     "text format for map",
			format:   "text",
			contains: `"name"`,
		},
		{
			name:    "unsupported format",
			format:  "xml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format(data, tt.format)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tt.contains) {
				t.Errorf("Format() = %q, want to contain %q", got, tt.contains)
			}
		})
	}
}

func TestFormat_JSON(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "compact object",
			input:    map[string]any{"a": float64(1), "b": "two"},
			expected: `{"a":1,"b":"two"}`,
		},
		{
			name:     "compact array",
			input:    []any{float64(1), float64(2), float64(3)},
			expected: `[1,2,3]`,
		},
		{
			name:     "string passthrough",
			input:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "null",
			input:    nil,
			expected: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format(tt.input, "json")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Format(json) = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormat_Text(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "string passthrough",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "non-string falls back to json",
			input:    map[string]any{"a": float64(1)},
			expected: `{"a":1}`,
		},
		{
			name:     "number to json",
			input:    float64(42),
			expected: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format(tt.input, "text")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Format(text) = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormat_CSV_ViaDispatcher(t *testing.T) {
	input := []any{
		map[string]any{"id": float64(1), "name": "Alice"},
		map[string]any{"id": float64(2), "name": "Bob"},
	}

	got, err := Format(input, "csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "id,name\n1,Alice\n2,Bob\n"
	if got != expected {
		t.Errorf("Format(csv) = %q, want %q", got, expected)
	}
}

func TestFormat_TOON_ViaDispatcher(t *testing.T) {
	input := map[string]any{
		"count":  float64(3),
		"name":   "test",
		"active": true,
	}

	got, err := Format(input, "toon")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "active: true\ncount: 3\nname: test\n"
	if got != expected {
		t.Errorf("Format(toon) = %q, want %q", got, expected)
	}
}

func TestFormat_UnsupportedFormat(t *testing.T) {
	_, err := Format("data", "xml")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported output format") {
		t.Errorf("unexpected error: %v", err)
	}
}
