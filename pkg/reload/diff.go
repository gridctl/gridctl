package reload

import (
	"maps"
	"reflect"

	"github.com/gridctl/gridctl/pkg/config"
)

// ConfigDiff represents the differences between two stack configurations.
type ConfigDiff struct {
	MCPServers MCPServerDiff
	Resources  ResourceDiff
	// NetworkChanged indicates if the network config changed (requires full restart)
	NetworkChanged bool
	// ClientsChanged indicates the per-client access (`clients:`) block changed.
	// It needs an in-memory policy refresh (via the reload's onConfigApplied hook)
	// but no container or network work, so it must still mark the diff non-empty.
	ClientsChanged bool
	// LimitsChanged indicates the rate-limit (`limits:`) block
	// changed. Like ClientsChanged it needs only an in-memory policy rebuild
	// via the onConfigApplied hook (in-memory bucket state rebuilds) but must still mark the diff non-empty.
	LimitsChanged bool
	// PolicyChanged indicates the per-tool call (`policy:`) block changed.
	// Like LimitsChanged it needs only an in-memory gate rebuild via the
	// onConfigApplied hook and must still mark the diff non-empty. A
	// policy-only edit does not recreate containers.
	PolicyChanged bool
	// ToolDiscoveryChanged indicates the gateway.tool_discovery block
	// changed (mode and always_expose, order-sensitive). Like PolicyChanged
	// it needs only an in-memory refresh via the onConfigApplied hook and
	// must still mark the diff non-empty. A discovery-only edit does not
	// recreate containers.
	ToolDiscoveryChanged bool
	// GroupsChanged indicates the tool-group (`groups:`) block changed.
	// Like ClientsChanged it needs only an in-memory policy rebuild via the
	// onConfigApplied hook but must still mark the diff non-empty.
	GroupsChanged bool
	// ExperimentalChanged indicates the `experimental:` flag map changed.
	// Like ClientsChanged it needs only an in-memory flag re-resolution via
	// the onConfigApplied hook but must still mark the diff non-empty.
	ExperimentalChanged bool
	// SkillsPolicyChanged indicates the skill exposure (`skills:`) block
	// changed. Like ClientsChanged it needs only an in-memory policy rebuild
	// via the onConfigApplied hook but must still mark the diff non-empty.
	SkillsPolicyChanged bool
	// ModelPreferencesChanged indicates the `model_preferences:` block
	// changed. Like ClientsChanged it needs only the compiled projection
	// model policy swapped via the onConfigApplied hook (the next
	// projection reconcile applies it); no containers are touched.
	ModelPreferencesChanged bool
	// RunsChanged indicates the `runs:` block changed. Like ClientsChanged
	// it needs only the in-process recorder swapped via onConfigApplied;
	// no containers are touched.
	RunsChanged bool
}

// MCPServerDiff contains changes to MCP servers.
type MCPServerDiff struct {
	Added    []config.MCPServer
	Removed  []config.MCPServer
	Modified []MCPServerChange
	// Disabled lists servers whose enabled flag flipped true to false.
	// Other field edits on the same flip stay here: the server is retired
	// and is not restarted, and pins are kept.
	Disabled []config.MCPServer
	// Enabled lists servers whose enabled flag flipped false to true.
	// They follow the Added start path.
	Enabled []config.MCPServer
	// Dormant lists servers that are disabled on both sides and whose other
	// fields differ. No runtime work runs. A server added while disabled is
	// also recorded here (Old.Name empty) so a non-initial reload still
	// swaps currentCfg; initial load strips those entries.
	Dormant []MCPServerChange
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
	// Disabled, Enabled, and Dormant mirror MCPServerDiff for resources.
	Disabled []config.Resource
	Enabled  []config.Resource
	Dormant  []ResourceChange
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
		len(d.MCPServers.Disabled) == 0 &&
		len(d.MCPServers.Enabled) == 0 &&
		len(d.MCPServers.Dormant) == 0 &&
		len(d.MCPServers.AutoscalePolicyChanges) == 0 &&
		len(d.Resources.Added) == 0 &&
		len(d.Resources.Removed) == 0 &&
		len(d.Resources.Modified) == 0 &&
		len(d.Resources.Disabled) == 0 &&
		len(d.Resources.Enabled) == 0 &&
		len(d.Resources.Dormant) == 0 &&
		!d.NetworkChanged &&
		!d.ClientsChanged &&
		!d.LimitsChanged &&
		!d.PolicyChanged &&
		!d.ToolDiscoveryChanged &&
		!d.GroupsChanged &&
		!d.ExperimentalChanged &&
		!d.SkillsPolicyChanged &&
		!d.ModelPreferencesChanged &&
		!d.RunsChanged
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

	// Detect per-client access (`clients:`) changes
	diff.ClientsChanged = clientsChanged(old, new)

	// Detect rate-limit (`limits:`) changes
	diff.LimitsChanged = limitsChanged(old, new)

	// Detect per-tool call policy (`policy:`) changes
	diff.PolicyChanged = policyChanged(old, new)

	// Detect gateway.tool_discovery changes. Order-sensitive; no container work.
	diff.ToolDiscoveryChanged = toolDiscoveryChanged(old, new)

	// Detect tool-group (`groups:`) changes
	diff.GroupsChanged = groupsChanged(old, new)

	// Detect experimental flag (`experimental:`) changes
	diff.ExperimentalChanged = experimentalChanged(old, new)

	// Detect skill exposure policy (`skills:`) changes
	diff.SkillsPolicyChanged = skillsPolicyChanged(old, new)

	// Detect projection model preference (`model_preferences:`) changes
	diff.ModelPreferencesChanged = modelPreferencesChanged(old, new)

	diff.RunsChanged = runsChanged(old, new)

	return diff
}

func toolDiscoveryChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(toolDiscoveryOf(old), toolDiscoveryOf(new))
}

func toolDiscoveryOf(s *config.Stack) *config.ToolDiscoveryConfig {
	if s == nil || s.Gateway == nil {
		return nil
	}
	return s.Gateway.ToolDiscovery
}

func runsChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(old.Runs, new.Runs)
}

// modelPreferencesChanged reports whether the `model_preferences:` block
// differs between two stacks. A change needs only the compiled projection
// model policy swapped (via the reload's onConfigApplied hook); no
// containers are touched. DeepEqual handles the nil-to-set transitions
// directly.
func modelPreferencesChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(old.ModelPreferences, new.ModelPreferences)
}

// skillsPolicyChanged reports whether the `skills:` block differs between two
// stacks. A change needs only the gateway's in-memory skill policy rebuilt
// (via the reload's onConfigApplied hook); no containers are touched.
// DeepEqual handles the nil-to-set transitions directly.
func skillsPolicyChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(old.Skills, new.Skills)
}

// experimentalChanged reports whether the `experimental:` flag map differs
// between two stacks. A change needs only the resolved flag set rebuilt (via
// the reload's onConfigApplied hook); no containers are touched. maps.Equal
// handles the nil-to-set transitions directly.
func experimentalChanged(old, new *config.Stack) bool {
	return !maps.Equal(old.Experimental, new.Experimental)
}

// groupsChanged reports whether the `groups:` block differs between two
// stacks. A change needs only the gateway's in-memory group policy rebuilt
// (via the reload's onConfigApplied hook); no containers are touched.
func groupsChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(old.Groups, new.Groups)
}

// limitsChanged reports whether the `limits:` block differs between two
// stacks. A change needs only the gateway's in-memory limits policy rebuilt
// (via the reload's onConfigApplied hook); no containers are touched.
// DeepEqual handles the nil-to-set transitions directly.
func limitsChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(old.Limits, new.Limits)
}

// policyChanged reports whether the `policy:` block differs between two
// stacks. A change needs only the gateway's in-memory call gates rebuilt
// (via the reload's onConfigApplied hook); no containers are touched.
// DeepEqual handles the nil-to-set transitions directly. The helpers in
// pkg/config/equality.go compare servers and sources and do not apply here.
func policyChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(old.Policy, new.Policy)
}

// clientsChanged reports whether the per-client access (`clients:`) block
// differs between two stacks. A change here requires the gateway's in-memory
// ClientAccessPolicy to be rebuilt (via the reload's onConfigApplied hook) but
// touches no containers, networks, or resources. DeepEqual handles the nil↔set
// transitions (block added or removed) directly.
func clientsChanged(old, new *config.Stack) bool {
	return !reflect.DeepEqual(old.Clients, new.Clients)
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

	// Find added and modified. An enable flip is classified before field
	// equality so a disable is never a Modified restart and never resets pins.
	for _, newServer := range newServers {
		oldServer, exists := oldMap[newServer.Name]
		if !exists {
			if newServer.IsEnabled() {
				diff.Added = append(diff.Added, newServer)
			} else {
				diff.Dormant = append(diff.Dormant, MCPServerChange{Name: newServer.Name, New: newServer})
			}
			continue
		}
		oldOn, newOn := oldServer.IsEnabled(), newServer.IsEnabled()
		switch {
		case oldOn && !newOn:
			diff.Disabled = append(diff.Disabled, oldServer)
			continue
		case !oldOn && newOn:
			diff.Enabled = append(diff.Enabled, newServer)
			continue
		case !oldOn && !newOn:
			if !mcpServerEqual(oldServer, newServer) {
				diff.Dormant = append(diff.Dormant, MCPServerChange{
					Name: newServer.Name,
					Old:  oldServer,
					New:  newServer,
				})
			}
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

	// Find added and modified. Enable flips are not Added or Modified.
	for _, newRes := range newResources {
		oldRes, exists := oldMap[newRes.Name]
		if !exists {
			if newRes.IsEnabled() {
				diff.Added = append(diff.Added, newRes)
			} else {
				diff.Dormant = append(diff.Dormant, ResourceChange{Name: newRes.Name, New: newRes})
			}
			continue
		}
		oldOn, newOn := oldRes.IsEnabled(), newRes.IsEnabled()
		switch {
		case oldOn && !newOn:
			diff.Disabled = append(diff.Disabled, oldRes)
			continue
		case !oldOn && newOn:
			diff.Enabled = append(diff.Enabled, newRes)
			continue
		case !oldOn && !newOn:
			if !resourceEqual(oldRes, newRes) {
				diff.Dormant = append(diff.Dormant, ResourceChange{
					Name: newRes.Name,
					Old:  oldRes,
					New:  newRes,
				})
			}
			continue
		}
		if !resourceEqual(oldRes, newRes) {
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
	return config.MCPServerEqual(a, b)
}

// resourceEqual checks if two resource configs are equivalent.
func resourceEqual(a, b config.Resource) bool {
	// enabled: true and an omitted key are the same state.
	if a.Name != b.Name || a.Image != b.Image || a.Network != b.Network || a.IsEnabled() != b.IsEnabled() {
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
