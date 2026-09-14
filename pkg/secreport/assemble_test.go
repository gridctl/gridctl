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

func TestAssemble_ScanDisabledKeepsHistoricalFindings(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack:  &StackView{Name: "demo", Servers: []ServerView{{Name: "fetch", Kind: "container"}}, Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}},
		Pins: &PinView{Available: true, Enabled: true, Scan: &ScanDeclView{Enabled: false, Ignore: []string{"P004"}}, Servers: map[string]ServerPinView{
			"fetch": {Status: "pinned", CurrentSchemeCount: 1, Findings: []FindingView{
				{Code: "P001", Severity: "warn", Confidence: "high", Field: "description"},
			}},
		}},
	})
	found := false
	for _, c := range report.Checks {
		if c.Predicate == PredPinScanFindings && c.Subject.Name == "fetch" {
			found = true
			if c.Outcome != OutcomeWarn {
				t.Fatalf("outcome=%s", c.Outcome)
			}
			if len(c.Facts.FindingCodes) == 0 || c.Facts.FindingCodes[0] != "P001" {
				t.Fatalf("facts=%+v", c.Facts)
			}
		}
	}
	if !found {
		t.Fatal("missing findings check")
	}
}

func TestAssemble_FullySuppressedFindingsRemain(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack:  &StackView{Name: "demo", Servers: []ServerView{{Name: "fetch", Kind: "container"}}, Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}},
		Pins: &PinView{Available: true, Enabled: true, Scan: &ScanDeclView{Enabled: true, Ignore: []string{"P004"}}, Servers: map[string]ServerPinView{
			"fetch": {Status: "pinned", CurrentSchemeCount: 1, Findings: []FindingView{
				{Code: "P004", Severity: "warn", Confidence: "low", Field: "description", Suppressed: true},
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
			if c.ReasonCode != "findings_present_suppressed" {
				t.Fatalf("reason=%s", c.ReasonCode)
			}
		}
	}
}

func TestAssemble_VariableCountsExcludeNetworks(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack: &StackView{
			Name:       "demo",
			SetMembers: map[string][]string{"keys": {"TOKEN"}},
			Servers:    []ServerView{{Name: "fetch", Kind: "container"}},
			Gateway:    &GatewayDeclView{},
			References: map[string][]ReferenceSite{
				"TOKEN": {
					{Kind: "mcp-server", Name: "fetch"},
					{Kind: "mcp-server", Name: "fetch"},
					{Kind: "resource", Name: "disk"},
					{Kind: "network", Name: "frontend"},
					{Kind: "secrets-set", Name: "keys", Target: "fetch", TargetKind: "mcp-server"},
					{Kind: "secrets-set", Name: "all", Untargeted: true},
				},
			},
		},
	})
	for _, c := range report.Checks {
		if c.Predicate == PredVarReferences {
			if c.Facts.ReferenceSites == nil || *c.Facts.ReferenceSites != 6 {
				t.Fatalf("sites=%v", c.Facts.ReferenceSites)
			}
			if c.Facts.WorkloadConsumers == nil || *c.Facts.WorkloadConsumers != 2 {
				t.Fatalf("workloads=%v want 2 (fetch and disk)", c.Facts.WorkloadConsumers)
			}
			if c.Facts.UnscopedConsumers == nil || *c.Facts.UnscopedConsumers != 1 {
				t.Fatalf("unscoped=%v", c.Facts.UnscopedConsumers)
			}
		}
	}
}

func TestAssemble_ExecutionInstanceAndLocalFailure(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"},
		Stack: &StackView{Name: "demo", Gateway: &GatewayDeclView{}, SetMembers: map[string][]string{}, Servers: []ServerView{
			{Name: "local", Kind: "local-process"},
		}},
		Execution: []ExecutionView{
			{Server: "local", Replica: "1", Kind: "local-process", Mode: "local", Outcome: "failed", Instance: "proc-1", Revision: "rev-9", ObservedAt: time.Now().UTC(), Runtime: "process"},
		},
	})
	found := false
	for _, c := range report.Checks {
		if c.Predicate == PredExecutionEnforcement && c.Subject.Name == "local:1" {
			found = true
			if c.Outcome != OutcomeFail {
				t.Fatalf("local failure should be fail, got %s", c.Outcome)
			}
			if c.Facts.Instance != "proc-1" || c.Facts.Revision != "rev-9" {
				t.Fatalf("facts=%+v", c.Facts)
			}
		}
	}
	if !found {
		t.Fatal("missing enforcement check")
	}
}
