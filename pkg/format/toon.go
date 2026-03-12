// Package format provides output format conversion for MCP tool call results.
// Supported formats: TOON (Token-Oriented Object Notation), CSV, JSON, and text.
package format

import (
	"fmt"
	"sort"
	"strings"
)

// ToTOON converts a parsed JSON value to TOON v3.0 notation.
// TOON is a compact, LLM-readable format that reduces token consumption
// compared to JSON while preserving structural information.
func ToTOON(data any) (string, error) {
	var b strings.Builder
	if err := writeTOON(&b, "", data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// writeTOON recursively writes data in TOON format with the given indentation prefix.
func writeTOON(b *strings.Builder, indent string, data any) error {
	switch v := data.(type) {
	case map[string]any:
		return writeTOONMap(b, indent, v)
	case []any:
		return writeTOONTopArray(b, indent, v)
	default:
		// Scalar at top level
		b.WriteString(formatTOONValue(data))
		b.WriteByte('\n')
		return nil
	}
}

// writeTOONMap writes a map as TOON key-value pairs at the given indent level.
func writeTOONMap(b *strings.Builder, indent string, m map[string]any) error {
	keys := sortedKeys(m)
	for _, key := range keys {
		val := m[key]
		switch v := val.(type) {
		case map[string]any:
			b.WriteString(indent)
			b.WriteString(key)
			b.WriteString(":\n")
			if err := writeTOONMap(b, indent+"  ", v); err != nil {
				return err
			}
		case []any:
			if err := writeTOONArray(b, indent, key, v); err != nil {
				return err
			}
		default:
			b.WriteString(indent)
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(formatTOONValue(val))
			b.WriteByte('\n')
		}
	}
	return nil
}

// writeTOONTopArray writes a top-level array (not associated with a key).
func writeTOONTopArray(b *strings.Builder, indent string, arr []any) error {
	if len(arr) == 0 {
		b.WriteString(indent)
		b.WriteString("[0]:\n")
		return nil
	}

	if fields, ok := uniformObjectFields(arr); ok {
		// Tabular format
		b.WriteString(indent)
		b.WriteString(fmt.Sprintf("[%d]{%s}:\n", len(arr), strings.Join(fields, ",")))
		childIndent := indent + "  "
		for _, item := range arr {
			m := item.(map[string]any)
			b.WriteString(childIndent)
			writeTOONRow(b, m, fields)
			b.WriteByte('\n')
		}
		return nil
	}

	if allPrimitive(arr) {
		b.WriteString(indent)
		b.WriteString(fmt.Sprintf("[%d]: ", len(arr)))
		writeTOONPrimitiveList(b, arr)
		b.WriteByte('\n')
		return nil
	}

	// Mixed array — write each element indented
	b.WriteString(indent)
	b.WriteString(fmt.Sprintf("[%d]:\n", len(arr)))
	childIndent := indent + "  "
	for _, item := range arr {
		if err := writeTOON(b, childIndent, item); err != nil {
			return err
		}
	}
	return nil
}

// writeTOONArray writes a named array value in TOON format.
func writeTOONArray(b *strings.Builder, indent, key string, arr []any) error {
	if len(arr) == 0 {
		b.WriteString(indent)
		b.WriteString(key)
		b.WriteString("[0]:\n")
		return nil
	}

	// Check for tabular (uniform objects)
	if fields, ok := uniformObjectFields(arr); ok {
		b.WriteString(indent)
		b.WriteString(fmt.Sprintf("%s[%d]{%s}:\n", key, len(arr), strings.Join(fields, ",")))
		childIndent := indent + "  "
		for _, item := range arr {
			m := item.(map[string]any)
			b.WriteString(childIndent)
			writeTOONRow(b, m, fields)
			b.WriteByte('\n')
		}
		return nil
	}

	// Check for simple primitive array
	if allPrimitive(arr) {
		b.WriteString(indent)
		b.WriteString(fmt.Sprintf("%s[%d]: ", key, len(arr)))
		writeTOONPrimitiveList(b, arr)
		b.WriteByte('\n')
		return nil
	}

	// Mixed array — write each element indented
	b.WriteString(indent)
	b.WriteString(fmt.Sprintf("%s[%d]:\n", key, len(arr)))
	childIndent := indent + "  "
	for _, item := range arr {
		if err := writeTOON(b, childIndent, item); err != nil {
			return err
		}
	}
	return nil
}

// writeTOONRow writes a single tabular row with values in field order.
func writeTOONRow(b *strings.Builder, m map[string]any, fields []string) {
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(formatTOONValue(m[f]))
	}
}

// writeTOONPrimitiveList writes comma-separated primitive values.
func writeTOONPrimitiveList(b *strings.Builder, arr []any) {
	for i, item := range arr {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(formatTOONValue(item))
	}
}

// formatTOONValue formats a single value for TOON output.
// Strings are quoted only when they contain special characters.
func formatTOONValue(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		// Use %g to avoid trailing zeros, but handle integers cleanly
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case string:
		return formatTOONString(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatTOONString returns a TOON string representation.
// Plain strings are unquoted; strings with special characters are double-quoted
// with standard Go-style escaping.
func formatTOONString(s string) string {
	if s == "" {
		return `""`
	}
	if needsQuoting(s) {
		return quoteTOONString(s)
	}
	return s
}

// needsQuoting returns true if the string contains characters that require
// double-quoting in TOON format.
func needsQuoting(s string) bool {
	if s[0] == ' ' || s[0] == '\t' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t' {
		return true
	}
	for _, c := range s {
		switch c {
		case ',', ':', '"', '\n', '\t':
			return true
		}
	}
	return false
}

// quoteTOONString wraps a string in double quotes with escaping.
func quoteTOONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// uniformObjectFields checks if all elements in the array are maps with
// identical key sets. Returns the sorted field names if uniform.
func uniformObjectFields(arr []any) ([]string, bool) {
	if len(arr) == 0 {
		return nil, false
	}

	first, ok := arr[0].(map[string]any)
	if !ok {
		return nil, false
	}

	fields := sortedKeys(first)
	if len(fields) == 0 {
		return nil, false
	}

	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		if len(m) != len(fields) {
			return nil, false
		}
		for _, f := range fields {
			val, exists := m[f]
			if !exists {
				return nil, false
			}
			// Tabular only works for primitive values
			switch val.(type) {
			case map[string]any, []any:
				return nil, false
			}
		}
	}
	return fields, true
}

// allPrimitive returns true if every element is a scalar (not map or slice).
func allPrimitive(arr []any) bool {
	for _, item := range arr {
		switch item.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
