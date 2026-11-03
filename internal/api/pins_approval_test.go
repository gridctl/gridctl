package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestHandleApprovePins_AttributionAndRefusal(t *testing.T) {
	server, ps := setupPinsServer(t)
	tools := []mcp.Tool{{Name: "echo", Description: "original"}}
	if _, err := ps.VerifyOrPin("myserver", tools); err != nil {
		t.Fatal(err)
	}
	server.gateway.Router().AddClient(newMockAgentClient("myserver", tools))

	approve := func(t *testing.T, body, principal string) *httptest.ResponseRecorder {
		t.Helper()
		req := loopbackRequest(http.MethodPost, "/api/pins/myserver/approve", strings.NewReader(body))
		req.SetPathValue("server", "myserver")
		if principal != "" {
			req = req.WithContext(mcp.WithPrincipal(req.Context(), principal))
		}
		w := httptest.NewRecorder()
		server.handleApprovePins(w, req)
		return w
	}

	w := approve(t, `{"evidence":["Bearer abc"]}`, "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "bearer token") {
		t.Fatalf("credential refusal = %d %s", w.Code, w.Body.String())
	}
	if sp, ok := ps.GetServer("myserver"); ok && len(sp.Approvals) != 0 {
		t.Fatal("refused reference was recorded")
	}

	w = approve(t, `{"approved_by":"Bearer eyJ"}`, "oauth-principal")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "bearer token") {
		t.Fatalf("declared approver refusal = %d %s", w.Code, w.Body.String())
	}

	w = approve(t, `{"approved_by":"`+strings.Repeat("a", 300)+`"}`, "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "longer than") {
		t.Fatalf("long approver = %d %s", w.Code, w.Body.String())
	}

	w = approve(t, `{}`, "Bearer abc")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "bearer token") {
		t.Fatalf("oauth approver refusal = %d %s", w.Code, w.Body.String())
	}

	w = approve(t, `{"approved_by":"declared-name","reason":"reviewed"}`, "oauth-principal")
	if w.Code != http.StatusOK {
		t.Fatalf("oauth approve = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"approved_by":"oauth-principal"`) || !strings.Contains(w.Body.String(), `"approved_by_source":"oauth"`) {
		t.Fatalf("oauth response = %s", w.Body.String())
	}
	sp, ok := ps.GetServer("myserver")
	if !ok || len(sp.Approvals) != 1 || sp.Approvals[0].ApprovedBy != "oauth-principal" || sp.Approvals[0].ApprovedBySource != "oauth" {
		t.Fatalf("oauth entry = %+v", sp)
	}
	if sp.Approvals[0].ApprovedHash != sp.ServerHash {
		t.Fatalf("approved_hash %s != server_hash %s", sp.Approvals[0].ApprovedHash, sp.ServerHash)
	}

	w = approve(t, `{"approved_by":"declared-name","evidence":["https://example.test/pr/1"]}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("declared approve = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"evidence_count":1`) || !strings.Contains(w.Body.String(), `"approved_by_source":"declared"`) {
		t.Fatalf("declared response = %s", w.Body.String())
	}
	sp, _ = ps.GetServer("myserver")
	if sp.Approvals[0].ApprovedBy != "declared-name" || sp.Approvals[0].ApprovedBySource != "declared" {
		t.Fatalf("declared entry = %+v", sp.Approvals[0])
	}

	w = approve(t, `{}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("os user approve = %d %s", w.Code, w.Body.String())
	}
	sp, _ = ps.GetServer("myserver")
	if sp.Approvals[0].ApprovedBySource != "os_user" && sp.Approvals[0].ApprovedBy != "" {
		t.Fatalf("os user entry = %+v", sp.Approvals[0])
	}
	if sp.Approvals[0].ApprovedBy == "" && sp.Approvals[0].ApprovedBySource != "" {
		t.Fatalf("empty approver kept a source: %+v", sp.Approvals[0])
	}
}

func TestHandleApprovePins_BodyTooLarge(t *testing.T) {
	server, ps := setupPinsServer(t)
	tools := []mcp.Tool{{Name: "echo", Description: "original"}}
	if _, err := ps.VerifyOrPin("myserver", tools); err != nil {
		t.Fatal(err)
	}
	body := `{"reason":"` + strings.Repeat("a", approveRequestMaxBytes) + `"}`
	req := loopbackRequest(http.MethodPost, "/api/pins/myserver/approve", strings.NewReader(body))
	req.SetPathValue("server", "myserver")
	w := httptest.NewRecorder()
	server.handleApprovePins(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "too large") {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	if sp, ok := ps.GetServer("myserver"); ok && len(sp.Approvals) != 0 {
		t.Fatal("oversized body was recorded")
	}
}

func TestHandleApproveSkillPin_ApproverRefusal(t *testing.T) {
	server, ps, _ := setupSkillPinsServer(t, "alpha")
	approve := func(t *testing.T, body, principal string) *httptest.ResponseRecorder {
		t.Helper()
		req := loopbackRequest(http.MethodPost, "/api/skill-pins/alpha/approve", strings.NewReader(body))
		req.SetPathValue("name", "alpha")
		if principal != "" {
			req = req.WithContext(mcp.WithPrincipal(req.Context(), principal))
		}
		w := httptest.NewRecorder()
		server.handleApproveSkillPin(w, req)
		return w
	}

	w := approve(t, `{"approved_by":"Bearer abc"}`, "oauth-principal")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "bearer token") {
		t.Fatalf("declared approver = %d %s", w.Code, w.Body.String())
	}
	if pin, ok := ps.Get("alpha"); ok && len(pin.Approvals) != 0 {
		t.Fatal("refused approver was recorded")
	}

	oversized := `{"reason":"` + strings.Repeat("b", approveRequestMaxBytes) + `"}`
	w = approve(t, oversized, "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "too large") {
		t.Fatalf("oversized = %d %s", w.Code, w.Body.String())
	}
}
