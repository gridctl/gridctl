package registry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Default executor limits.
const (
	defaultMaxResultSize = 1 << 20 // 1MB per step result
	defaultMaxDepth      = 10      // max nested skill composition depth
)

// ToolCaller is the interface the executor uses to invoke tools.
// It matches the existing mcp.ToolCaller interface.
type ToolCaller = mcp.ToolCaller

// callStackKey is the context key for tracking nested skill calls.
type callStackKey struct{}

// withCallStack stores the call stack in the context.
func withCallStack(ctx context.Context, stack []string) context.Context {
	return context.WithValue(ctx, callStackKey{}, stack)
}

// callStackFromCtx retrieves the call stack from the context.
func callStackFromCtx(ctx context.Context) []string {
	if v, ok := ctx.Value(callStackKey{}).([]string); ok {
		return v
	}
	return nil
}

// Executor runs skill workflows by dispatching steps through a ToolCaller.
type Executor struct {
	caller        ToolCaller
	logger        *slog.Logger
	maxResultSize int
	maxDepth      int
}

// NewExecutor creates a workflow executor that routes calls through the given ToolCaller.
func NewExecutor(caller ToolCaller, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		caller:        caller,
		logger:        logger,
		maxResultSize: defaultMaxResultSize,
		maxDepth:      defaultMaxDepth,
	}
}

// SetLogger updates the executor's logger. Used when the logger is created
// after the executor (e.g., in gateway builder where logging is Phase 2).
func (e *Executor) SetLogger(logger *slog.Logger) {
	if logger != nil {
		e.logger = logger
	}
}

// ExecutionResult captures the full result of a workflow execution.
type ExecutionResult struct {
	Skill      string                `json:"skill"`
	Status     string                `json:"status"` // "completed", "failed", "partial"
	StartedAt  time.Time             `json:"startedAt"`
	FinishedAt time.Time             `json:"finishedAt"`
	DurationMs int64                 `json:"durationMs"`
	Steps      []StepExecutionResult `json:"steps"`
	Output     *mcp.ToolCallResult   `json:"output,omitempty"`
	Error      string                `json:"error,omitempty"`
}

// StepExecutionResult captures the result of a single workflow step.
type StepExecutionResult struct {
	ID         string `json:"id"`
	Tool       string `json:"tool"`
	Status     string `json:"status"` // "success", "failed", "skipped"
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
}

// Execute runs a skill workflow. This is the entry point for CallTool().
func (e *Executor) Execute(ctx context.Context, skill *AgentSkill, arguments map[string]any) (*mcp.ToolCallResult, error) {
	startedAt := time.Now()

	// Cross-skill safety check
	stack := callStackFromCtx(ctx)
	for _, name := range stack {
		if name == skill.Name {
			path := strings.Join(append(stack, skill.Name), " -> ")
			return nil, fmt.Errorf("circular dependency detected: %s", path)
		}
	}
	if len(stack) >= e.maxDepth {
		path := strings.Join(append(stack, skill.Name), " -> ")
		return nil, fmt.Errorf("max workflow depth (%d) exceeded: %s", e.maxDepth, path)
	}
	ctx = withCallStack(ctx, append(append([]string{}, stack...), skill.Name))

	// Validate workflow exists
	if len(skill.Workflow) == 0 {
		return nil, fmt.Errorf("skill %q has no workflow steps", skill.Name)
	}

	// Validate inputs
	args, err := e.validateInputs(skill, arguments)
	if err != nil {
		return nil, fmt.Errorf("input validation: %w", err)
	}

	// Build DAG
	levels, err := BuildWorkflowDAG(skill.Workflow)
	if err != nil {
		return nil, fmt.Errorf("building workflow DAG: %w", err)
	}

	// Initialize template context
	tmplCtx := &TemplateContext{
		Inputs: args,
		Steps:  make(map[string]*StepResult),
	}

	// Track execution results and skipped steps
	var stepResults []StepExecutionResult
	skipped := make(map[string]bool)
	status := "completed"

	// Execute steps level by level, sequentially within each level
	for _, level := range levels {
		for _, step := range level {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("workflow cancelled: %w", err)
			}

			stepStart := time.Now()
			ser := StepExecutionResult{
				ID:        step.ID,
				Tool:      step.Tool,
				StartedAt: stepStart,
			}

			// Check if any dependency was skipped
			if e.isDependencySkipped(step, skipped) {
				ser.Status = "skipped"
				ser.Error = "dependency was skipped"
				ser.DurationMs = time.Since(stepStart).Milliseconds()
				stepResults = append(stepResults, ser)
				skipped[step.ID] = true
				e.logger.Info("step skipped (dependency skipped)",
					slog.String("skill", skill.Name),
					slog.String("step", step.ID))
				continue
			}

			// Evaluate condition
			if step.Condition != "" {
				condResult, condErr := EvaluateCondition(step.Condition, tmplCtx)
				if condErr != nil {
					ser.Status = "failed"
					ser.Error = fmt.Sprintf("condition evaluation: %v", condErr)
					ser.DurationMs = time.Since(stepStart).Milliseconds()
					stepResults = append(stepResults, ser)
					status = "failed"
					return e.buildResult(skill.Name, status, startedAt, stepResults, nil, ser.Error), nil
				}
				if !condResult {
					ser.Status = "skipped"
					ser.Error = "condition evaluated to false"
					ser.DurationMs = time.Since(stepStart).Milliseconds()
					stepResults = append(stepResults, ser)
					skipped[step.ID] = true
					e.logger.Info("step skipped (condition false)",
						slog.String("skill", skill.Name),
						slog.String("step", step.ID))
					continue
				}
			}

			// Resolve template args
			resolvedArgs, resolveErr := ResolveArgs(step.Args, tmplCtx)
			if resolveErr != nil {
				ser.Status = "failed"
				ser.Error = fmt.Sprintf("template resolution: %v", resolveErr)
				ser.DurationMs = time.Since(stepStart).Milliseconds()
				stepResults = append(stepResults, ser)

				result, halt := e.handleStepError(skill.Name, step, ser.Error, skipped)
				if halt {
					status = "failed"
					return e.buildResult(skill.Name, status, startedAt, stepResults, nil, ser.Error), nil
				}
				if result == "skip" {
					continue
				}
				// continue policy: store error and proceed
				tmplCtx.Steps[step.ID] = &StepResult{Result: ser.Error, IsError: true}
				status = "partial"
				continue
			}

			// Call tool
			toolResult, toolErr := e.caller.CallTool(ctx, step.Tool, resolvedArgs)
			ser.DurationMs = time.Since(stepStart).Milliseconds()

			if toolErr != nil {
				ser.Status = "failed"
				ser.Error = toolErr.Error()
				stepResults = append(stepResults, ser)

				result, halt := e.handleStepError(skill.Name, step, ser.Error, skipped)
				if halt {
					status = "failed"
					return e.buildResult(skill.Name, status, startedAt, stepResults, nil, ser.Error), nil
				}
				if result == "skip" {
					continue
				}
				tmplCtx.Steps[step.ID] = &StepResult{Result: ser.Error, IsError: true}
				status = "partial"
				continue
			}

			// Check for tool-level error (isError flag in result)
			if toolResult != nil && toolResult.IsError {
				ser.Status = "failed"
				errText := extractText(toolResult)
				ser.Error = errText
				stepResults = append(stepResults, ser)

				result, halt := e.handleStepError(skill.Name, step, errText, skipped)
				if halt {
					status = "failed"
					return e.buildResult(skill.Name, status, startedAt, stepResults, nil, errText), nil
				}
				if result == "skip" {
					continue
				}
				tmplCtx.Steps[step.ID] = NewStepResult(errText, true)
				status = "partial"
				continue
			}

			// Store success result
			ser.Status = "success"
			stepResults = append(stepResults, ser)
			resultText := extractText(toolResult)
			tmplCtx.Steps[step.ID] = NewStepResult(resultText, false)

			e.logger.Info("step completed",
				slog.String("skill", skill.Name),
				slog.String("step", step.ID),
				slog.Int64("duration_ms", ser.DurationMs))
		}
	}

	// Assemble output
	output, err := e.assembleOutput(skill, tmplCtx, skipped)
	if err != nil {
		return e.buildResult(skill.Name, "failed", startedAt, stepResults, nil, err.Error()), nil
	}

	return e.buildResult(skill.Name, status, startedAt, stepResults, output, ""), nil
}

// validateInputs applies defaults and validates required inputs.
func (e *Executor) validateInputs(skill *AgentSkill, arguments map[string]any) (map[string]any, error) {
	result := make(map[string]any)

	// Copy provided arguments
	for k, v := range arguments {
		result[k] = v
	}

	// Apply defaults and check required
	for name, input := range skill.Inputs {
		if _, ok := result[name]; !ok {
			if input.Default != nil {
				result[name] = input.Default
			} else if input.Required {
				return nil, fmt.Errorf("required input %q is missing", name)
			}
		}
	}

	return result, nil
}

// isDependencySkipped checks if any of the step's dependencies were skipped.
func (e *Executor) isDependencySkipped(step WorkflowStep, skipped map[string]bool) bool {
	for _, dep := range step.DependsOn {
		if skipped[dep] {
			return true
		}
	}
	return false
}

// handleStepError handles step failure based on the on_error policy.
// Returns ("skip"|"continue", shouldHalt).
func (e *Executor) handleStepError(skillName string, step WorkflowStep, errMsg string, skipped map[string]bool) (string, bool) {
	policy := step.OnError
	if policy == "" {
		policy = "fail"
	}

	e.logger.Warn("step failed",
		slog.String("skill", skillName),
		slog.String("step", step.ID),
		slog.String("policy", policy),
		slog.String("error", errMsg))

	switch policy {
	case "skip":
		skipped[step.ID] = true
		return "skip", false
	case "continue":
		return "continue", false
	default: // "fail"
		return "", true
	}
}

// assembleOutput builds the final output based on the skill's output configuration.
func (e *Executor) assembleOutput(skill *AgentSkill, tmplCtx *TemplateContext, skipped map[string]bool) (*mcp.ToolCallResult, error) {
	format := "merged"
	if skill.Output != nil && skill.Output.Format != "" {
		format = skill.Output.Format
	}

	switch format {
	case "merged":
		return e.assembleOutputMerged(skill, tmplCtx, skipped)
	case "last":
		return e.assembleOutputLast(skill, tmplCtx, skipped)
	case "custom":
		if skill.Output == nil || skill.Output.Template == "" {
			return nil, fmt.Errorf("output format 'custom' requires a template")
		}
		return e.assembleOutputCustom(skill.Output.Template, tmplCtx)
	default:
		return nil, fmt.Errorf("unknown output format: %q", format)
	}
}

// assembleOutputMerged joins step results with separator.
func (e *Executor) assembleOutputMerged(skill *AgentSkill, tmplCtx *TemplateContext, skipped map[string]bool) (*mcp.ToolCallResult, error) {
	includeSet := make(map[string]bool)
	if skill.Output != nil && len(skill.Output.Include) > 0 {
		for _, id := range skill.Output.Include {
			includeSet[id] = true
		}
	}

	var parts []string
	for _, step := range skill.Workflow {
		if skipped[step.ID] {
			continue
		}
		if len(includeSet) > 0 && !includeSet[step.ID] {
			continue
		}
		if sr, ok := tmplCtx.Steps[step.ID]; ok && !sr.IsError {
			if sr.Result != "" {
				parts = append(parts, sr.Result)
			}
		}
	}

	text := strings.Join(parts, "\n\n---\n\n")
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(text)},
	}, nil
}

// assembleOutputLast returns only the last step's result.
func (e *Executor) assembleOutputLast(skill *AgentSkill, tmplCtx *TemplateContext, skipped map[string]bool) (*mcp.ToolCallResult, error) {
	// Find the last non-skipped step with a result
	for i := len(skill.Workflow) - 1; i >= 0; i-- {
		step := skill.Workflow[i]
		if skipped[step.ID] {
			continue
		}
		if sr, ok := tmplCtx.Steps[step.ID]; ok {
			return &mcp.ToolCallResult{
				Content: []mcp.Content{mcp.NewTextContent(sr.Result)},
				IsError: sr.IsError,
			}, nil
		}
	}
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("")},
	}, nil
}

// assembleOutputCustom resolves a custom template.
func (e *Executor) assembleOutputCustom(tmpl string, tmplCtx *TemplateContext) (*mcp.ToolCallResult, error) {
	resolved, err := ResolveTemplate(tmpl, tmplCtx)
	if err != nil {
		return nil, fmt.Errorf("resolving output template: %w", err)
	}
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(resolved)},
	}, nil
}

// buildResult creates the final ToolCallResult, logging the execution record.
func (e *Executor) buildResult(skillName, status string, startedAt time.Time, steps []StepExecutionResult, output *mcp.ToolCallResult, errMsg string) *mcp.ToolCallResult {
	finishedAt := time.Now()
	record := ExecutionResult{
		Skill:      skillName,
		Status:     status,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		DurationMs: finishedAt.Sub(startedAt).Milliseconds(),
		Steps:      steps,
		Output:     output,
		Error:      errMsg,
	}

	e.logger.Info("workflow execution complete",
		slog.String("skill", record.Skill),
		slog.String("status", record.Status),
		slog.Int64("duration_ms", record.DurationMs),
		slog.Int("steps", len(record.Steps)))

	if status == "failed" {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Workflow %q failed: %s", skillName, errMsg))},
			IsError: true,
		}
	}

	if output != nil {
		return output
	}

	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Workflow %q completed with status: %s", skillName, status))},
	}
}

// extractText extracts the text content from a ToolCallResult.
func extractText(result *mcp.ToolCallResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}
