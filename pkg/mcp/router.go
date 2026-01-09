package mcp

import (
	"fmt"
	"strings"
	"sync"
)

// Router routes tool calls to the appropriate agent.
type Router struct {
	mu      sync.RWMutex
	clients map[string]AgentClient // agentName -> client
	tools   map[string]string      // prefixedToolName -> agentName
}

// NewRouter creates a new tool router.
func NewRouter() *Router {
	return &Router{
		clients: make(map[string]AgentClient),
		tools:   make(map[string]string),
	}
}

// AddClient adds an agent client to the router.
func (r *Router) AddClient(client AgentClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[client.Name()] = client
}

// RemoveClient removes an agent client from the router.
func (r *Router) RemoveClient(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, name)

	// Remove tools for this agent
	for tool, agent := range r.tools {
		if agent == name {
			delete(r.tools, tool)
		}
	}
}

// GetClient returns a client by agent name.
func (r *Router) GetClient(name string) AgentClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[name]
}

// Clients returns all registered clients.
func (r *Router) Clients() []AgentClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clients := make([]AgentClient, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	return clients
}

// RefreshTools updates the tool registry from all agents.
func (r *Router) RefreshTools() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear existing tool mappings
	r.tools = make(map[string]string)

	// Register tools from each agent with prefixes
	for name, client := range r.clients {
		for _, tool := range client.Tools() {
			prefixedName := PrefixTool(name, tool.Name)
			r.tools[prefixedName] = name
		}
	}
}

// AggregatedTools returns all tools from all agents with prefixed names.
func (r *Router) AggregatedTools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []Tool
	for name, client := range r.clients {
		for _, tool := range client.Tools() {
			// Use original tool name as title for UI display
			title := tool.Name
			if tool.Title != "" {
				title = tool.Title
			}
			prefixedTool := Tool{
				Name:        PrefixTool(name, tool.Name),
				Title:       title,
				Description: fmt.Sprintf("[%s] %s", name, tool.Description),
				InputSchema: tool.InputSchema,
			}
			tools = append(tools, prefixedTool)
		}
	}
	return tools
}

// RouteToolCall routes a tool call to the appropriate agent.
func (r *Router) RouteToolCall(prefixedName string) (AgentClient, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agentName, toolName, err := ParsePrefixedTool(prefixedName)
	if err != nil {
		return nil, "", err
	}

	client, ok := r.clients[agentName]
	if !ok {
		return nil, "", fmt.Errorf("unknown agent: %s", agentName)
	}

	return client, toolName, nil
}

// ToolNameDelimiter is the separator between agent name and tool name in prefixed tool names.
// Format: "agentname::toolname"
const ToolNameDelimiter = "::"

// PrefixTool creates a prefixed tool name: "agent::tool"
func PrefixTool(agentName, toolName string) string {
	return agentName + ToolNameDelimiter + toolName
}

// ParsePrefixedTool parses a prefixed tool name into agent and tool names.
func ParsePrefixedTool(prefixed string) (agentName, toolName string, err error) {
	parts := strings.SplitN(prefixed, ToolNameDelimiter, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid tool name format: %s (expected agent::tool)", prefixed)
	}
	return parts[0], parts[1], nil
}
