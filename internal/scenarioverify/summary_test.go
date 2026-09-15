package scenarioverify

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteText_OmitsRawOutput(t *testing.T) {
	var buf bytes.Buffer
	rep := &Report{
		Lane:           "unit",
		Required:       "FAIL",
		VerifierStatus: 1,
		Scenarios: []ScenarioResult{{
			ID:       "required-child",
			Status:   "fail",
			Reason:   ReasonSelectorAbsent,
			Boundary: "denied before dispatch",
			Observed: "required package/test run and pass not observed",
		}},
	}
	if err := WriteText(&buf, rep); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "curl http://evil.test") || strings.Contains(got, "PASS: TestFoo") {
		t.Fatalf("summary leaked raw output: %s", got)
	}
	if !strings.Contains(got, "FAIL required-child reason=SELECTOR_ABSENT") {
		t.Fatalf("missing scenario line: %s", got)
	}
}

func TestWriteJSON_StableFields(t *testing.T) {
	var buf bytes.Buffer
	rep := &Report{
		Lane:           "unit",
		Revision:       "abc",
		GoStatus:       1,
		CaptureStatus:  0,
		VerifierStatus: 1,
		Required:       "FAIL",
		LaneReason:     ReasonTestFailure,
		Scenarios: []ScenarioResult{{
			ID:       "required-child",
			Package:  "example.com/mcp",
			Test:     "TestFoo",
			Status:   "fail",
			Reason:   ReasonTestFailure,
			Boundary: "denied before dispatch",
			Observed: "TestFoo failed",
		}},
	}
	if err := WriteJSON(&buf, rep); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["lane"] != "unit" || parsed["revision"] != "abc" {
		t.Fatalf("parsed = %#v", parsed)
	}
}
