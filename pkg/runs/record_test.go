package runs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBoundIdentifier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		stored  string
		omitted string
	}{
		{"github__create_issue", "github__create_issue", ""},
		{"", "", ""},
		{strings.Repeat("a", MaxIdentifierBytes), strings.Repeat("a", MaxIdentifierBytes), ""},
		{strings.Repeat("a", MaxIdentifierBytes+1), "", OmittedOversized},
		{"../etc/passwd", "", OmittedMalformed},
		{"https://evil.example/x", "", OmittedMalformed},
		{"tool with space", "", OmittedMalformed},
		{"has\nnewline", "", OmittedMalformed},
	}
	for _, tc := range tests {
		stored, omitted := BoundIdentifier(tc.in)
		if stored != tc.stored || omitted != tc.omitted {
			t.Errorf("BoundIdentifier(%q) = %q, %q; want %q, %q", tc.in, stored, omitted, tc.stored, tc.omitted)
		}
	}
}

func TestRecordAllowlistJSON(t *testing.T) {
	t.Parallel()
	id := 0
	rec := Record{
		SchemaVersion:      SchemaVersion,
		RecorderInstanceID: "abc",
		Sequence:           1,
		AttemptID:          "att",
		Disposition:        DispositionCompleted,
		Stage:              StageDownstream,
		Reason:             ReasonOK,
		ReplicaID:          &id,
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"arguments", "result", "error", "stack", "headers", "url", "code", "hash", "token", "requestState", "inputResponses", "meta"}
	body := string(raw)
	for _, key := range forbidden {
		if _, ok := m[key]; ok {
			t.Errorf("record JSON contains forbidden key %q", key)
		}
		if strings.Contains(strings.ToLower(body), strings.ToLower(`"`+key+`"`)) {
			t.Errorf("record JSON mentions forbidden key %q: %s", key, body)
		}
	}
}

func TestFilterMatch(t *testing.T) {
	t.Parallel()
	rec := Record{
		RequestedName:  "s__t",
		ResolvedServer: "s",
		ResolvedTool:   "t",
		Disposition:    DispositionDenied,
		ClientLabel:    "claude",
		AttemptID:      "a1",
	}
	if !(Filter{Disposition: DispositionDenied}).match(rec) {
		t.Fatal("expected disposition match")
	}
	if (Filter{Disposition: DispositionCompleted}).match(rec) {
		t.Fatal("did not expect completed match")
	}
	if !(Filter{ResolvedServer: "s", ResolvedTool: "t"}).match(rec) {
		t.Fatal("expected target match")
	}
}
