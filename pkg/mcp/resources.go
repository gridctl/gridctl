package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// listFanoutTimeout bounds one prompt or resource list fan-out. A single
// slow server delays the aggregate at most this long. It is not configurable.
const listFanoutTimeout = 10 * time.Second

// inputRequiredRelayMessage is the handshake-era explanation when a
// stateless downstream read returns resultType input_required. It follows
// the tools/call wording in streamable.go.
const inputRequiredRelayMessage = "resource requires additional input via MRTR (2026-07-28), which this session's protocol generation cannot relay; use a client that speaks the stateless generation"

// ResourceNotFoundError is a gateway-originated resources/read miss.
// The handshake edge maps it to -32002 and the stateless edge to -32602,
// both with data.uri. It is indistinguishable from an out-of-scope URI.
type ResourceNotFoundError struct {
	URI     string
	Message string
}

func (e *ResourceNotFoundError) Error() string {
	if e == nil {
		return "resource not found"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.URI == "" {
		return "resource not found"
	}
	return "resource not found: " + e.URI
}

// DownstreamRelayError is a downstream read or get failure. An RPC error
// is returned with its original code. A timeout or transport failure names
// the server and a category, never payload bytes.
type DownstreamRelayError struct {
	Server   string
	Category string
	RPC      *RPCError
}

func (e *DownstreamRelayError) Error() string {
	if e == nil {
		return "downstream error"
	}
	switch e.Category {
	case "timeout":
		return fmt.Sprintf("downstream server %s: timeout", e.Server)
	case "canceled":
		return fmt.Sprintf("downstream server %s: canceled", e.Server)
	default:
		return fmt.Sprintf("downstream server %s: transport error", e.Server)
	}
}

// relayResult is a verbatim JSON result plus the stateless-edge cache
// decision. passThrough means the raw bytes already carry the downstream
// stateless fields and must not be rewritten.
type relayResult struct {
	raw           json.RawMessage
	passThrough   bool
	inputRequired bool
	fields        StatelessResultFields
}

func notFound(uri, msg string) error {
	return &ResourceNotFoundError{URI: uri, Message: msg}
}

func classifyDownstreamErr(server string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &DownstreamRelayError{Server: server, Category: "timeout"}
	}
	if errors.Is(err, context.Canceled) {
		return &DownstreamRelayError{Server: server, Category: "canceled"}
	}
	if rpc := rpcErrorFrom(err); rpc != nil {
		return &DownstreamRelayError{Server: server, Category: "rpc", RPC: rpc}
	}
	return &DownstreamRelayError{Server: server, Category: "transport"}
}

func errorCategory(err error) string {
	var de *DownstreamRelayError
	if errors.As(err, &de) && de.Category != "" {
		return de.Category
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if rpcErrorFrom(err) != nil {
		return "rpc"
	}
	return "transport"
}

// serverVisible is the single chokepoint for downstream prompt and resource
// access. Registry entries do not pass through it. A nil client policy allows
// every server. Group membership reuses groupDef.isMember so exclude wins.
func (g *Gateway) serverVisible(ctx context.Context, server string) bool {
	if !g.clientAccessPolicy().AllowsServer(ClientAccessIDFromContext(ctx), server) {
		return false
	}
	if group := GroupFromContext(ctx); group != "" {
		if !g.CurrentGroupPolicy().ServerIsMember(group, server, g.router.ServerToolNames(server)) {
			return false
		}
	}
	return true
}

type listedPage struct {
	name     string
	page     RawListPage
	err      error
	category string
}

func (g *Gateway) fanOutList(ctx context.Context, method string, want func(Capabilities) bool, list func(context.Context, AgentClient) (RawListPage, error)) []listedPage {
	ctx, cancel := context.WithTimeout(ctx, listFanoutTimeout)
	defer cancel()

	for _, set := range g.router.ReplicaSets() {
		if set.Client() == nil {
			g.logger.Debug("skipping scale-to-zero server in list fan-out", "server", set.Name(), "method", method)
		}
	}

	var targets []AgentClient
	for _, client := range g.router.Clients() {
		if client.Name() == "registry" {
			continue
		}
		src, ok := client.(interface{ DownstreamCapabilities() Capabilities })
		if !ok || !want(src.DownstreamCapabilities()) {
			continue
		}
		targets = append(targets, client)
	}

	out := make([]listedPage, len(targets))
	if len(targets) == 0 {
		return nil
	}
	done := make(chan struct{}, len(targets))
	for i, client := range targets {
		go func(i int, client AgentClient) {
			defer func() { done <- struct{}{} }()
			page, err := list(ctx, client)
			item := listedPage{name: client.Name(), page: page}
			if err != nil {
				item.err = err
				item.category = errorCategory(err)
				g.logger.Warn("downstream list failed", "server", client.Name(), "method", method, "category", item.category)
			}
			out[i] = item
		}(i, client)
	}
	for range targets {
		<-done
	}
	return out
}

func wantsResources(c Capabilities) bool { return c.Resources != nil }
func wantsPrompts(c Capabilities) bool   { return c.Prompts != nil }

func (g *Gateway) listResourcesOf(ctx context.Context, client AgentClient) (RawListPage, error) {
	lister, ok := client.(ResourceLister)
	if !ok {
		return RawListPage{}, fmt.Errorf("transport does not list resources")
	}
	return lister.ListResources(ctx)
}

func (g *Gateway) listTemplatesOf(ctx context.Context, client AgentClient) (RawListPage, error) {
	lister, ok := client.(ResourceLister)
	if !ok {
		return RawListPage{}, fmt.Errorf("transport does not list resources")
	}
	return lister.ListResourceTemplates(ctx)
}

func (g *Gateway) listPromptsOf(ctx context.Context, client AgentClient) (RawListPage, error) {
	lister, ok := client.(PromptLister)
	if !ok {
		return RawListPage{}, fmt.Errorf("transport does not list prompts")
	}
	return lister.ListPrompts(ctx)
}

func (g *Gateway) logCollisions(fresh []ResourceCollision) {
	for _, c := range fresh {
		g.logger.Warn("resource URI collision", "uri", c.URI, "server", c.Loser, "winner", c.Winner)
	}
}

func (g *Gateway) applyResourcePages(resources, templates []listedPage) {
	failed := map[string]string{}
	for _, item := range resources {
		if item.err != nil {
			failed[item.name] = item.category
			g.router.SetResourceListError(item.name, item.category)
			continue
		}
		g.logCollisions(g.router.ReplaceServerURIs(item.name, resourceURIs(item.page.Entries)))
	}
	for _, item := range templates {
		if item.err != nil {
			if _, ok := failed[item.name]; !ok {
				failed[item.name] = item.category
				g.router.SetResourceListError(item.name, item.category)
			}
			continue
		}
		g.logCollisions(g.router.ReplaceServerTemplates(item.name, templateURIs(item.page.Entries)))
	}
	for _, item := range append(append([]listedPage{}, resources...), templates...) {
		if item.err == nil {
			if _, still := failed[item.name]; !still {
				g.router.SetResourceListError(item.name, "")
			}
		}
	}
}

func resourceURIs(entries []json.RawMessage) []string {
	var uris []string
	for _, raw := range entries {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		if uri := jsonString(obj["uri"]); uri != "" {
			uris = append(uris, uri)
		}
	}
	return uris
}

func templateURIs(entries []json.RawMessage) []string {
	var uris []string
	for _, raw := range entries {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		if uri := jsonString(obj["uriTemplate"]); uri != "" {
			uris = append(uris, uri)
		}
	}
	return uris
}

// HandleResourcesList returns registry skill resources followed by downstream
// resources with unchanged URIs. An upstream cursor is ignored.
func (g *Gateway) HandleResourcesList(ctx context.Context) (*RawResourcesListResult, error) {
	registry, registryContributed := g.registryResourceEntries()
	pages := g.fanOutList(ctx, "resources/list", wantsResources, g.listResourcesOf)
	g.applyResourcePages(pages, nil)

	entries := append([]json.RawMessage{}, registry...)
	var contributing []RawListPage
	for _, item := range pages {
		if item.err != nil || !g.serverVisible(ctx, item.name) {
			continue
		}
		kept := g.visibleResourceEntries(item.name, item.page.Entries)
		if len(kept) == 0 {
			continue
		}
		entries = append(entries, kept...)
		page := item.page
		page.Entries = kept
		contributing = append(contributing, page)
	}
	if entries == nil {
		entries = []json.RawMessage{}
	}
	result := &RawResourcesListResult{Resources: entries}
	result.StatelessResultFields = aggregateRawListCacheMeta(registryContributed, contributing)
	return result, nil
}

// HandleResourceTemplatesList returns downstream templates. The registry
// contributes none. Losing identical templates are omitted.
func (g *Gateway) HandleResourceTemplatesList(ctx context.Context) *RawResourceTemplatesListResult {
	pages := g.fanOutList(ctx, "resources/templates/list", wantsResources, g.listTemplatesOf)
	g.applyResourcePages(nil, pages)

	var entries []json.RawMessage
	var contributing []RawListPage
	for _, item := range pages {
		if item.err != nil || !g.serverVisible(ctx, item.name) {
			continue
		}
		kept := g.visibleTemplateEntries(item.name, item.page.Entries)
		if len(kept) == 0 {
			continue
		}
		entries = append(entries, kept...)
		page := item.page
		page.Entries = kept
		contributing = append(contributing, page)
	}
	if entries == nil {
		entries = []json.RawMessage{}
	}
	result := &RawResourceTemplatesListResult{ResourceTemplates: entries}
	result.StatelessResultFields = aggregateRawListCacheMeta(false, contributing)
	return result
}

func (g *Gateway) visibleResourceEntries(server string, entries []json.RawMessage) []json.RawMessage {
	var kept []json.RawMessage
	for _, raw := range entries {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		uri := jsonString(obj["uri"])
		if uri == "" {
			continue
		}
		owner, ok := g.router.ResourceOwner(uri)
		if ok && owner == server {
			kept = append(kept, raw)
		}
	}
	return kept
}

func (g *Gateway) visibleTemplateEntries(server string, entries []json.RawMessage) []json.RawMessage {
	var kept []json.RawMessage
	for _, raw := range entries {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		uri := jsonString(obj["uriTemplate"])
		if uri == "" {
			continue
		}
		if g.router.TemplateOwner(server, uri) {
			kept = append(kept, raw)
		}
	}
	return kept
}

func (g *Gateway) registryResourceEntries() ([]json.RawMessage, bool) {
	pp := g.promptProvider()
	if pp == nil {
		return nil, false
	}
	policy := g.CurrentSkillPolicy()
	var entries []json.RawMessage
	for _, p := range pp.ListPromptData() {
		if !policy.Allows(p.Name) {
			continue
		}
		raw, err := json.Marshal(MCPResource{
			URI:         "skills://registry/" + p.Name,
			Name:        p.Name,
			Description: p.Description,
			MimeType:    "text/markdown",
		})
		if err != nil {
			continue
		}
		entries = append(entries, raw)
	}
	return entries, len(entries) > 0
}

// HandleResourcesRead resolves a URI and relays the downstream contents
// verbatim. Registry URIs keep their current behavior. The raw params, when
// present, are forwarded unchanged.
func (g *Gateway) HandleResourcesRead(ctx context.Context, params ResourcesReadParams) (json.RawMessage, error) {
	outcome, err := g.relayResourcesRead(ctx, params, nil)
	if err != nil {
		return nil, err
	}
	return outcome.raw, nil
}

func (g *Gateway) relayResourcesRead(ctx context.Context, params ResourcesReadParams, rawParams json.RawMessage) (relayResult, error) {
	if isRegistryResourceURI(params.URI) {
		return g.readRegistryResource(params.URI)
	}
	server, ok := g.resolveResource(ctx, params.URI)
	if !ok || !g.serverVisible(ctx, server) {
		return relayResult{}, notFound(params.URI, "")
	}
	return g.relayToServer(ctx, server, "resources/read", rawParams, map[string]string{"uri": params.URI})
}

func isRegistryResourceURI(uri string) bool {
	return strings.HasPrefix(uri, "skills://registry/") || strings.HasPrefix(uri, "prompt://")
}

func (g *Gateway) readRegistryResource(uri string) (relayResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return relayResult{}, notFound(uri, "registry not available")
	}
	name := strings.TrimPrefix(uri, "skills://registry/")
	if name == uri {
		name = strings.TrimPrefix(uri, "prompt://")
	}
	if name == "" {
		return relayResult{}, notFound(uri, fmt.Sprintf("empty resource name in URI: %s", uri))
	}
	if !g.CurrentSkillPolicy().Allows(name) {
		return relayResult{}, notFound(uri, fmt.Sprintf("skill %q not found", name))
	}
	p, err := pp.GetPromptData(name)
	if err != nil {
		return relayResult{}, notFound(uri, fmt.Sprintf("skill %q not found", name))
	}
	raw, err := json.Marshal(ResourcesReadResult{
		Contents: []ResourceContents{{
			URI:      uri,
			MimeType: "text/markdown",
			Text:     p.Content,
		}},
	})
	if err != nil {
		return relayResult{}, err
	}
	var fields StatelessResultFields
	attachSkillCacheMeta(&fields)
	return relayResult{raw: raw, fields: fields}, nil
}

func (g *Gateway) resolveResource(ctx context.Context, uri string) (string, bool) {
	if server, ok := g.router.ResolveResource(uri); ok {
		return server, true
	}
	g.refreshResourceIndex(ctx)
	return g.router.ResolveResource(uri)
}

func (g *Gateway) refreshResourceIndex(ctx context.Context) {
	pages := g.fanOutList(ctx, "resources/list", wantsResources, g.listResourcesOf)
	templates := g.fanOutList(ctx, "resources/templates/list", wantsResources, g.listTemplatesOf)
	g.applyResourcePages(pages, templates)
}

func (g *Gateway) relayToServer(ctx context.Context, server, method string, rawParams json.RawMessage, fallback any) (relayResult, error) {
	replica, err := g.router.PickServer(server)
	if err != nil {
		return relayResult{}, notFound("", "")
	}
	relayer, ok := replica.Client().(rawRelayer)
	if !ok {
		return relayResult{}, &DownstreamRelayError{Server: server, Category: "transport"}
	}
	ctx, span := otel.Tracer("gridctl.gateway").Start(ctx, method)
	span.SetAttributes(attribute.String("mcp.server", server))
	defer span.End()

	params := rawParams
	if len(params) == 0 {
		encoded, err := json.Marshal(fallback)
		if err != nil {
			return relayResult{}, err
		}
		params = encoded
	}
	raw, err := relayer.RelayRaw(ctx, method, params)
	if err != nil {
		span.SetStatus(codes.Error, errorCategory(err))
		return relayResult{}, classifyDownstreamErr(server, err)
	}
	return classifyRelay(raw, protocolGenerationOf(replica.Client())), nil
}

func classifyRelay(raw json.RawMessage, era string) relayResult {
	var fields StatelessResultFields
	_ = json.Unmarshal(raw, &fields)
	if fields.ResultType == ResultTypeInputRequired {
		return relayResult{raw: raw, passThrough: true, inputRequired: true}
	}
	if era == string(EraStateless) || fields.ResultType != "" {
		return relayResult{raw: raw, passThrough: true}
	}
	ttl := int64(0)
	return relayResult{
		raw: raw,
		fields: StatelessResultFields{
			ResultType: ResultTypeComplete,
			TTLMs:      &ttl,
			CacheScope: CacheScopePrivate,
		},
	}
}

func mergeStatelessFields(raw json.RawMessage, fields StatelessResultFields) json.RawMessage {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return raw
	}
	if fields.ResultType != "" {
		encoded, err := json.Marshal(fields.ResultType)
		if err == nil {
			obj["resultType"] = encoded
		}
	}
	if fields.TTLMs != nil {
		encoded, err := json.Marshal(fields.TTLMs)
		if err == nil {
			obj["ttlMs"] = encoded
		}
	}
	if fields.CacheScope != "" {
		encoded, err := json.Marshal(fields.CacheScope)
		if err == nil {
			obj["cacheScope"] = encoded
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

func (r *RawPromptsListResult) withoutCacheFields() *RawPromptsListResult {
	if r == nil {
		return &RawPromptsListResult{Prompts: []json.RawMessage{}}
	}
	out := *r
	out.StatelessResultFields = StatelessResultFields{}
	if out.Prompts == nil {
		out.Prompts = []json.RawMessage{}
	}
	return &out
}

func (r *RawResourcesListResult) withoutCacheFields() *RawResourcesListResult {
	if r == nil {
		return &RawResourcesListResult{Resources: []json.RawMessage{}}
	}
	out := *r
	out.StatelessResultFields = StatelessResultFields{}
	if out.Resources == nil {
		out.Resources = []json.RawMessage{}
	}
	return &out
}

func (r *RawResourceTemplatesListResult) withoutCacheFields() *RawResourceTemplatesListResult {
	if r == nil {
		return &RawResourceTemplatesListResult{ResourceTemplates: []json.RawMessage{}}
	}
	out := *r
	out.StatelessResultFields = StatelessResultFields{}
	if out.ResourceTemplates == nil {
		out.ResourceTemplates = []json.RawMessage{}
	}
	return &out
}

// indexServerResources builds one server's resource index after registration.
// Failures are logged and leave that server's index empty.
func (g *Gateway) indexServerResources(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), listFanoutTimeout)
	defer cancel()
	set := g.router.GetReplicaSet(name)
	if set == nil {
		return
	}
	reps := set.Replicas()
	if len(reps) == 0 {
		return
	}
	client := reps[0].Client()
	g.indexUIFromClient(name, client)

	var resPage, tmplPage RawListPage
	var resErr, tmplErr error
	if declares(client, wantsResources) {
		resPage, resErr = g.listResourcesOf(ctx, client)
		tmplPage, tmplErr = g.listTemplatesOf(ctx, client)
	}
	var promptPage RawListPage
	var promptErr error
	if declares(client, wantsPrompts) {
		promptPage, promptErr = g.listPromptsOf(ctx, client)
	}
	if g.router.GetReplicaSet(name) != set {
		return
	}
	if resErr != nil || tmplErr != nil {
		category := errorCategory(resErr)
		if category == "transport" && tmplErr != nil {
			category = errorCategory(tmplErr)
		}
		if resErr != nil {
			category = errorCategory(resErr)
		}
		g.logger.Warn("resource index build failed", "server", name, "method", "resources/list", "category", category)
		g.router.SetResourceListError(name, category)
	} else if declares(client, wantsResources) {
		g.router.SetResourceListError(name, "")
		g.logCollisions(g.router.SetResourceIndex(name, resourceURIs(resPage.Entries), templateURIs(tmplPage.Entries)))
	}
	if promptErr != nil {
		g.logger.Warn("prompt index build failed", "server", name, "method", "prompts/list", "category", errorCategory(promptErr))
	} else if declares(client, wantsPrompts) {
		g.router.SetPromptCount(name, len(promptPage.Entries))
	}
}

func (g *Gateway) indexUIFromClient(name string, client AgentClient) {
	if client == nil {
		return
	}
	var uris []string
	for _, tool := range client.Tools() {
		uris = append(uris, uiResourceURIs(tool.Meta)...)
	}
	g.router.SetUIResourceIndex(name, uris)
}

func declares(client AgentClient, want func(Capabilities) bool) bool {
	src, ok := client.(interface{ DownstreamCapabilities() Capabilities })
	return ok && want(src.DownstreamCapabilities())
}

// scheduleResourceIndex starts the post-registration index build. The
// caller must already have refreshed tools.
func (g *Gateway) scheduleResourceIndex(name string) {
	go g.indexServerResources(name)
}
