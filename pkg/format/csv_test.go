package format

import (
	"strings"
	"testing"
)

func TestToCSV_ValidInput(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name: "simple objects",
			input: []any{
				map[string]any{"age": float64(30), "name": "Alice"},
				map[string]any{"age": float64(25), "name": "Bob"},
			},
			expected: "age,name\n30,Alice\n25,Bob\n",
		},
		{
			name: "single row",
			input: []any{
				map[string]any{"id": float64(1), "value": "test"},
			},
			expected: "id,value\n1,test\n",
		},
		{
			name:     "empty array",
			input:    []any{},
			expected: "",
		},
		{
			name: "headers sorted alphabetically",
			input: []any{
				map[string]any{"z": "last", "a": "first", "m": "mid"},
			},
			expected: "a,m,z\nfirst,mid,last\n",
		},
		{
			name: "null values as empty",
			input: []any{
				map[string]any{"a": nil, "b": "present"},
			},
			expected: "a,b\n,present\n",
		},
		{
			name: "boolean values",
			input: []any{
				map[string]any{"active": true, "deleted": false},
			},
			expected: "active,deleted\ntrue,false\n",
		},
		{
			name: "float values",
			input: []any{
				map[string]any{"rate": float64(3.14), "whole": float64(42)},
			},
			expected: "rate,whole\n3.14,42\n",
		},
		{
			name: "strings needing csv quoting",
			input: []any{
				map[string]any{"desc": "has, comma", "name": "simple"},
			},
			expected: "desc,name\n\"has, comma\",simple\n",
		},
		{
			name: "missing keys in subsequent rows",
			input: []any{
				map[string]any{"a": "1", "b": "2"},
				map[string]any{"a": "3"},
			},
			expected: "a,b\n1,2\n3,\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToCSV(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ToCSV() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestToCSV_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input any
		errOk func(string) bool
	}{
		{
			name:  "non-array input",
			input: map[string]any{"a": float64(1)},
			errOk: func(s string) bool { return strings.Contains(s, "requires an array") },
		},
		{
			name:  "array of primitives",
			input: []any{"a", "b", "c"},
			errOk: func(s string) bool { return strings.Contains(s, "requires array of objects") },
		},
		{
			name:  "array with non-object element",
			input: []any{map[string]any{"a": float64(1)}, "not an object"},
			errOk: func(s string) bool { return strings.Contains(s, "element 1") },
		},
		{
			name:  "string input",
			input: "hello",
			errOk: func(s string) bool { return strings.Contains(s, "requires an array") },
		},
		{
			name:  "number input",
			input: float64(42),
			errOk: func(s string) bool { return strings.Contains(s, "requires an array") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ToCSV(tt.input)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.errOk(err.Error()) {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	}
}

func TestToCSV_LargeDataset(t *testing.T) {
	rows := make([]any, 100)
	for i := range rows {
		rows[i] = map[string]any{
			"id":    float64(i),
			"name":  "user",
			"score": float64(i * 10),
		}
	}

	got, err := ToCSV(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 101 { // 1 header + 100 rows
		t.Errorf("expected 101 lines, got %d", len(lines))
	}
	if lines[0] != "id,name,score" {
		t.Errorf("header = %q, want %q", lines[0], "id,name,score")
	}
}
