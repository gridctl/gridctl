package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/secreport"
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
