package scenarioverify

import (
	"context"
	"io"
	"strings"
)

type VerifyOptions struct {
	Lane          string
	Revision      string
	GoStatus      int
	CaptureStatus int
	GoStatusSet   bool
}

type Report struct {
	Lane           string
	Revision       string
	GoStatus       int
	CaptureStatus  int
	VerifierStatus int
	Required       string
	LaneReason     string
	Scenarios      []ScenarioResult
	ObservedFail   bool
}

type ScenarioResult struct {
	ID       string
	Package  string
	Test     string
	Status   string
	Reason   string
	Boundary string
	Observed string
}

type testLife struct {
	ran       bool
	terminals []string
}

type pkgLife struct {
	terminal string
	tests    map[string]*testLife
}

func Verify(ctx context.Context, idx *Index, events io.Reader, opts VerifyOptions) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	required, err := idx.Required(opts.Lane)
	if err != nil {
		return nil, err
	}
	rep := &Report{
		Lane:          opts.Lane,
		Revision:      opts.Revision,
		GoStatus:      opts.GoStatus,
		CaptureStatus: opts.CaptureStatus,
		Required:      "FAIL",
	}

	decoded, err := DecodeEvents(events)
	if err != nil {
		rep.LaneReason = ReasonIncompleteEvidence
		rep.VerifierStatus = 1
		for _, sc := range required {
			rep.Scenarios = append(rep.Scenarios, failResult(sc, ReasonIncompleteEvidence, "capture could not be decoded through EOF"))
		}
		return rep, nil
	}
	if len(decoded) == 0 {
		rep.LaneReason = ReasonIncompleteEvidence
		rep.VerifierStatus = 1
		for _, sc := range required {
			rep.Scenarios = append(rep.Scenarios, failResult(sc, ReasonIncompleteEvidence, "capture was empty"))
		}
		return rep, nil
	}

	pkgs, captureReason, failed := collectLifecycles(decoded)
	if captureReason != "" {
		rep.LaneReason = captureReason
		rep.ObservedFail = failed
		rep.VerifierStatus = 1
		for _, sc := range required {
			rep.Scenarios = append(rep.Scenarios, failResult(sc, captureReason, "capture did not satisfy the single-count contract"))
		}
		return rep, nil
	}

	laneReason := ""
	if failed {
		laneReason = ReasonTestFailure
		rep.ObservedFail = true
	}
	for pkg, life := range pkgs {
		switch life.terminal {
		case "fail":
			laneReason = ReasonPackageFailure
			rep.ObservedFail = true
		case "skip":
			if packageHasRequired(pkg, required) && laneReason == "" {
				laneReason = ReasonPackageFailure
			}
		}
		for name, tl := range life.tests {
			if last(tl.terminals) == "fail" {
				if requiredName(pkg, name, required) {
					// per-scenario reason applied below
				} else if laneReason == "" {
					laneReason = ReasonTestFailure
				}
				rep.ObservedFail = true
			}
		}
	}

	allPass := true
	for _, sc := range required {
		result := evaluateScenario(sc, pkgs)
		if result.Status != "pass" {
			allPass = false
			if laneReason == "" {
				laneReason = result.Reason
			}
		}
		rep.Scenarios = append(rep.Scenarios, result)
	}

	if opts.CaptureStatus != 0 {
		allPass = false
		if laneReason == "" {
			laneReason = ReasonIncompleteEvidence
		}
	}
	if opts.GoStatusSet && opts.GoStatus != 0 {
		allPass = false
		if laneReason == "" {
			if failed {
				laneReason = ReasonTestFailure
			} else {
				laneReason = ReasonIncompleteEvidence
			}
		}
	}

	if allPass && laneReason == "" {
		rep.Required = "PASS"
		rep.VerifierStatus = 0
		return rep, nil
	}
	rep.Required = "FAIL"
	rep.LaneReason = laneReason
	if rep.LaneReason == "" {
		rep.LaneReason = ReasonIncompleteEvidence
	}
	rep.VerifierStatus = 1
	return rep, nil
}

func collectLifecycles(events []Event) (map[string]*pkgLife, string, bool) {
	pkgs := make(map[string]*pkgLife)
	failed := false
	for _, ev := range events {
		if ev.Package == "" {
			continue
		}
		pkg := pkgs[ev.Package]
		if pkg == nil {
			pkg = &pkgLife{tests: make(map[string]*testLife)}
			pkgs[ev.Package] = pkg
		}
		if ev.Test == "" {
			switch ev.Action {
			case "fail":
				if pkg.terminal != "" && pkg.terminal != ev.Action {
					return nil, ReasonIncompleteEvidence, true
				}
				if pkg.terminal == "fail" {
					return nil, ReasonIncompleteEvidence, true
				}
				pkg.terminal = "fail"
				failed = true
			case "pass", "skip":
				if pkg.terminal != "" {
					return nil, ReasonIncompleteEvidence, true
				}
				pkg.terminal = ev.Action
			}
			continue
		}
		tl := pkg.tests[ev.Test]
		if tl == nil {
			tl = &testLife{}
			pkg.tests[ev.Test] = tl
		}
		switch ev.Action {
		case "run":
			if tl.ran || len(tl.terminals) > 0 {
				return nil, ReasonIncompleteEvidence, failed
			}
			tl.ran = true
		case "pause", "cont", "output", "attr", "start":
		case "pass", "fail", "skip":
			if !tl.ran {
				return nil, ReasonIncompleteEvidence, failed
			}
			if len(tl.terminals) > 0 {
				return nil, ReasonIncompleteEvidence, true
			}
			tl.terminals = append(tl.terminals, ev.Action)
			if ev.Action == "fail" {
				failed = true
			}
		}
	}
	return pkgs, "", failed
}

func evaluateScenario(sc Scenario, pkgs map[string]*pkgLife) ScenarioResult {
	pkg := pkgs[sc.Package]
	if pkg == nil {
		return failResult(sc, ReasonSelectorAbsent, "required package/test run and pass not observed")
	}
	if pkg.terminal == "fail" {
		return failResult(sc, ReasonPackageFailure, "containing package failed")
	}
	if pkg.terminal == "" {
		return failResult(sc, ReasonIncompleteEvidence, "containing package did not report a terminal result")
	}
	if pkg.terminal != "pass" {
		return failResult(sc, ReasonPackageFailure, "containing package did not pass")
	}

	for _, name := range ancestorNames(sc.Test) {
		tl := pkg.tests[name]
		if tl == nil || !tl.ran {
			if name == sc.Test {
				return failResult(sc, ReasonSelectorAbsent, "required package/test run and pass not observed")
			}
			return failResult(sc, ReasonIncompleteEvidence, "required ancestor "+name+" did not run")
		}
		term := last(tl.terminals)
		if term == "" {
			return failResult(sc, ReasonIncompleteEvidence, name+" ran without a terminal result")
		}
		if term == "skip" {
			if name == sc.Test {
				return failResult(sc, ReasonRequiredSkip, "required case skipped")
			}
			return failResult(sc, ReasonRequiredSkip, "required ancestor "+name+" skipped")
		}
		if term == "fail" {
			return failResult(sc, ReasonTestFailure, name+" failed")
		}
		if term != "pass" {
			return failResult(sc, ReasonIncompleteEvidence, name+" ended with "+term)
		}
	}
	return ScenarioResult{
		ID:       sc.ID,
		Package:  sc.Package,
		Test:     sc.Test,
		Status:   "pass",
		Boundary: sc.ExpectedBoundary,
		Observed: "required package/test run and pass observed",
	}
}

func ancestorNames(test string) []string {
	parts := strings.Split(test, "/")
	names := make([]string, 0, len(parts))
	cur := ""
	for _, part := range parts {
		if cur == "" {
			cur = part
		} else {
			cur += "/" + part
		}
		names = append(names, cur)
	}
	return names
}

func failResult(sc Scenario, reason, observed string) ScenarioResult {
	return ScenarioResult{
		ID:       sc.ID,
		Package:  sc.Package,
		Test:     sc.Test,
		Status:   "fail",
		Reason:   reason,
		Boundary: sc.ExpectedBoundary,
		Observed: observed,
	}
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func packageHasRequired(pkg string, required []Scenario) bool {
	for _, sc := range required {
		if sc.Package == pkg {
			return true
		}
	}
	return false
}

func requiredName(pkg, name string, required []Scenario) bool {
	for _, sc := range required {
		if sc.Package == pkg && sc.Test == name {
			return true
		}
		if sc.Package == pkg {
			for _, anc := range ancestorNames(sc.Test) {
				if anc == name {
					return true
				}
			}
		}
	}
	return false
}

func (r *Report) Failed() bool {
	return r.VerifierStatus != 0 || r.Required != "PASS"
}
