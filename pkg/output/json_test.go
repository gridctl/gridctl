package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeJSON(&buf, map[string]any{"gateways": []string{}, "ok": true}); err != nil {
		t.Fatalf("EncodeJSON: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "\033") {
		t.Errorf("expected no ANSI escapes in JSON output, got %q", out)
	}
	if !strings.Contains(out, "  \"gateways\"") {
		t.Errorf("expected two-space indentation, got %q", out)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded["ok"] != true {
		t.Errorf("round-trip mismatch: %v", decoded)
	}
}
