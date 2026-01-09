package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"agentlab/pkg/a2a"
	"agentlab/pkg/mcp"
)

// A2AClientAdapter adapts an A2A agent to the mcp.AgentClient interface.
// This enables agents to be "equipped" as skills by other agents through the
// unified MCP gateway, treating A2A agents as tool providers.
type A2AClientAdapter struct {
	name      string
	a2aClient *a2a.Client

	mu          sync.RWMutex
	initialized bool
	tools       []mcp.Tool
	serverInfo  mcp.ServerInfo
}

// NewA2AClientAdapter creates a new adapter wrapping an A2A client.
func NewA2AClientAdapter(name string, endpoint string) *A2AClientAdapter {
	return &A2AClientAdapter{
		name:      name,
		a2aClient: a2a.NewClient(name, endpoint),
	}
}

// Name returns the adapter name used for tool prefixing.
func (a *A2AClientAdapter) Name() string {
	return a.name
}

// Initialize fetches the A2A agent card and converts skills to tools.
func (a *A2AClientAdapter) Initialize(ctx context.Context) error {
	card, err := a.a2aClient.FetchAgentCard(ctx)
	if err != nil {
		return fmt.Errorf("fetching agent card: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.serverInfo = mcp.ServerInfo{
		Name:    card.Name,
		Version: card.Version,
	}

	a.tools = skillsToTools(card.Skills)
	a.initialized = true

	return nil
}

// InitializeFromSkills initializes the adapter directly from skill data.
// This is used for local A2A agents where we already have the skill info
// from the topology config and don't need to make an HTTP call.
func (a *A2AClientAdapter) InitializeFromSkills(version string, skills []a2a.Skill) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.serverInfo = mcp.ServerInfo{
		Name:    a.name,
		Version: version,
	}

	a.tools = skillsToTools(skills)
	a.initialized = true
}

// RefreshTools re-fetches the A2A agent card and updates tools.
func (a *A2AClientAdapter) RefreshTools(ctx context.Context) error {
	card, err := a.a2aClient.FetchAgentCard(ctx)
	if err != nil {
		return fmt.Errorf("refreshing tools: %w", err)
	}

	a.mu.Lock()
	a.tools = skillsToTools(card.Skills)
	a.mu.Unlock()

	return nil
}

// Tools returns MCP tools derived from A2A skills.
func (a *A2AClientAdapter) Tools() []mcp.Tool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tools
}

// CallTool invokes an A2A skill using the message/send method.
func (a *A2AClientAdapter) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	// Build message with tool invocation details
	argsJSON, err := json.Marshal(arguments)
	if err != nil {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Error marshaling arguments: %v", err))},
			IsError: true,
		}, nil
	}
	messageText := fmt.Sprintf("Invoke skill '%s' with arguments: %s", name, string(argsJSON))

	params := a2a.SendMessageParams{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{a2a.NewTextPart(messageText)},
			Metadata: map[string]string{
				"skill_id":  name,
				"arguments": string(argsJSON),
			},
		},
	}

	result, err := a.a2aClient.SendMessage(ctx, params)
	if err != nil {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Error: %v", err))},
			IsError: true,
		}, nil
	}

	// Wait for task completion if the result contains an async task
	if result.Task != nil && !result.Task.Status.State.IsTerminal() {
		result.Task, err = a.waitForTaskCompletion(ctx, result.Task.ID)
		if err != nil {
			return &mcp.ToolCallResult{
				Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("Error waiting for completion: %v", err))},
				IsError: true,
			}, nil
		}
	}

	return a2aResultToMCPResult(result), nil
}

// IsInitialized returns whether the adapter has been initialized.
func (a *A2AClientAdapter) IsInitialized() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.initialized
}

// ServerInfo returns server info from the A2A agent card.
func (a *A2AClientAdapter) ServerInfo() mcp.ServerInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.serverInfo
}

// WaitForReady waits for the A2A agent to become available with retries.
func (a *A2AClientAdapter) WaitForReady(ctx context.Context, timeout time.Duration) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	deadline := time.After(timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for A2A agent %s", a.name)
		case <-ticker.C:
			if _, err := a.a2aClient.FetchAgentCard(ctx); err == nil {
				return nil
			}
		}
	}
}

// waitForTaskCompletion polls the task until it reaches a terminal state.
func (a *A2AClientAdapter) waitForTaskCompletion(ctx context.Context, taskID string) (*a2a.Task, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for task completion")
		case <-ticker.C:
			task, err := a.a2aClient.GetTask(ctx, taskID, 0)
			if err != nil {
				return nil, err
			}
			if task.Status.State.IsTerminal() {
				return task, nil
			}
		}
	}
}

// skillsToTools converts A2A skills to MCP tools.
func skillsToTools(skills []a2a.Skill) []mcp.Tool {
	tools := make([]mcp.Tool, len(skills))
	for i, skill := range skills {
		schema := mcp.InputSchemaObject{
			Type: "object",
			Properties: map[string]mcp.Property{
				"message": {
					Type:        "string",
					Description: "Natural language message describing what you want the agent to do",
				},
			},
			Required: []string{"message"},
		}
		schemaBytes, _ := json.Marshal(schema)
		tools[i] = mcp.Tool{
			Name:        skill.ID,
			Title:       skill.Name,
			Description: skill.Description,
			InputSchema: schemaBytes,
		}
	}
	return tools
}

// a2aResultToMCPResult converts an A2A result to MCP format.
func a2aResultToMCPResult(result *a2a.SendMessageResult) *mcp.ToolCallResult {
	var contents []mcp.Content
	isError := false

	if result.Task != nil {
		if result.Task.Status.State == a2a.TaskStateFailed {
			isError = true
			contents = append(contents, mcp.NewTextContent(result.Task.Status.Message))
		} else {
			// Extract text from agent messages in the task
			for _, msg := range result.Task.Messages {
				if msg.Role == a2a.RoleAgent {
					for _, part := range msg.Parts {
						if part.Type == a2a.PartTypeText {
							contents = append(contents, mcp.NewTextContent(part.Text))
						}
					}
				}
			}
			// Include artifacts as additional content
			for _, artifact := range result.Task.Artifacts {
				for _, part := range artifact.Parts {
					if part.Type == a2a.PartTypeText {
						contents = append(contents, mcp.NewTextContent(part.Text))
					}
				}
			}
		}
	}

	if len(contents) == 0 {
		contents = append(contents, mcp.NewTextContent("Task completed"))
	}

	return &mcp.ToolCallResult{
		Content: contents,
		IsError: isError,
	}
}
