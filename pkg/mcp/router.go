package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
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

	// Resource routing. Exact URIs win over the ui:// index, which wins
	// over templates. Winners are the alphabetically first server.
	resources        map[string]string
	uiResources      map[string]string
	templates        []compiledTemplate
	serverURIs       map[string][]string
	serverTemplates  map[string][]string
	serverUI         map[string][]string
	collisions       map[string]int
	loggedCollisions map[string]struct{}
	promptCounts     map[string]int
	resourceCounts   map[string]int
	templateCounts   map[string]int
	listErrors       map[string]string
}

type compiledTemplate struct {
	server  string
	raw     string
	matcher *regexp.Regexp
}

// ResourceCollision is one URI a later server lost to an earlier one.
type ResourceCollision struct {
	URI    string
	Winner string
	Loser  string
}

// ResourceServerStatus is the cached prompt and resource accounting for
// one server. Status reads this; it does not fan out.
type ResourceServerStatus struct {
	PromptCount   int
	ResourceCount int
	TemplateCount int
	Collisions    int
	ListError     string
}

// NewRouter creates a new tool router.
func NewRouter() *Router {
	return &Router{
		sets:             make(map[string]*ReplicaSet),
		tools:            make(map[string]string),
		resources:        make(map[string]string),
		uiResources:      make(map[string]string),
		serverURIs:       make(map[string][]string),
		serverTemplates:  make(map[string][]string),
		serverUI:         make(map[string][]string),
		collisions:       make(map[string]int),
		loggedCollisions: make(map[string]struct{}),
		promptCounts:     make(map[string]int),
		resourceCounts:   make(map[string]int),
		templateCounts:   make(map[string]int),
		listErrors:       make(map[string]string),
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
	r.clearResourceServerLocked(name)
	r.rebuildResourceWinnersLocked()
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
	r.serverUI = make(map[string][]string)

	for name, set := range r.sets {
		var ui []string
		for _, tool := range toolsOf(set) {
			prefixedName := PrefixTool(name, tool.Name)
			r.tools[prefixedName] = name
			ui = append(ui, uiResourceURIs(tool.Meta)...)
		}
		if len(ui) > 0 {
			r.serverUI[name] = ui
		}
	}
	r.rebuildResourceWinnersLocked()
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

// projectTool copies definition fields the gateway must forward unchanged.
// Callers set name, title, and description because aggregation and the
// catalog rewrite those differently. Both copy sites use this helper so a
// new pass-through field cannot land in only one of them.
func projectTool(src Tool) Tool {
	return Tool{
		InputSchema:  src.InputSchema,
		OutputSchema: src.OutputSchema,
		Annotations:  src.Annotations,
		Icons:        src.Icons,
		Execution:    src.Execution,
		Meta:         src.Meta,
		Extra:        src.Extra,
	}
}

// aggregatedToolTitle keeps a downstream display title when it is distinct
// from the bare tool name. An empty title, or a title that only repeats the
// bare name, becomes the prefixed name so clients do not see an unprefixed
// alias. Group renames still match synthesized titles because those equal
// the aggregated name.
func aggregatedToolTitle(prefixedName string, tool Tool) string {
	if tool.Title == "" || tool.Title == tool.Name {
		return prefixedName
	}
	return tool.Title
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
			prefixedTool := projectTool(tool)
			prefixedTool.Name = prefixedName
			prefixedTool.Title = aggregatedToolTitle(prefixedName, tool)
			prefixedTool.Description = fmt.Sprintf("MCP server: %s. Call using the exact tool name %q. %s", name, prefixedName, tool.Description)
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
	return r.catalogTools(toolsOf)
}

// AllCatalogTools is CatalogTools over the pre-whitelist tool set, so the
// management UI can show descriptions, schemas, and annotations for tools an
// operator has disabled (the rows they are deciding whether to re-enable).
// Informational only; never served to MCP clients.
func (r *Router) AllCatalogTools() []Tool {
	return r.catalogTools(allDownstreamToolsOf)
}

// catalogTools is the single copy site both catalog variants share (the
// annotations field-completeness test counts on copies staying centralized).
func (r *Router) catalogTools(source func(*ReplicaSet) []Tool) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.sets))
	for name := range r.sets {
		names = append(names, name)
	}
	sort.Strings(names)

	var tools []Tool
	for _, name := range names {
		for _, tool := range source(r.sets[name]) {
			projected := projectTool(tool)
			projected.Name = PrefixTool(name, tool.Name)
			projected.Title = tool.Title
			projected.Description = tool.Description
			tools = append(tools, projected)
		}
	}
	return tools
}

// allDownstreamToolsOf mirrors toolsOf but ignores any configured whitelist.
// When every replica has been reaped the cache is the only source, and it
// holds the filtered surface, an acceptable degradation for an informational
// read (the full set returns with the next replica start).
func allDownstreamToolsOf(set *ReplicaSet) []Tool {
	reps := set.Replicas()
	if len(reps) == 0 {
		return set.CachedTools()
	}
	return allToolsOf(reps[0].Client())
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

// ServerToolNames returns the prefixed tool names currently aggregated for
// server. Group membership asks isMember on these names so exclude wins.
func (r *Router) ServerToolNames(server string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for prefixed, owner := range r.tools {
		if owner == server {
			names = append(names, prefixed)
		}
	}
	sort.Strings(names)
	return names
}

// SetResourceIndex replaces one server's listed URIs and templates, then
// rebuilds the winner tables. Newly observed collisions are returned so
// the caller can log each (uri, loser) pair once.
func (r *Router) SetResourceIndex(server string, uris, templates []string) []ResourceCollision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureResourceMapsLocked()
	r.storeURIsLocked(server, uris)
	r.storeTemplatesLocked(server, templates)
	return r.rebuildResourceWinnersLocked()
}

// ReplaceServerURIs updates only the concrete URI index for server.
func (r *Router) ReplaceServerURIs(server string, uris []string) []ResourceCollision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureResourceMapsLocked()
	r.storeURIsLocked(server, uris)
	return r.rebuildResourceWinnersLocked()
}

// ReplaceServerTemplates updates only the template index for server.
func (r *Router) ReplaceServerTemplates(server string, templates []string) []ResourceCollision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureResourceMapsLocked()
	r.storeTemplatesLocked(server, templates)
	return r.rebuildResourceWinnersLocked()
}

func (r *Router) storeURIsLocked(server string, uris []string) {
	if len(uris) == 0 {
		delete(r.serverURIs, server)
		return
	}
	r.serverURIs[server] = append([]string(nil), uris...)
}

func (r *Router) storeTemplatesLocked(server string, templates []string) {
	if len(templates) == 0 {
		delete(r.serverTemplates, server)
		return
	}
	r.serverTemplates[server] = append([]string(nil), templates...)
}

// SetUIResourceIndex replaces one server's ui:// index built from tool
// _meta. An empty list clears that server's UI URIs.
func (r *Router) SetUIResourceIndex(server string, uris []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureResourceMapsLocked()
	if len(uris) == 0 {
		delete(r.serverUI, server)
	} else {
		r.serverUI[server] = append([]string(nil), uris...)
	}
	r.rebuildResourceWinnersLocked()
}

// ResolveResource returns the server that owns uri. Exact listed URIs
// win over the ui:// index, which wins over templates.
func (r *Router) ResolveResource(uri string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if server, ok := r.resources[uri]; ok {
		return server, true
	}
	if server, ok := r.uiResources[uri]; ok {
		return server, true
	}
	for _, tmpl := range r.templates {
		if tmpl.matcher != nil && tmpl.matcher.MatchString(uri) {
			return tmpl.server, true
		}
	}
	return "", false
}

// ResourceOwner returns the server that listed uri exactly, if any.
func (r *Router) ResourceOwner(uri string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	server, ok := r.resources[uri]
	return server, ok
}

// TemplateOwner reports whether server's uriTemplate survived collision
// omission. Identical templates keep the alphabetically first server.
func (r *Router) TemplateOwner(server, raw string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, tmpl := range r.templates {
		if tmpl.raw == raw && tmpl.server == server {
			return true
		}
	}
	return false
}

// PickServer chooses a replica for a prompt or resource dispatch, including
// scale-to-zero cold start. Lists must not use this; they skip unhealthy sets.
func (r *Router) PickServer(name string) (*Replica, error) {
	r.mu.RLock()
	set, ok := r.sets[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown server: %s", name)
	}
	replica, err := set.Pick()
	if err != nil {
		return nil, fmt.Errorf("server %s: %w", name, err)
	}
	return replica, nil
}

// SetPromptCount records the last successful prompts/list size for server.
func (r *Router) SetPromptCount(server string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureResourceMapsLocked()
	r.promptCounts[server] = n
}

// SetResourceListError records the last resources list failure category.
// An empty message clears it.
func (r *Router) SetResourceListError(server, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureResourceMapsLocked()
	if msg == "" {
		delete(r.listErrors, server)
		return
	}
	r.listErrors[server] = msg
}

// ResourceStatus returns cached counts. It does not contact downstream servers.
func (r *Router) ResourceStatus(server string) ResourceServerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return ResourceServerStatus{
		PromptCount:   r.promptCounts[server],
		ResourceCount: r.resourceCounts[server],
		TemplateCount: r.templateCounts[server],
		Collisions:    r.collisions[server],
		ListError:     r.listErrors[server],
	}
}

func (r *Router) ensureResourceMapsLocked() {
	if r.resources == nil {
		r.resources = make(map[string]string)
	}
	if r.uiResources == nil {
		r.uiResources = make(map[string]string)
	}
	if r.serverURIs == nil {
		r.serverURIs = make(map[string][]string)
	}
	if r.serverTemplates == nil {
		r.serverTemplates = make(map[string][]string)
	}
	if r.serverUI == nil {
		r.serverUI = make(map[string][]string)
	}
	if r.collisions == nil {
		r.collisions = make(map[string]int)
	}
	if r.loggedCollisions == nil {
		r.loggedCollisions = make(map[string]struct{})
	}
	if r.promptCounts == nil {
		r.promptCounts = make(map[string]int)
	}
	if r.resourceCounts == nil {
		r.resourceCounts = make(map[string]int)
	}
	if r.templateCounts == nil {
		r.templateCounts = make(map[string]int)
	}
	if r.listErrors == nil {
		r.listErrors = make(map[string]string)
	}
}

func (r *Router) clearResourceServerLocked(name string) {
	r.ensureResourceMapsLocked()
	delete(r.serverURIs, name)
	delete(r.serverTemplates, name)
	delete(r.serverUI, name)
	delete(r.collisions, name)
	delete(r.promptCounts, name)
	delete(r.resourceCounts, name)
	delete(r.templateCounts, name)
	delete(r.listErrors, name)
	suffix := "\x00" + name
	for key := range r.loggedCollisions {
		if strings.HasSuffix(key, suffix) {
			delete(r.loggedCollisions, key)
		}
	}
}

func (r *Router) rebuildResourceWinnersLocked() []ResourceCollision {
	r.ensureResourceMapsLocked()
	r.resources = make(map[string]string)
	r.uiResources = make(map[string]string)
	r.templates = nil
	r.collisions = make(map[string]int)
	var fresh []ResourceCollision

	uriOwners := map[string][]string{}
	for server, uris := range r.serverURIs {
		for _, uri := range uris {
			uriOwners[uri] = append(uriOwners[uri], server)
		}
	}
	for uri, owners := range uriOwners {
		uniq := uniqueSorted(owners)
		if len(uniq) == 0 {
			continue
		}
		r.resources[uri] = uniq[0]
		for _, loser := range uniq[1:] {
			r.collisions[loser]++
			fresh = append(fresh, r.noteCollisionLocked(uri, uniq[0], loser)...)
		}
	}

	tmplOwners := map[string][]string{}
	for server, tmpls := range r.serverTemplates {
		for _, raw := range tmpls {
			tmplOwners[raw] = append(tmplOwners[raw], server)
		}
	}
	for raw, owners := range tmplOwners {
		uniq := uniqueSorted(owners)
		if len(uniq) == 0 {
			continue
		}
		if matcher, err := compileURITemplate(raw); err == nil {
			r.templates = append(r.templates, compiledTemplate{server: uniq[0], raw: raw, matcher: matcher})
		}
		for _, loser := range uniq[1:] {
			r.collisions[loser]++
			fresh = append(fresh, r.noteCollisionLocked("template:"+raw, uniq[0], loser)...)
		}
	}
	sort.Slice(r.templates, func(i, j int) bool {
		if r.templates[i].server == r.templates[j].server {
			return r.templates[i].raw < r.templates[j].raw
		}
		return r.templates[i].server < r.templates[j].server
	})

	uiOwners := map[string][]string{}
	for server, uris := range r.serverUI {
		for _, uri := range uris {
			uiOwners[uri] = append(uiOwners[uri], server)
		}
	}
	for uri, owners := range uiOwners {
		if _, exact := r.resources[uri]; exact {
			continue
		}
		uniq := uniqueSorted(owners)
		if len(uniq) == 0 {
			continue
		}
		r.uiResources[uri] = uniq[0]
		for _, loser := range uniq[1:] {
			r.collisions[loser]++
			fresh = append(fresh, r.noteCollisionLocked(uri, uniq[0], loser)...)
		}
	}
	r.recomputeResourceCountsLocked()
	return fresh
}

func (r *Router) recomputeResourceCountsLocked() {
	nextRes := make(map[string]int, len(r.serverURIs))
	nextTmpl := make(map[string]int, len(r.serverTemplates))
	for server := range r.serverURIs {
		nextRes[server] = r.winningURICountLocked(server)
	}
	for server := range r.serverTemplates {
		nextTmpl[server] = r.winningTemplateCountLocked(server)
	}
	for server := range r.resourceCounts {
		if _, ok := nextRes[server]; !ok {
			nextRes[server] = 0
		}
	}
	for server := range r.templateCounts {
		if _, ok := nextTmpl[server]; !ok {
			nextTmpl[server] = 0
		}
	}
	r.resourceCounts = nextRes
	r.templateCounts = nextTmpl
}

func (r *Router) noteCollisionLocked(uri, winner, loser string) []ResourceCollision {
	key := uri + "\x00" + loser
	if _, seen := r.loggedCollisions[key]; seen {
		return nil
	}
	r.loggedCollisions[key] = struct{}{}
	return []ResourceCollision{{URI: uri, Winner: winner, Loser: loser}}
}

func (r *Router) winningURICountLocked(server string) int {
	n := 0
	for _, owner := range r.resources {
		if owner == server {
			n++
		}
	}
	return n
}

func (r *Router) winningTemplateCountLocked(server string) int {
	n := 0
	for _, tmpl := range r.templates {
		if tmpl.server == server {
			n++
		}
	}
	return n
}

func uniqueSorted(owners []string) []string {
	if len(owners) == 0 {
		return nil
	}
	sort.Strings(owners)
	out := owners[:0]
	var prev string
	for i, owner := range owners {
		if i > 0 && owner == prev {
			continue
		}
		out = append(out, owner)
		prev = owner
	}
	return out
}

// compileURITemplate implements RFC 6570 level-1 matching: {var} is one
// path segment and {+var} crosses segments. Literal segments are quoted
// so metacharacters are not patterns. The match is anchored at both ends.
func compileURITemplate(tmpl string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '{' {
			j := i
			for j < len(tmpl) && tmpl[j] != '{' {
				j++
			}
			b.WriteString(regexp.QuoteMeta(tmpl[i:j]))
			i = j
			continue
		}
		end := strings.IndexByte(tmpl[i:], '}')
		if end < 0 {
			b.WriteString(regexp.QuoteMeta(tmpl[i:]))
			break
		}
		expr := tmpl[i+1 : i+end]
		if strings.HasPrefix(expr, "+") {
			b.WriteString(".+")
		} else {
			b.WriteString("[^/]+")
		}
		i += end + 1
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// uiResourceURIs reads SEP-1865 resource URIs from a tool's _meta. The
// index is a no-op when _meta is absent.
func uiResourceURIs(meta json.RawMessage) []string {
	if len(meta) == 0 || string(meta) == "null" {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(meta, &obj); err != nil {
		return nil
	}
	var uris []string
	if raw, ok := obj["ui"]; ok {
		var ui map[string]json.RawMessage
		if json.Unmarshal(raw, &ui) == nil {
			if uri := jsonString(ui["resourceUri"]); strings.HasPrefix(uri, "ui://") {
				uris = append(uris, uri)
			}
		}
	}
	if uri := jsonString(obj["ui/resourceUri"]); strings.HasPrefix(uri, "ui://") {
		uris = append(uris, uri)
	}
	return uris
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
