package registry

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// executionContext tracks state during a skill's tool chain execution.
type executionContext struct {
	input       map[string]any // original skill arguments
	stepResults map[int]string // stepN -> result text
}

// executeSkill runs a skill's tool chain sequentially.
func (s *Server) executeSkill(ctx context.Context, skill *Skill, arguments map[string]any) (*mcp.ToolCallResult, error) {
	execCtx := &executionContext{
		input:       arguments,
		stepResults: make(map[int]string),
	}

	var lastResult *mcp.ToolCallResult

	for i, step := range skill.Steps {
		if err := ctx.Err(); err != nil {
			return &mcp.ToolCallResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf(
					"skill execution timed out at step %d/%d (%s): %v",
					i+1, len(skill.Steps), step.Tool, err,
				))},
				IsError: true,
			}, nil
		}

		resolvedArgs, err := resolveStepArguments(step.Arguments, execCtx)
		if err != nil {
			return &mcp.ToolCallResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf(
					"failed to resolve arguments for step %d (%s): %v",
					i+1, step.Tool, err,
				))},
				IsError: true,
			}, nil
		}

		result, err := s.toolCaller.CallTool(ctx, step.Tool, resolvedArgs)
		if err != nil {
			return &mcp.ToolCallResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf(
					"step %d/%d failed (%s): %v",
					i+1, len(skill.Steps), step.Tool, err,
				))},
				IsError: true,
			}, nil
		}

		if result.IsError {
			return &mcp.ToolCallResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf(
					"step %d/%d returned error (%s): %s",
					i+1, len(skill.Steps), step.Tool, extractTextContent(result),
				))},
				IsError: true,
			}, nil
		}

		execCtx.stepResults[i+1] = extractTextContent(result)
		lastResult = result
	}

	if lastResult == nil {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent("skill completed with no output")},
		}, nil
	}

	return lastResult, nil
}

// templatePattern matches {{input.name}} and {{stepN.result}} patterns.
var templatePattern = regexp.MustCompile(`\{\{(input\.(\w+)|step(\d+)\.result)\}\}`)

// resolveStepArguments substitutes template variables in step arguments.
func resolveStepArguments(args map[string]string, execCtx *executionContext) (map[string]any, error) {
	resolved := make(map[string]any, len(args))
	for key, tmpl := range args {
		value, err := resolveTemplate(tmpl, execCtx)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", key, err)
		}
		resolved[key] = value
	}
	return resolved, nil
}

// resolveTemplate replaces template patterns in a single string value.
func resolveTemplate(tmpl string, execCtx *executionContext) (string, error) {
	var resolveErr error

	result := templatePattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		submatch := templatePattern.FindStringSubmatch(match)
		if submatch == nil {
			return match
		}

		// {{input.name}} pattern
		if submatch[2] != "" {
			inputName := submatch[2]
			if val, ok := execCtx.input[inputName]; ok {
				return fmt.Sprintf("%v", val)
			}
			resolveErr = fmt.Errorf("input argument not found: %s", inputName)
			return match
		}

		// {{stepN.result}} pattern
		if submatch[3] != "" {
			stepNum, err := strconv.Atoi(submatch[3])
			if err != nil {
				resolveErr = fmt.Errorf("invalid step number: %s", submatch[3])
				return match
			}
			if val, ok := execCtx.stepResults[stepNum]; ok {
				return val
			}
			resolveErr = fmt.Errorf("step %d result not available", stepNum)
			return match
		}

		return match
	})

	return result, resolveErr
}

// extractTextContent gets the first text content item from a tool call result.
func extractTextContent(result *mcp.ToolCallResult) string {
	if result == nil {
		return ""
	}
	for _, c := range result.Content {
		if c.Type == "text" {
			return c.Text
		}
	}
	return ""
}
