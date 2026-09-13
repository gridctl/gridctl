package secreport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSource(t *testing.T) {
	ref, err := ParseSource("file:stack.yaml")
	if err != nil || ref.Kind != SourceFile || ref.Value != "stack.yaml" {
		t.Fatalf("%+v %v", ref, err)
	}
	ref, err = ParseSource("gateway:http://localhost:8180")
	if err != nil || ref.Kind != SourceGateway || ref.Value != "http://localhost:8180" {
		t.Fatalf("%+v %v", ref, err)
	}
	if _, err := ParseSource(""); !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("err=%v", err)
	}
	if _, err := ParseSource("other:x"); !errors.Is(err, ErrSourceUnsupported) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFileOffline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte("version: \"1\"\nname: demo\nmcp-servers:\n  - name: fetch\n    image: example/fetch:1\n    env:\n      TOKEN: canary-secret-value\n    command: [\"leak-command\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doer := &countingDoer{}
	report, err := Load(context.Background(), SourceRef{Kind: SourceFile, Value: path}, doer)
	if err != nil {
		t.Fatal(err)
	}
	if doer.n != 0 {
		t.Fatalf("file mode issued %d HTTP calls", doer.n)
	}
	raw := reportString(report)
	for _, canary := range []string{"canary-secret-value", "leak-command", "TOKEN:"} {
		if strings.Contains(raw, canary) {
			t.Fatalf("leaked %q in %s", canary, raw)
		}
	}
}

func TestLoadFileUnreadable(t *testing.T) {
	_, err := InputsFromStackFile(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, ErrSourceUnreadable) {
		t.Fatalf("err=%v", err)
	}
}

func TestSnapshotUnknownFieldsRejected(t *testing.T) {
	payload := []byte(`{"schema_version":"gridctl.security-report.v1","generated_at":"2026-09-13T00:00:00Z","source":{"kind":"file","display":"x"},"coverage":{"status":"none","predicates_total":0,"predicates_evaluated":0,"predicates_unknown":0,"included_scopes":[],"excluded_scopes":[],"unknown_gaps":0},"checks":[],"limitations":[],"fail_count":0,"warn_count":0,"unknown_count":0,"not_applicable_count":0,"pass_count":0,"evil":"canary-unknown-field"}`)
	if _, err := ParseSnapshot(payload); !errors.Is(err, ErrSourceInvalidFmt) {
		t.Fatalf("err=%v", err)
	}
}

func TestSnapshotPreservesGeneratedAt(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	original := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.GeneratedAt.Equal(now) {
		t.Fatalf("generated_at=%s", parsed.GeneratedAt)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSnapshot(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Source.Historical {
		t.Fatal("snapshot must be historical")
	}
	if loaded.FailCount != original.FailCount {
		t.Fatalf("fail_count=%d want %d", loaded.FailCount, original.FailCount)
	}
}

func TestGatewayRejectsRedirectAndUserinfo(t *testing.T) {
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/steal", http.StatusFound)
	}))
	t.Cleanup(redir.Close)
	client := &http.Client{CheckRedirect: RedirectCheck}
	_, err := FetchGatewayReport(context.Background(), redir.URL, client)
	if err == nil {
		t.Fatal("expected redirect failure")
	}
	if _, err := gatewayReportURL("http://user:pass@localhost:8180"); !errors.Is(err, ErrCredentialURL) {
		t.Fatalf("err=%v", err)
	}
	if _, err := gatewayReportURL("http://localhost:8180/?token=secret"); !errors.Is(err, ErrSourceInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestGatewayFetchesFixedPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		report := Assemble(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC), Inputs{Source: SourceIdentity{Kind: SourceGateway, Display: "localhost"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
		_ = json.NewEncoder(w).Encode(report)
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{CheckRedirect: RedirectCheck}
	report, err := FetchGatewayReport(context.Background(), srv.URL, client)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/security-report" {
		t.Fatalf("path=%s", gotPath)
	}
	if report.Source.Kind != SourceGateway {
		t.Fatalf("kind=%s", report.Source.Kind)
	}
}

func TestLoadSnapshotOffline(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	original := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snap.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	doer := &countingDoer{}
	if _, err := Load(context.Background(), SourceRef{Kind: SourceSnapshot, Value: path}, doer); err != nil {
		t.Fatal(err)
	}
	if doer.n != 0 {
		t.Fatalf("snapshot mode issued %d HTTP calls", doer.n)
	}
}

func TestSanitizeIdentifierStripsMarkup(t *testing.T) {
	got := SanitizeIdentifier("<script>alert(1)</script>fetch")
	if strings.Contains(got, "<") || strings.Contains(got, "script") && strings.Contains(got, "alert") {
		if strings.Contains(got, "<script>") {
			t.Fatalf("got %q", got)
		}
	}
	if action := viewPinsAction("fetch/../evil"); action != nil {
		t.Fatalf("path traversal action: %+v", action)
	}
	if !strings.HasPrefix(viewPinsAction("fetch").Path, "/pins?server=") {
		t.Fatal("expected pins path")
	}
}

type countingDoer struct{ n int }

func (c *countingDoer) Do(*http.Request) (*http.Response, error) {
	c.n++
	return nil, errors.New("http called")
}

func reportString(report *Report) string {
	var b strings.Builder
	_ = WriteJSON(&b, report)
	return b.String()
}
