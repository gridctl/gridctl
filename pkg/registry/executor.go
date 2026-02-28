package registry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Default executor limits.
const (
	defaultMaxResultSize    = 1 << 20       // 1MB per step result
	defaultMaxDepth         = 10            // max nested skill composition depth
	defaultMaxParallel      = 4             // max concurrent steps per DAG level
	defaultWorkflowTimeout  = 5 * time.Minute
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
	caller          ToolCaller
	logger          *slog.Logger
	maxResultSize   int
	maxDepth        int
	maxParallel     int
	workflowTimeout time.Duration
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

// WithMaxParallel sets the maximum number of concurrent steps per DAG level.
func WithMaxParallel(n int) ExecutorOption {
	return func(e *Executor) {
		if n > 0 {
			e.maxParallel = n
		}
	}
}

// WithMaxResultSize sets the maximum size of a single step result.
func WithMaxResultSize(n int) ExecutorOption {
	return func(e *Executor) {
		if n > 0 {
			e.maxResultSize = n
		}
	}
}

// WithWorkflowTimeout sets the maximum duration for an entire workflow execution.
func WithWorkflowTimeout(d time.Duration) ExecutorOption {
	return func(e *Executor) {
		if d > 0 {
			e.workflowTimeout = d
		}
	}
}

// NewExecutor creates a workflow executor that routes calls through the given ToolCaller.
func NewExecutor(caller ToolCaller, logger *slog.Logger, opts ...ExecutorOption) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	e := &Executor{
		caller:          caller,
		logger:          logger,
		maxResultSize:   defaultMaxResultSize,
		maxDepth:        defaultMaxDepth,
		maxParallel:     defaultMaxParallel,
		workflowTimeout: defaultWorkflowTimeout,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
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
	ID         string    `json:"id"`
	Tool       string    `json:"tool"`
	Status     string    `json:"status"` // "success", "failed", "skipped"
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
	Attempts   int       `json:"attempts,omitempty"`   // retry count (1 = no retry)
	SkipReason string    `json:"skipReason,omitempty"` // why step was skipped
	Level      int       `json:"level"`                // DAG level (0-indexed)
}

// safeStepMap provides thread-safe access to step results.
type safeStepMap struct {
	mu sync.RWMutex
	m  map[string]*StepResult
}

func newSafeStepMap() *safeStepMap {
	return &safeStepMap{m: make(map[string]*StepResult)}
}

func (s *safeStepMap) Set(id string, result *StepResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = result
}

func (s *safeStepMap) Get(id string) (*StepResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[id]
	return v, ok
}

// Snapshot returns a plain map copy for use in output assembly (not concurrent).
func (s *safeStepMap) Snapshot() map[string]*StepResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]*StepResult, len(s.m))
	for k, v := range s.m {
		cp[k] = v
	}
	return cp
}

// safeSkipMap provides thread-safe access to the skipped step set.
type safeSkipMap struct {
	mu sync.RWMutex
	m  map[string]string // step ID -> reason
}

func newSafeSkipMap() *safeSkipMap {
	return &safeSkipMap{m: make(map[string]string)}
}

func (s *safeSkipMap) Set(id, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = reason
}

func (s *safeSkipMap) IsSkipped(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reason, ok := s.m[id]
	return reason, ok
}

// SkippedSet returns a plain bool map for output assembly.
func (s *safeSkipMap) SkippedSet() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]bool, len(s.m))
	for k := range s.m {
		cp[k] = true
	}
	return cp
}

// Execute runs a skill workflow. This is the entry point for CallTool().
func (e *Executor) Execute(ctx context.Context, skill *AgentSkill, arguments map[string]any) (*mcp.ToolCallResult, error) {
	startedAt := time.Now()

	// Apply workflow-level timeout
	if e.workflowTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.workflowTimeout)
		defer cancel()
	}

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

	// Initialize thread-safe template context and tracking
	stepMap := newSafeStepMap()
	skipped := newSafeSkipMap()
	var stepResults []StepExecutionResult
	status := "completed"

	// Build dependency graph for transitive skip propagation
	depGraph := buildDependencyGraph(skill.Workflow)

	// Execute steps level by level, parallel within each level
	for levelIdx, level := range levels {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("workflow cancelled: %w", err)
		}

		// Separate steps into skipped-by-dependency and executable
		var executable []WorkflowStep
		for _, step := range level {
			if reason, ok := skipped.IsSkipped(step.ID); ok {
				stepResults = append(stepResults, StepExecutionResult{
					ID:         step.ID,
					Tool:       step.Tool,
					Status:     "skipped",
					StartedAt:  time.Now(),
					SkipReason: reason,
					Level:      levelIdx,
				})
				e.logger.Debug("step skipped (dependency)",
					slog.String("skill", skill.Name),
					slog.String("step", step.ID),
					slog.String("reason", reason))
				continue
			}
			executable = append(executable, step)
		}

		if len(executable) == 0 {
			continue
		}

		// Execute steps in parallel within this level
		sem := make(chan struct{}, e.maxParallel)
		var wg sync.WaitGroup

		type stepOutput struct {
			ser    StepExecutionResult
			result *StepResult // nil if skipped or halting
			policy string      // "skip", "continue", or "" (fail/success)
			halt   bool
		}
		outputs := make([]stepOutput, len(executable))

		for i, step := range executable {
			wg.Add(1)
			go func(idx int, step WorkflowStep) {
				defer wg.Done()

				// Acquire semaphore with context awareness
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					outputs[idx] = stepOutput{
						ser: StepExecutionResult{
							ID:         step.ID,
							Tool:       step.Tool,
							Status:     "skipped",
							StartedAt:  time.Now(),
							SkipReason: "workflow cancelled",
							Level:      levelIdx,
						},
						policy: "skip",
					}
					return
				}

				if ctx.Err() != nil {
					outputs[idx] = stepOutput{
						ser: StepExecutionResult{
							ID:         step.ID,
							Tool:       step.Tool,
							Status:     "skipped",
							StartedAt:  time.Now(),
							SkipReason: "workflow cancelled",
							Level:      levelIdx,
						},
						policy: "skip",
					}
					return
				}

				// Build a read-only template context snapshot for this step.
				// Within a level, steps don't depend on each other, so the
				// snapshot from prior levels is sufficient.
				tmplCtx := &TemplateContext{
					Inputs: args,
					Steps:  stepMap.Snapshot(),
				}

				ser, result, policy, halt := e.executeStepFull(ctx, skill.Name, step, tmplCtx, levelIdx)
				outputs[idx] = stepOutput{ser: ser, result: result, policy: policy, halt: halt}
			}(i, step)
		}
		wg.Wait()

		// Process results sequentially to maintain deterministic ordering.
		// levelFailed captures whether any step triggered a "fail" halt.
		var levelFailed bool
		var failErr string
		for i, out := range outputs {
			stepResults = append(stepResults, out.ser)

			if out.halt {
				// Preserve first failure only
				if !levelFailed {
					levelFailed = true
					failErr = out.ser.Error
				}
				continue
			}

			step := executable[i]
			switch out.policy {
			case "skip":
				// Mark step and all transitive dependents as skipped
				reason := fmt.Sprintf("dependency '%s' failed", step.ID)
				if out.ser.SkipReason != "" {
					reason = out.ser.SkipReason
				}
				skipped.Set(step.ID, reason)
				e.markTransitiveDependentsSkipped(step.ID, depGraph, skipped)
			case "continue":
				if out.result != nil {
					stepMap.Set(step.ID, out.result)
				}
				status = "partial"
			default: // success
				if out.result != nil {
					stepMap.Set(step.ID, out.result)
				}
			}
		}

		if levelFailed {
			status = "failed"
			tmplCtx := &TemplateContext{Inputs: args, Steps: stepMap.Snapshot()}
			return e.buildResult(skill.Name, status, startedAt, stepResults, nil, failErr, tmplCtx), nil
		}
	}

	// Assemble output
	tmplCtx := &TemplateContext{Inputs: args, Steps: stepMap.Snapshot()}
	output, err := e.assembleOutput(skill, tmplCtx, skipped.SkippedSet())
	if err != nil {
		return e.buildResult(skill.Name, "failed", startedAt, stepResults, nil, err.Error(), tmplCtx), nil
	}

	return e.buildResult(skill.Name, status, startedAt, stepResults, output, "", tmplCtx), nil
}

// executeStepFull executes a single step with condition evaluation, retry, and timeout.
// Returns the execution result, step result for template context, error policy, and halt flag.
func (e *Executor) executeStepFull(ctx context.Context, skillName string, step WorkflowStep, tmplCtx *TemplateContext, level int) (StepExecutionResult, *StepResult, string, bool) {
	stepStart := time.Now()
	ser := StepExecutionResult{
		ID:        step.ID,
		Tool:      step.Tool,
		StartedAt: stepStart,
		Level:     level,
	}

	// Evaluate condition
	if step.Condition != "" {
		condResult, condErr := EvaluateCondition(step.Condition, tmplCtx)
		if condErr != nil {
			ser.Status = "failed"
			ser.Error = fmt.Sprintf("condition evaluation: %v", condErr)
			ser.DurationMs = time.Since(stepStart).Milliseconds()
			// Condition evaluation errors always halt (same as "fail" policy)
			return ser, nil, "", true
		}
		if !condResult {
			ser.Status = "skipped"
			ser.SkipReason = "condition evaluated to false"
			ser.DurationMs = time.Since(stepStart).Milliseconds()
			e.logger.Debug("step skipped (condition false)",
				slog.String("skill", skillName),
				slog.String("step", step.ID))
			return ser, nil, "skip", false
		}
	}

	// Execute with retry
	result, attempts, err := e.executeStepWithRetry(ctx, step, tmplCtx)
	ser.DurationMs = time.Since(stepStart).Milliseconds()
	ser.Attempts = attempts

	if err != nil {
		ser.Status = "failed"
		ser.Error = err.Error()
		policy, halt := e.resolveErrorPolicy(skillName, step, ser.Error)
		if halt {
			return ser, nil, "", true
		}
		if policy == "skip" {
			return ser, nil, "skip", false
		}
		// continue: store error in step result
		return ser, NewStepResult(ser.Error, true), "continue", false
	}

	// Check for tool-level error (isError flag)
	if result != nil && result.IsError {
		errText := extractText(result)
		ser.Status = "failed"
		ser.Error = errText
		policy, halt := e.resolveErrorPolicy(skillName, step, errText)
		if halt {
			return ser, nil, "", true
		}
		if policy == "skip" {
			return ser, nil, "skip", false
		}
		return ser, NewStepResult(errText, true), "continue", false
	}

	// Success
	ser.Status = "success"
	resultText := extractText(result)
	e.logger.Debug("step completed",
		slog.String("skill", skillName),
		slog.String("step", step.ID),
		slog.Int64("duration_ms", ser.DurationMs),
		slog.Int("level", level))
	return ser, NewStepResult(resultText, false), "", false
}

// executeStepWithRetry wraps executeStep with retry logic.
func (e *Executor) executeStepWithRetry(ctx context.Context, step WorkflowStep, tmplCtx *TemplateContext) (*mcp.ToolCallResult, int, error) {
	maxAttempts := 1
	backoff := time.Second

	if step.Retry != nil {
		maxAttempts = step.Retry.MaxAttempts
		if maxAttempts < 1 {
			maxAttempts = 1
		}
		if step.Retry.Backoff != "" {
			dur, err := time.ParseDuration(step.Retry.Backoff)
			if err == nil {
				backoff = dur
			}
		}
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := e.executeStep(ctx, step, tmplCtx)
		if err == nil && (result == nil || !result.IsError) {
			return result, attempt, nil
		}
		lastErr = err
		if result != nil && result.IsError {
			lastErr = fmt.Errorf("step returned error: %s", extractText(result))
		}

		if attempt < maxAttempts {
			e.logger.Warn("step failed, retrying",
				slog.String("step", step.ID),
				slog.Int("attempt", attempt),
				slog.Int("max_attempts", maxAttempts),
				slog.String("error", lastErr.Error()))
			select {
			case <-ctx.Done():
				return nil, attempt, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unknown error")
	}
	return nil, maxAttempts, fmt.Errorf("step %q failed after %d attempts: %w", step.ID, maxAttempts, lastErr)
}

// executeStep executes a single tool call with per-step timeout.
func (e *Executor) executeStep(ctx context.Context, step WorkflowStep, tmplCtx *TemplateContext) (*mcp.ToolCallResult, error) {
	// Apply per-step timeout
	if step.Timeout != "" {
		dur, err := time.ParseDuration(step.Timeout)
		if err != nil {
			return nil, fmt.Errorf("step %q: invalid timeout %q: %w", step.ID, step.Timeout, err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dur)
		defer cancel()
	}

	// Resolve template args
	resolvedArgs, err := ResolveArgs(step.Args, tmplCtx)
	if err != nil {
		return nil, fmt.Errorf("template resolution: %v", err)
	}

	// Call tool
	result, err := e.caller.CallTool(ctx, step.Tool, resolvedArgs)
	if err != nil {
		if ctx.Err() != nil {
			// Distinguish timeout from cancellation
			if step.Timeout != "" {
				dur, _ := time.ParseDuration(step.Timeout)
				return nil, fmt.Errorf("step '%s' timed out after %s", step.ID, dur)
			}
		}
		return nil, err
	}
	return result, nil
}

// resolveErrorPolicy determines the error handling policy for a failed step.
// Returns (policy, shouldHalt).
func (e *Executor) resolveErrorPolicy(skillName string, step WorkflowStep, errMsg string) (string, bool) {
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
		return "skip", false
	case "continue":
		return "continue", false
	default: // "fail"
		return "", true
	}
}

// buildDependencyGraph builds a map from step ID to the list of step IDs that depend on it.
func buildDependencyGraph(steps []WorkflowStep) map[string][]string {
	graph := make(map[string][]string)
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			graph[dep] = append(graph[dep], step.ID)
		}
	}
	return graph
}

// markTransitiveDependentsSkipped marks all transitive dependents of a step as skipped.
func (e *Executor) markTransitiveDependentsSkipped(stepID string, depGraph map[string][]string, skipped *safeSkipMap) {
	reason := fmt.Sprintf("dependency '%s' failed", stepID)
	queue := depGraph[stepID]
	visited := make(map[string]bool)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		skipped.Set(current, reason)
		queue = append(queue, depGraph[current]...)
	}
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
func (e *Executor) buildResult(skillName, status string, startedAt time.Time, steps []StepExecutionResult, output *mcp.ToolCallResult, errMsg string, tmplCtx *TemplateContext) *mcp.ToolCallResult {
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

	logLevel := slog.LevelInfo
	if status == "failed" {
		logLevel = slog.LevelError
	} else if status == "partial" {
		logLevel = slog.LevelWarn
	}
	e.logger.Log(context.Background(), logLevel, "workflow execution complete",
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
