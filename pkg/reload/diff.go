package reload

import (
	"github.com/gridctl/gridctl/pkg/config"
)

// ConfigDiff represents the differences between two stack configurations.
type ConfigDiff struct {
	MCPServers MCPServerDiff
	Resources  ResourceDiff
	// NetworkChanged indicates if the network config changed (requires full restart)
	NetworkChanged bool
}

// MCPServerDiff contains changes to MCP servers.
type MCPServerDiff struct {
	Added    []config.MCPServer
	Removed  []config.MCPServer
	Modified []MCPServerChange
	// AutoscalePolicyChanges lists servers whose autoscale block fields
	// changed but whose other config is stable. The reload handler applies
	// these via Autoscaler.UpdatePolicy without restarting the server.
	AutoscalePolicyChanges []MCPServerChange
}

// MCPServerChange represents a modification to an existing MCP server.
type MCPServerChange struct {
	Name string
	Old  config.MCPServer
	New  config.MCPServer
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
		len(d.MCPServers.AutoscalePolicyChanges) == 0 &&
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
			continue
		}
		if mcpServerEqual(oldServer, newServer) {
			continue
		}
		// Autoscale-only policy updates are applied in-place without
		// restarting the server so in-flight tool calls are not disrupted.
		// Switching between autoscale and static replicas is a full restart.
		if isAutoscalePolicyOnlyChange(oldServer, newServer) {
			diff.AutoscalePolicyChanges = append(diff.AutoscalePolicyChanges, MCPServerChange{
				Name: newServer.Name,
				Old:  oldServer,
				New:  newServer,
			})
			continue
		}
		diff.Modified = append(diff.Modified, MCPServerChange{
			Name: newServer.Name,
			Old:  oldServer,
			New:  newServer,
		})
	}

	// Find removed
	for _, oldServer := range oldServers {
		if _, exists := newMap[oldServer.Name]; !exists {
			diff.Removed = append(diff.Removed, oldServer)
		}
	}

	return diff
}

// isAutoscalePolicyOnlyChange reports whether the only difference between two
// server configs is inside the autoscale block. Transitions between static
// replicas and autoscale always return false so those are restarted cleanly.
func isAutoscalePolicyOnlyChange(oldServer, newServer config.MCPServer) bool {
	// Both must already be autoscaled; switching in/out is a restart.
	if oldServer.Autoscale == nil || newServer.Autoscale == nil {
		return false
	}
	// Ignore autoscale deltas while comparing everything else.
	oldCopy := oldServer
	newCopy := newServer
	oldCopy.Autoscale = nil
	newCopy.Autoscale = nil
	if !mcpServerEqual(oldCopy, newCopy) {
		return false
	}
	// Only an autoscale change remains — and we already know the configs
	// differ overall, so the autoscale block must carry the diff.
	return !autoscaleEqual(oldServer.Autoscale, newServer.Autoscale)
}

func autoscaleEqual(a, b *config.AutoscaleConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Compare resolved durations so YAML strings that parse to the same
	// duration ("30s" vs "30000ms") don't trigger a spurious policy update.
	return a.Min == b.Min &&
		a.Max == b.Max &&
		a.TargetInFlight == b.TargetInFlight &&
		a.ResolvedScaleUpAfter() == b.ResolvedScaleUpAfter() &&
		a.ResolvedScaleDownAfter() == b.ResolvedScaleDownAfter() &&
		a.WarmPool == b.WarmPool &&
		a.IdleToZero == b.IdleToZero
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
	// Compare the autoscale block so transitions between static replicas and
	// autoscale (or field changes inside an existing autoscale block) surface
	// here. Static replicas count / policy are intentionally NOT compared to
	// preserve pre-existing hot-reload behavior.
	if !autoscaleEqual(a.Autoscale, b.Autoscale) {
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

