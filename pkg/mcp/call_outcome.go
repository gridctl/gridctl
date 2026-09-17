package mcp

import (
	"context"
	"errors"

	"github.com/gridctl/gridctl/pkg/runs"
)

// Completion values describe whether a downstream tool was observed to run.
// These are gateway observations, not exactly-once guarantees.
const (
	CompletionComplete      = "complete"
	CompletionNotStarted    = "not_started"
	CompletionInputRequired = "input_required"
	CompletionUnknown       = "unknown"
)

// DetailExecutionAdmission is the fixed typed detail when the execution
// wrapper refuses admission before forwarding a call.
const DetailExecutionAdmission = "execution_admission"

// CallOutcome is the request-local typed result of one dispatch decision.
// It is assigned at the same points as run-attempt outcomes and does not
// depend on recorder availability.
type CallOutcome struct {
	Disposition string `json:"disposition"`
	Stage       string `json:"stage"`
	Reason      string `json:"reason"`
	Completion  string `json:"completion"`
	Gate        string `json:"gate,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

type dispatchOptions struct {
	CanonicalOnly bool
}

type callState struct {
	attempt RunAttempt
	outcome CallOutcome
}

func (s *callState) set(disposition, stage, reason, completion string) {
	s.attempt.SetOutcome(disposition, stage, reason)
	s.outcome.Disposition = disposition
	s.outcome.Stage = stage
	s.outcome.Reason = reason
	s.outcome.Completion = completion
}

func (s *callState) setGate(name string) {
	s.outcome.Gate = name
}

func (s *callState) setDetail(detail string) {
	s.outcome.Detail = detail
}

func classifyCall(result *ToolCallResult, callErr error, ctx context.Context) CallOutcome {
	if callErr != nil {
		out := CallOutcome{
			Disposition: runs.DispositionTransportError,
			Stage:       runs.StageDownstream,
			Reason:      runs.ReasonTransportError,
			Completion:  CompletionUnknown,
		}
		switch ctx.Err() {
		case context.Canceled:
			out.Disposition = runs.DispositionCancelled
			out.Reason = runs.ReasonContextCanceled
		case context.DeadlineExceeded:
			out.Disposition = runs.DispositionTimeout
			out.Reason = runs.ReasonDeadlineExceeded
		}
		var admit *ExecutionAdmissionError
		if errors.As(callErr, &admit) {
			out.Detail = DetailExecutionAdmission
			if ctx.Err() == nil {
				out.Completion = CompletionNotStarted
			}
		}
		return out
	}
	if result != nil && (result.RequestState != "" || result.ResultType == ResultTypeInputRequired) {
		return CallOutcome{
			Disposition: runs.DispositionInputRequired,
			Stage:       runs.StageDownstream,
			Reason:      runs.ReasonInputRequired,
			Completion:  CompletionInputRequired,
		}
	}
	if result != nil && result.IsError {
		return CallOutcome{
			Disposition: runs.DispositionToolError,
			Stage:       runs.StageDownstream,
			Reason:      runs.ReasonToolError,
			Completion:  CompletionComplete,
		}
	}
	return CallOutcome{
		Disposition: runs.DispositionCompleted,
		Stage:       runs.StageDownstream,
		Reason:      runs.ReasonOK,
		Completion:  CompletionComplete,
	}
}

func (s *callState) classifyDownstream(result *ToolCallResult, callErr error, ctx context.Context) {
	out := classifyCall(result, callErr, ctx)
	classifyDownstream(s.attempt, result, callErr, ctx)
	s.outcome.Disposition = out.Disposition
	s.outcome.Stage = out.Stage
	s.outcome.Reason = out.Reason
	s.outcome.Completion = out.Completion
	if out.Detail != "" {
		s.setDetail(out.Detail)
	}
}
