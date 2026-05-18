// Package registry — acceptance criteria runner.
//
// A skill's `acceptance_criteria` frontmatter is a list of Given/When/Then
// prose strings. The runner evaluates each criterion against the skill's
// body and frontmatter, returning a TestReport that callers (CLI, API,
// Web UI) render directly.
//
// Two evaluators ship:
//
//   - LLMEvaluator: hands the skill + criterion to an agent.ChatModel
//     and asks for a structured pass/fail verdict. Production path; the
//     prose contract stays free-form.
//   - DeterministicEvaluator: a no-LLM adapter for CI and unit tests.
//     It looks for "PASS:" or "FAIL:" markers inside each criterion so
//     fixture skills can encode expected outcomes without standing up an
//     LLM provider.
//
// The runner is read-only — it never mutates the skill or its files.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/agent"
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

// LLMEvaluator is the production evaluator. It asks an agent.ChatModel
// to render a verdict on each criterion against the skill's body, then
// parses a strict JSON envelope from the model's response.
//
// The prompt instructs the judge to emit exactly one JSON object with
// `verdict` (pass|fail) and `rationale` (one sentence). Anything else
// is treated as TestSeverityError so the user sees the model misbehaved
// rather than guessing whether absence-of-marker meant pass or fail.
type LLMEvaluator struct {
	// Model is the canonical model ID handed to ChatRequest.Model.
	// Falls back to defaultJudgeModel when empty.
	Model string

	// Provider is the gridctl LLM provider. Required; constructing
	// the evaluator without one and calling Evaluate produces
	// TestSeverityError on every criterion.
	Provider agent.ChatModel

	// MaxTokens caps the judge's output. Defaults to 256 — verdicts
	// fit comfortably in a few sentences.
	MaxTokens int
}

const defaultJudgeModel = "claude-haiku-4-5-20251001"

// Name returns "llm".
func (LLMEvaluator) Name() string { return "llm" }

// Evaluate prompts the judge and parses its verdict. Errors at any
// stage become TestSeverityError so the report stays complete.
func (e LLMEvaluator) Evaluate(ctx context.Context, skill *AgentSkill, idx int, criterion string) TestResult {
	out := TestResult{Index: idx, Criterion: criterion}
	if e.Provider == nil {
		out.Severity = TestSeverityError
		out.Message = "no LLM provider configured (set ANTHROPIC_API_KEY in the vault and restart)"
		return out
	}

	model := e.Model
	if model == "" {
		model = defaultJudgeModel
	}
	maxTokens := e.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}

	prompt := buildJudgePrompt(skill, criterion)
	req := agent.ChatRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    judgeSystemPrompt,
		Messages:  []agent.Message{{Role: agent.RoleUser, Content: prompt}},
	}

	resp, err := e.Provider.Generate(ctx, req)
	if err != nil {
		out.Severity = TestSeverityError
		out.Message = "LLM judge: " + err.Error()
		return out
	}
	verdict, rationale, parseErr := parseJudgeResponse(resp.Content)
	if parseErr != nil {
		out.Severity = TestSeverityError
		out.Message = "LLM judge returned unparseable response: " + parseErr.Error()
		return out
	}
	out.Severity = verdict
	out.Message = rationale
	return out
}

const judgeSystemPrompt = `You are an acceptance-criteria judge for an Agent Skills file.

You will be given:
1. The skill's name and body (the markdown a model would read at runtime).
2. A single Given/When/Then-style criterion the skill is supposed to satisfy.

Your job is to decide whether the skill's behavior, as documented in its body, satisfies the criterion.

Reply with EXACTLY one JSON object on a single line, no surrounding prose, no markdown fence:

{"verdict":"pass","rationale":"one short sentence"}

verdict MUST be "pass" or "fail". rationale MUST be one sentence under 200 characters explaining the verdict.`

func buildJudgePrompt(skill *AgentSkill, criterion string) string {
	var b strings.Builder
	b.WriteString("Skill name: ")
	b.WriteString(skill.Name)
	b.WriteString("\n\nSkill description: ")
	b.WriteString(skill.Description)
	b.WriteString("\n\nSkill body:\n---\n")
	b.WriteString(skill.Body)
	b.WriteString("\n---\n\nCriterion to evaluate:\n")
	b.WriteString(criterion)
	b.WriteString("\n\nReturn the JSON envelope now.")
	return b.String()
}

// parseJudgeResponse extracts the first JSON object from raw and
// validates it. Models often emit prefatory whitespace or, despite the
// system prompt, a leading fence; we scan for the first '{' rather
// than rejecting on framing noise.
func parseJudgeResponse(raw string) (TestSeverity, string, error) {
	trimmed := strings.TrimSpace(raw)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end < 0 || end <= start {
		return TestSeverityError, "", fmt.Errorf("no JSON object found in response: %q", clip(trimmed))
	}
	payload := trimmed[start : end+1]
	var env struct {
		Verdict   string `json:"verdict"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return TestSeverityError, "", fmt.Errorf("invalid JSON: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(env.Verdict)) {
	case "pass":
		return TestSeverityPass, env.Rationale, nil
	case "fail":
		return TestSeverityFail, env.Rationale, nil
	default:
		return TestSeverityError, "", fmt.Errorf("unknown verdict %q", env.Verdict)
	}
}

// clip caps a string at 120 characters so error messages stay readable
// when the model emits a long blob.
func clip(s string) string {
	const max = 120
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
