package secreport

import (
	"strings"
	"testing"
	"time"
)

func TestAssemble_UnknownWhenOptionalProducersMissing(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	report := Assemble(now, Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack: &StackView{
			Name: "demo",
			Servers: []ServerView{
				{Name: "fetch", Kind: "container", Image: "example/fetch:1"},
			},
			SetMembers: map[string][]string{},
			Gateway:    &GatewayDeclView{},
		},
	})
	if !report.GenerationOK {
		t.Fatal("generation should succeed")
	}
	if report.FailCount != 0 {
		t.Fatalf("fail_count=%d", report.FailCount)
	}
	if report.UnknownCount == 0 {
		t.Fatal("expected unknown checks for missing producers")
	}
	if report.Coverage.Status != CoveragePartial {
		t.Fatalf("coverage=%s", report.Coverage.Status)
	}
	if ExitCode(report) != 0 {
		t.Fatalf("exit=%d want 0 with unknowns only", ExitCode(report))
	}
}

func TestAssemble_PinDriftIsFailure(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack:  &StackView{Name: "demo", Servers: []ServerView{{Name: "fetch", Kind: "container", Image: "example/fetch:1"}}, Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}},
		Pins: &PinView{Available: true, Enabled: true, Servers: map[string]ServerPinView{
			"fetch": {Status: "drift", CurrentSchemeCount: 1},
		}},
	})
	if report.FailCount == 0 {
		t.Fatal("drift should fail")
	}
	if ExitCode(report) != 1 {
		t.Fatalf("exit=%d", ExitCode(report))
	}
}

func TestAssemble_MissingPinsAreNotFailures(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack:  &StackView{Name: "demo", Servers: []ServerView{{Name: "fetch", Kind: "container"}}, Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}},
		Pins:   &PinView{Available: true, Enabled: true, Servers: map[string]ServerPinView{}},
	})
	for _, c := range report.Checks {
		if c.Predicate == PredPinBaseline && c.Outcome == OutcomeFail {
			t.Fatalf("absent pins must not fail: %+v", c)
		}
	}
}

func TestAssemble_EmptyFindingsAreUnknownNotPass(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack:  &StackView{Name: "demo", Servers: []ServerView{{Name: "fetch", Kind: "container"}}, Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}},
		Pins: &PinView{Available: true, Enabled: true, Scan: &ScanDeclView{Enabled: true}, Servers: map[string]ServerPinView{
			"fetch": {Status: "pinned", CurrentSchemeCount: 1},
		}},
	})
	found := false
	for _, c := range report.Checks {
		if c.Predicate == PredPinScanFindings {
			found = true
			if c.Outcome != OutcomeUnknown {
				t.Fatalf("outcome=%s want unknown", c.Outcome)
			}
		}
	}
	if !found {
		t.Fatal("missing findings check")
	}
}

func TestAssemble_LegacySchemeWarns(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack:  &StackView{Name: "demo", Servers: []ServerView{{Name: "fetch", Kind: "container"}}, Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}},
		Pins: &PinView{Available: true, Enabled: true, Servers: map[string]ServerPinView{
			"fetch": {Status: "pinned", LegacySchemeCount: 2},
		}},
	})
	for _, c := range report.Checks {
		if c.Predicate == PredPinScheme && c.Outcome != OutcomeWarn {
			t.Fatalf("scheme outcome=%s", c.Outcome)
		}
	}
}

func TestAssemble_SuppressionDoesNotPass(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack:  &StackView{Name: "demo", Servers: []ServerView{{Name: "fetch", Kind: "container"}}, Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}},
		Pins: &PinView{Available: true, Enabled: true, Scan: &ScanDeclView{Enabled: true, Ignore: []string{"P004"}}, Servers: map[string]ServerPinView{
			"fetch": {Status: "pinned", CurrentSchemeCount: 1, Findings: []FindingView{
				{Code: "P004", Severity: "warn", Confidence: "low", Field: "description", Suppressed: true},
				{Code: "P001", Severity: "warn", Confidence: "high", Field: "description"},
			}},
		}},
	})
	for _, c := range report.Checks {
		if c.Predicate == PredPinScanFindings {
			if c.Outcome != OutcomeWarn {
				t.Fatalf("outcome=%s", c.Outcome)
			}
			if c.Suppression == nil || len(c.Suppression.Codes) == 0 {
				t.Fatal("expected suppression metadata")
			}
		}
	}
}

func TestAssemble_NilMembershipIsUnknown(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack: &StackView{
			Name:             "demo",
			SetMembers:       nil,
			UnscopedSetCount: 1,
			References:       map[string][]ReferenceSite{"TOKEN": {{Kind: "mcp-server", Name: "fetch"}}},
			Gateway:          &GatewayDeclView{},
			Servers:          []ServerView{{Name: "fetch", Kind: "container"}},
		},
	})
	for _, c := range report.Checks {
		if c.Predicate == PredVarCompleteness && c.Outcome != OutcomeUnknown {
			t.Fatalf("completeness=%s", c.Outcome)
		}
		if c.Predicate == PredVarReferences && !strings.Contains(c.Explanation, "reference") && c.ReasonCode != "references_partial" {
			t.Fatalf("unexpected references: %+v", c)
		}
	}
}

func TestAssemble_ExecutionFailureAndNonContainerNA(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}, Servers: []ServerView{
			{Name: "hardened", Kind: "container"},
			{Name: "remote", Kind: "external"},
		}},
		Execution: []ExecutionView{
			{Server: "hardened", Replica: "1", Mode: "hardened", Outcome: "failed", ObservedAt: time.Now().UTC(), Runtime: "docker-compatible"},
		},
	})
	var sawFail, sawNA bool
	for _, c := range report.Checks {
		if c.Predicate == PredExecutionEnforcement && c.Subject.Name == "hardened:1" && c.Outcome == OutcomeFail {
			sawFail = true
		}
		if c.Predicate == PredExecutionEnforcement && c.Subject.Name == "remote" && c.Outcome == OutcomeNotApplicable {
			sawNA = true
		}
	}
	if !sawFail || !sawNA {
		t.Fatalf("fail=%v na=%v", sawFail, sawNA)
	}
	if ExitCode(report) != 1 {
		t.Fatalf("exit=%d", ExitCode(report))
	}
}

func TestAssemble_DigestIsNotVerified(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}, Servers: []ServerView{
			{Name: "fetch", Kind: "container", BuildDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		}},
	})
	for _, c := range report.Checks {
		if c.Predicate == PredSourceBuildDigest {
			if c.Evidence.Basis == BasisVerified {
				t.Fatal("digest declaration must not become verified")
			}
			if c.Outcome != OutcomePass {
				t.Fatalf("outcome=%s", c.Outcome)
			}
		}
		if c.Predicate == PredSourceSignature && c.Outcome != OutcomeUnknown {
			t.Fatalf("signature outcome=%s", c.Outcome)
		}
	}
}

func TestAssemble_StartupDistinctFromDeclaration(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source:  SourceIdentity{Kind: SourceGateway, Display: "localhost:8180"},
		Stack:   &StackView{Name: "demo", Gateway: &GatewayDeclView{AuthDeclared: true, AuthType: "bearer"}, SetMembers: map[string][]string{}},
		Startup: &StartupView{AuthEnabled: true, AuthType: "bearer", Bind: "127.0.0.1"},
	})
	var declared, startup bool
	for _, c := range report.Checks {
		if c.Predicate == PredGatewayAuthDeclared && c.ReasonCode == "auth_declared" {
			declared = true
		}
		if c.Predicate == PredGatewayAuthStartup && c.ReasonCode == "startup_auth_enabled" {
			startup = true
			if strings.Contains(c.Explanation, "secret") {
				t.Fatal("token leaked")
			}
		}
	}
	if !declared || !startup {
		t.Fatalf("declared=%v startup=%v", declared, startup)
	}
}

func TestQuietSummary(t *testing.T) {
	report := &Report{Coverage: Coverage{Status: CoveragePartial}}
	if got := QuietSummary(report); !strings.Contains(got, "No failing checks") {
		t.Fatalf("got %q", got)
	}
}
