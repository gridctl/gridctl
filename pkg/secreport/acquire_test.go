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
	"syscall"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skillpins"
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

func TestParseSnapshotRejectsInvalid(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	valid := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.GeneratedAt.IsZero() {
		t.Fatal("generated_at required")
	}

	missingTime := []byte(`{"schema_version":"gridctl.security-report.v1","source":{"kind":"file","display":"x"},"coverage":{"status":"none","predicates_total":0,"predicates_evaluated":0,"predicates_unknown":0,"included_scopes":[],"excluded_scopes":[],"unknown_gaps":0},"checks":[],"limitations":[],"fail_count":0,"warn_count":0,"unknown_count":0,"not_applicable_count":0,"pass_count":0}`)
	if _, err := ParseSnapshot(missingTime); !errors.Is(err, ErrSourceInvalidFmt) {
		t.Fatalf("missing generated_at err=%v", err)
	}

	dup := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	if len(dup.Checks) < 2 {
		t.Fatal("need checks")
	}
	dup.Checks[1].ID = dup.Checks[0].ID
	dupRaw, _ := json.Marshal(dup)
	if _, err := ParseSnapshot(dupRaw); !errors.Is(err, ErrSourceInvalidFmt) {
		t.Fatalf("duplicate id err=%v", err)
	}

	invalidEnum := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	invalidEnum.Checks[0].Outcome = "excellent"
	enumRaw, _ := json.Marshal(invalidEnum)
	if _, err := ParseSnapshot(enumRaw); !errors.Is(err, ErrSourceInvalidFmt) {
		t.Fatalf("invalid outcome err=%v", err)
	}

	badFail := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	for i := range badFail.Checks {
		if badFail.Checks[i].Predicate == PredVarCompleteness {
			badFail.Checks[i].Outcome = OutcomeFail
			break
		}
	}
	failRaw, _ := json.Marshal(badFail)
	if _, err := ParseSnapshot(failRaw); !errors.Is(err, ErrSourceInvalidFmt) {
		t.Fatalf("invalid fail predicate err=%v", err)
	}

	malformed := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	malformed.Checks[0].Evidence.Basis = BasisVerified
	malformed.Checks[0].Evidence.VerificationMethod = ""
	malformed.Checks[0].Evidence.SubjectBinding = ""
	malformed.Checks[0].Evidence.PredicateScope = ""
	malRaw, _ := json.Marshal(malformed)
	if _, err := ParseSnapshot(malRaw); !errors.Is(err, ErrSourceInvalidFmt) {
		t.Fatalf("malformed verified err=%v", err)
	}
}

func TestParseSnapshotSparseIsPartial(t *testing.T) {
	payload := []byte(`{"schema_version":"gridctl.security-report.v1","generated_at":"2026-09-13T00:00:00Z","source":{"kind":"file","display":"x"},"coverage":{"status":"complete","predicates_total":19,"predicates_evaluated":1,"predicates_unknown":0,"included_scopes":[],"excluded_scopes":[],"unknown_gaps":0},"checks":[{"id":"pin.schema.store.gateway","predicate":"pin.schema.store","subject":{"kind":"gateway","name":"gateway"},"outcome":"pass","reason_code":"store_available","explanation":"x","evidence":{"basis":"declared","availability":"available","freshness":"unknown"}}],"limitations":[],"fail_count":0,"warn_count":0,"unknown_count":0,"not_applicable_count":0,"pass_count":1}`)
	report, err := ParseSnapshot(payload)
	if err != nil {
		t.Fatal(err)
	}
	if report.Coverage.Status != CoveragePartial && report.Coverage.Status != CoverageNone {
		t.Fatalf("coverage=%s", report.Coverage.Status)
	}
	if len(report.Coverage.ExcludedScopes) == 0 {
		t.Fatal("expected excluded scopes for sparse snapshot")
	}
}

func TestImportedVerifiedIsDemoted(t *testing.T) {
	payload := []byte(`{"schema_version":"gridctl.security-report.v1","generated_at":"2026-09-13T00:00:00Z","source":{"kind":"snapshot","display":"x","historical":true},"coverage":{"status":"partial","predicates_total":19,"predicates_evaluated":1,"predicates_unknown":0,"included_scopes":[],"excluded_scopes":[],"unknown_gaps":0},"checks":[{"id":"source.signature.fetch","predicate":"source.signature","subject":{"kind":"server","name":"fetch"},"outcome":"pass","reason_code":"signature_bound","explanation":"trusted","evidence":{"basis":"verified","availability":"available","freshness":"current","verification_method":"bound_producer","subject_binding":"fetch","predicate_scope":"source.signature"}}],"limitations":[],"fail_count":0,"warn_count":0,"unknown_count":0,"not_applicable_count":0,"pass_count":1}`)
	report, err := ParseSnapshot(payload)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks[0].Evidence.Basis == BasisVerified {
		t.Fatal("imported snapshot must not keep verified")
	}
	if report.Checks[0].Evidence.Freshness == FreshnessCurrent {
		t.Fatal("imported snapshot must not keep freshness current")
	}
	if strings.Contains(report.Checks[0].Explanation, "bound trusted producer") {
		t.Fatalf("explanation=%s", report.Checks[0].Explanation)
	}
}

func TestExpansionOperandsOmittedFromSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	body := "version: \"1\"\nname: demo\nmcp-servers:\n  - name: demo\n    image: \"${IMAGE:-canary-default-operand}\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Load(context.Background(), SourceRef{Kind: SourceFile, Value: path}, &countingDoer{})
	if err != nil {
		t.Fatal(err)
	}
	raw := reportString(report)
	if strings.Contains(raw, "canary-default-operand") {
		t.Fatal("default operand leaked")
	}
}

func TestSnapshotFreeTextLimitationsDropped(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	report := Assemble(now, Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	report.Checks[0].Limitations = append(report.Checks[0].Limitations, "canary-private-free-text")
	report.Checks[0].ReasonCode = "findings_present"
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	raw := reportString(parsed)
	if strings.Contains(raw, "canary-private-free-text") {
		t.Fatal("free text limitation retained")
	}
}

func TestSnapshotRoundTripPreservesFacts(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	original := Assemble(now, Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack: &StackView{
			Name: "demo", Gateway: &GatewayDeclView{AuthDeclared: true, AuthType: "bearer", Bind: "127.0.0.1"}, SetMembers: map[string][]string{},
			Servers:    []ServerView{{Name: "demo", Kind: "container", Image: "example/demo:1"}},
			References: map[string][]ReferenceSite{"TOKEN": {{Kind: "mcp-server", Name: "demo"}, {Kind: "mcp-server", Name: "demo"}}},
		},
	})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	var origSrc, parsedSrc string
	var origSites, parsedSites int
	for _, c := range original.Checks {
		if c.Predicate == PredSourceDeclared && c.Facts.DeclaredSource != "" {
			origSrc = c.Facts.DeclaredSource
		}
		if c.Predicate == PredVarReferences && c.Facts.ReferenceSites != nil {
			origSites = *c.Facts.ReferenceSites
		}
	}
	for _, c := range parsed.Checks {
		if c.Predicate == PredSourceDeclared {
			parsedSrc = c.Facts.DeclaredSource
			if !strings.Contains(c.Explanation, "example/demo:1") {
				t.Fatalf("explanation lost source identity: %s", c.Explanation)
			}
		}
		if c.Predicate == PredVarReferences && c.Facts.ReferenceSites != nil {
			parsedSites = *c.Facts.ReferenceSites
			if !strings.Contains(c.Explanation, "Counted") {
				t.Fatalf("explanation lost counts: %s", c.Explanation)
			}
		}
	}
	if origSrc != parsedSrc || origSrc == "" {
		t.Fatalf("source facts orig=%q parsed=%q", origSrc, parsedSrc)
	}
	if origSites != parsedSites || origSites == 0 {
		t.Fatalf("sites orig=%d parsed=%d", origSites, parsedSites)
	}
}

func TestLoadSnapshotBoundsAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	oversize := filepath.Join(dir, "big.json")
	if err := os.WriteFile(oversize, []byte(strings.Repeat("a", maxSnapshotBytes+2)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSnapshot(context.Background(), oversize); !errors.Is(err, ErrSourceInvalidFmt) {
		t.Fatalf("oversize err=%v", err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSnapshot(context.Background(), fifo); !errors.Is(err, ErrSourceUnreadable) {
		t.Fatalf("fifo err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadSnapshot(ctx, filepath.Join(dir, "missing.json")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err=%v", err)
	}
}

func TestPinViewFromStorePreservesFindingMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.json")
	body := `{
  "version": "2",
  "stack": "demo",
  "created_at": "2026-01-01T00:00:00Z",
  "servers": {
    "fetch": {
      "server_hash": "h2:abc",
      "pinned_at": "2026-01-01T00:00:00Z",
      "last_verified_at": "2026-01-02T00:00:00Z",
      "tool_count": 1,
      "status": "pinned",
      "tools": {
        "get": {
          "hash": "h2:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          "name": "get",
          "pinned_at": "2026-01-01T00:00:00Z",
          "findings": [
            {"code": "P001", "severity": "warn", "confidence": "high", "field": "description", "message": "canary-finding-message", "snippet": "canary-snippet"}
          ]
        }
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ps := pins.NewWithPath(dir, "demo")
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	view := PinViewFromStore(ps, true, &ScanDeclView{Enabled: true})
	if view == nil || view.Servers["fetch"].Findings[0].Code != "P001" {
		t.Fatalf("%+v", view)
	}
	if view.Servers["fetch"].Findings[0].Severity != "warn" || view.Servers["fetch"].Findings[0].Confidence != "high" {
		t.Fatal("expected severity and confidence")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "canary-snippet") || strings.Contains(string(raw), "canary-finding-message") {
		t.Fatal("snippet leaked")
	}
}

func TestSkillPinViewFromStorePreservesStatus(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "version": "1",
  "stack": "demo",
  "created_at": "2026-01-01T00:00:00Z",
  "skills": {
    "notes": {
      "skill_hash": "abc",
      "source": "git",
      "pinned_at": "2026-01-01T00:00:00Z",
      "last_verified_at": "2026-01-02T00:00:00Z",
      "status": "drift",
      "findings": [{"code": "P001", "severity": "warn", "confidence": "low", "field": "body", "message": "canary-skill-message"}]
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "demo.skills.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ps := skillpins.NewWithPath(dir, "demo")
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	view := SkillPinViewFromStore(ps)
	if view == nil || view.Skills["notes"].Status != "drift" {
		t.Fatalf("%+v", view)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "canary-skill-message") {
		t.Fatal("skill finding message leaked")
	}
}

func TestWriteTextPropagatesErrors(t *testing.T) {
	report := Assemble(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC), Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "x"}, Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}}})
	if err := WriteText(errWriter{}, report, false); err == nil {
		t.Fatal("expected text write error")
	}
	if err := WriteText(errWriter{}, report, true); err == nil {
		t.Fatal("expected quiet text write error")
	}
	if err := WriteJSON(errWriter{}, report); err == nil {
		t.Fatal("expected json write error")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("sink closed") }
