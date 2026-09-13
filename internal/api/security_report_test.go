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
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/gridctl/gridctl/pkg/execution"
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

func TestHandleSecurityReport_FailedRegistration(t *testing.T) {
	srv := newTestServer(t)
	docker := &countingDocker{}
	srv.SetDockerClient(docker)
	srv.gateway.RecordExecutionFailure("hardened", &execution.Report{
		Mode: "hardened", Outcome: "failed", Instance: "ctr-1", Revision: "rev-a",
		ObservedAt: time.Now().UTC(), Runtime: "docker-compatible",
	}, errors.New("replacement failed"))
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
	found := false
	for _, c := range report.Checks {
		if c.Predicate == secreport.PredExecutionEnforcement && strings.Contains(c.Subject.Name, "hardened") {
			found = true
			if c.Outcome != secreport.OutcomeFail {
				t.Fatalf("outcome=%s", c.Outcome)
			}
			if c.Facts.Instance != "ctr-1" || c.Facts.Revision != "rev-a" {
				t.Fatalf("facts=%+v", c.Facts)
			}
		}
	}
	if !found {
		t.Fatal("expected failed-registration enforcement check")
	}
}

func TestHandleSecurityReport_PrimaryInputErrorAndNoMutation(t *testing.T) {
	srv := newTestServer(t)
	docker := &countingDocker{}
	srv.SetDockerClient(docker)
	srv.SetStackFile(filepath.Join(t.TempDir(), "missing.yaml"))
	dir := t.TempDir()
	ps := pins.NewWithPath(dir, "demo")
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	srv.SetPinStore(ps)
	pinPath := filepath.Join(dir, "demo.json")
	if err := os.WriteFile(pinPath, []byte(`{"version":"2","stack":"demo","servers":{}}`), 0o400); err != nil {
		t.Fatal(err)
	}
	store := vault.NewStore(t.TempDir())
	srv.SetVaultStore(store)
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
	src, err := os.ReadFile("security_report.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, forbidden := range []string{"VerifyOrPin", "GetSetSecrets", "vaultStore", "ImagePull", "ScanTool"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("security report path references %s", forbidden)
		}
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
