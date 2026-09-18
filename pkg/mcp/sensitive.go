package mcp

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/token"
	"go.opentelemetry.io/otel/trace"
)

type sensitiveExecutionKey struct{}

type diagnosticCallError struct {
	message string
	cause   error
}

func (e diagnosticCallError) Error() string { return e.message }
func (e diagnosticCallError) Unwrap() error { return e.cause }

func sanitizedCallError(err error) error {
	message := logging.RedactString(err.Error())
	if message == err.Error() {
		return err
	}
	var cause error
	if errors.Is(err, context.Canceled) {
		cause = context.Canceled
	} else if errors.Is(err, context.DeadlineExceeded) {
		cause = context.DeadlineExceeded
	}
	return diagnosticCallError{message: message, cause: cause}
}

func withSensitiveExecution(ctx context.Context) context.Context {
	if _, ok := ctx.Value(sensitiveExecutionKey{}).(*atomic.Bool); ok {
		return ctx
	}
	return context.WithValue(ctx, sensitiveExecutionKey{}, &atomic.Bool{})
}

func markSensitiveExecution(ctx context.Context) {
	if flag, ok := ctx.Value(sensitiveExecutionKey{}).(*atomic.Bool); ok {
		flag.Store(true)
	}
}

func isSensitiveExecution(ctx context.Context) bool {
	flag, ok := ctx.Value(sensitiveExecutionKey{}).(*atomic.Bool)
	return ok && flag.Load()
}

// safeCallError preserves cancellation identity without retaining an untrusted
// error chain, URL, parser excerpt, or body in instrumentation.
func safeCallError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		return context.Canceled
	default:
		for _, category := range []error{
			errCapabilityUnavailable, errCapabilityCapacity, errCapabilityInProgress,
			errCapabilitySuperseded, errCapabilityUncertain, errCapabilityConflict, errCapabilityRandom,
		} {
			if errors.Is(err, category) {
				return category
			}
		}
		return errors.New("sensitive_call_failed")
	}
}

func sensitiveObservation(server string, replica int, tool string, duration time.Duration, args map[string]any, result *ToolCallResult) SensitiveToolCallObservation {
	operation := "skill"
	switch tool {
	case "send", "task_get", "task_cancel":
		operation = tool
	}
	// Never use the configured counter here: it may send content to an API.
	// This local counter does not retain text and reports approximate usage.
	counter := token.NewHeuristicCounter(4)
	obs := SensitiveToolCallObservation{
		ServerName: logging.RedactString(server), ReplicaID: replica,
		Operation: operation, Duration: duration,
		Usage:  ToolCallSummary{InputTokens: token.CountJSON(counter, args)},
		Failed: result == nil || result.IsError,
	}
	if result != nil {
		for _, content := range result.Content {
			obs.Usage.OutputTokens += counter.Count(content.Text)
		}
	}
	return obs
}

func (g *Gateway) observeSensitiveCall(span trace.Span, server string, replica int, tool string, duration time.Duration, args map[string]any, result *ToolCallResult) {
	observation := sensitiveObservation(server, replica, tool, duration, args, result)
	g.mu.RLock()
	observer := g.toolCallObserver
	g.mu.RUnlock()
	if safe, ok := observer.(SensitiveToolCallObserver); ok {
		safe.ObserveSensitiveToolCall(observation)
	}
	setGenAISpanAttributes(span, observation.ServerName, observation.Operation, "", observation.Usage, nil)
}
