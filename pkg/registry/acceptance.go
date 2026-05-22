// Package registry — acceptance criteria runner.
//
// A skill's `acceptance_criteria` frontmatter is a list of Given/When/Then
// prose strings. The runner evaluates each criterion against the skill's
// body and frontmatter, returning a TestReport that callers can render.
//
// The DeterministicEvaluator ships in-tree: it looks for "PASS:" or
// "FAIL:" markers inside each criterion so fixture skills can encode
// expected outcomes without standing up an LLM provider. Production
// evaluation against a live model was previously provided by the
// LLMEvaluator; that path moved out when the agent runtime was
// retired.
//
// The runner is read-only — it never mutates the skill or its files.
package registry

import (
	"context"
	"errors"
	"strings"
	"time"
)

// TestSeverity classifies a single criterion's outcome. The vocabulary
// mirrors pkg/optimize.Severity so renderers can share badge styling.
type TestSeverity string

const (
	// TestSeverityPass means the evaluator verified the criterion is
	// satisfied by the skill as written.
	TestSeverityPass TestSeverity = "pass"

	// TestSeverityFail means the evaluator believes the criterion is
	// NOT satisfied. This is the case the runner reports back to CI.
	TestSeverityFail TestSeverity = "fail"

	// TestSeverityError means evaluation could not complete (LLM
	// unavailable, malformed criterion, etc.). Mapped to exit code 2
	// by the CLI so infrastructure failures never look like a real
	// fail verdict.
	TestSeverityError TestSeverity = "error"
)

// TestResult is the verdict for one criterion. Mirrors optimize.Finding's
// shape so the JSON envelope feels familiar.
type TestResult struct {
	// Index is the criterion's position in the skill's
	// AcceptanceCriteria slice (zero-based). Stable across runs.
	Index int `json:"index"`

	// Criterion is the verbatim Given/When/Then string from the
	// skill's frontmatter.
	Criterion string `json:"criterion"`

	// Severity classifies the outcome.
	Severity TestSeverity `json:"severity"`

	// Message is a short human-readable rationale. Required for fail
	// and error; optional for pass.
	Message string `json:"message,omitempty"`

	// EvaluatedAt is the wall-clock time the verdict was rendered.
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// TestReport is the runner's full output for one skill.
type TestReport struct {
	// SkillName echoes the skill the runner ran against.
	SkillName string `json:"skill_name"`

	// Results is the per-criterion verdicts in criterion order.
	Results []TestResult `json:"results"`

	// PassCount, FailCount, ErrorCount summarize Results so callers
	// don't re-tally.
	PassCount  int `json:"pass_count"`
	FailCount  int `json:"fail_count"`
	ErrorCount int `json:"error_count"`

	// Evaluator names the backend that ran the criteria (e.g. "llm",
	// "deterministic"). Surfaced in JSON output so CI logs make the
	// provenance explicit.
	Evaluator string `json:"evaluator"`

	// DryRun is true when Results was populated by listing criteria
	// without executing them. Each TestResult in that case carries
	// Severity = "" so renderers can show "—" instead of a verdict.
	DryRun bool `json:"dry_run,omitempty"`

	// GeneratedAt is the wall-clock time the report was assembled.
	GeneratedAt time.Time `json:"generated_at"`
}

// HasFailures reports whether any criterion failed. The CLI uses this
// to map to exit code 1. Error-severity results are separate and map
// to exit code 2 in the CLI; here we only flag fail.
func (r TestReport) HasFailures() bool {
	return r.FailCount > 0
}

// HasErrors reports whether any criterion errored during evaluation.
// The CLI uses this to map to exit code 2.
func (r TestReport) HasErrors() bool {
	return r.ErrorCount > 0
}

// Evaluator runs a single acceptance criterion against a skill and
// returns the verdict. Implementations MUST honor ctx cancellation.
type Evaluator interface {
	// Name identifies the evaluator backend for the TestReport.
	Name() string

	// Evaluate produces a verdict for criterion idx against skill.
	// Implementations MUST NOT return both a TestResult and an error;
	// transient infrastructure failures should be returned via a
	// TestResult with Severity = TestSeverityError so the report
	// remains complete for the user.
	Evaluate(ctx context.Context, skill *AgentSkill, idx int, criterion string) TestResult
}

// RunOptions tunes a single Run pass.
type RunOptions struct {
	// CriterionIndex, when non-negative, scopes the run to a single
	// criterion. -1 (the zero value via NewRunOptions) means "all".
	CriterionIndex int

	// DryRun lists criteria without evaluating them. Severity on each
	// result is empty.
	DryRun bool

	// Now overrides the wall-clock used for EvaluatedAt and
	// GeneratedAt. Tests inject a fixed time; production callers
	// leave this zero so the runner defaults to time.Now().
	Now time.Time
}

// NewRunOptions returns RunOptions with CriterionIndex set to -1, the
// "run every criterion" sentinel. Use this instead of a literal zero
// value so future-added fields stay backward-compatible.
func NewRunOptions() RunOptions {
	return RunOptions{CriterionIndex: -1}
}

// ErrNoCriteria signals that the skill has no acceptance_criteria
// frontmatter. The runner returns this distinct error so callers can
// emit a clear "nothing to test" message and exit cleanly rather than
// reporting zero failures (which is ambiguous: did the skill pass, or
// was there nothing to check?).
var ErrNoCriteria = errors.New("skill has no acceptance_criteria")

// ErrCriterionOutOfRange is returned when RunOptions.CriterionIndex
// names a criterion that does not exist on the skill. The CLI maps
// this to exit code 2.
var ErrCriterionOutOfRange = errors.New("criterion index out of range")

// RunAcceptance evaluates the skill's acceptance_criteria using the
// supplied evaluator and returns a populated TestReport.
//
// Returns:
//   - ErrNoCriteria when the skill has no acceptance_criteria.
//   - ErrCriterionOutOfRange when opts.CriterionIndex is set and
//     points outside the skill's criteria slice.
//
// Per-criterion infrastructure failures are reported as TestResults
// with Severity = TestSeverityError; only the two errors above stop
// the whole run.
func RunAcceptance(ctx context.Context, skill *AgentSkill, ev Evaluator, opts RunOptions) (TestReport, error) {
	if skill == nil {
		return TestReport{}, errors.New("registry: skill is nil")
	}
	if len(skill.AcceptanceCriteria) == 0 {
		return TestReport{}, ErrNoCriteria
	}
	if opts.CriterionIndex >= len(skill.AcceptanceCriteria) ||
		(opts.CriterionIndex < 0 && opts.CriterionIndex != -1) {
		return TestReport{}, ErrCriterionOutOfRange
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	report := TestReport{
		SkillName:   skill.Name,
		GeneratedAt: now,
	}
	if ev != nil {
		report.Evaluator = ev.Name()
	}
	if opts.DryRun {
		report.DryRun = true
		report.Evaluator = "dry-run"
	}

	for idx, criterion := range skill.AcceptanceCriteria {
		if opts.CriterionIndex >= 0 && idx != opts.CriterionIndex {
			continue
		}
		if opts.DryRun {
			report.Results = append(report.Results, TestResult{
				Index:       idx,
				Criterion:   criterion,
				EvaluatedAt: now,
			})
			continue
		}
		if ev == nil {
			report.Results = append(report.Results, TestResult{
				Index:       idx,
				Criterion:   criterion,
				Severity:    TestSeverityError,
				Message:     "no evaluator configured",
				EvaluatedAt: now,
			})
			report.ErrorCount++
			continue
		}
		result := ev.Evaluate(ctx, skill, idx, criterion)
		if result.EvaluatedAt.IsZero() {
			result.EvaluatedAt = now
		}
		// Trust evaluator-supplied index but fall back to the loop
		// index when the evaluator left it zero on a non-zero
		// criterion — defensive, since the evaluator should set it.
		if result.Index == 0 && idx != 0 {
			result.Index = idx
		}
		report.Results = append(report.Results, result)
		switch result.Severity {
		case TestSeverityPass:
			report.PassCount++
		case TestSeverityFail:
			report.FailCount++
		case TestSeverityError:
			report.ErrorCount++
		}
	}
	return report, nil
}

// DeterministicEvaluator is the zero-LLM evaluator used by CI and
// unit tests. It inspects each criterion for the case-sensitive prefix
// markers "PASS:" or "FAIL:" and produces the corresponding verdict;
// criteria without a marker return TestSeverityError so test authors
// know the fixture needs to be explicit.
//
// This adapter exists so the acceptance contract can be exercised end
// to end (CLI → API → evaluator) without standing up an LLM provider.
type DeterministicEvaluator struct{}

// Name returns "deterministic".
func (DeterministicEvaluator) Name() string { return "deterministic" }

// Evaluate parses the marker convention. Markers are checked
// case-sensitively to avoid false positives in natural-language
// criteria like "should pass validation" — only explicit "PASS:" or
// "FAIL:" at the start of the trimmed criterion fires.
func (DeterministicEvaluator) Evaluate(_ context.Context, _ *AgentSkill, idx int, criterion string) TestResult {
	trimmed := strings.TrimSpace(criterion)
	switch {
	case strings.HasPrefix(trimmed, "PASS:"):
		return TestResult{
			Index:     idx,
			Criterion: criterion,
			Severity:  TestSeverityPass,
			Message:   strings.TrimSpace(strings.TrimPrefix(trimmed, "PASS:")),
		}
	case strings.HasPrefix(trimmed, "FAIL:"):
		return TestResult{
			Index:     idx,
			Criterion: criterion,
			Severity:  TestSeverityFail,
			Message:   strings.TrimSpace(strings.TrimPrefix(trimmed, "FAIL:")),
		}
	default:
		return TestResult{
			Index:     idx,
			Criterion: criterion,
			Severity:  TestSeverityError,
			Message:   "deterministic evaluator requires 'PASS:' or 'FAIL:' prefix; configure an LLM provider for prose criteria",
		}
	}
}

