package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// CodeMode orchestrates search and execute operations for code mode.
// When active, it replaces individual tool definitions with two meta-tools
// (search and execute) that allow LLM agents to discover and call tools
// via JavaScript code in a sandboxed goja runtime.
type CodeMode struct {
	sandbox *Sandbox
	logger  *slog.Logger
}

// NewCodeMode creates a new CodeMode instance with the given execution timeout.
func NewCodeMode(timeout time.Duration) *CodeMode {
	return &CodeMode{
		sandbox: NewSandbox(timeout),
		logger:  slog.Default(),
	}
}

// SetLogger sets the logger for code mode operations.
func (cm *CodeMode) SetLogger(logger *slog.Logger) {
	if logger != nil {
		cm.logger = logger
	}
}

// ToolsList returns the two meta-tools (search and execute).
func (cm *CodeMode) ToolsList() *ToolsListResult {
	return &ToolsListResult{
		Tools: []Tool{
			SearchTool(),
			ExecuteTool(),
		},
	}
}

// IsMetaTool returns true if the tool name is a code mode meta-tool.
func (cm *CodeMode) IsMetaTool(name string) bool {
	return name == MetaToolSearch || name == MetaToolExecute
}

// HandleCall handles a code mode tool call with the full tool set.
func (cm *CodeMode) HandleCall(ctx context.Context, params ToolCallParams, caller ToolCaller, allTools []Tool) (*ToolCallResult, error) {
	return cm.HandleCallWithScope(ctx, params, caller, allTools)
}

// HandleCallWithScope handles a code mode tool call with a scoped tool set.
// The allowedTools parameter controls which tools are available in the sandbox.
func (cm *CodeMode) HandleCallWithScope(ctx context.Context, params ToolCallParams, caller ToolCaller, allowedTools []Tool) (*ToolCallResult, error) {
	switch params.Name {
	case MetaToolSearch:
		return cm.handleSearch(params, allowedTools)
	case MetaToolExecute:
		return cm.handleExecute(ctx, params, caller, allowedTools)
	default:
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Unknown code mode tool: %s", params.Name))},
			IsError: true,
		}, nil
	}
}

// handleSearch handles the search meta-tool.
func (cm *CodeMode) handleSearch(params ToolCallParams, tools []Tool) (*ToolCallResult, error) {
	query, _ := params.Arguments["query"].(string)

	index := NewSearchIndex(tools)
	matches := index.Search(query)

	type toolResult struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}

	results := make([]toolResult, len(matches))
	for i, tool := range matches {
		results[i] = toolResult{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}
	}

	jsonBytes, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Failed to format results: %v", err))},
			IsError: true,
		}, nil
	}

	header := fmt.Sprintf("Found %d tool(s)", len(matches))
	if query != "" {
		header += fmt.Sprintf(" matching '%s'", query)
	}
	header += fmt.Sprintf(" (out of %d total):\n\n", len(tools))

	return &ToolCallResult{
		Content: []Content{NewTextContent(header + string(jsonBytes))},
	}, nil
}

// handleExecute handles the execute meta-tool.
func (cm *CodeMode) handleExecute(ctx context.Context, params ToolCallParams, caller ToolCaller, allowedTools []Tool) (*ToolCallResult, error) {
	code, ok := params.Arguments["code"].(string)
	if !ok || code == "" {
		return &ToolCallResult{
			Content: []Content{NewTextContent("'code' parameter is required and must be a non-empty string")},
			IsError: true,
		}, nil
	}

	cm.logger.Info("code mode execution started", "code_size", len(code))
	start := time.Now()

	result, err := cm.sandbox.Execute(ctx, code, caller, allowedTools)
	duration := time.Since(start)

	if err != nil {
		cm.logger.Warn("code mode execution failed", "duration", duration, "error", err)

		errMsg := err.Error()
		var hint string

		switch {
		case strings.Contains(errMsg, "syntax error"):
			hint = "Fix the syntax and retry."
		case strings.Contains(errMsg, "access denied"):
			hint = "Use search() to discover available tools."
		case strings.Contains(errMsg, "timeout"):
			hint = fmt.Sprintf("Execution exceeded %s timeout. Simplify the operation.", cm.sandbox.timeout)
		case strings.Contains(errMsg, "code too large"):
			hint = fmt.Sprintf("Maximum code size is %d bytes.", MaxCodeSize)
		default:
			hint = "Check the error message and retry."
		}

		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Error: %s\n\nHint: %s", errMsg, hint))},
			IsError: true,
		}, nil
	}

	cm.logger.Info("code mode execution finished", "duration", duration)

	var parts []string
	if result.Value != "" {
		parts = append(parts, result.Value)
	}
	if len(result.Console) > 0 {
		consoleBlock := "--- Console Output ---\n" + strings.Join(result.Console, "\n")
		parts = append(parts, consoleBlock)
	}

	text := strings.Join(parts, "\n\n")
	if text == "" {
		text = "(no output)"
	}

	return &ToolCallResult{
		Content: []Content{NewTextContent(text)},
	}, nil
}
