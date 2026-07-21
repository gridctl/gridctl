package mcp

import (
	"fmt"
	"sort"
	"strings"
)

// GroupOverrideSpec is the config-agnostic form of one tool override inside
// a group. Hint pointers follow the annotations contract: nil passes the
// downstream value through, a set value overrides it.
type GroupOverrideSpec struct {
	Name            string // exposure-layer rename (flat, no "__")
	Description     string // verbatim replacement; empty keeps the original
	ReadOnlyHint    *bool
	DestructiveHint *bool
	IdempotentHint  *bool
	OpenWorldHint   *bool
}

// GroupSpec is the config-agnostic description of one `groups:` entry. The
// controller builds these from stack.yaml without pkg/mcp importing
// pkg/config (the ClientAccessSpec pattern).
type GroupSpec struct {
	Description string
	Servers     []string
	Tools       []string
	Exclude     []string
	Overrides   map[string]GroupOverrideSpec // keyed by canonical prefixed name
}

// GroupsSpec is the whole block, keyed by group name.
type GroupsSpec map[string]GroupSpec

// groupDef is the resolved, read-optimized form of one group. Membership is
// declarative and matched at read time against whatever tools the router
// currently aggregates, so servers that connect late join their groups
// without a policy rebuild (the clientProfile.allowsTool model).
type groupDef struct {
	name        string
	description string
	servers     map[string]bool
	tools       map[string]bool
	exclude     map[string]bool
	overrides   map[string]GroupOverrideSpec // canonical -> override
	alias       map[string]string            // exposed rename -> canonical
}

// isMember reports whether the canonical prefixed tool belongs to the group.
func (d *groupDef) isMember(prefixed string) bool {
	if d.exclude[prefixed] {
		return false
	}
	if d.tools[prefixed] {
		return true
	}
	server, _, err := ParsePrefixedTool(prefixed)
	return err == nil && d.servers[server]
}

// GroupPolicy is the compiled `groups:` block: the exposure-layer curation
// axis. A nil *GroupPolicy means no block is configured; every method is
// nil-safe. Renames and rewrites exist ONLY here — dispatch, scoping,
// limits, pins, and telemetry always operate on canonical names, with
// ResolveAlias translating inbound calls at the dispatch boundary.
type GroupPolicy struct {
	groups map[string]*groupDef
}

// NewGroupPolicy compiles a spec. A nil or empty spec returns a nil policy
// (no group endpoints, legacy behavior).
func NewGroupPolicy(spec GroupsSpec) *GroupPolicy {
	if len(spec) == 0 {
		return nil
	}
	p := &GroupPolicy{groups: make(map[string]*groupDef, len(spec))}
	for name, g := range spec {
		def := &groupDef{
			name:        name,
			description: g.Description,
			servers:     make(map[string]bool, len(g.Servers)),
			tools:       make(map[string]bool, len(g.Tools)),
			exclude:     make(map[string]bool, len(g.Exclude)),
			overrides:   make(map[string]GroupOverrideSpec, len(g.Overrides)),
			alias:       make(map[string]string),
		}
		for _, s := range g.Servers {
			def.servers[s] = true
		}
		for _, t := range g.Tools {
			def.tools[t] = true
		}
		for _, t := range g.Exclude {
			def.exclude[t] = true
		}
		for canonical, ov := range g.Overrides {
			def.overrides[canonical] = ov
			if ov.Name != "" {
				def.alias[ov.Name] = canonical
			}
		}
		p.groups[name] = def
	}
	return p
}

// Has reports whether a group with the given name is configured. Used by
// the transport to 404 unknown group endpoints before session creation.
func (p *GroupPolicy) Has(name string) bool {
	if p == nil {
		return false
	}
	_, ok := p.groups[name]
	return ok
}

// Names returns the configured group names, sorted.
func (p *GroupPolicy) Names() []string {
	if p == nil {
		return nil
	}
	names := make([]string, 0, len(p.groups))
	for n := range p.groups {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// FilterAndRewrite narrows tools to the group's members and applies its
// overrides. Input tools carry canonical prefixed names (and are already
// client-scoped); output is the exposure surface: renamed names, rewritten
// descriptions, and merged annotations. Tools and annotations are copied
// before mutation so the router's cached definitions are never touched.
// An unknown group yields an empty (non-nil) slice: a session bound to a
// removed group sees nothing rather than everything.
func (p *GroupPolicy) FilterAndRewrite(group string, tools []Tool) []Tool {
	return p.filterAndRewrite(group, tools, false)
}

// FilterAndRewritePrefixed is FilterAndRewrite with renames exposed in
// server-prefixed form ("alpha__shout" instead of "shout"). The code-mode
// universe uses it: the sandbox's ACL and its callTool(server, tool)
// construction both require names that split into a server half, and the
// prefixed alias is exactly what a model reading the surface reconstructs.
func (p *GroupPolicy) FilterAndRewritePrefixed(group string, tools []Tool) []Tool {
	return p.filterAndRewrite(group, tools, true)
}

func (p *GroupPolicy) filterAndRewrite(group string, tools []Tool, prefixAliases bool) []Tool {
	out := []Tool{}
	if p == nil {
		return out
	}
	def, ok := p.groups[group]
	if !ok {
		return out
	}
	for _, tool := range tools {
		if !def.isMember(tool.Name) {
			continue
		}
		ov, hasOverride := def.overrides[tool.Name]
		if !hasOverride {
			out = append(out, tool)
			continue
		}
		rewritten := tool
		if ov.Name != "" {
			exposed := ov.Name
			if prefixAliases {
				if server, _, err := ParsePrefixedTool(tool.Name); err == nil {
					exposed = PrefixTool(server, ov.Name)
				}
			}
			// The aggregated description embeds the canonical name in its
			// call-routing wrapper ("Call using the exact tool name ..."),
			// so a rename must not leave the old name as the instruction.
			// Only the quoted form is patched: a bare substring replacement
			// would corrupt mentions of sibling tools whose names contain
			// this one ("github__create_issue_comment").
			rewritten.Description = strings.ReplaceAll(rewritten.Description,
				fmt.Sprintf("%q", tool.Name), fmt.Sprintf("%q", exposed))
			rewritten.Name = exposed
			if rewritten.Title == tool.Name {
				rewritten.Title = exposed
			}
		}
		if ov.Description != "" {
			rewritten.Description = ov.Description
		}
		rewritten.Annotations = mergeAnnotations(tool.Annotations, ov)
		out = append(out, rewritten)
	}
	return out
}

// mergeAnnotations lays override hints over the downstream annotations:
// set pointers win, nil pointers pass the downstream value through. Returns
// nil when neither side declares anything.
func mergeAnnotations(downstream *ToolAnnotations, ov GroupOverrideSpec) *ToolAnnotations {
	merged := downstream.Clone()
	if ov.ReadOnlyHint == nil && ov.DestructiveHint == nil && ov.IdempotentHint == nil && ov.OpenWorldHint == nil {
		return merged
	}
	if merged == nil {
		merged = &ToolAnnotations{}
	}
	if ov.ReadOnlyHint != nil {
		merged.ReadOnlyHint = ov.ReadOnlyHint
	}
	if ov.DestructiveHint != nil {
		merged.DestructiveHint = ov.DestructiveHint
	}
	if ov.IdempotentHint != nil {
		merged.IdempotentHint = ov.IdempotentHint
	}
	if ov.OpenWorldHint != nil {
		merged.OpenWorldHint = ov.OpenWorldHint
	}
	return merged
}

// ResolveAlias translates an inbound tool name on a group session to its
// canonical prefixed form. exists reports whether a prefixed name routes to
// a live aggregated tool (the gateway passes Router.HasTool); it arbitrates
// between a real tool and an alias-built form sharing the same name.
//
// Accepted forms, in order: an override's exposed rename ("create_issue"),
// a LIVE canonical member name ("github__create_issue", kept callable so
// code-mode sandbox calls constructed as server__tool keep working), a
// server-prefixed rename ("github__find_code" built by the sandbox from a
// renamed tool's alias, applied only when no live tool holds that literal
// name so an alias can never shadow a real sibling), and finally a
// declarative member that is not yet live (its call will fail routing with
// a clear unknown-tool error rather than being denied by the group).
// ok=false means the call must be denied: the name is not part of this
// group's surface.
func (p *GroupPolicy) ResolveAlias(group, name string, exists func(string) bool) (string, bool) {
	if p == nil {
		return "", false
	}
	def, ok := p.groups[group]
	if !ok {
		return "", false
	}
	if canonical, isAlias := def.alias[name]; isAlias {
		return canonical, true
	}
	if exists != nil && exists(name) && def.isMember(name) {
		return name, true
	}
	if server, tail, err := ParsePrefixedTool(name); err == nil {
		if canonical, isAlias := def.alias[tail]; isAlias {
			if cs, _, cerr := ParsePrefixedTool(canonical); cerr == nil && cs == server {
				return canonical, true
			}
		}
	}
	if def.isMember(name) {
		return name, true
	}
	return "", false
}

// GroupsReport is the full GET /api/groups payload, shared by the API
// handler and the `gridctl groups` CLI so the wire shape is defined once.
type GroupsReport struct {
	Configured bool          `json:"configured"`
	Groups     []GroupStatus `json:"groups"`
}

// GroupStatus is one group's resolved snapshot for GET /api/groups and
// `gridctl groups`.
type GroupStatus struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Endpoint    string `json:"endpoint"`
	MemberCount int    `json:"member_count"`
	// Tools are the exposed (post-rewrite) tool names, sorted.
	Tools []string `json:"tools"`
	// Overrides maps canonical member names to their exposed names for
	// renamed tools, and to "" for description/annotation-only overrides.
	Overrides map[string]string `json:"overrides,omitempty"`
}

// Status resolves every group against the given live tool surface.
func (p *GroupPolicy) Status(liveTools []Tool) []GroupStatus {
	if p == nil {
		return []GroupStatus{}
	}
	out := make([]GroupStatus, 0, len(p.groups))
	for _, name := range p.Names() {
		def := p.groups[name]
		exposed := p.FilterAndRewrite(name, liveTools)
		toolNames := make([]string, 0, len(exposed))
		for _, t := range exposed {
			toolNames = append(toolNames, t.Name)
		}
		sort.Strings(toolNames)
		overrides := make(map[string]string, len(def.overrides))
		for canonical, ov := range def.overrides {
			overrides[canonical] = ov.Name
		}
		out = append(out, GroupStatus{
			Name:        name,
			Description: def.description,
			Endpoint:    fmt.Sprintf("/groups/%s/mcp", name),
			MemberCount: len(exposed),
			Tools:       toolNames,
			Overrides:   overrides,
		})
	}
	return out
}

// GroupsRewritingTool returns the names of groups whose overrides rewrite
// the description of the given canonical tool. The pins drift surfaces use
// this to flag rewrites that may have gone stale against a drifted
// upstream definition.
func (p *GroupPolicy) GroupsRewritingTool(canonical string) []string {
	if p == nil {
		return nil
	}
	var names []string
	for _, name := range p.Names() {
		def := p.groups[name]
		if ov, ok := def.overrides[canonical]; ok && ov.Description != "" && def.isMember(canonical) {
			names = append(names, name)
		}
	}
	return names
}

// RenamedOriginals returns, per group, the map of canonical member names to
// exposed renames. The controller's skill-reference lint uses it to warn
// when an active skill still references a renamed tool's original name.
func (p *GroupPolicy) RenamedOriginals() map[string]map[string]string {
	if p == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(p.groups))
	for name, def := range p.groups {
		renames := make(map[string]string)
		for canonical, ov := range def.overrides {
			if ov.Name != "" {
				renames[canonical] = ov.Name
			}
		}
		if len(renames) > 0 {
			out[name] = renames
		}
	}
	return out
}
