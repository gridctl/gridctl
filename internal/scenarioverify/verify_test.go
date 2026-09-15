package scenarioverify

import (
	"context"
	"strconv"
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
    test: `+strconv.Quote(testName)+`
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
	}, "unit")
	if !strings.Contains(cmd, `^TestFoo$/^name\.with\+meta$`) {
		t.Fatalf("command = %q", cmd)
	}
	if !strings.Contains(cmd, "./pkg/mcp") {
		t.Fatalf("package dir missing: %q", cmd)
	}
	if strings.Contains(cmd, "-tags=integration") || strings.Contains(cmd, "GRIDCTL_RUNTIME") {
		t.Fatalf("unit command included another lane: %q", cmd)
	}

	integ := FocusedCommand(Scenario{
		Package: "github.com/gridctl/gridctl/tests/integration",
		Test:    "TestHTTPTransportConnect",
	}, "integration")
	if !strings.Contains(integ, "-tags=integration") || !strings.Contains(integ, "-timeout 15m") {
		t.Fatalf("integration command = %q", integ)
	}
	if !strings.Contains(integ, "./tests/integration") {
		t.Fatalf("integration package dir missing: %q", integ)
	}

	podman := FocusedCommand(Scenario{
		Package: "github.com/gridctl/gridctl/tests/integration",
		Test:    "TestPodmanRootless_MultiContainerNetworking",
	}, "podman-integration")
	if !strings.HasPrefix(podman, "GRIDCTL_RUNTIME=podman ") {
		t.Fatalf("podman command = %q", podman)
	}
	if !strings.Contains(podman, "-tags=integration") || !strings.Contains(podman, "-timeout 15m") {
		t.Fatalf("podman command missing suite flags: %q", podman)
	}
}

func TestVerify_RejectsInvalidAndIncompleteCapture(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	required := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`

	t.Run("unfinished unrelated package", func(t *testing.T) {
		events := required + `{"Action":"start","Package":"example.com/other"}` + "\n"
		events += `{"Action":"run","Package":"example.com/other","Test":"TestNoise"}` + "\n"
		rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
		if !rep.Failed() || rep.LaneReason != ReasonIncompleteEvidence {
			t.Fatalf("unfinished unrelated package accepted: %+v", rep)
		}
	})

	t.Run("build-fail", func(t *testing.T) {
		events := required + `{"Action":"build-fail","ImportPath":"example.com/broken"}` + "\n"
		rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
		if !rep.Failed() || !rep.ObservedFail || rep.LaneReason != ReasonTestFailure {
			t.Fatalf("build-fail accepted: %+v", rep)
		}
	})

	t.Run("null event", func(t *testing.T) {
		events := required + "null\n"
		rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
		if !rep.Failed() || rep.LaneReason != ReasonIncompleteEvidence {
			t.Fatalf("null event accepted: %+v", rep)
		}
	})

	t.Run("test after package complete", func(t *testing.T) {
		events := required + `{"Action":"run","Package":"example.com/mcp","Test":"TestLate"}` + "\n"
		rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
		if !rep.Failed() || rep.LaneReason != ReasonIncompleteEvidence {
			t.Fatalf("test after package complete accepted: %+v", rep)
		}
	})

	t.Run("parent pass before child run", func(t *testing.T) {
		childIdx := sampleIndex(t, "TestFoo/bar")
		events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo/bar"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo/bar"}
{"Action":"pass","Package":"example.com/mcp"}
`
		rep := verifyString(t, childIdx, events, VerifyOptions{GoStatusSet: true})
		if !rep.Failed() || rep.LaneReason != ReasonIncompleteEvidence {
			t.Fatalf("parent-before-child accepted: %+v", rep)
		}
	})

	t.Run("missing action package", func(t *testing.T) {
		events := `{"Action":"run","Test":"TestFoo"}` + "\n" + required
		rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
		if !rep.Failed() || rep.LaneReason != ReasonIncompleteEvidence {
			t.Fatalf("event without package accepted: %+v", rep)
		}
	})
}

func TestVerify_ChildSkipFailsWhenParentPasses(t *testing.T) {
	idx := sampleIndex(t, "TestAuth/{name}")
	events := `
{"Action":"run","Package":"example.com/mcp","Test":"TestAuth"}
{"Action":"run","Package":"example.com/mcp","Test":"TestAuth/{name}"}
{"Action":"skip","Package":"example.com/mcp","Test":"TestAuth/{name}"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestAuth"}
{"Action":"pass","Package":"example.com/mcp"}
`
	rep := verifyString(t, idx, events, VerifyOptions{GoStatusSet: true})
	if !rep.Failed() || rep.Scenarios[0].Reason != ReasonRequiredSkip {
		t.Fatalf("skipped designated child was accepted: %+v", rep.Scenarios[0])
	}
}

func TestVerify_DecoderBounds(t *testing.T) {
	idx := sampleIndex(t, "TestFoo")
	required := `
{"Action":"run","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp","Test":"TestFoo"}
{"Action":"pass","Package":"example.com/mcp"}
`
	t.Run("oversize record", func(t *testing.T) {
		pad := strings.Repeat("x", 64)
		events := `{"Action":"output","Package":"example.com/noise","Output":"` + pad + `"}` + "\n" + required
		rep := verifyString(t, idx, events, VerifyOptions{
			GoStatusSet: true,
			Limits:      DecodeLimits{MaxRecordBytes: 32},
		})
		if !rep.Failed() || rep.LaneReason != ReasonIncompleteEvidence {
			t.Fatalf("oversize record accepted: %+v", rep)
		}
	})
	t.Run("event volume", func(t *testing.T) {
		events := required + `{"Action":"pass","Package":"example.com/other"}` + "\n"
		rep := verifyString(t, idx, events, VerifyOptions{
			GoStatusSet: true,
			Limits:      DecodeLimits{MaxEvents: 3},
		})
		if !rep.Failed() || rep.LaneReason != ReasonIncompleteEvidence {
			t.Fatalf("event volume accepted: %+v", rep)
		}
	})
}
