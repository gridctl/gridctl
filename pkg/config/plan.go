package config

import (
	"fmt"
	"strings"
)

// DiffAction describes what changed.
type DiffAction string

const (
	DiffAdd    DiffAction = "add"
	DiffRemove DiffAction = "remove"
	DiffChange DiffAction = "change"
)

// DiffItem represents a single change in the plan.
type DiffItem struct {
	Action   DiffAction `json:"action"`
	Kind     string     `json:"kind"`     // "mcp-server", "agent", "resource", "a2a-agent", "gateway", "network"
	Name     string     `json:"name"`
	Details  []string   `json:"details,omitempty"` // human-readable change descriptions
}

// PlanDiff is the complete diff between two stack specs.
type PlanDiff struct {
	HasChanges bool       `json:"hasChanges"`
	Items      []DiffItem `json:"items"`
	Summary    string     `json:"summary"`
}

// ComputePlan compares a new spec against the currently running spec and returns a structured diff.
func ComputePlan(proposed, current *Stack) *PlanDiff {
	diff := &PlanDiff{}

	if proposed == nil {
		proposed = &Stack{}
	}
	if current == nil {
		current = &Stack{}
	}

	// Compare MCP servers
	diffNamedItems(diff, "mcp-server", mcpServerMap(proposed), mcpServerMap(current), compareMCPServers)

	// Compare agents
	diffNamedItems(diff, "agent", agentMap(proposed), agentMap(current), compareAgents)

	// Compare resources
	diffNamedItems(diff, "resource", resourceMap(proposed), resourceMap(current), compareResources)

	// Compare A2A agents
	diffNamedItems(diff, "a2a-agent", a2aAgentMap(proposed), a2aAgentMap(current), compareA2AAgents)

	// Compare gateway config
	diffGateway(diff, proposed.Gateway, current.Gateway)

	// Compare network config
	diffNetworks(diff, proposed, current)

	diff.HasChanges = len(diff.Items) > 0
	diff.Summary = buildSummary(diff.Items)

	return diff
}

// diffNamedItems compares two maps of named items and produces diff items.
func diffNamedItems[T any](diff *PlanDiff, kind string, proposed, current map[string]T, compare func(a, b T) []string) {
	// Find added items
	for name := range proposed {
		if _, exists := current[name]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action: DiffAdd,
				Kind:   kind,
				Name:   name,
			})
		}
	}

	// Find removed items
	for name := range current {
		if _, exists := proposed[name]; !exists {
			diff.Items = append(diff.Items, DiffItem{
				Action: DiffRemove,
				Kind:   kind,
				Name:   name,
			})
		}
	}

	// Find changed items
	for name, p := range proposed {
		if c, exists := current[name]; exists {
			if details := compare(p, c); len(details) > 0 {
				diff.Items = append(diff.Items, DiffItem{
					Action:  DiffChange,
					Kind:    kind,
					Name:    name,
					Details: details,
				})
			}
		}
	}
}

// Map builders

func mcpServerMap(s *Stack) map[string]MCPServer {
	m := make(map[string]MCPServer, len(s.MCPServers))
	for _, srv := range s.MCPServers {
		m[srv.Name] = srv
	}
	return m
}

func agentMap(s *Stack) map[string]Agent {
	m := make(map[string]Agent, len(s.Agents))
	for _, a := range s.Agents {
		m[a.Name] = a
	}
	return m
}

func resourceMap(s *Stack) map[string]Resource {
	m := make(map[string]Resource, len(s.Resources))
	for _, r := range s.Resources {
		m[r.Name] = r
	}
	return m
}

func a2aAgentMap(s *Stack) map[string]A2AAgent {
	m := make(map[string]A2AAgent, len(s.A2AAgents))
	for _, a := range s.A2AAgents {
		m[a.Name] = a
	}
	return m
}

// Comparison functions

func compareMCPServers(a, b MCPServer) []string {
	var details []string
	if a.Image != b.Image {
		details = append(details, fmt.Sprintf("image: %s → %s", b.Image, a.Image))
	}
	if a.URL != b.URL {
		details = append(details, fmt.Sprintf("url: %s → %s", b.URL, a.URL))
	}
	if a.Port != b.Port {
		details = append(details, fmt.Sprintf("port: %d → %d", b.Port, a.Port))
	}
	if a.Transport != b.Transport {
		details = append(details, fmt.Sprintf("transport: %s → %s", b.Transport, a.Transport))
	}
	if a.Network != b.Network {
		details = append(details, fmt.Sprintf("network: %s → %s", b.Network, a.Network))
	}
	if a.OutputFormat != b.OutputFormat {
		details = append(details, fmt.Sprintf("output_format: %s → %s", b.OutputFormat, a.OutputFormat))
	}
	if !envEqual(a.Env, b.Env) {
		details = append(details, "env changed")
	}
	if compareSource(a.Source, b.Source) {
		details = append(details, "source changed")
	}
	if compareSSH(a.SSH, b.SSH) {
		details = append(details, "ssh config changed")
	}
	return details
}

func compareAgents(a, b Agent) []string {
	var details []string
	if a.Image != b.Image {
		details = append(details, fmt.Sprintf("image: %s → %s", b.Image, a.Image))
	}
	if a.Runtime != b.Runtime {
		details = append(details, fmt.Sprintf("runtime: %s → %s", b.Runtime, a.Runtime))
	}
	if a.Prompt != b.Prompt {
		details = append(details, "prompt changed")
	}
	if a.Network != b.Network {
		details = append(details, fmt.Sprintf("network: %s → %s", b.Network, a.Network))
	}
	if !envEqual(a.Env, b.Env) {
		details = append(details, "env changed")
	}
	if !usesEqual(a.Uses, b.Uses) {
		details = append(details, "uses changed")
	}
	if compareSource(a.Source, b.Source) {
		details = append(details, "source changed")
	}
	return details
}

func compareResources(a, b Resource) []string {
	var details []string
	if a.Image != b.Image {
		details = append(details, fmt.Sprintf("image: %s → %s", b.Image, a.Image))
	}
	if a.Network != b.Network {
		details = append(details, fmt.Sprintf("network: %s → %s", b.Network, a.Network))
	}
	if !envEqual(a.Env, b.Env) {
		details = append(details, "env changed")
	}
	if !stringSliceEqual(a.Ports, b.Ports) {
		details = append(details, "ports changed")
	}
	if !stringSliceEqual(a.Volumes, b.Volumes) {
		details = append(details, "volumes changed")
	}
	return details
}

func compareA2AAgents(a, b A2AAgent) []string {
	var details []string
	if a.URL != b.URL {
		details = append(details, fmt.Sprintf("url: %s → %s", b.URL, a.URL))
	}
	return details
}

func diffGateway(diff *PlanDiff, proposed, current *GatewayConfig) {
	if proposed == nil && current == nil {
		return
	}
	if proposed == nil && current != nil {
		diff.Items = append(diff.Items, DiffItem{Action: DiffRemove, Kind: "gateway", Name: "gateway"})
		return
	}
	if proposed != nil && current == nil {
		diff.Items = append(diff.Items, DiffItem{Action: DiffAdd, Kind: "gateway", Name: "gateway"})
		return
	}

	var details []string
	if proposed.OutputFormat != current.OutputFormat {
		details = append(details, fmt.Sprintf("output_format: %s → %s", current.OutputFormat, proposed.OutputFormat))
	}
	if proposed.CodeMode != current.CodeMode {
		details = append(details, fmt.Sprintf("code_mode: %s → %s", current.CodeMode, proposed.CodeMode))
	}
	if len(details) > 0 {
		diff.Items = append(diff.Items, DiffItem{Action: DiffChange, Kind: "gateway", Name: "gateway", Details: details})
	}
}

func diffNetworks(diff *PlanDiff, proposed, current *Stack) {
	// Simple mode comparison
	if len(proposed.Networks) == 0 && len(current.Networks) == 0 {
		if proposed.Network.Name != current.Network.Name || proposed.Network.Driver != current.Network.Driver {
			diff.Items = append(diff.Items, DiffItem{
				Action:  DiffChange,
				Kind:    "network",
				Name:    proposed.Network.Name,
				Details: []string{fmt.Sprintf("driver: %s → %s", current.Network.Driver, proposed.Network.Driver)},
			})
		}
		return
	}

	// Advanced mode comparison
	pNets := make(map[string]Network, len(proposed.Networks))
	for _, n := range proposed.Networks {
		pNets[n.Name] = n
	}
	cNets := make(map[string]Network, len(current.Networks))
	for _, n := range current.Networks {
		cNets[n.Name] = n
	}

	for name, pNet := range pNets {
		cNet, exists := cNets[name]
		if !exists {
			diff.Items = append(diff.Items, DiffItem{Action: DiffAdd, Kind: "network", Name: name})
		} else if pNet.Driver != cNet.Driver {
			diff.Items = append(diff.Items, DiffItem{
				Action:  DiffChange,
				Kind:    "network",
				Name:    name,
				Details: []string{fmt.Sprintf("driver: %s → %s", cNet.Driver, pNet.Driver)},
			})
		}
	}
	for name := range cNets {
		if _, exists := pNets[name]; !exists {
			diff.Items = append(diff.Items, DiffItem{Action: DiffRemove, Kind: "network", Name: name})
		}
	}
}

// Helper comparison functions

func envEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func usesEqual(a, b []ToolSelector) bool {
	if len(a) != len(b) {
		return false
	}
	// Build lookup from both sides and compare
	aMap := make(map[string]string, len(a))
	for _, ts := range a {
		aMap[ts.Server] = strings.Join(ts.Tools, ",")
	}
	bMap := make(map[string]string, len(b))
	for _, ts := range b {
		bMap[ts.Server] = strings.Join(ts.Tools, ",")
	}
	for k, v := range aMap {
		if bMap[k] != v {
			return false
		}
	}
	return true
}

func compareSource(a, b *Source) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return a.Type != b.Type || a.URL != b.URL || a.Ref != b.Ref || a.Path != b.Path || a.Dockerfile != b.Dockerfile
}

func compareSSH(a, b *SSHConfig) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return a.Host != b.Host || a.User != b.User || a.Port != b.Port || a.IdentityFile != b.IdentityFile
}

func buildSummary(items []DiffItem) string {
	if len(items) == 0 {
		return "No changes detected."
	}

	adds, removes, changes := 0, 0, 0
	for _, item := range items {
		switch item.Action {
		case DiffAdd:
			adds++
		case DiffRemove:
			removes++
		case DiffChange:
			changes++
		}
	}

	var parts []string
	if adds > 0 {
		parts = append(parts, fmt.Sprintf("%d to add", adds))
	}
	if changes > 0 {
		parts = append(parts, fmt.Sprintf("%d to change", changes))
	}
	if removes > 0 {
		parts = append(parts, fmt.Sprintf("%d to remove", removes))
	}
	return strings.Join(parts, ", ")
}
