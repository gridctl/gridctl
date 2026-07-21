package mcp

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Router routes tool calls to the appropriate MCP server.
//
// Internally the Router keys on server name and stores a *ReplicaSet per name.
// A single-client registration (via AddClient) is wrapped in a
// single-replica round-robin set so callers outside this package observe the
// same behavior as before replicas existed.
type Router struct {
	mu    sync.RWMutex
	sets  map[string]*ReplicaSet // serverName -> replica set
	tools map[string]string      // prefixedToolName -> serverName
}

// NewRouter creates a new tool router.
func NewRouter() *Router {
	return &Router{
		sets:  make(map[string]*ReplicaSet),
		tools: make(map[string]string),
	}
}

// AddClient adds a client to the router as a single-replica set.
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

// RemoveClient removes a server (replica set) and its tools from the router.
func (r *Router) RemoveClient(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sets, name)

	// Remove tools for this server
	for tool, server := range r.tools {
		if server == name {
			delete(r.tools, tool)
		}
	}
}

// GetClient returns one client for the named server, chosen by the set's
// dispatch policy. Returns nil if the server is not registered or no replica
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

// GetReplicaSet returns the replica set for the named server, or nil if the
// server is not registered. Useful for callers that need per-replica access
// (health monitor, status reporting).
func (r *Router) GetReplicaSet(name string) *ReplicaSet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sets[name]
}

// Clients returns one representative AgentClient per registered server,
// sorted by server name. Each representative is chosen via the set's policy,
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

// ReplicaSets returns all registered replica sets, sorted by server name.
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
// the same tool surface, so reading from replica-0 is sufficient. When every
// replica has been reaped (e.g. scale-to-zero), falls back to the set's tool
// cache so clients still see the tool surface and can trigger a cold-start.
func toolsOf(set *ReplicaSet) []Tool {
	reps := set.Replicas()
	if len(reps) == 0 {
		return set.CachedTools()
	}
	tools := reps[0].Client().Tools()
	// Opportunistically refresh the cache on every successful read so a
	// subsequent scale-to-zero serves the most recent tool surface.
	if len(tools) > 0 {
		set.SetToolCache(tools)
	}
	return tools
}

// RefreshTools updates the tool registry from all servers.
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

// HasTool reports whether a prefixed name routes to a live aggregated tool.
// Group alias resolution uses it to arbitrate between a real tool and an
// alias-built form sharing the same name.
func (r *Router) HasTool(prefixedName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[prefixedName]
	return ok
}

// AggregatedTools returns all tools from all servers with prefixed names.
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
				Name:         prefixedName,
				Title:        prefixedName,
				Description:  fmt.Sprintf("MCP server: %s. Call using the exact tool name %q. %s", name, prefixedName, tool.Description),
				InputSchema:  tool.InputSchema,
				OutputSchema: tool.OutputSchema,
				Annotations:  tool.Annotations,
			}
			tools = append(tools, prefixedTool)
		}
	}
	return tools
}

// CatalogTools returns the full downstream tool inventory for informational
// (web console) use: prefixed names with each tool's own raw description and
// input schema. Unlike AggregatedTools it does not wrap descriptions with
// call-routing instructions, so consumers get the tool's documentation
// verbatim. Callers use this to surface tool detail regardless of code mode.
func (r *Router) CatalogTools() []Tool {
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
			tools = append(tools, Tool{
				Name:         PrefixTool(name, tool.Name),
				Title:        tool.Title,
				Description:  tool.Description,
				InputSchema:  tool.InputSchema,
				OutputSchema: tool.OutputSchema,
				Annotations:  tool.Annotations,
			})
		}
	}
	return tools
}

// RouteToolCall routes a tool call to the appropriate server. The concrete
// replica is chosen by the set's dispatch policy.
func (r *Router) RouteToolCall(prefixedName string) (AgentClient, string, error) {
	replica, toolName, err := r.RouteToolCallReplica(prefixedName)
	if err != nil {
		return nil, "", err
	}
	return replica.Client(), toolName, nil
}

// RouteToolCallReplica behaves like RouteToolCall but returns the chosen
// replica itself. Callers that need the replica id (for per-replica logging,
// tracing, and in-flight accounting) should use this variant.
func (r *Router) RouteToolCallReplica(prefixedName string) (*Replica, string, error) {
	serverName, toolName, err := ParsePrefixedTool(prefixedName)
	if err != nil {
		return nil, "", err
	}

	r.mu.RLock()
	set, ok := r.sets[serverName]
	r.mu.RUnlock()
	if !ok {
		return nil, "", fmt.Errorf("unknown server: %s", serverName)
	}

	replica, err := set.Pick()
	if err != nil {
		return nil, "", fmt.Errorf("server %s: %w", serverName, err)
	}
	return replica, toolName, nil
}

// ToolNameDelimiter is the separator between server name and tool name in prefixed tool names.
// Format: "servername__toolname"
// Uses double underscore to be compatible with Claude Desktop's tool name validation: ^[a-zA-Z0-9_-]{1,64}$
const ToolNameDelimiter = "__"

// PrefixTool creates a prefixed tool name: "server__tool"
func PrefixTool(serverName, toolName string) string {
	return serverName + ToolNameDelimiter + toolName
}

// ParsePrefixedTool parses a prefixed tool name into server and tool names.
func ParsePrefixedTool(prefixed string) (serverName, toolName string, err error) {
	parts := strings.SplitN(prefixed, ToolNameDelimiter, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid tool name format: %s (expected server__tool)", prefixed)
	}
	return parts[0], parts[1], nil
}
