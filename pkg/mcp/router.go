package mcp

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Router routes tool calls to the appropriate agent.
//
// Internally the Router keys on server name and stores a *ReplicaSet per name.
// A single-client registration (via AddClient) is wrapped in a
// single-replica round-robin set so callers outside this package observe the
// same behavior as before replicas existed.
type Router struct {
	mu    sync.RWMutex
	sets  map[string]*ReplicaSet // agentName -> replica set
	tools map[string]string      // prefixedToolName -> agentName
}

// NewRouter creates a new tool router.
func NewRouter() *Router {
	return &Router{
		sets:  make(map[string]*ReplicaSet),
		tools: make(map[string]string),
	}
}

// AddClient adds an agent client to the router as a single-replica set.
// Preserves the pre-replicas API so existing callers keep working unchanged.
func (r *Router) AddClient(client AgentClient) {
	set := NewReplicaSet(client.Name(), ReplicaPolicyRoundRobin, []AgentClient{client})
	r.AddReplicaSet(set)
}

// AddReplicaSet registers a replica set under its logical server name.
// Replaces any existing set with the same name.
func (r *Router) AddReplicaSet(set *ReplicaSet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sets[set.Name()] = set
}

// RemoveClient removes an agent (replica set) and its tools from the router.
func (r *Router) RemoveClient(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sets, name)

	// Remove tools for this agent
	for tool, agent := range r.tools {
		if agent == name {
			delete(r.tools, tool)
		}
	}
}

// GetClient returns one client for the named agent, chosen by the set's
// dispatch policy. Returns nil if the agent is not registered or no replica
// is currently healthy.
func (r *Router) GetClient(name string) AgentClient {
	r.mu.RLock()
	set, ok := r.sets[name]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	return set.Client()
}

// GetReplicaSet returns the replica set for the named agent, or nil if the
// agent is not registered. Useful for callers that need per-replica access
// (health monitor, status reporting).
func (r *Router) GetReplicaSet(name string) *ReplicaSet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sets[name]
}

// Clients returns one representative AgentClient per registered agent,
// sorted by agent name. Each representative is chosen via the set's policy,
// so a single-replica set returns its only client. Skips sets with no
// currently-healthy replica.
func (r *Router) Clients() []AgentClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.sets))
	for n := range r.sets {
		names = append(names, n)
	}
	sort.Strings(names)
	clients := make([]AgentClient, 0, len(names))
	for _, n := range names {
		if c := r.sets[n].Client(); c != nil {
			clients = append(clients, c)
		}
	}
	return clients
}

// ReplicaSets returns all registered replica sets, sorted by agent name.
func (r *Router) ReplicaSets() []*ReplicaSet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.sets))
	for n := range r.sets {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*ReplicaSet, 0, len(names))
	for _, n := range names {
		out = append(out, r.sets[n])
	}
	return out
}

// toolsOf returns the Tools list to advertise for a set. All replicas share
// the same tool surface, so reading from replica-0 is sufficient.
func toolsOf(set *ReplicaSet) []Tool {
	reps := set.Replicas()
	if len(reps) == 0 {
		return nil
	}
	return reps[0].Client().Tools()
}

// RefreshTools updates the tool registry from all agents.
func (r *Router) RefreshTools() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear existing tool mappings
	r.tools = make(map[string]string)

	for name, set := range r.sets {
		for _, tool := range toolsOf(set) {
			prefixedName := PrefixTool(name, tool.Name)
			r.tools[prefixedName] = name
		}
	}
}

// AggregatedTools returns all tools from all agents with prefixed names.
func (r *Router) AggregatedTools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.sets))
	for name := range r.sets {
		names = append(names, name)
	}
	sort.Strings(names)

	var tools []Tool
	for _, name := range names {
		for _, tool := range toolsOf(r.sets[name]) {
			prefixedName := PrefixTool(name, tool.Name)
			prefixedTool := Tool{
				Name:        prefixedName,
				Title:       prefixedName,
				Description: fmt.Sprintf("MCP server: %s. Call using the exact tool name %q. %s", name, prefixedName, tool.Description),
				InputSchema: tool.InputSchema,
			}
			tools = append(tools, prefixedTool)
		}
	}
	return tools
}

// RouteToolCall routes a tool call to the appropriate agent. The concrete
// replica is chosen by the set's dispatch policy.
func (r *Router) RouteToolCall(prefixedName string) (AgentClient, string, error) {
	agentName, toolName, err := ParsePrefixedTool(prefixedName)
	if err != nil {
		return nil, "", err
	}

	r.mu.RLock()
	set, ok := r.sets[agentName]
	r.mu.RUnlock()
	if !ok {
		return nil, "", fmt.Errorf("unknown agent: %s", agentName)
	}

	replica, err := set.Pick()
	if err != nil {
		return nil, "", fmt.Errorf("agent %s: %w", agentName, err)
	}
	return replica.Client(), toolName, nil
}

// ToolNameDelimiter is the separator between agent name and tool name in prefixed tool names.
// Format: "agentname__toolname"
// Uses double underscore to be compatible with Claude Desktop's tool name validation: ^[a-zA-Z0-9_-]{1,64}$
const ToolNameDelimiter = "__"

// PrefixTool creates a prefixed tool name: "agent__tool"
func PrefixTool(agentName, toolName string) string {
	return agentName + ToolNameDelimiter + toolName
}

// ParsePrefixedTool parses a prefixed tool name into agent and tool names.
func ParsePrefixedTool(prefixed string) (agentName, toolName string, err error) {
	parts := strings.SplitN(prefixed, ToolNameDelimiter, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid tool name format: %s (expected agent__tool)", prefixed)
	}
	return parts[0], parts[1], nil
}
