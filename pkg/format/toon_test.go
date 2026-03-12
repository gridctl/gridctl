package format

import (
	"testing"
)

func TestToTOON_KeyValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "simple string value",
			input:    map[string]any{"name": "John Doe"},
			expected: "name: John Doe\n",
		},
		{
			name:     "number value",
			input:    map[string]any{"count": float64(42)},
			expected: "count: 42\n",
		},
		{
			name:     "float value",
			input:    map[string]any{"rate": float64(3.14)},
			expected: "rate: 3.14\n",
		},
		{
			name:     "boolean true",
			input:    map[string]any{"active": true},
			expected: "active: true\n",
		},
		{
			name:     "boolean false",
			input:    map[string]any{"active": false},
			expected: "active: false\n",
		},
		{
			name:     "null value",
			input:    map[string]any{"value": nil},
			expected: "value: null\n",
		},
		{
			name:  "multiple keys sorted",
			input: map[string]any{"z": "last", "a": "first", "m": "middle"},
			expected: "a: first\n" +
				"m: middle\n" +
				"z: last\n",
		},
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToTOON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestToTOON_NestedObjects(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name: "single level nesting",
			input: map[string]any{
				"address": map[string]any{
					"city":   "Springfield",
					"street": "123 Main St",
				},
			},
			expected: "address:\n" +
				"  city: Springfield\n" +
				"  street: 123 Main St\n",
		},
		{
			name: "two levels deep",
			input: map[string]any{
				"person": map[string]any{
					"address": map[string]any{
						"city": "Springfield",
					},
				},
			},
			expected: "person:\n" +
				"  address:\n" +
				"    city: Springfield\n",
		},
		{
			name: "mixed nested and scalar",
			input: map[string]any{
				"name": "John",
				"address": map[string]any{
					"city": "Springfield",
				},
			},
			expected: "address:\n" +
				"  city: Springfield\n" +
				"name: John\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToTOON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestToTOON_Arrays(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name: "simple primitive array",
			input: map[string]any{
				"tags": []any{"go", "mcp", "gateway"},
			},
			expected: "tags[3]: go,mcp,gateway\n",
		},
		{
			name: "numeric array",
			input: map[string]any{
				"scores": []any{float64(1), float64(2), float64(3)},
			},
			expected: "scores[3]: 1,2,3\n",
		},
		{
			name: "tabular array of uniform objects",
			input: map[string]any{
				"users": []any{
					map[string]any{"age": float64(30), "email": "john@example.com", "name": "John"},
					map[string]any{"age": float64(25), "email": "jane@example.com", "name": "Jane"},
				},
			},
			expected: "users[2]{age,email,name}:\n" +
				"  30,john@example.com,John\n" +
				"  25,jane@example.com,Jane\n",
		},
		{
			name: "empty array",
			input: map[string]any{
				"items": []any{},
			},
			expected: "items[0]:\n",
		},
		{
			name: "mixed-type array fallback",
			input: map[string]any{
				"data": []any{float64(1), "two", true},
			},
			expected: "data[3]: 1,two,true\n",
		},
		{
			name: "array of non-uniform objects",
			input: map[string]any{
				"records": []any{
					map[string]any{"a": float64(1), "b": float64(2)},
					map[string]any{"a": float64(3), "c": float64(4)},
				},
			},
			expected: "records[2]:\n" +
				"  a: 1\n" +
				"  b: 2\n" +
				"  a: 3\n" +
				"  c: 4\n",
		},
		{
			name: "array containing nested objects",
			input: map[string]any{
				"items": []any{
					map[string]any{"nested": map[string]any{"a": float64(1)}},
				},
			},
			expected: "items[1]:\n" +
				"  nested:\n" +
				"    a: 1\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToTOON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestToTOON_StringQuoting(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "string with comma",
			input:    map[string]any{"desc": "Has commas, like this"},
			expected: "desc: \"Has commas, like this\"\n",
		},
		{
			name:     "string with colon",
			input:    map[string]any{"time": "12:30:00"},
			expected: "time: \"12:30:00\"\n",
		},
		{
			name:     "string with newline",
			input:    map[string]any{"multi": "Line one\nLine two"},
			expected: "multi: \"Line one\\nLine two\"\n",
		},
		{
			name:     "string with tab",
			input:    map[string]any{"tabbed": "col1\tcol2"},
			expected: "tabbed: \"col1\\tcol2\"\n",
		},
		{
			name:     "string with double quote",
			input:    map[string]any{"quoted": `She said "hello"`},
			expected: "quoted: \"She said \\\"hello\\\"\"\n",
		},
		{
			name:     "string with leading space",
			input:    map[string]any{"padded": " hello"},
			expected: "padded: \" hello\"\n",
		},
		{
			name:     "string with trailing space",
			input:    map[string]any{"padded": "hello "},
			expected: "padded: \"hello \"\n",
		},
		{
			name:     "plain string no quoting",
			input:    map[string]any{"plain": "No quotes needed here"},
			expected: "plain: No quotes needed here\n",
		},
		{
			name:     "empty string",
			input:    map[string]any{"empty": ""},
			expected: "empty: \"\"\n",
		},
		{
			name:     "string with backslash and colon",
			input:    map[string]any{"path": `C:\Users\test`},
			expected: "path: \"C:\\\\Users\\\\test\"\n",
		},
		{
			name:     "string with only backslash",
			input:    map[string]any{"sep": `\`},
			expected: "sep: \\\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToTOON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestToTOON_TopLevelScalar(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "string",
			input:    "hello",
			expected: "hello\n",
		},
		{
			name:     "number",
			input:    float64(42),
			expected: "42\n",
		},
		{
			name:     "boolean",
			input:    true,
			expected: "true\n",
		},
		{
			name:     "null",
			input:    nil,
			expected: "null\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToTOON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestToTOON_TopLevelArray(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "primitive array",
			input:    []any{"a", "b", "c"},
			expected: "[3]: a,b,c\n",
		},
		{
			name: "tabular array",
			input: []any{
				map[string]any{"id": float64(1), "name": "Alice"},
				map[string]any{"id": float64(2), "name": "Bob"},
			},
			expected: "[2]{id,name}:\n" +
				"  1,Alice\n" +
				"  2,Bob\n",
		},
		{
			name:     "empty array",
			input:    []any{},
			expected: "[0]:\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToTOON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, tt.expected)
			}
		})
	}
}

func TestToTOON_ComplexStructure(t *testing.T) {
	input := map[string]any{
		"api": map[string]any{
			"endpoints": []any{
				map[string]any{"method": "GET", "path": "/users"},
				map[string]any{"method": "POST", "path": "/users"},
			},
			"version": "v2",
		},
		"name":   "my-service",
		"status": true,
		"tags":   []any{"production", "stable"},
	}

	expected := "api:\n" +
		"  endpoints[2]{method,path}:\n" +
		"    GET,/users\n" +
		"    POST,/users\n" +
		"  version: v2\n" +
		"name: my-service\n" +
		"status: true\n" +
		"tags[2]: production,stable\n"

	got, err := ToTOON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, expected)
	}
}

func TestToTOON_TabularWithSpecialChars(t *testing.T) {
	input := map[string]any{
		"records": []any{
			map[string]any{"name": "Alice, PhD", "role": "admin"},
			map[string]any{"name": "Bob", "role": "user: basic"},
		},
	}

	expected := "records[2]{name,role}:\n" +
		"  \"Alice, PhD\",admin\n" +
		"  Bob,\"user: basic\"\n"

	got, err := ToTOON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Errorf("ToTOON() =\n%q\nwant:\n%q", got, expected)
	}
}

func TestUniformObjectFields(t *testing.T) {
	tests := []struct {
		name    string
		input   []any
		fields  []string
		uniform bool
	}{
		{
			name:    "empty array",
			input:   []any{},
			uniform: false,
		},
		{
			name:    "non-object elements",
			input:   []any{"a", "b"},
			uniform: false,
		},
		{
			name: "uniform objects",
			input: []any{
				map[string]any{"a": float64(1), "b": float64(2)},
				map[string]any{"a": float64(3), "b": float64(4)},
			},
			fields:  []string{"a", "b"},
			uniform: true,
		},
		{
			name: "non-uniform objects",
			input: []any{
				map[string]any{"a": float64(1)},
				map[string]any{"b": float64(2)},
			},
			uniform: false,
		},
		{
			name: "different key counts",
			input: []any{
				map[string]any{"a": float64(1), "b": float64(2)},
				map[string]any{"a": float64(3)},
			},
			uniform: false,
		},
		{
			name:    "empty objects",
			input:   []any{map[string]any{}},
			uniform: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, ok := uniformObjectFields(tt.input)
			if ok != tt.uniform {
				t.Errorf("uniformObjectFields() uniform = %v, want %v", ok, tt.uniform)
			}
			if tt.uniform && len(fields) != len(tt.fields) {
				t.Errorf("uniformObjectFields() fields = %v, want %v", fields, tt.fields)
			}
		})
	}
}

func TestAllPrimitive(t *testing.T) {
	tests := []struct {
		name     string
		input    []any
		expected bool
	}{
		{"strings", []any{"a", "b"}, true},
		{"numbers", []any{float64(1), float64(2)}, true},
		{"mixed primitives", []any{"a", float64(1), true, nil}, true},
		{"contains map", []any{"a", map[string]any{"b": float64(1)}}, false},
		{"contains array", []any{"a", []any{"b"}}, false},
		{"empty", []any{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allPrimitive(tt.input)
			if got != tt.expected {
				t.Errorf("allPrimitive() = %v, want %v", got, tt.expected)
			}
		})
	}
}
