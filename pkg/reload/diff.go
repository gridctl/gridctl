package reload

import (
	"github.com/gridctl/gridctl/pkg/config"
)

// ConfigDiff represents the differences between two stack configurations.
type ConfigDiff struct {
	MCPServers MCPServerDiff
	Agents     AgentDiff
	Resources  ResourceDiff
	// NetworkChanged indicates if the network config changed (requires full restart)
	NetworkChanged bool
}

// MCPServerDiff contains changes to MCP servers.
type MCPServerDiff struct {
	Added    []config.MCPServer
	Removed  []config.MCPServer
	Modified []MCPServerChange
}

// MCPServerChange represents a modification to an existing MCP server.
type MCPServerChange struct {
	Name string
	Old  config.MCPServer
	New  config.MCPServer
}

// AgentDiff contains changes to agents.
type AgentDiff struct {
	Added    []config.Agent
	Removed  []config.Agent
	Modified []AgentChange
}

// AgentChange represents a modification to an existing agent.
type AgentChange struct {
	Name string
	Old  config.Agent
	New  config.Agent
}

// ResourceDiff contains changes to resources.
type ResourceDiff struct {
	Added    []config.Resource
	Removed  []config.Resource
	Modified []ResourceChange
}

// ResourceChange represents a modification to an existing resource.
type ResourceChange struct {
	Name string
	Old  config.Resource
	New  config.Resource
}

// IsEmpty returns true if there are no changes.
func (d *ConfigDiff) IsEmpty() bool {
	return len(d.MCPServers.Added) == 0 &&
		len(d.MCPServers.Removed) == 0 &&
		len(d.MCPServers.Modified) == 0 &&
		len(d.Agents.Added) == 0 &&
		len(d.Agents.Removed) == 0 &&
		len(d.Agents.Modified) == 0 &&
		len(d.Resources.Added) == 0 &&
		len(d.Resources.Removed) == 0 &&
		len(d.Resources.Modified) == 0 &&
		!d.NetworkChanged
}

// ComputeDiff computes the differences between two stack configurations.
func ComputeDiff(old, new *config.Stack) *ConfigDiff {
	diff := &ConfigDiff{}

	// Check network changes
	diff.NetworkChanged = isNetworkChanged(old, new)

	// Diff MCP servers
	diff.MCPServers = diffMCPServers(old.MCPServers, new.MCPServers)

	// Diff agents
	diff.Agents = diffAgents(old.Agents, new.Agents)

	// Diff resources
	diff.Resources = diffResources(old.Resources, new.Resources)

	return diff
}

func isNetworkChanged(old, new *config.Stack) bool {
	// Compare simple network mode
	if old.Network.Name != new.Network.Name || old.Network.Driver != new.Network.Driver {
		return true
	}

	// Compare advanced network mode
	if len(old.Networks) != len(new.Networks) {
		return true
	}

	oldNets := make(map[string]config.Network)
	for _, n := range old.Networks {
		oldNets[n.Name] = n
	}

	for _, n := range new.Networks {
		oldNet, ok := oldNets[n.Name]
		if !ok || oldNet.Driver != n.Driver {
			return true
		}
	}

	return false
}

func diffMCPServers(oldServers, newServers []config.MCPServer) MCPServerDiff {
	diff := MCPServerDiff{}

	oldMap := make(map[string]config.MCPServer)
	for _, s := range oldServers {
		oldMap[s.Name] = s
	}

	newMap := make(map[string]config.MCPServer)
	for _, s := range newServers {
		newMap[s.Name] = s
	}

	// Find added and modified
	for _, newServer := range newServers {
		oldServer, exists := oldMap[newServer.Name]
		if !exists {
			diff.Added = append(diff.Added, newServer)
		} else if !mcpServerEqual(oldServer, newServer) {
			diff.Modified = append(diff.Modified, MCPServerChange{
				Name: newServer.Name,
				Old:  oldServer,
				New:  newServer,
			})
		}
	}

	// Find removed
	for _, oldServer := range oldServers {
		if _, exists := newMap[oldServer.Name]; !exists {
			diff.Removed = append(diff.Removed, oldServer)
		}
	}

	return diff
}

func diffAgents(oldAgents, newAgents []config.Agent) AgentDiff {
	diff := AgentDiff{}

	oldMap := make(map[string]config.Agent)
	for _, a := range oldAgents {
		oldMap[a.Name] = a
	}

	newMap := make(map[string]config.Agent)
	for _, a := range newAgents {
		newMap[a.Name] = a
	}

	// Find added and modified
	for _, newAgent := range newAgents {
		oldAgent, exists := oldMap[newAgent.Name]
		if !exists {
			diff.Added = append(diff.Added, newAgent)
		} else if !agentEqual(oldAgent, newAgent) {
			diff.Modified = append(diff.Modified, AgentChange{
				Name: newAgent.Name,
				Old:  oldAgent,
				New:  newAgent,
			})
		}
	}

	// Find removed
	for _, oldAgent := range oldAgents {
		if _, exists := newMap[oldAgent.Name]; !exists {
			diff.Removed = append(diff.Removed, oldAgent)
		}
	}

	return diff
}

func diffResources(oldResources, newResources []config.Resource) ResourceDiff {
	diff := ResourceDiff{}

	oldMap := make(map[string]config.Resource)
	for _, r := range oldResources {
		oldMap[r.Name] = r
	}

	newMap := make(map[string]config.Resource)
	for _, r := range newResources {
		newMap[r.Name] = r
	}

	// Find added and modified
	for _, newRes := range newResources {
		oldRes, exists := oldMap[newRes.Name]
		if !exists {
			diff.Added = append(diff.Added, newRes)
		} else if !resourceEqual(oldRes, newRes) {
			diff.Modified = append(diff.Modified, ResourceChange{
				Name: newRes.Name,
				Old:  oldRes,
				New:  newRes,
			})
		}
	}

	// Find removed
	for _, oldRes := range oldResources {
		if _, exists := newMap[oldRes.Name]; !exists {
			diff.Removed = append(diff.Removed, oldRes)
		}
	}

	return diff
}

// mcpServerEqual checks if two MCP server configs are equivalent.
func mcpServerEqual(a, b config.MCPServer) bool {
	// Compare basic fields
	if a.Name != b.Name || a.Image != b.Image || a.Port != b.Port ||
		a.Transport != b.Transport || a.URL != b.URL || a.Network != b.Network {
		return false
	}

	// Compare commands
	if !stringSliceEqual(a.Command, b.Command) {
		return false
	}

	// Compare tools whitelist
	if !stringSliceEqual(a.Tools, b.Tools) {
		return false
	}

	// Compare env maps
	if !stringMapEqual(a.Env, b.Env) {
		return false
	}

	// Compare source configs
	if !sourceEqual(a.Source, b.Source) {
		return false
	}

	// Compare SSH config
	if !sshEqual(a.SSH, b.SSH) {
		return false
	}

	// Compare OpenAPI config
	if !openAPIEqual(a.OpenAPI, b.OpenAPI) {
		return false
	}

	return true
}

// agentEqual checks if two agent configs are equivalent.
func agentEqual(a, b config.Agent) bool {
	if a.Name != b.Name || a.Image != b.Image ||
		a.Description != b.Description || a.Network != b.Network {
		return false
	}

	if !stringSliceEqual(a.Command, b.Command) {
		return false
	}

	if !stringMapEqual(a.Env, b.Env) {
		return false
	}

	if !sourceEqual(a.Source, b.Source) {
		return false
	}

	// Compare uses (tool selectors) - order independent
	if !toolSelectorsEqual(a.Uses, b.Uses) {
		return false
	}

	// Compare capabilities
	if !stringSliceEqual(a.Capabilities, b.Capabilities) {
		return false
	}

	// Compare build args
	if !stringMapEqual(a.BuildArgs, b.BuildArgs) {
		return false
	}

	// Compare A2A config
	if !a2aEqual(a.A2A, b.A2A) {
		return false
	}

	return true
}

// resourceEqual checks if two resource configs are equivalent.
func resourceEqual(a, b config.Resource) bool {
	if a.Name != b.Name || a.Image != b.Image || a.Network != b.Network {
		return false
	}

	if !stringMapEqual(a.Env, b.Env) {
		return false
	}

	if !stringSliceEqual(a.Volumes, b.Volumes) {
		return false
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

func stringMapEqual(a, b map[string]string) bool {
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

// toolSelectorsEqual compares tool selectors in an order-independent way.
func toolSelectorsEqual(a, b []config.ToolSelector) bool {
	if len(a) != len(b) {
		return false
	}

	// Build map of a's selectors
	aMap := make(map[string][]string)
	for _, s := range a {
		aMap[s.Server] = s.Tools
	}

	// Check all of b's selectors exist in a with same tools
	for _, s := range b {
		aTools, ok := aMap[s.Server]
		if !ok {
			return false
		}
		if !stringSliceSetEqual(aTools, s.Tools) {
			return false
		}
	}

	return true
}

// stringSliceSetEqual compares two string slices as sets (order-independent).
func stringSliceSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSet := make(map[string]bool)
	for _, s := range a {
		aSet[s] = true
	}
	for _, s := range b {
		if !aSet[s] {
			return false
		}
	}
	return true
}

func sourceEqual(a, b *config.Source) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Type == b.Type && a.URL == b.URL && a.Ref == b.Ref &&
		a.Path == b.Path && a.Dockerfile == b.Dockerfile
}

func sshEqual(a, b *config.SSHConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Host == b.Host && a.User == b.User &&
		a.Port == b.Port && a.IdentityFile == b.IdentityFile
}

func openAPIEqual(a, b *config.OpenAPIConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Spec != b.Spec || a.BaseURL != b.BaseURL {
		return false
	}
	// Compare auth
	if (a.Auth == nil) != (b.Auth == nil) {
		return false
	}
	if a.Auth != nil && b.Auth != nil {
		if a.Auth.Type != b.Auth.Type || a.Auth.TokenEnv != b.Auth.TokenEnv ||
			a.Auth.Header != b.Auth.Header || a.Auth.ValueEnv != b.Auth.ValueEnv {
			return false
		}
	}
	// Compare operations
	if (a.Operations == nil) != (b.Operations == nil) {
		return false
	}
	if a.Operations != nil && b.Operations != nil {
		if !stringSliceEqual(a.Operations.Include, b.Operations.Include) ||
			!stringSliceEqual(a.Operations.Exclude, b.Operations.Exclude) {
			return false
		}
	}
	return true
}

func a2aEqual(a, b *config.A2AConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Enabled != b.Enabled || a.Version != b.Version {
		return false
	}
	// Compare skills - order independent
	if len(a.Skills) != len(b.Skills) {
		return false
	}
	aSkills := make(map[string]config.A2ASkill)
	for _, s := range a.Skills {
		aSkills[s.ID] = s
	}
	for _, bSkill := range b.Skills {
		aSkill, ok := aSkills[bSkill.ID]
		if !ok {
			return false
		}
		if aSkill.Name != bSkill.Name || aSkill.Description != bSkill.Description {
			return false
		}
		if !stringSliceSetEqual(aSkill.Tags, bSkill.Tags) {
			return false
		}
	}
	return true
}
