package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPatchGatewayDefaultModel_CreatesGatewayBlock(t *testing.T) {
	source := []byte(`# my stack
name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
`)
	out, err := patchGatewayDefaultModel(source, "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "gateway:") || !strings.Contains(got, "default_model: claude-haiku-4-5") {
		t.Errorf("expected gateway.default_model; got:\n%s", got)
	}
	if !strings.Contains(got, "# my stack") {
		t.Errorf("comment lost:\n%s", got)
	}
}

func TestPatchGatewayDefaultModel_ReplaceExisting(t *testing.T) {
	source := []byte(`name: test
gateway:
  default_model: claude-haiku-4-5   # stack floor
  code_mode: on
`)
	out, err := patchGatewayDefaultModel(source, "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "default_model: claude-sonnet-4-6") {
		t.Errorf("expected replaced model; got:\n%s", got)
	}
	if !strings.Contains(got, "code_mode:") {
		t.Errorf("sibling gateway key lost:\n%s", got)
	}
}

func TestPatchGatewayDefaultModel_ClearRemovesKeyKeepsBlock(t *testing.T) {
	source := []byte(`name: test
gateway:
  default_model: claude-haiku-4-5
  code_mode: on
`)
	out, err := patchGatewayDefaultModel(source, "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "default_model") {
		t.Errorf("cleared key must be deleted:\n%s", got)
	}
	if !strings.Contains(got, "gateway:") || !strings.Contains(got, "code_mode:") {
		t.Errorf("gateway block with other keys must survive:\n%s", got)
	}
}

func TestPatchGatewayDefaultModel_ClearLastKeyDropsBlock(t *testing.T) {
	source := []byte(`name: test
gateway:
  default_model: claude-haiku-4-5
mcp-servers:
  - name: github
    image: mcp/github:latest
`)
	out, err := patchGatewayDefaultModel(source, "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "gateway") {
		t.Errorf("a gateway block emptied by the clear must be removed:\n%s", got)
	}
	var roundTrip map[string]any
	if err := yaml.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	if roundTrip["name"] != "test" {
		t.Errorf("sibling keys lost: %v", roundTrip)
	}
}

func TestPatchGatewayDefaultModel_ClearAbsentIsNoOp(t *testing.T) {
	source := []byte("name: test\n")
	out, err := patchGatewayDefaultModel(source, "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if strings.Contains(string(out), "gateway") {
		t.Errorf("clearing with no gateway block must not create one:\n%s", out)
	}
}

func TestPatchGatewayDefaultModel_ClearPreservesPreexistingEmptyBlock(t *testing.T) {
	// A bare `gateway:` line (explicit null) was not created by this
	// endpoint; a clear must leave it alone rather than deleting user YAML.
	source := []byte("name: test\ngateway:\n")
	out, err := patchGatewayDefaultModel(source, "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if !strings.Contains(string(out), "gateway:") {
		t.Errorf("pre-existing bare gateway block must survive a clear:\n%s", out)
	}
}

func TestHandleSetDefaultModel_RoundTrip(t *testing.T) {
	srv := newTestServer(t)
	stackFile := writeTempStack(t, `name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
`)
	srv.SetStackFile(stackFile)
	handler := srv.Handler()

	// Set.
	req := httptest.NewRequest(http.MethodPut, "/api/gateway/default-model",
		strings.NewReader(`{"model":"claude-haiku-4-5"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp setDefaultModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Model != "claude-haiku-4-5" {
		t.Errorf("response = %+v", resp)
	}
	assertStackContains(t, stackFile, "default_model: claude-haiku-4-5")

	// Clear — the gateway block this endpoint created must round-trip away.
	req = httptest.NewRequest(http.MethodPut, "/api/gateway/default-model",
		strings.NewReader(`{"model":""}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertStackNotContains(t, stackFile, "gateway")
}

func TestHandleSetDefaultModel_NoStackFile(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPut, "/api/gateway/default-model",
		strings.NewReader(`{"model":"claude-haiku-4-5"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
