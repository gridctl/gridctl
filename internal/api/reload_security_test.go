package api

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/reload"
)

func TestReloadSecurity_InitializePreservesAPIState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candidate.yaml")
	token := rand.Text()
	t.Setenv("GRIDCTL_TEST_AUTH", token)
	if err := os.WriteFile(path, []byte("name: candidate\ngateway:\n  auth:\n    type: bearer\n    token: ${GRIDCTL_TEST_AUTH}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := reload.NewHandler("", nil, nil, nil, 8180, 9000, nil, nil)
	h.SetSecurityPreflight(reload.NewSecurityPreflight(nil, "", false))
	s := &Server{stacksDir: dir, reloadHandler: h, startWatcher: func(string) { t.Fatal("watcher started on rejection") }}
	for range 2 {
		w := httptest.NewRecorder()
		s.handleStackInitialize(w, httptest.NewRequest("POST", "/api/stack/initialize", strings.NewReader(`{"name":"candidate"}`)))
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d", w.Code)
		}
		var result reload.ReloadResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Code != "restart_required" || result.Success {
			t.Fatal("missing structured rejection")
		}
		if s.stackFile != "" || s.stackName != "" || h.CurrentConfig() != nil {
			t.Fatal("initialization accepted rejected state")
		}
		if strings.Contains(w.Body.String(), token) {
			t.Fatal("credential leaked")
		}
	}
}

func TestWritePreflightFailure(t *testing.T) {
	for _, code := range []string{"restart_required", "invalid_candidate", ""} {
		w := httptest.NewRecorder()
		result := &reload.ReloadResult{Code: code, ChangedFields: []string{"gateway.auth.token"}, Message: "active settings unchanged"}
		if got := writePreflightFailure(w, result); got != (code != "") {
			t.Fatal("unexpected classification")
		}
		if code != "" && !strings.Contains(w.Body.String(), `"code":"`+code+`"`) {
			t.Fatal("code lost")
		}
	}
}

func TestAuthRejectionMarker_OnlyMiddlewareDenials(t *testing.T) {
	token := rand.Text()
	for _, valid := range []bool{false, true} {
		h := authMiddleware("bearer", token, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }))
		r := httptest.NewRequest("GET", "/api/status", nil)
		if valid {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if (w.Header().Get("Gridctl-Auth-Rejected") == "1") == valid {
			t.Fatal("gateway/downstream rejection provenance conflated")
		}
	}
}

func TestReloadSecurity_InvalidCandidateIsValueFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	token := rand.Text()
	if err := os.WriteFile(path, []byte("name: candidate\ngateway: "+token), 0600); err != nil {
		t.Fatal(err)
	}
	baseline := &config.Stack{Name: "active"}
	h := reload.NewHandler(path, baseline, nil, nil, 8180, 9000, nil, nil)
	h.SetSecurityPreflight(reload.NewSecurityPreflight(baseline, "", false))
	result, err := h.Reload(t.Context())
	if err != nil || result.Success || result.Code != "invalid_candidate" {
		t.Fatal("invalid candidate accepted")
	}
	if strings.Contains(result.Message, token) || h.CurrentConfig() != baseline {
		t.Fatal("invalid load changed or disclosed state")
	}
	if err := os.WriteFile(path, []byte("name: candidate\ngateway:\n  auth:\n    type: invalid\n    token: "+token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = h.Reload(t.Context())
	if err != nil || result.Success || result.Code != "invalid_candidate" || h.CurrentConfig() != baseline || strings.Contains(result.Message, token) {
		t.Fatal("validation failure changed or disclosed state")
	}
}
