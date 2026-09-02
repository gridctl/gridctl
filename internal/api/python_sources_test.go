package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/mcp"
	runtimedocker "github.com/gridctl/gridctl/pkg/runtime/docker"
)

type stubPythonSourcePlanner struct {
	plan     *builder.ResolvedBuildPlan
	versions *builder.PyPIVersions
	err      error
	lastOpts builder.BuildOptions
}

func (s *stubPythonSourcePlanner) Plan(_ context.Context, opts builder.BuildOptions) (*builder.ResolvedBuildPlan, error) {
	s.lastOpts = opts
	return s.plan, s.err
}

func (s *stubPythonSourcePlanner) Versions(_ context.Context, _ string) (*builder.PyPIVersions, error) {
	return s.versions, s.err
}

func TestHandleStackResourceValidate_MCPServerSnippet(t *testing.T) {
	srv := newTestServer(t)
	body := `{"resourceType":"mcp-server","yaml":"name: fetch\nsource:\n  type: pypi\n  package: mcp-server-fetch\n  ref: 0.6.0\n"}`
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/stack/resource/validate", strings.NewReader(body)))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":true`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleStackResourceValidate_InvalidSnippet(t *testing.T) {
	srv := newTestServer(t)
	body := `{"resourceType":"mcp-server","yaml":"name: fetch\nsource:\n  type: pypi\n  package: mcp-server-fetch\n  ref: latest\n"}`
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/stack/resource/validate", strings.NewReader(body)))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":false`) || !strings.Contains(rec.Body.String(), "exact published PEP 440") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePythonPackageVersions(t *testing.T) {
	srv := newTestServer(t)
	srv.pythonSources = &stubPythonSourcePlanner{versions: &builder.PyPIVersions{
		Package: "mcp-server-fetch", Latest: "0.6.0", Versions: []string{"0.6.0", "0.5.0"},
	}}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/python/packages/mcp-server-fetch/versions", nil))

	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "private, max-age=300" {
		t.Fatalf("status = %d, cache = %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec.Body.String(), `"latest":"0.6.0"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandlePythonResolveAndGeneratedFile(t *testing.T) {
	plan := &builder.ResolvedBuildPlan{
		DeclaredIdentity:    builder.SourceIdentity{Type: "pypi", Package: "demo", Ref: "1.2.3"},
		ResolvedIdentity:    builder.SourceIdentity{Type: "pypi", Package: "demo", Version: "1.2.3", Artifact: "demo.whl"},
		Python:              "3.12",
		Command:             []string{"demo"},
		GeneratedDockerfile: "FROM python@sha256:abc\n",
		BuildInputDigest:    strings.Repeat("a", 64),
		ImageTag:            "gridctl-preview-demo:1.2.3-aaaaaaaaaaaa",
	}
	planner := &stubPythonSourcePlanner{plan: plan}
	srv := newTestServer(t)
	srv.pythonSources = planner
	body := `{"stackName":"preview","server":{"Name":"demo","Source":{"Type":"pypi","Ref":"1.2.3","Package":"demo"}}}`

	resolveRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resolveRec, loopbackRequest(http.MethodPost, "/api/python/resolve", strings.NewReader(body)))
	if resolveRec.Code != http.StatusOK || !strings.Contains(resolveRec.Body.String(), `"generatedFile"`) {
		t.Fatalf("resolve status = %d, body = %s", resolveRec.Code, resolveRec.Body.String())
	}
	if planner.lastOpts.Package != "demo" || planner.lastOpts.ServerName != "demo" {
		t.Fatalf("build options = %+v", planner.lastOpts)
	}

	fileRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(fileRec, loopbackRequest(http.MethodPost, "/api/python/generated-file", strings.NewReader(body)))
	if fileRec.Code != http.StatusOK || !strings.Contains(fileRec.Body.String(), "FROM python@sha256:abc") {
		t.Fatalf("generated file status = %d, body = %s", fileRec.Code, fileRec.Body.String())
	}
}

func TestHandleStatus_PythonContainerProvenance(t *testing.T) {
	stackFile := filepath.Join(t.TempDir(), "stack.yaml")
	stackYAML := "name: demo\nnetwork:\n  name: demo\nmcp-servers:\n  - name: fetch\n    source:\n      type: pypi\n      package: mcp-server-fetch\n      ref: 0.6.0\n"
	if err := os.WriteFile(stackFile, []byte(stackYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)
	srv.stackFile = stackFile
	srv.stackName = "demo"
	srv.gateway.Router().AddClient(newMockAgentClient("fetch", nil))
	registerMockServerMeta(srv.gateway, "fetch", mcp.TransportStdio)
	srv.dockerClient = &mockDockerClient{
		containers: []container.Summary{{
			Image:  "gridctl-demo-fetch:0.6.0-aaaaaaaaaaaa",
			Labels: map[string]string{runtimedocker.LabelManaged: "true", runtimedocker.LabelStack: "demo", runtimedocker.LabelMCPServer: "fetch"},
		}},
		images: []image.Summary{{Labels: map[string]string{
			builder.LabelSourcePackage: "mcp-server-fetch", builder.LabelSourceVersion: "0.6.0", builder.LabelSourceArtifact: "fetch.whl",
		}}},
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Servers []MCPServerStatus `json:"mcp-servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Servers) != 1 || response.Servers[0].Kind != "Python container" || response.Servers[0].LocalProcess {
		t.Fatalf("server status = %+v", response.Servers)
	}
	status := response.Servers[0]
	if status.Image != "gridctl-demo-fetch:0.6.0-aaaaaaaaaaaa" || status.Source == nil || status.Source.Artifact != "fetch.whl" {
		t.Fatalf("source status = %+v", status)
	}

	direct := httptest.NewRecorder()
	srv.Handler().ServeHTTP(direct, loopbackRequest(http.MethodGet, "/api/mcp-servers", nil))
	var directStatuses []MCPServerStatus
	if err := json.Unmarshal(direct.Body.Bytes(), &directStatuses); err != nil {
		t.Fatal(err)
	}
	if len(directStatuses) != 1 || directStatuses[0].Kind != status.Kind || directStatuses[0].Image != status.Image {
		t.Fatalf("direct MCP status = %+v, aggregate = %+v", directStatuses, status)
	}
}
