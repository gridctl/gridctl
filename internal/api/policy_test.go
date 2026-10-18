package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/toolpolicy"
)

func TestHandlePolicy_Unconfigured(t *testing.T) {
	s := &Server{}
	req := loopbackRequest(http.MethodGet, "/api/policy", nil)
	w := httptest.NewRecorder()
	s.handlePolicy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var got toolpolicy.StatusReport
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Configured || got.Rules == nil || got.Tools == nil {
		t.Fatalf("body = %s", w.Body.String())
	}
	if bytesContainNull(w.Body.Bytes()) {
		t.Fatalf("null collection: %s", w.Body.String())
	}
}

func TestHandlePolicy_Configured(t *testing.T) {
	s := &Server{}
	s.SetPolicyStatusFunc(func() toolpolicy.StatusReport {
		return toolpolicy.StatusReport{
			Configured:       true,
			Default:          "allow",
			BlockDestructive: boolPtr(false),
			Rules:            []toolpolicy.RuleStatus{{Index: 0, Match: "vault__*", Action: "block"}},
			Tools: map[string]toolpolicy.ToolStatus{
				"vault__read_secret": {Action: "block", Source: "rule", RuleIndex: intPtr(0), Rule: "vault__*", Exposed: true},
				"vault__purge":       {Action: "block", Source: "block_destructive", Exposed: false},
			},
		}
	})
	req := loopbackRequest(http.MethodGet, "/api/policy", nil)
	w := httptest.NewRecorder()
	s.handlePolicy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["configured"] != true {
		t.Fatalf("body = %s", w.Body.String())
	}
	tools, _ := got["tools"].(map[string]any)
	hidden, _ := tools["vault__purge"].(map[string]any)
	if hidden["action"] != "block" || hidden["exposed"] != false || hidden["source"] != "block_destructive" {
		t.Fatalf("hidden row = %#v", hidden)
	}
}

func TestOutcomeError_PolicyBlocked(t *testing.T) {
	out := mcp.CallOutcome{Reason: "policy_blocked"}
	if outcomeErrorCode(out) != errCodePolicyBlocked {
		t.Fatalf("code = %q", outcomeErrorCode(out))
	}
	if safeOutcomeMessage(out) != "tool blocked by policy" {
		t.Fatalf("message = %q", safeOutcomeMessage(out))
	}
	if safeOutcomeMessage(mcp.CallOutcome{Reason: "gate_denied"}) != "tool call denied by a call gate" {
		t.Fatal("rate-limit safe message changed")
	}
}

func bytesContainNull(raw []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return true
	}
	for _, key := range []string{"rules", "tools"} {
		if string(probe[key]) == "null" {
			return true
		}
	}
	return false
}
