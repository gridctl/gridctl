package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/secreport"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestHandleSecurityReport_Passive(t *testing.T) {
	srv := newTestServer(t)
	docker := &countingDocker{}
	srv.SetDockerClient(docker)
	srv.SetStackName("demo")
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte("version: \"1\"\nname: demo\nmcp-servers:\n  - name: fetch\n    image: example/fetch:1\n    command: [\"canary-command\"]\n    env:\n      TOKEN: canary-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.SetStackFile(path)
	store := vault.NewStore(t.TempDir())
	srv.SetVaultStore(store)
	ps := pins.NewWithPath(t.TempDir(), "demo")
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	srv.SetPinStore(ps)

	handler := srv.Handler()
	req := loopbackRequest(http.MethodGet, "/api/security-report", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if docker.calls != 0 {
		t.Fatalf("docker calls=%d", docker.calls)
	}
	body := rec.Body.String()
	for _, canary := range []string{"canary-command", "canary-secret"} {
		if strings.Contains(body, canary) {
			t.Fatalf("leaked %q", canary)
		}
	}
	var report secreport.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Source.Kind != secreport.SourceGateway {
		t.Fatalf("kind=%s", report.Source.Kind)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["ok"]; ok {
		t.Fatal("security JSON must not define ok")
	}
}

func TestHandleSecurityReport_AuthRequired(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAuth("bearer", "secret-token", "")
	handler := srv.Handler()
	req := loopbackRequest(http.MethodGet, "/api/security-report", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestHandleSecurityReport_MissingOptionalEvidence(t *testing.T) {
	srv := newTestServer(t)
	docker := &countingDocker{}
	srv.SetDockerClient(docker)
	handler := srv.Handler()
	req := loopbackRequest(http.MethodGet, "/api/security-report", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if docker.calls != 0 {
		t.Fatalf("docker calls=%d", docker.calls)
	}
	var report secreport.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.UnknownCount == 0 {
		t.Fatal("expected unknown checks")
	}
}

func TestHandleSecurityReport_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()
	req := loopbackRequest(http.MethodPost, "/api/security-report", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestIsProtectedPath_SecurityReport(t *testing.T) {
	if !isProtectedPath("/api/security-report") {
		t.Fatal("expected protected")
	}
}

type countingDocker struct {
	mockDockerClient
	calls int
}

func (c *countingDocker) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	c.calls++
	return nil, errors.New("container list called")
}

func (c *countingDocker) ImageList(context.Context, image.ListOptions) ([]image.Summary, error) {
	c.calls++
	return nil, errors.New("image list called")
}

func (c *countingDocker) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	c.calls++
	return container.InspectResponse{}, errors.New("inspect called")
}

func (c *countingDocker) ImagePull(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
	c.calls++
	return nil, errors.New("pull called")
}
