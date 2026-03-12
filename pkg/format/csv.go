package format

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
)

// ToCSV converts a parsed JSON value to CSV format.
// The input must be an array of objects ([]any of map[string]any).
// Headers are derived from the first object's keys in sorted order.
// Returns an error if the data is not tabular.
func ToCSV(data any) (string, error) {
	arr, ok := data.([]any)
	if !ok {
		return "", fmt.Errorf("csv format requires an array, got %T", data)
	}
	if len(arr) == 0 {
		return "", nil
	}

	// Extract headers from first object
	first, ok := arr[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("csv format requires array of objects, got array of %T", arr[0])
	}

	headers := make([]string, 0, len(first))
	for k := range first {
		headers = append(headers, k)
	}
	sort.Strings(headers)

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// Write header row
	if err := w.Write(headers); err != nil {
		return "", fmt.Errorf("csv write headers: %w", err)
	}

	// Write data rows
	row := make([]string, len(headers))
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("csv format requires array of objects, element %d is %T", i, item)
		}
		for j, h := range headers {
			row[j] = formatCSVValue(m[h])
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("csv write row %d: %w", i, err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("csv flush: %w", err)
	}
	return buf.String(), nil
}

// formatCSVValue converts a value to its string representation for CSV output.
func formatCSVValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}
