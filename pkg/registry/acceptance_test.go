package registry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunAcceptance_NoCriteria(t *testing.T) {
	skill := &AgentSkill{Name: "empty"}
	_, err := RunAcceptance(context.Background(), skill, DeterministicEvaluator{}, NewRunOptions())
	if !errors.Is(err, ErrNoCriteria) {
		t.Fatalf("expected ErrNoCriteria, got %v", err)
	}
}

func TestRunAcceptance_NilSkill(t *testing.T) {
	_, err := RunAcceptance(context.Background(), nil, DeterministicEvaluator{}, NewRunOptions())
	if err == nil {
		t.Fatal("expected error on nil skill")
	}
}

func TestRunAcceptance_CriterionOutOfRange(t *testing.T) {
	skill := &AgentSkill{
		Name:               "one-criterion",
		AcceptanceCriteria: []string{"PASS: something"},
	}
	opts := NewRunOptions()
	opts.CriterionIndex = 5
	_, err := RunAcceptance(context.Background(), skill, DeterministicEvaluator{}, opts)
	if !errors.Is(err, ErrCriterionOutOfRange) {
		t.Fatalf("expected ErrCriterionOutOfRange, got %v", err)
	}
}

func TestRunAcceptance_DeterministicHappyPath(t *testing.T) {
	skill := &AgentSkill{
		Name: "fixture",
		AcceptanceCriteria: []string{
			"PASS: given valid input, when run, then return success",
			"FAIL: given missing config, when run, then error",
			"PASS: idempotent re-runs do not double-write",
		},
	}
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	opts := NewRunOptions()
	opts.Now = now

	report, err := RunAcceptance(context.Background(), skill, DeterministicEvaluator{}, opts)
	if err != nil {
		t.Fatalf("RunAcceptance: %v", err)
	}
	if report.SkillName != "fixture" {
		t.Errorf("SkillName = %q, want fixture", report.SkillName)
	}
	if report.Evaluator != "deterministic" {
		t.Errorf("Evaluator = %q, want deterministic", report.Evaluator)
	}
	if !report.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt = %v, want %v", report.GeneratedAt, now)
	}
	if report.PassCount != 2 || report.FailCount != 1 || report.ErrorCount != 0 {
		t.Errorf("counts pass=%d fail=%d err=%d, want 2/1/0", report.PassCount, report.FailCount, report.ErrorCount)
	}
	if !report.HasFailures() {
		t.Error("HasFailures should be true")
	}
	if report.HasErrors() {
		t.Error("HasErrors should be false")
	}
	if len(report.Results) != 3 {
		t.Fatalf("Results len = %d, want 3", len(report.Results))
	}
	if report.Results[0].Severity != TestSeverityPass {
		t.Errorf("Results[0].Severity = %q, want pass", report.Results[0].Severity)
	}
	if report.Results[0].Index != 0 {
		t.Errorf("Results[0].Index = %d, want 0", report.Results[0].Index)
	}
	if report.Results[1].Severity != TestSeverityFail {
		t.Errorf("Results[1].Severity = %q, want fail", report.Results[1].Severity)
	}
	if report.Results[1].Index != 1 {
		t.Errorf("Results[1].Index = %d, want 1", report.Results[1].Index)
	}
}

func TestRunAcceptance_DeterministicMissingMarker(t *testing.T) {
	skill := &AgentSkill{
		Name:               "ambiguous",
		AcceptanceCriteria: []string{"Given X, when Y, then Z"},
	}
	report, err := RunAcceptance(context.Background(), skill, DeterministicEvaluator{}, NewRunOptions())
	if err != nil {
		t.Fatalf("RunAcceptance: %v", err)
	}
	if report.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", report.ErrorCount)
	}
	if report.Results[0].Severity != TestSeverityError {
		t.Errorf("Severity = %q, want error", report.Results[0].Severity)
	}
}

func TestRunAcceptance_DryRun(t *testing.T) {
	skill := &AgentSkill{
		Name: "fixture",
		AcceptanceCriteria: []string{
			"PASS: c1",
			"FAIL: c2",
		},
	}
	opts := NewRunOptions()
	opts.DryRun = true
	report, err := RunAcceptance(context.Background(), skill, DeterministicEvaluator{}, opts)
	if err != nil {
		t.Fatalf("RunAcceptance: %v", err)
	}
	if !report.DryRun {
		t.Error("DryRun should be true")
	}
	if report.Evaluator != "dry-run" {
		t.Errorf("Evaluator = %q, want dry-run", report.Evaluator)
	}
	if report.PassCount != 0 || report.FailCount != 0 || report.ErrorCount != 0 {
		t.Errorf("dry-run should not tally counts, got pass=%d fail=%d err=%d",
			report.PassCount, report.FailCount, report.ErrorCount)
	}
	if len(report.Results) != 2 {
		t.Fatalf("Results len = %d, want 2", len(report.Results))
	}
	for i, r := range report.Results {
		if r.Severity != "" {
			t.Errorf("Results[%d].Severity = %q, want empty", i, r.Severity)
		}
	}
}

func TestRunAcceptance_ScopedToOneCriterion(t *testing.T) {
	skill := &AgentSkill{
		Name: "fixture",
		AcceptanceCriteria: []string{
			"PASS: c0",
			"PASS: c1",
			"FAIL: c2",
		},
	}
	opts := NewRunOptions()
	opts.CriterionIndex = 2
	report, err := RunAcceptance(context.Background(), skill, DeterministicEvaluator{}, opts)
	if err != nil {
		t.Fatalf("RunAcceptance: %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("Results len = %d, want 1", len(report.Results))
	}
	if report.Results[0].Index != 2 {
		t.Errorf("Results[0].Index = %d, want 2", report.Results[0].Index)
	}
	if report.Results[0].Severity != TestSeverityFail {
		t.Errorf("Results[0].Severity = %q, want fail", report.Results[0].Severity)
	}
}

func TestRunAcceptance_NilEvaluator(t *testing.T) {
	skill := &AgentSkill{
		Name:               "fixture",
		AcceptanceCriteria: []string{"PASS: c0"},
	}
	report, err := RunAcceptance(context.Background(), skill, nil, NewRunOptions())
	if err != nil {
		t.Fatalf("RunAcceptance: %v", err)
	}
	if report.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", report.ErrorCount)
	}
	if report.Results[0].Severity != TestSeverityError {
		t.Errorf("Severity = %q, want error", report.Results[0].Severity)
	}
}

