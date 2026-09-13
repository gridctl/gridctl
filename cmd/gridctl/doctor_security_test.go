package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/secreport"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestValidateDoctorSecurityFlags(t *testing.T) {
	t.Cleanup(func() { doctorSecurity, doctorSource = false, "" })
	doctorSecurity, doctorSource = false, "file:stack.yaml"
	if err := validateDoctorSecurityFlags(); err == nil {
		t.Fatal("expected rejection of --source without --security")
	}
	doctorSecurity, doctorSource = true, ""
	if err := validateDoctorSecurityFlags(); err == nil {
		t.Fatal("expected required source")
	}
	doctorSecurity, doctorSource = true, "file:stack.yaml"
	if err := validateDoctorSecurityFlags(); err != nil {
		t.Fatal(err)
	}
}

func TestRunSecurityDoctorFileMode(t *testing.T) {
	t.Cleanup(func() { doctorSecurity, doctorSource, securityHTTPDoer = false, "", nil })
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte("version: \"1\"\nname: demo\nmcp-servers:\n  - name: fetch\n    image: example/fetch:1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doctorSecurity, doctorSource = true, "file:"+path
	doer := &countingDoer{}
	securityHTTPDoer = doer
	report, err := runSecurityDoctor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if doer.n != 0 {
		t.Fatalf("file mode HTTP calls=%d", doer.n)
	}
	var buf bytes.Buffer
	if exit := renderSecurityReport(&buf, report, true, true); exit != doctorExitOK {
		t.Fatalf("exit=%d", exit)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["ok"]; ok {
		t.Fatal("security JSON must not define ok")
	}
	if _, ok := decoded["checks"]; !ok {
		t.Fatal("missing checks")
	}
	var text bytes.Buffer
	renderSecurityReport(&text, report, false, true)
	out := text.String()
	if !strings.Contains(out, "Coverage:") || !strings.Contains(out, "unknown") && !strings.Contains(out, "Source:") {
		t.Fatalf("quiet text missing source/coverage: %s", out)
	}
}

func TestOrdinaryDoctorJSONUnchanged(t *testing.T) {
	report := summarizeDoctor([]doctorCheck{{ID: "npx", Status: doctorStatusWarn, Message: "not found"}})
	var buf bytes.Buffer
	if exit := renderDoctorReport(&buf, report, true, false); exit != doctorExitOK {
		t.Fatalf("exit=%d", exit)
	}
	var decoded doctorReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK || decoded.WarningCount != 1 {
		t.Fatalf("%+v", decoded)
	}
}

func TestSecurityDoctorMessageHidesRawErrors(t *testing.T) {
	msg := securityDoctorMessage(secreport.ErrSourceUnreadable)
	if strings.Contains(msg, "yaml") || strings.Contains(strings.ToLower(msg), "parse") {
		t.Fatalf("msg=%q", msg)
	}
}

type countingDoer struct{ n int }

func (c *countingDoer) Do(*http.Request) (*http.Response, error) {
	c.n++
	return nil, secreport.ErrGatewayRequest
}

func TestLocalControlPlaneOrigin(t *testing.T) {
	if !localControlPlaneOrigin("http://localhost:8180") {
		t.Fatal("localhost")
	}
	if !localControlPlaneOrigin("http://127.0.0.1:8180") {
		t.Fatal("loopback")
	}
	if localControlPlaneOrigin("https://unrelated.example:8180") {
		t.Fatal("foreign host")
	}
	if localControlPlaneOrigin("http://example.com") {
		t.Fatal("example.com")
	}
	if localControlPlaneOrigin("not-a-url") {
		t.Fatal("invalid")
	}
}

func TestGatewaySecurityDoerDoesNotAttachForeignCredentials(t *testing.T) {
	t.Cleanup(func() { securityHTTPDoer = nil })
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	st := &state.DaemonState{StackName: "demo", Port: 8180, AuthToken: "canary-local-token", AuthType: "bearer"}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}
	securityHTTPDoer = nil
	if _, ok := gatewaySecurityDoer("https://unrelated.example:8180").(authorizedDoer); ok {
		t.Fatal("foreign host must not use authorized doer")
	}
	local, ok := gatewaySecurityDoer("http://127.0.0.1:8180").(authorizedDoer)
	if !ok {
		t.Fatal("loopback must use authorized doer")
	}
	lreq, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8180/api/security-report", nil)
	if err != nil {
		t.Fatal(err)
	}
	local.api.authorize(lreq)
	if lreq.Header.Get("Authorization") != "Bearer canary-local-token" {
		t.Fatalf("local auth=%q", lreq.Header.Get("Authorization"))
	}
}

func TestExecuteDoctorSecuritySkipsOrdinaryChecks(t *testing.T) {
	t.Cleanup(func() {
		doctorSecurity, doctorSource, doctorJSON, doctorQuiet = false, "", false, false
		runOrdinaryDoctorChecks = runDoctorChecks
	})
	runOrdinaryDoctorChecks = func(context.Context) doctorReport {
		t.Fatal("ordinary doctor checks must not run")
		return doctorReport{}
	}
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte("version: \"1\"\nname: demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doctorSecurity, doctorSource, doctorJSON = true, "file:"+path, true
	var buf bytes.Buffer
	if exit := executeDoctor(context.Background(), &buf); exit != doctorExitOK && exit != doctorExitErrors {
		t.Fatalf("exit=%d", exit)
	}
	if strings.Contains(buf.String(), `"ok"`) {
		t.Fatal("security JSON must not be ordinary doctor schema")
	}
}

func TestRenderSecurityReportWriteError(t *testing.T) {
	report := secreport.Assemble(time.Now().UTC(), secreport.Inputs{Source: secreport.SourceIdentity{Kind: secreport.SourceFile, Display: "x"}})
	if exit := renderSecurityReport(errWriter{}, report, false, false); exit != doctorExitFailed {
		t.Fatalf("text exit=%d", exit)
	}
	if exit := renderSecurityReport(errWriter{}, report, false, true); exit != doctorExitFailed {
		t.Fatalf("quiet exit=%d", exit)
	}
	if exit := renderSecurityReport(errWriter{}, report, true, false); exit != doctorExitFailed {
		t.Fatalf("json exit=%d", exit)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("sink closed") }
