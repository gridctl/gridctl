package mcp

import "sort"

// ClientProfileSpec is the config-agnostic description of one client's
// allow-list. The controller builds these from the stack.yaml `clients:` block
// without pkg/mcp needing to import pkg/config.
//
// Servers and Tools are allow-lists. An empty Servers list means "all servers";
// an empty Tools list means "all tools within the allowed servers". Tools are
// matched against the router's prefixed names (e.g. "github__search-repos").
// Aliases are raw clientInfo.name values that should resolve to this profile,
// letting an operator reconcile a wire identity that differs from the profile
// key without depending on the built-in NormalizeClientID alias table.
type ClientProfileSpec struct {
	Aliases []string
	Servers []string
	Tools   []string
}

// ClientAccessSpec is the config-agnostic description of the whole `clients:`
// block. Default is the policy applied to clients that match no profile:
// "allow" exposes everything, any other value (including the empty string)
// denies. A nil *ClientAccessSpec means no block was configured at all.
type ClientAccessSpec struct {
	Default  string
	Profiles map[string]ClientProfileSpec
}

// clientProfile is the resolved, read-optimized form of a ClientProfileSpec.
type clientProfile struct {
	servers map[string]bool // allowed server names; empty = all servers
	tools   map[string]bool // allowed prefixed tool names; empty = all tools within servers
}

// allowsTool reports whether the given prefixed tool name is permitted by this
// profile. An unparseable name is denied: a profiled client only reaches tools
// that route to a known server.
func (p clientProfile) allowsTool(prefixedName string) bool {
	server, _, err := ParsePrefixedTool(prefixedName)
	if err != nil {
		return false
	}
	if len(p.servers) > 0 && !p.servers[server] {
		return false
	}
	if len(p.tools) > 0 && !p.tools[prefixedName] {
		return false
	}
	return true
}

// ClientAccessPolicy is the resolved per-client tool access filter applied at
// every gateway exposure path (tools/list, tools/call, and the code-mode tool
// universe). It is a read-time filter modeled on the per-server tool whitelist
// in client_base.go, keyed on the connecting client's stable access identifier.
//
// A nil *ClientAccessPolicy means no `clients:` block was configured: every
// client sees every tool (legacy behavior, Article IX). A non-nil policy
// applies NetworkPolicy semantics — a client matching no profile is governed by
// the default (deny unless `default: allow`).
type ClientAccessPolicy struct {
	defaultAllow bool
	profiles     map[string]clientProfile // keyed by normalized access id
	aliasIndex   map[string]string        // normalized alias -> profile key
}

// NewClientAccessPolicy builds a policy from a spec. A nil spec returns a nil
// policy (legacy "everyone sees everything"). Profile keys and aliases are
// normalized via NormalizeClientID so the configured identifier, the wire
// identity, and the UI identifier reconcile on a single canonical form.
func NewClientAccessPolicy(spec *ClientAccessSpec) *ClientAccessPolicy {
	if spec == nil {
		return nil
	}
	p := &ClientAccessPolicy{
		defaultAllow: spec.Default == "allow",
		profiles:     make(map[string]clientProfile, len(spec.Profiles)),
		aliasIndex:   make(map[string]string),
	}
	for name, prof := range spec.Profiles {
		key := NormalizeClientID(name)
		cp := clientProfile{
			servers: make(map[string]bool, len(prof.Servers)),
			tools:   make(map[string]bool, len(prof.Tools)),
		}
		for _, s := range prof.Servers {
			cp.servers[s] = true
		}
		for _, t := range prof.Tools {
			cp.tools[t] = true
		}
		p.profiles[key] = cp
		for _, alias := range prof.Aliases {
			if na := NormalizeClientID(alias); na != "" {
				p.aliasIndex[na] = key
			}
		}
	}
	return p
}

// resolveKey maps an access identifier to a profile key. It returns the key and
// whether an explicit profile exists for the client. Resolution order: direct
// profile match, then a configured alias, then the normalized id itself (which
// will not be listed and so falls to the default policy).
func (p *ClientAccessPolicy) resolveKey(accessID string) (key string, listed bool) {
	n := NormalizeClientID(accessID)
	if _, ok := p.profiles[n]; ok {
		return n, true
	}
	if k, ok := p.aliasIndex[n]; ok {
		return k, true
	}
	return n, false
}

// Allows reports whether the client identified by accessID may call the given
// prefixed tool. A nil policy allows everything.
func (p *ClientAccessPolicy) Allows(accessID, prefixedName string) bool {
	if p == nil {
		return true
	}
	key, listed := p.resolveKey(accessID)
	if !listed {
		return p.defaultAllow
	}
	return p.profiles[key].allowsTool(prefixedName)
}

// Filter returns the subset of tools visible to the client identified by
// accessID. A nil policy returns the tools unchanged.
func (p *ClientAccessPolicy) Filter(accessID string, tools []Tool) []Tool {
	if p == nil {
		return tools
	}
	key, listed := p.resolveKey(accessID)
	if !listed {
		if p.defaultAllow {
			return tools
		}
		return nil
	}
	prof := p.profiles[key]
	filtered := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if prof.allowsTool(t.Name) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ClientScopeResult reports a client's backend-computed effective scope: the
// servers and prefixed tool names it can actually reach after intersecting its
// allow-list with the live tool surface. It backs the per-client scope the
// topology/clients API surfaces.
type ClientScopeResult struct {
	// Configured reports whether a `clients:` block exists at all.
	Configured bool `json:"configured"`
	// Unscoped reports that the client reaches the full tool surface (no block,
	// or default-allow for an unlisted client, or a profile with no restriction).
	Unscoped bool `json:"unscoped"`
	// Servers are the reachable server names, sorted.
	Servers []string `json:"servers"`
	// Tools are the reachable prefixed tool names, sorted.
	Tools []string `json:"tools"`
}

// scopeResult computes the effective scope for accessID against a fully-known
// tool surface. allTools is the global (unscoped) catalog of prefixed tools.
func (p *ClientAccessPolicy) scopeResult(accessID string, allTools []Tool) ClientScopeResult {
	res := ClientScopeResult{Configured: p != nil}
	visible := p.Filter(accessID, allTools)

	// Unscoped when the policy did not narrow the surface.
	res.Unscoped = p == nil || len(visible) == len(allTools)

	serverSet := make(map[string]bool, len(visible))
	tools := make([]string, 0, len(visible))
	for _, t := range visible {
		tools = append(tools, t.Name)
		if server, _, err := ParsePrefixedTool(t.Name); err == nil {
			serverSet[server] = true
		}
	}
	servers := make([]string, 0, len(serverSet))
	for s := range serverSet {
		servers = append(servers, s)
	}
	sort.Strings(servers)
	sort.Strings(tools)
	res.Servers = servers
	res.Tools = tools
	return res
}
