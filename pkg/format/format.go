package format

import (
	"encoding/json"
	"fmt"
)

// Format converts a parsed JSON value to the specified output format.
// Supported formats: "json" (compact), "toon" (TOON v3.0), "csv" (RFC 4180), "text" (passthrough).
// Returns an error for unsupported formats or conversion failures.
func Format(data any, formatName string) (string, error) {
	switch formatName {
	case "json":
		return toJSON(data)
	case "toon":
		return ToTOON(data)
	case "csv":
		return ToCSV(data)
	case "text":
		return toText(data)
	default:
		return "", fmt.Errorf("unsupported output format: %q", formatName)
	}
}

// toJSON re-marshals data as compact JSON.
func toJSON(data any) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("json marshal: %w", err)
	}
	return string(b), nil
}

// toText converts data to its string representation.
// Strings pass through unchanged; other types are JSON-marshaled.
func toText(data any) (string, error) {
	if s, ok := data.(string); ok {
		return s, nil
	}
	return toJSON(data)
}
