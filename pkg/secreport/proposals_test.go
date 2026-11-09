package secreport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func proposalCheckOf(t *testing.T, report *Report) Check {
	t.Helper()
	for _, c := range report.Checks {
		if c.Predicate == PredProposalPending {
			return c
		}
	}
	t.Fatal("proposal.pending check missing")
	return Check{}
}

func TestProposalCheck_Outcomes(t *testing.T) {
	when := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		view    *ProposalView
		outcome string
		reason  string
		count   *int
	}{
		{"pending", &ProposalView{Available: true, Pending: 3}, OutcomePass, "proposals_pending", intPtr(3)},
		{"none", &ProposalView{Available: true}, OutcomePass, "no_pending_proposals", intPtr(0)},
		{"negative clamps", &ProposalView{Available: true, Pending: -2}, OutcomePass, "no_pending_proposals", intPtr(0)},
		{"unavailable", &ProposalView{}, OutcomeUnknown, "store_unavailable", nil},
		{"nil", nil, OutcomeUnknown, "store_unavailable", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := Assemble(when, Inputs{Source: SourceIdentity{Kind: SourceGateway, Display: "127.0.0.1:8180"}, Proposals: tc.view})
			c := proposalCheckOf(t, report)
			if c.ID != "proposal.pending.gateway" || c.Subject != gatewaySubject() {
				t.Fatalf("check identity = %s %+v", c.ID, c.Subject)
			}
			if c.Outcome != tc.outcome || c.ReasonCode != tc.reason {
				t.Fatalf("outcome = %s/%s, want %s/%s", c.Outcome, c.ReasonCode, tc.outcome, tc.reason)
			}
			switch {
			case tc.count == nil && c.Facts.PendingProposals != nil:
				t.Fatalf("pending_proposals = %d, want absent", *c.Facts.PendingProposals)
			case tc.count != nil && (c.Facts.PendingProposals == nil || *c.Facts.PendingProposals != *tc.count):
				t.Fatalf("pending_proposals = %v, want %d", c.Facts.PendingProposals, *tc.count)
			}
			if len(c.Limitations) != 1 || c.Limitations[0] != proposalLimitation || !knownLimitation(c.Limitations[0]) {
				t.Fatalf("limitations = %v", c.Limitations)
			}
			if report.FailCount != 0 {
				t.Fatalf("pending proposals affected fail_count: %d", report.FailCount)
			}
			if ExitCode(report) != 0 {
				t.Fatalf("exit code = %d", ExitCode(report))
			}
		})
	}
}

func TestProposalCheck_FileSourceUnknown(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{Source: SourceIdentity{Kind: SourceFile, Display: "stack.yaml"}})
	c := proposalCheckOf(t, report)
	if c.Outcome != OutcomeUnknown || c.ReasonCode != "store_unavailable" {
		t.Fatalf("file source check = %s/%s", c.Outcome, c.ReasonCode)
	}
}

func TestProposalCheck_NotAFailPredicate(t *testing.T) {
	if failPredicateAllowed(PredProposalPending) {
		t.Fatal("proposal.pending must not be a fail predicate")
	}
	if !knownPredicate(PredProposalPending) {
		t.Fatal("proposal.pending missing from the inventory")
	}
}

func TestProposalCheck_SnapshotRoundTripAndExplanation(t *testing.T) {
	when := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, n := range []int{0, 1, 4} {
		report := Assemble(when, Inputs{
			Source:    SourceIdentity{Kind: SourceGateway, Display: "127.0.0.1:8180"},
			Proposals: &ProposalView{Available: true, Pending: n},
		})
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 && !strings.Contains(string(data), `"pending_proposals":`) {
			t.Fatalf("pending_proposals missing from JSON: %s", data)
		}
		parsed, err := ParseSnapshot(data)
		if err != nil {
			t.Fatalf("import rejected snapshot with %d pending: %v", n, err)
		}
		c := proposalCheckOf(t, parsed)
		if c.Explanation == "" || c.Explanation == "Unrecognized claim." {
			t.Fatalf("explanation = %q", c.Explanation)
		}
		if n == 4 && c.Explanation != "4 pending proposals are held for review." {
			t.Fatalf("explanation = %q", c.Explanation)
		}
		if n == 1 && c.Explanation != "1 pending proposal is held for review." {
			t.Fatalf("explanation = %q", c.Explanation)
		}
		if c.Facts.PendingProposals == nil || *c.Facts.PendingProposals != n {
			t.Fatalf("round-trip pending_proposals = %v", c.Facts.PendingProposals)
		}
	}
}

func TestProposalCheck_ImportedFailRefused(t *testing.T) {
	report := Assemble(time.Now().UTC(), Inputs{
		Source:    SourceIdentity{Kind: SourceGateway, Display: "127.0.0.1:8180"},
		Proposals: &ProposalView{Available: true, Pending: 2},
	})
	for i := range report.Checks {
		if report.Checks[i].Predicate == PredProposalPending {
			report.Checks[i].Outcome = OutcomeFail
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSnapshot(data); err == nil {
		t.Fatal("snapshot with a failing proposal.pending check was accepted")
	}
}

func TestSanitizeFacts_PendingProposalsNonNegative(t *testing.T) {
	out := sanitizeFacts(CheckFacts{PendingProposals: intPtr(-5)})
	if out.PendingProposals == nil || *out.PendingProposals != 0 {
		t.Fatalf("sanitized = %v", out.PendingProposals)
	}
}
