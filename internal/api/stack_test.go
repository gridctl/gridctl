package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

// writeTestStack creates a temporary stack.yaml and returns its path.
func writeTestStack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "stack.yaml")
	content := `name: test-stack
network:
  name: test-net
mcp-servers:
  - name: server-a
    image: alpine
    port: 3000
    env:
      API_KEY: "${vault:MY_KEY}"
      DB_PASSWORD: secret123
      HOST: localhost
  - name: server-b
    image: nginx
    port: 3001
    env:
      AUTH_TOKEN: "${vault:AUTH_TOK}"
agents:
  - name: agent-1
    runtime: claude-code
    prompt: test
    uses:
      - server: server-a
`
	err := os.WriteFile(p, []byte(content), 0644)
	assert.NoError(t, err)
	return p
}

func TestHandleStackValidate_ValidYAML(t *testing.T) {
	s := &Server{}
	body := `
name: test
network:
  name: test-net
mcp-servers:
  - name: s1
    image: alpine
    port: 3000
`
	req := httptest.NewRequest(http.MethodPost, "/api/stack/validate", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleStackValidate(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":true`)
}

func TestHandleStackValidate_InvalidYAML(t *testing.T) {
	s := &Server{}
	body := `:::not yaml`
	req := httptest.NewRequest(http.MethodPost, "/api/stack/validate", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleStackValidate(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":false`)
	assert.Contains(t, w.Body.String(), `"severity":"error"`)
}

func TestHandleStackValidate_InvalidStack(t *testing.T) {
	s := &Server{}
	body := `
mcp-servers:
  - name: s1
`
	req := httptest.NewRequest(http.MethodPost, "/api/stack/validate", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleStackValidate(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":false`)
	assert.Contains(t, w.Body.String(), `"errorCount"`)
}

func TestHandleStackSpec_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/spec", nil)
	w := httptest.NewRecorder()

	s.handleStackSpec(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackPlan_NoStackDeployed(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/plan", nil)
	w := httptest.NewRecorder()

	s.handleStackPlan(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackHealth_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/health", nil)
	w := httptest.NewRecorder()

	s.handleStackHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"unknown"`)
}

func TestHandleStackExport_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/export", nil)
	w := httptest.NewRecorder()

	s.handleStackExport(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackRecipes(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/recipes", nil)
	w := httptest.NewRecorder()

	s.handleStackRecipes(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"rag-pipeline"`)
	assert.Contains(t, w.Body.String(), `"dev-toolbox"`)
}

func TestSanitizeStackSecrets(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{"already vault ref", "API_KEY", "${vault:MY_KEY}", "${vault:MY_KEY}"},
		{"already var ref", "API_KEY", "${var:MY_KEY}", "${var:MY_KEY}"},
		{"sensitive password", "DB_PASSWORD", "secret123", "${var:test_DB_PASSWORD}"},
		{"sensitive token", "AUTH_TOKEN", "tok_abc", "${var:test_AUTH_TOKEN}"},
		{"non-sensitive", "HOST", "localhost", "localhost"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{tc.key: tc.value}
			sanitizeStackSecrets(&config.Stack{
				MCPServers: []config.MCPServer{{Name: "test", Env: env}},
			})
			assert.Equal(t, tc.expected, env[tc.key])
		})
	}
}

func TestHandleStackSpec_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/spec", nil)
	w := httptest.NewRecorder()

	s.handleStackSpec(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "test-stack")
	assert.Contains(t, w.Body.String(), "server-a")
}

func TestHandleStackExport_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/export", nil)
	w := httptest.NewRecorder()

	s.handleStackExport(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "content")
	// Secrets should be sanitized — DB_PASSWORD should be vault ref
	assert.Contains(t, body, "${vault:")
	assert.NotContains(t, body, "secret123")
}

func TestHandleStackHealth_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf, stackName: "test-stack"}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/health", nil)
	w := httptest.NewRecorder()

	s.handleStackHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// Should have validation status
	assert.Contains(t, body, `"status"`)
	// Single-replica servers MUST NOT bloat the shape with a replicas map.
	assert.NotContains(t, body, `"replicas"`)
}

func TestHandleStackHealth_OmitsReplicasWhenSingleReplica(t *testing.T) {
	// No gateway and no stack means no replicas map in the response.
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/health", nil)
	w := httptest.NewRecorder()

	s.handleStackHealth(w, req)

	var got config.SpecHealth
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Nil(t, got.Replicas, "single-replica deployments must omit the replicas field")
}

func TestHandleStackPlan_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf, stackName: "test-stack"}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/plan", nil)
	w := httptest.NewRecorder()

	s.handleStackPlan(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "hasChanges")
}

func TestSanitizeStackSecrets_NilEnv(t *testing.T) {
	// Should not panic with nil env maps
	sanitizeStackSecrets(&config.Stack{
		MCPServers: []config.MCPServer{{Name: "test"}},
		Resources:  []config.Resource{{Name: "res"}},
	})
}

func TestSanitizeStackSecrets_AllTypes(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{{Name: "srv", Env: map[string]string{"DB_PASSWORD": "pass"}}},
		Resources:  []config.Resource{{Name: "res", Env: map[string]string{"AUTH_TOKEN": "tok"}}},
	}
	sanitizeStackSecrets(stack)
	assert.Equal(t, "${var:srv_DB_PASSWORD}", stack.MCPServers[0].Env["DB_PASSWORD"])
	assert.Equal(t, "${var:res_AUTH_TOKEN}", stack.Resources[0].Env["AUTH_TOKEN"])
}

// Regression: a canonical ${var:KEY} reference was treated as a raw secret and
// rewritten to ${vault:<server>_KEY}, inventing a key absent from the store and
// breaking export/re-apply round trips. Store references must survive export in
// whichever form they were written, on both servers and resources.
func TestSanitizeStackSecrets_PreservesVarRefs(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{{Name: "github", Env: map[string]string{
			"GITHUB_PERSONAL_ACCESS_TOKEN": "${var:GITHUB_PERSONAL_ACCESS_TOKEN}",
			"LEGACY_TOKEN":                 "${vault:LEGACY_TOKEN}",
			"RAW_TOKEN":                    "ghp_rawsecret",
		}}},
		Resources: []config.Resource{{Name: "db", Env: map[string]string{
			"DB_PASSWORD": "${var:DB_PASSWORD}",
		}}},
	}

	sanitizeStackSecrets(stack)

	srv := stack.MCPServers[0].Env
	assert.Equal(t, "${var:GITHUB_PERSONAL_ACCESS_TOKEN}", srv["GITHUB_PERSONAL_ACCESS_TOKEN"],
		"canonical var reference must not be rewritten")
	assert.Equal(t, "${vault:LEGACY_TOKEN}", srv["LEGACY_TOKEN"],
		"deprecated vault reference must not be rewritten")
	assert.Equal(t, "${var:github_RAW_TOKEN}", srv["RAW_TOKEN"],
		"a genuine raw secret is sanitized in canonical form")
	assert.Equal(t, "${var:DB_PASSWORD}", stack.Resources[0].Env["DB_PASSWORD"],
		"resource env follows the same rule")
}


func TestHandleStackAppend_MCPServer(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "name: server-new\nimage: nginx\nport: 9000\n",
		"resourceType": "mcp-server",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"resourceName":"server-new"`)

	data, err := os.ReadFile(sf)
	assert.NoError(t, err)
	var stack config.Stack
	assert.NoError(t, yaml.Unmarshal(data, &stack))
	assert.Equal(t, 3, len(stack.MCPServers))
	assert.Equal(t, "server-new", stack.MCPServers[2].Name)
}

func TestHandleStackAppend_Resource(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "name: redis\nimage: redis:7\n",
		"resourceType": "resource",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"resourceName":"redis"`)

	data, err := os.ReadFile(sf)
	assert.NoError(t, err)
	var stack config.Stack
	assert.NoError(t, yaml.Unmarshal(data, &stack))
	assert.Equal(t, 1, len(stack.Resources))
	assert.Equal(t, "redis", stack.Resources[0].Name)
}

func TestHandleStackAppend_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackAppend_InvalidResourceType(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "name: test\n",
		"resourceType": "stack",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStackAppend_InvalidYAML(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "[unclosed bracket",
		"resourceType": "agent",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStack_Routing(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{"validate POST", http.MethodPost, "/api/stack/validate", http.StatusOK},
		{"plan GET no stack", http.MethodGet, "/api/stack/plan", http.StatusServiceUnavailable},
		{"health GET", http.MethodGet, "/api/stack/health", http.StatusOK},
		{"spec GET no stack", http.MethodGet, "/api/stack/spec", http.StatusServiceUnavailable},
		{"export GET no stack", http.MethodGet, "/api/stack/export", http.StatusServiceUnavailable},
		{"recipes GET", http.MethodGet, "/api/stack/recipes", http.StatusOK},
		{"unknown path", http.MethodGet, "/api/stack/unknown", http.StatusNotFound},
		{"validate wrong method", http.MethodGet, "/api/stack/validate", http.StatusMethodNotAllowed},
		{"append POST no stack", http.MethodPost, "/api/stack/append", http.StatusServiceUnavailable},
		{"stacks GET", http.MethodGet, "/api/stacks", http.StatusOK},
		{"initialize POST no body", http.MethodPost, "/api/stack/initialize", http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.method == http.MethodPost {
				body = strings.NewReader(`{}`)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			w := httptest.NewRecorder()

			s.Handler().ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
		})
	}
}

// --- Stack library tests ---

func TestHandleStacksList_EmptyDir(t *testing.T) {
	s := &Server{stacksDir: t.TempDir()}
	req := httptest.NewRequest(http.MethodGet, "/api/stacks", nil)
	w := httptest.NewRecorder()

	s.handleStacksList(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"stacks":[]`)
}

func TestHandleStacksSave_Success(t *testing.T) {
	dir := t.TempDir()

	body, _ := json.Marshal(map[string]string{
		"name": "my-stack",
		"yaml": "name: my-stack\nnetwork:\n  name: net\n",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s := &Server{stacksDir: dir}

	s.handleStacksSave(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"success":true`)
	assert.Contains(t, w.Body.String(), `"name":"my-stack"`)
	if _, err := os.Stat(filepath.Join(dir, "my-stack.yaml")); err != nil {
		t.Fatalf("saved stack not found in injected dir: %v", err)
	}
}

func TestHandleStacksSave_InvalidName(t *testing.T) {
	tests := []struct {
		name     string
		stackName string
	}{
		{"slash in name", "my/stack"},
		{"dotdot traversal", "../etc"},
		{"space in name", "my stack"},
		{"empty name", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{
				"name": tc.stackName,
				"yaml": "name: test\n",
			})
			req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
			w := httptest.NewRecorder()

			s := &Server{}
			s.handleStacksSave(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestHandleStacksSave_InvalidYAML(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"name": "test-stack",
		"yaml": ":::not yaml",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s := &Server{}
	s.handleStacksSave(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid YAML")
}

func TestHandleStacksSave_MissingYAML(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"name": "test-stack",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s := &Server{}
	s.handleStacksSave(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStackInitialize_AlreadyLoaded(t *testing.T) {
	s := &Server{stackFile: "/some/existing/stack.yaml"}

	body, _ := json.Marshal(map[string]string{"name": "my-stack"})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "already loaded")
}

func TestHandleStackInitialize_NotFound(t *testing.T) {
	s := &Server{}

	body, _ := json.Marshal(map[string]string{"name": "nonexistent-stack-xyz"})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Stack not found")
}

func TestHandleStackInitialize_MissingName(t *testing.T) {
	s := &Server{}

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStackInitialize_NoReloadHandler(t *testing.T) {
	// An injected stacks dir without the requested file yields 404.
	s := &Server{stacksDir: t.TempDir()}
	body, _ := json.Marshal(map[string]string{"name": "test-stack"})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleStackInitialize_SuccessNoReloadHandler(t *testing.T) {
	// Full initialize flow with no reloadHandler (stackless mode without
	// --watch), against an injected stacks dir.
	stacksDir := t.TempDir()

	stackName := "gridctl-test-init-stack"
	stackPath := filepath.Join(stacksDir, stackName+".yaml")
	content := "name: gridctl-test-init-stack\nnetwork:\n  name: net\n"
	if err := os.WriteFile(stackPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test stack: %v", err)
	}

	s := &Server{stacksDir: stacksDir} // no reloadHandler

	body, _ := json.Marshal(map[string]string{"name": stackName})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"success":true`)
	assert.Contains(t, w.Body.String(), `"watching":false`)

	// Verify server state was updated
	assert.Equal(t, stackPath, s.stackFile)
	assert.Equal(t, stackName, s.stackName)
}

// TestHandleStackInitialize_SurfacesPerServerErrors verifies that when the
// reload handler reports per-server registration failures, the HTTP handler
// returns HTTP 400 with an errors array in the body instead of 200 OK. This
// guards the stackless Save & Load silent-green failure mode.
func TestHandleStackInitialize_SurfacesPerServerErrors(t *testing.T) {
	stacksDir := t.TempDir()

	stackName := "gridctl-test-init-partial-failure"
	stackPath := filepath.Join(stacksDir, stackName+".yaml")
	content := `name: ` + stackName + `
network:
  name: test-net
mcp-servers:
  - name: ext
    url: https://example.com/mcp
    transport: http
`
	if err := os.WriteFile(stackPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing test stack: %v", err)
	}

	gw := mcp.NewGateway()
	orch := runtime.NewOrchestrator(nil, nil)
	rh := reload.NewHandler("", &config.Stack{Name: "gridctl"}, gw, orch, 8180, 9000, nil, nil)
	rh.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []reload.ReplicaRuntime, stackPath string) error {
		return fmt.Errorf("simulated registration failure for %s", server.Name)
	})

	s := &Server{stacksDir: stacksDir}
	s.SetReloadHandler(rh)

	body, _ := json.Marshal(map[string]string{"name": stackName})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Stack initialization failed")
	assert.Contains(t, w.Body.String(), `"errors"`)
	assert.Contains(t, w.Body.String(), "simulated registration failure for ext")

	// stackFile must NOT have been persisted since the initialization failed
	assert.Empty(t, s.stackFile, "stackFile should not be set on failure")
	assert.Empty(t, s.stackName, "stackName should not be set on failure")
}

func TestHandleStacksList_WithFiles(t *testing.T) {
	// Write stacks to an injected stacks dir and verify they appear in
	// the list.
	stacksDir := t.TempDir()

	stackName := "gridctl-test-list-stack"
	stackPath := filepath.Join(stacksDir, stackName+".yaml")
	if err := os.WriteFile(stackPath, []byte("name: test\n"), 0644); err != nil {
		t.Fatalf("writing test stack: %v", err)
	}

	s := &Server{stacksDir: stacksDir}
	req := httptest.NewRequest(http.MethodGet, "/api/stacks", nil)
	w := httptest.NewRecorder()

	s.handleStacksList(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), stackName)
}

