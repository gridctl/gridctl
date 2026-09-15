package scenarioverify

import (
	"context"
	"strings"
	"testing"
)

func sampleIndex(t *testing.T, testName string) *Index {
	t.Helper()
	idx, err := ParseIndex(context.Background(), []byte(`
version: 1
lanes: [unit]
scenarios:
  - id: required-child
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: example.com/mcp
    test: `+testName+`
    lanes: [unit]
    expected_boundary: denied before dispatch
`))
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func verifyString(t *testing.T, idx *Index, events string, opts VerifyOptions) *Report {
	t.Helper()
	if opts.Lane == "" {
		opts.Lane = "unit"
	}
	rep, err := Verify(context.Background(), idx, strings.NewReader(events), opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestVerify_ExactPassInterleaved(t *testing.T) {
	idx := sampleIndex(t, "TestFoo/bar")
	events := `
{"Action":"run","Package":"example.com/other","Test":"TestNoise"}
{"Action":"pass","Package":"example.com/other","Test":"TestNoise"}
{"Action":"pass","Package":"example.com/other"}
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pause","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo/bar"}
{"Action":"cont","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo/bar"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
	if rep.Failed() || rep.Scenarios[0].Status != "pass" {
		t.Fatalf("expected pass, got %+v", rep)
	}
}

func TestVerify_ParentPassChildSkip(t *testing.T) {
	idx := sampleIndex(t, "TestFoo/bar")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo/bar"}
{"Action":"skip","Package":"example.com/mcp","Test":"TestFoo/bar"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if !rep.Failed() || rep.Scenarios[0].Reason != ReasonRequiredSkip {
		t.Fatalf("expected REQUIRED_SKIP, got %+v", rep.Scenarios[0])
	}
}

func TestVerify_AbsentSelector(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestOther"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestOther"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if rep.Scenarios[0].Reason != ReasonSelectorAbsent {
		t.Fatalf("reason = %q", rep.Scenarios[0].Reason)
	}
}

func TestVerify_WrongPackage(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"run","Package":"example.com/other","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/other","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/other"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if rep.Scenarios[0].Reason != ReasonSelectorAbsent {
		t.Fatalf("wrong package credited: %+v", rep.Scenarios[0])
	}
}

func TestVerify_SiblingDoesNotSatisfy(t *testing.T) {
	idx := sampleIndex(t, "TestFoo/required")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo/sibling"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo/sibling"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if rep.Scenarios[0].Reason != ReasonSelectorAbsent {
		t.Fatalf("sibling credited: %+v", rep.Scenarios[0])
	}
}

func TestVerify_PackageFailureAfterChildPass(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"fail","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if !rep.Failed() || rep.Scenarios[0].Reason != ReasonPackageFailure {
		t.Fatalf("package cleanup failure not reported: %+v", rep.Scenarios[0])
	}
}

func TestVerify_UnrelatedFailureInvalidatesLane(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
{"Action":"run","Package":"example.com/other","Test":"TestNoise"}
{"Action":"fail","Package":"example.com/other","Test":"TestNoise"}
{"Action":"fail","Package":"example.com/other"}
`
	rep := verifyString(t, idx, events, VerifyOptions{GoStatus: 1, GoStatusSet: true})
	if !rep.Failed() || !rep.ObservedFail {
		t.Fatalf("unrelated failure was ignored: %+v", rep)
	}
	if rep.Scenarios[0].Status != "pass" {
		t.Fatalf("required case should still be pass, got %+v", rep.Scenarios[0])
	}
}

func TestVerify_MalformedAndTruncated(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	rep := verifyString(t, idx, `{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"`, VerifyOptions{})
	if rep.LaneReason != ReasonIncompleteEvidence {
		t.Fatalf("truncated stream reason = %q", rep.LaneReason)
	}
	rep = verifyString(t, idx, "", VerifyOptions{})
	if rep.LaneReason != ReasonIncompleteEvidence {
		t.Fatalf("empty stream reason = %q", rep.LaneReason)
	}
}

func TestVerify_RepeatedAndConflicting(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	repeated := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, repeated, VerifyOptions{})
	if rep.LaneReason != ReasonIncompleteEvidence {
		t.Fatalf("repeated execution reason = %q", rep.LaneReason)
	}
	conflict := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"fail","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep = verifyString(t, idx, conflict, VerifyOptions{})
	if rep.LaneReason != ReasonIncompleteEvidence {
		t.Fatalf("conflicting terminals reason = %q", rep.LaneReason)
	}
}

func TestVerify_PermittedUnrelatedSkip(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
{"Action":"run","Package":"example.com/other","Test":"TestOptional"}
{"Action":"skip","Package":"example.com/other","Test":"TestOptional"}
{"Action":"pass","Package":"example.com/other"}
`
	rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
	if rep.Failed() {
		t.Fatalf("unrelated skip failed the lane: %+v", rep)
	}
}

func TestVerify_OutputIsNotEvidence(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"output","Package":"example.com/mcp","Test":"TestFoo","Output":"PASS: TestFoo\n"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if rep.Scenarios[0].Reason != ReasonSelectorAbsent {
		t.Fatalf("output string was treated as execution: %+v", rep.Scenarios[0])
	}
}

func TestVerify_IncompleteRun(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if rep.Scenarios[0].Reason != ReasonIncompleteEvidence {
		t.Fatalf("unfinished lifecycle reason = %q", rep.Scenarios[0].Reason)
	}
}

func TestVerify_UnknownJSONFieldsAllowed(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo","Extra":{"k":1}}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo","Elapsed":0.01}
{"Action":"pass","Package":"example.com/mcp","Elapsed":0.02}
`
	rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
	if rep.Failed() {
		t.Fatalf("unknown fields broke decoding: %+v", rep)
	}
}

func TestVerify_PrefixPassDoesNotSatisfyChild(t *testing.T) {
	idx := sampleIndex(t, "TestFoo/bar")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{})
	if rep.Scenarios[0].Reason != ReasonSelectorAbsent {
		t.Fatalf("parent pass substituted for child: %+v", rep.Scenarios[0])
	}
}

func TestFocusedCommandEscapes(t *testing.T) {
	cmd := FocusedCommand(Scenario{
		Package: "github.com/gridctl/gridctl/pkg/mcp",
		Test:    "TestFoo/name.with+meta",
	})
	if !strings.Contains(cmd, `^TestFoo$/^name\.with\+meta$`) {
		t.Fatalf("command = %q", cmd)
	}
	if !strings.Contains(cmd, "./pkg/mcp") {
		t.Fatalf("package dir missing: %q", cmd)
	}
}
