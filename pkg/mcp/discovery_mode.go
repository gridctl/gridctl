package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/runs"
)

const (
	// MetaToolSearchTools is the deferred-discovery search meta-tool.
	MetaToolSearchTools = "search_tools"
	// MetaToolCallTool is the deferred-discovery call meta-tool.
	MetaToolCallTool = "call_tool"

	// DiscoveryFull is today's tools/list behavior.
	DiscoveryFull = "full"
	// DiscoveryDeferred replaces the catalog with search_tools, call_tool,
	// and the pinned set.
	DiscoveryDeferred = "deferred"
)

// searchCursorRestartMessage is the in-band error for a cursor whose view
// no longer matches. Callers restart from the first page.
const searchCursorRestartMessage = "catalog or scope changed since this cursor was issued; restart from the first page"

const callToolMetaError = "Error: call_tool cannot call a meta-tool; call the target tool by name"

func isDeferredMetaTool(name string) bool {
	return name == MetaToolSearchTools || name == MetaToolCallTool
}

// SearchToolsTool is the tools/list definition for search_tools.
func SearchToolsTool() Tool {
	schema := InputSchemaObject{
		Type: "object",
		Properties: map[string]Property{
			"query": {
				Type:        "string",
				Description: "Case-insensitive substring matched against tool names, descriptions, and parameter names. Empty lists the visible catalog.",
			},
			"server": {
				Type:        "string",
				Description: "Exact downstream server name. Omit to search every visible server.",
			},
			"cursor": {
				Type:        "string",
				Description: "Opaque cursor from a previous page. A cursor is bound to the query, server, and catalog view that issued it.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum tools to return. Default 20, maximum 200. A page may return fewer when the byte budget is reached.",
			},
		},
	}
	schemaJSON, _ := json.Marshal(schema)
	output := []byte(`{"type":"object","properties":{"tools":{"type":"array","description":"Full tool definitions, the same objects tools/list would return."},"total_visible":{"type":"integer"},"matched":{"type":"integer"},"returned":{"type":"integer"},"nextCursor":{"type":"string","description":"Present when another page remains."}},"required":["tools","total_visible","matched","returned"]}`)
	return Tool{
		Name:         MetaToolSearchTools,
		Description:  "Search the tools this session can see. Returns full tool definitions, paged. Call with an empty query to list everything. A policy-blocked tool is listed here and denied when called. Invoke a returned name through call_tool or directly.",
		InputSchema:  schemaJSON,
		OutputSchema: output,
	}
}

// CallToolTool is the tools/list definition for call_tool.
func CallToolTool() Tool {
	schema := InputSchemaObject{
		Type: "object",
		Properties: map[string]Property{
			"name": {
				Type:        "string",
				Description: "Exact tool name from search_tools or tools/list. Meta-tool names are rejected.",
			},
			"arguments": {
				Type:        "object",
				Description: "Arguments object for the named tool. Defaults to {}.",
			},
		},
		Required: []string{"name"},
	}
	schemaJSON, _ := json.Marshal(schema)
	return Tool{
		Name:        MetaToolCallTool,
		Description: "Call one tool by the exact name search_tools returned. The call uses the same scope, whitelist, group, policy, and rate-limit checks as a direct tools/call. On the stateless generation, continue an input_required result by retrying this call with the same name and arguments plus the standard requestState and inputResponses fields.",
		InputSchema: schemaJSON,
	}
}

// SetToolDiscovery installs the stack-level discovery mode. An empty or
// unknown mode means full. alwaysExpose is copied. A change re-fingerprints
// handshake sessions and listen subscribers. The pinned-name lint never
// fails the call.
func (g *Gateway) SetToolDiscovery(mode string, alwaysExpose []string) {
	if mode != DiscoveryDeferred {
		mode = DiscoveryFull
	}
	pins := append([]string(nil), alwaysExpose...)
	g.mu.Lock()
	prev := g.discoveryMode
	if prev == "" {
		prev = DiscoveryFull
	}
	g.discoveryMode = mode
	g.alwaysExpose = pins
	g.alwaysExposeLint = ""
	g.mu.Unlock()
	if prev != mode && g.logger != nil {
		g.logger.Info("tool discovery mode updated", "mode", mode)
	}
	if g.listChanges != nil {
		g.listChanges.trigger()
	}
	g.lintAlwaysExpose()
}

// ToolDiscoveryStatus returns "full" or "deferred".
func (g *Gateway) ToolDiscoveryStatus() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.discoveryMode == DiscoveryDeferred {
		return DiscoveryDeferred
	}
	return DiscoveryFull
}

// effectiveDiscovery returns the override when it is a known mode, otherwise
// the stack mode. An empty override means the stack default. Unknown values
// are not an override: HTTP handlers reject them before they reach here.
func (g *Gateway) effectiveDiscovery(override string) string {
	override = strings.TrimSpace(override)
	if override == DiscoveryDeferred || override == DiscoveryFull {
		return override
	}
	g.mu.RLock()
	mode := g.discoveryMode
	g.mu.RUnlock()
	if mode == DiscoveryDeferred {
		return DiscoveryDeferred
	}
	return DiscoveryFull
}

// lintAlwaysExpose warns once per missing-name set when a pinned name is
// absent from the pre-whitelist catalog. An empty catalog is skipped so
// startup, before any server has registered, stays quiet. The lint never
// returns an error and never blocks startup or reload.
func (g *Gateway) lintAlwaysExpose() {
	if g == nil || g.router == nil {
		return
	}
	g.mu.RLock()
	pins := append([]string(nil), g.alwaysExpose...)
	g.mu.RUnlock()
	if len(pins) == 0 {
		return
	}
	catalog := g.router.AllCatalogTools()
	if len(catalog) == 0 {
		return
	}
	have := make(map[string]struct{}, len(catalog))
	for _, tool := range catalog {
		have[tool.Name] = struct{}{}
	}
	var missing []string
	for _, name := range pins {
		if _, ok := have[name]; !ok {
			missing = append(missing, name)
		}
	}
	key := strings.Join(missing, "\x00")
	g.mu.Lock()
	if g.alwaysExposeLint == key {
		g.mu.Unlock()
		return
	}
	g.alwaysExposeLint = key
	logger := g.logger
	g.mu.Unlock()
	if len(missing) == 0 || logger == nil {
		return
	}
	logger.Warn("tool_discovery always_expose names are not in the catalog",
		"missing", missing,
		"hint", "pinned names must be canonical server__tool names present before the whitelist")
}

func (g *Gateway) searchPageByteBudget() int {
	g.mu.RLock()
	limit := g.maxToolResultBytes
	g.mu.RUnlock()
	if limit <= 0 {
		limit = defaultMaxToolResultBytes
	}
	budget := limit / 2
	if budget < 1 {
		return 1
	}
	return budget
}

// pinnedVisible returns always_expose tools that survive scope, whitelist,
// and group projection, in catalog order, using the group's exposure names.
// Matching is on the canonical name before rewrite so a group rename still
// pins the tool the operator named.
func (g *Gateway) pinnedVisible(accessID, group string, principal bool, pins []string) []Tool {
	if len(pins) == 0 {
		return nil
	}
	want := make(map[string]struct{}, len(pins))
	for _, name := range pins {
		want[name] = struct{}{}
	}
	g.mu.RLock()
	policy := g.clientPolicy
	groups := g.groupPolicy
	g.mu.RUnlock()
	tools := g.router.AggregatedTools()
	if policy != nil {
		tools = policy.forPrincipal(principal).Filter(accessID, tools)
	}
	selected := make([]Tool, 0, len(pins))
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			selected = append(selected, tool)
		}
	}
	if group != "" {
		selected = groups.FilterAndRewrite(group, selected)
	}
	return selected
}

func discoveryError(text string) *ToolCallResult {
	return &ToolCallResult{
		Content: []Content{NewTextContent(text)},
		IsError: true,
	}
}

type searchToolsPage struct {
	Tools        []Tool `json:"tools"`
	TotalVisible int    `json:"total_visible"`
	Matched      int    `json:"matched"`
	Returned     int    `json:"returned"`
	NextCursor   string `json:"nextCursor,omitempty"`
}

func (g *Gateway) handleSearchTools(ctx context.Context, params ToolCallParams) (*ToolCallResult, error) {
	ctx = stripMRTRRelay(ctx)
	args := params.Arguments
	if args == nil {
		args = map[string]any{}
	}
	query, errText := optionalStringArg(args, "query")
	if errText != "" {
		return discoveryError(errText), nil
	}
	server, errText := optionalStringArg(args, "server")
	if errText != "" {
		return discoveryError(errText), nil
	}
	cursor, errText := optionalStringArg(args, "cursor")
	if errText != "" {
		return discoveryError(errText), nil
	}
	limit := defaultDiscoverLimit
	if raw, ok := args["limit"]; ok && raw != nil {
		n, ok := integerArg(raw)
		if !ok || n < 1 || n > maxDiscoverLimit {
			return discoveryError("Error: limit must be an integer from 1 to 200"), nil
		}
		limit = n
	}

	canonical := g.scopeToolsForContext(ctx, g.router.AggregatedTools())
	group := GroupFromContext(ctx)
	totalVisible := len(canonical)
	if group != "" {
		totalVisible = len(g.CurrentGroupPolicy().FilterAndRewrite(group, canonical))
	}
	filtered := canonical
	if server != "" {
		matchedServer := make([]Tool, 0, len(canonical))
		for _, tool := range canonical {
			got, _, err := ParsePrefixedTool(tool.Name)
			if err == nil && got == server {
				matchedServer = append(matchedServer, tool)
			}
		}
		filtered = matchedServer
	}
	candidates := filtered
	if group != "" {
		candidates = g.CurrentGroupPolicy().FilterAndRewrite(group, filtered)
	}
	candidates = excludeMetaTools(candidates)
	if candidates == nil {
		candidates = []Tool{}
	}
	view := fingerprintTools(candidates)
	matched := NewSearchIndex(candidates).Search(query)
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Name < matched[j].Name
	})
	if matched == nil {
		matched = []Tool{}
	}

	offset := 0
	if cursor != "" {
		parsed, errText := decodeSearchCursor(cursor, query, server, view, len(matched))
		if errText != "" {
			return discoveryError(errText), nil
		}
		offset = parsed
	}
	if offset > len(matched) {
		return discoveryError("Error: cursor offset is out of range"), nil
	}

	page, next := pageTools(matched[offset:], limit, g.searchPageByteBudget())
	var nextCursor string
	if next {
		nextCursor = encodeSearchCursor(offset+len(page), query, server, view)
	}
	if page == nil {
		page = []Tool{}
	}
	body := searchToolsPage{
		Tools:        page,
		TotalVisible: totalVisible,
		Matched:      len(matched),
		Returned:     len(page),
		NextCursor:   nextCursor,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return discoveryError("Error: failed to encode search_tools result"), nil
	}
	header := fmt.Sprintf("Found %d tool(s) matching '%s' (out of %d visible)", len(matched), query, totalVisible)
	if nextCursor != "" {
		header += "; next cursor: " + nextCursor
	} else {
		header += ":"
	}
	return &ToolCallResult{
		Content:           []Content{NewTextContent(header + "\n\n" + string(raw))},
		StructuredContent: raw,
	}, nil
}

func (g *Gateway) handleCallTool(ctx context.Context, params ToolCallParams) (*ToolCallResult, error) {
	args := params.Arguments
	if args == nil {
		args = map[string]any{}
	}
	rawName, ok := args["name"]
	if ok && rawName != nil {
		if _, isString := rawName.(string); !isString {
			return discoveryError("Error: name must be a string"), nil
		}
	}
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return discoveryError("Error: call_tool requires a non-empty name"), nil
	}
	if isMetaToolName(name) {
		return discoveryError(callToolMetaError), nil
	}
	callArgs := map[string]any{}
	if raw, present := args["arguments"]; present && raw != nil {
		object, ok := raw.(map[string]any)
		if !ok {
			return discoveryError("Error: arguments must be an object"), nil
		}
		callArgs = object
	}
	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: name, Arguments: callArgs})
	if result != nil {
		result.unknownTool = false
	}
	return result, err
}

func excludeMetaTools(tools []Tool) []Tool {
	if len(tools) == 0 {
		return tools
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if isMetaToolName(tool.Name) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func optionalStringArg(args map[string]any, key string) (string, string) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return "", ""
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Sprintf("Error: %s must be a string", key)
	}
	return text, ""
}

func stringArg(args map[string]any, key string) string {
	text, _ := args[key].(string)
	return text
}

func integerArg(raw any) (int, bool) {
	switch n := raw.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		v, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(v), true
	default:
		return 0, false
	}
}

// pageTools takes a prefix of tools that fits limit and the serialized-array
// byte budget. The first tool is always included, even when it alone exceeds
// the budget. next is true when tools remain after the page.
func pageTools(tools []Tool, limit, budget int) ([]Tool, bool) {
	if limit < 1 {
		limit = 1
	}
	page := make([]Tool, 0, min(limit, len(tools)))
	used := 2 // "[]"
	for _, tool := range tools {
		if len(page) >= limit {
			break
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		add := len(raw)
		if len(page) > 0 {
			add++ // comma
		}
		if len(page) > 0 && used+add > budget {
			break
		}
		page = append(page, tool)
		used += add
	}
	return page, len(page) < len(tools)
}

func encodeSearchCursor(offset int, query, server, view string) string {
	payload := fmt.Sprintf("v1:%d:%s", offset, searchCursorHash(query, server, view))
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

func decodeSearchCursor(cursor, query, server, view string, matched int) (int, string) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "Error: cursor could not be decoded"
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "v1" {
		return 0, "Error: cursor could not be decoded"
	}
	if parts[2] != searchCursorHash(query, server, view) {
		return 0, searchCursorRestartMessage
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil || offset < 0 || offset > matched {
		return 0, "Error: cursor offset is out of range"
	}
	return offset, ""
}

func searchCursorHash(query, server, view string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(query))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(server))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(view))
	return strconv.FormatUint(h.Sum64(), 16)
}

// setDiscoveryOutcome records the outer meta-tool attempt. The inner
// dispatch, when there is one, is a nested attempt.
func setDiscoveryOutcome(state *callState, result *ToolCallResult, err error, reason string) {
	switch {
	case err != nil:
		state.set(runs.DispositionToolError, runs.StageDiscovery, runs.ReasonToolError, CompletionUnknown)
	case result != nil && (result.RequestState != "" || result.ResultType == ResultTypeInputRequired):
		state.set(runs.DispositionInputRequired, runs.StageDiscovery, runs.ReasonInputRequired, CompletionInputRequired)
	case result != nil && result.IsError:
		state.set(runs.DispositionToolError, runs.StageDiscovery, runs.ReasonToolError, CompletionComplete)
	default:
		state.set(runs.DispositionCompleted, runs.StageDiscovery, reason, CompletionComplete)
	}
}

func unsupportedDiscoveryMessage(mode string) string {
	return fmt.Sprintf("unsupported discovery mode %q (accepted: deferred, full)", strings.TrimSpace(mode))
}

func discoveryValueRejected(mode string) bool {
	mode = strings.TrimSpace(mode)
	return mode != "" && mode != DiscoveryDeferred && mode != DiscoveryFull
}
