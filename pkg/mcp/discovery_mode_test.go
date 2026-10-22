package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/mock/gomock"

	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/tracing"
)

func deferredFixture(t *testing.T) (*Gateway, *gomock.Controller) {
	t.Helper()
	g := NewGateway()
	t.Cleanup(g.Close)
	return g, gomock.NewController(t)
}

func addNamedTools(g *Gateway, ctrl *gomock.Controller, server string, tools []Tool) *MockAgentClient {
	client := setupMockAgentClient(ctrl, server, tools)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	return client
}

func TestVisibleTools_ProjectionAndFingerprintAgreement(t *testing.T) {
	tools := []Tool{
		{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "add", Description: "add", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	for _, codeMode := range []bool{false, true} {
		for _, mode := range []string{DiscoveryFull, DiscoveryDeferred} {
			t.Run(fmt.Sprintf("code=%v/%s", codeMode, mode), func(t *testing.T) {
				g, ctrl := deferredFixture(t)
				addNamedTools(g, ctrl, "alpha", tools)
				if codeMode {
					g.SetCodeMode(time.Second)
				}
				g.SetToolDiscovery(mode, []string{"alpha__echo"})
				ctx := withDiscovery(context.Background(), mode)
				_, session, err := g.HandleInitialize(InitializeParams{
					ProtocolVersion: MCPProtocolVersion,
					ClientInfo:      ClientInfo{Name: "tester", Version: "1"},
				}, "", "", mode)
				if err != nil {
					t.Fatal(err)
				}
				listed, err := g.HandleToolsList(ctx)
				if err != nil {
					t.Fatal(err)
				}
				want := fingerprintTools(g.visibleToolsFor(session.AccessID, session.Group, false, mode))
				if got := fingerprintTools(listed.Tools); got != want {
					t.Fatalf("tools/list fingerprint %s, visible %s", got, want)
				}
				if session.toolFingerprint != want {
					t.Fatalf("initialize fingerprint %s, visible %s", session.toolFingerprint, want)
				}
				prev, ok := g.sessions.SwapToolFingerprint(session.ID, fingerprintTools(listed.Tools))
				if !ok || prev != want {
					t.Fatalf("notifier swap = %q %v, want %s", prev, ok, want)
				}
			})
		}
	}
}

func TestVisibleTools_DeferredPinnedSetAndGroupExposure(t *testing.T) {
	g, ctrl := deferredFixture(t)
	addNamedTools(g, ctrl, "alpha", []Tool{
		{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "add", Description: "add two", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	addNamedTools(g, ctrl, "beta", []Tool{
		{Name: "ping", Description: "ping", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	g.SetToolDiscovery(DiscoveryDeferred, []string{"alpha__add", "missing__nope"})
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"alice": {Servers: []string{"alpha"}},
		},
	}))
	g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
		"dev": {
			Servers: []string{"alpha"},
			Overrides: map[string]GroupOverrideSpec{
				"alpha__add": {Name: "sum"},
			},
		},
	}))

	alice := WithClientAccessID(context.Background(), "alice")
	listed, err := g.HandleToolsList(withDiscovery(alice, DiscoveryDeferred))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "search_tools,call_tool,alpha__add" {
		t.Fatalf("alice list = %v", names)
	}

	groupCtx := WithGroup(withDiscovery(alice, DiscoveryDeferred), "dev")
	grouped, err := g.HandleToolsList(groupCtx)
	if err != nil {
		t.Fatal(err)
	}
	var groupNames []string
	for _, tool := range grouped.Tools {
		groupNames = append(groupNames, tool.Name)
	}
	if strings.Join(groupNames, ",") != "search_tools,call_tool,sum" {
		t.Fatalf("group list = %v", groupNames)
	}
}

func TestVisibleTools_PrincipalAliasMatchesFullList(t *testing.T) {
	g, ctrl := deferredFixture(t)
	addNamedTools(g, ctrl, "alpha", []Tool{{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	addNamedTools(g, ctrl, "beta", []Tool{{Name: "ping", Description: "ping", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"narrow": {Servers: []string{"alpha"}, Aliases: []string{"RawSubject"}},
			"wide":   {Servers: []string{"alpha", "beta"}},
		},
	}))
	g.SetToolDiscovery(DiscoveryDeferred, nil)
	ctx := withDiscovery(WithClientID(WithClientAccessID(WithPrincipal(context.Background(), "RawSubject"), "RawSubject"), "wide"), DiscoveryDeferred)
	full := withDiscovery(WithClientAccessID(WithPrincipal(context.Background(), "RawSubject"), "RawSubject"), DiscoveryFull)
	fullList, err := g.HandleToolsList(full)
	if err != nil {
		t.Fatal(err)
	}
	var fullNames []string
	for _, tool := range fullList.Tools {
		fullNames = append(fullNames, tool.Name)
	}
	res, err := g.HandleToolsCall(ctx, ToolCallParams{Name: MetaToolSearchTools, Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("search: %v %+v", err, res)
	}
	var page searchToolsPage
	if err := json.Unmarshal(res.StructuredContent, &page); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range page.Tools {
		got = append(got, tool.Name)
	}
	if strings.Join(got, ",") != strings.Join(fullNames, ",") || strings.Join(got, ",") != "alpha__echo" {
		t.Fatalf("candidates %v, full list %v", got, fullNames)
	}
	denied, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolCallTool,
		Arguments: map[string]any{"name": "beta__ping"},
	})
	if err != nil || denied == nil || !denied.IsError || !strings.Contains(denied.Content[0].Text, "access scope") {
		t.Fatalf("call_tool widened scope: %v %+v", err, denied)
	}
}

func TestSearchTools_PagingBudgetAndStaleCursor(t *testing.T) {
	g, ctrl := deferredFixture(t)
	big := strings.Repeat("x", 80)
	var tools []Tool
	for i := 0; i < 5; i++ {
		tools = append(tools, Tool{
			Name:        fmt.Sprintf("t%d", i),
			Description: big,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		})
	}
	addNamedTools(g, ctrl, "alpha", tools)
	g.SetToolDiscovery(DiscoveryDeferred, nil)
	g.SetMaxToolResultBytes(200)
	ctx := withDiscovery(context.Background(), DiscoveryDeferred)

	first, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolSearchTools,
		Arguments: map[string]any{"limit": 20},
	})
	if err != nil || first.IsError {
		t.Fatalf("first page: %v %+v", err, first)
	}
	var page searchToolsPage
	if err := json.Unmarshal(first.StructuredContent, &page); err != nil {
		t.Fatal(err)
	}
	if page.Returned >= 20 || page.NextCursor == "" || page.Returned < 1 {
		t.Fatalf("byte budget page = %+v", page)
	}
	raw, _ := json.Marshal(page.Tools[0])
	if len(raw) > g.searchPageByteBudget() && page.Returned != 1 {
		t.Fatalf("oversized definition was not isolated: %d bytes, returned %d", len(raw), page.Returned)
	}

	seen := map[string]int{}
	cursor := ""
	for {
		args := map[string]any{"limit": 1}
		if cursor != "" {
			args["cursor"] = cursor
		}
		res, err := g.HandleToolsCall(ctx, ToolCallParams{Name: MetaToolSearchTools, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("page: %v %+v", err, res)
		}
		var got searchToolsPage
		if err := json.Unmarshal(res.StructuredContent, &got); err != nil {
			t.Fatal(err)
		}
		for _, tool := range got.Tools {
			seen[tool.Name]++
			encoded, _ := json.Marshal(tool)
			if !bytes.Contains(encoded, tool.InputSchema) {
				t.Fatal("definition truncated")
			}
		}
		if got.NextCursor == "" {
			break
		}
		cursor = got.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("enumerated %v", seen)
	}
	for name, n := range seen {
		if n != 1 {
			t.Fatalf("%s seen %d times", name, n)
		}
	}

	alice := WithClientAccessID(ctx, "alice")
	issued, err := g.HandleToolsCall(alice, ToolCallParams{Name: MetaToolSearchTools, Arguments: map[string]any{"limit": 1}})
	if err != nil || issued.IsError {
		t.Fatal(err)
	}
	var issuedPage searchToolsPage
	if err := json.Unmarshal(issued.StructuredContent, &issuedPage); err != nil {
		t.Fatal(err)
	}
	if issuedPage.NextCursor == "" {
		t.Fatal("expected a cursor before the scope change")
	}
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default:  "deny",
		Profiles: map[string]ClientProfileSpec{"alice": {Servers: []string{"missing"}}},
	}))
	stale, err := g.HandleToolsCall(alice, ToolCallParams{
		Name:      MetaToolSearchTools,
		Arguments: map[string]any{"cursor": issuedPage.NextCursor, "limit": 1},
	})
	if err != nil || stale == nil || !stale.IsError || !strings.Contains(stale.Content[0].Text, searchCursorRestartMessage) {
		t.Fatalf("stale cursor: %v %+v", err, stale)
	}

	bad, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolSearchTools,
		Arguments: map[string]any{"limit": 0},
	})
	if err != nil || bad == nil || !bad.IsError || !strings.Contains(bad.Content[0].Text, "limit") {
		t.Fatalf("bad limit: %v %+v", err, bad)
	}
}

func TestCallTool_GatesMarkerAndSensitiveFrame(t *testing.T) {
	g, ctrl := deferredFixture(t)
	client := addNamedTools(g, ctrl, "agent", []Tool{{Name: "send", Description: "send", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	g.SetToolDiscovery(DiscoveryDeferred, nil)
	g.serverMeta["agent"] = MCPServerConfig{sensitiveCalls: true}
	sentinel := sensitiveSentinel(t)
	client.EXPECT().CallTool(gomock.Any(), "send", gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent(sentinel)}, StructuredContent: json.RawMessage(`{"ok":true}`)}, nil,
	)
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewJSONHandler(&logs, nil)))
	buf := tracing.NewBuffer(10, time.Hour)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(buf)))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(old) })
	obs := &sensitiveObserverSpy{}
	g.SetToolCallObserver(obs)
	ctx := withDiscovery(context.Background(), DiscoveryDeferred)
	res, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name: MetaToolCallTool,
		Arguments: map[string]any{
			"name":      "agent__send",
			"arguments": map[string]any{"secret": sentinel},
		},
	})
	if err != nil || res == nil || res.IsError || res.Content[0].Text != sentinel {
		t.Fatalf("call_tool delivery: %v %+v", err, res)
	}
	if res.unknownTool {
		t.Fatal("unknownTool marker leaked")
	}
	if obs.legacy.count() != 0 || obs.rich.count() != 0 || len(obs.safe) != 1 {
		t.Fatalf("observations legacy=%d rich=%d safe=%d", obs.legacy.count(), obs.rich.count(), len(obs.safe))
	}
	if strings.Contains(logs.String(), sentinel) || strings.Contains(fmt.Sprint(buf.GetRecent(10)), sentinel) {
		t.Fatal("inner secret leaked into logs or spans")
	}

	hidden, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolCallTool,
		Arguments: map[string]any{"name": "agent__missing"},
	})
	if err != nil || hidden == nil || !hidden.IsError || hidden.unknownTool {
		t.Fatalf("hidden marker: %v unknown=%v res=%+v", err, hidden != nil && hidden.unknownTool, hidden)
	}
	meta, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolCallTool,
		Arguments: map[string]any{"name": MetaToolSearchTools},
	})
	if err != nil || meta == nil || !strings.Contains(meta.Content[0].Text, callToolMetaError) {
		t.Fatalf("meta inner: %v %+v", err, meta)
	}
}

func TestCallTool_PolicyDoesNotConsumeRateToken(t *testing.T) {
	g, ctrl := deferredFixture(t)
	client := addNamedTools(g, ctrl, "alpha", []Tool{
		{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "add", Description: "add", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	g.SetToolDiscovery(DiscoveryDeferred, nil)
	var tokens int
	g.SetCallGates([]CallGate{
		&scriptedPolicyGate{block: map[string]string{
			"alpha__add": "Error: tool blocked by policy (rule 1: alpha__add); do not retry",
		}},
		gateFunc{name: "rate-limits", fn: func(_ context.Context, call GateCall) GateDecision {
			if call.PrefixedTool != "alpha__echo" {
				return GateAllow()
			}
			tokens++
			if tokens > 1 {
				return GateDeny("Error: rate limit exceeded")
			}
			return GateAllow()
		}},
	})
	ctx := withDiscovery(context.Background(), DiscoveryDeferred)
	blocked, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolCallTool,
		Arguments: map[string]any{"name": "alpha__add"},
	})
	if err != nil || blocked == nil || !strings.HasPrefix(blocked.Content[0].Text, "Error: tool blocked by policy") {
		t.Fatalf("policy: %v %+v", err, blocked)
	}
	if tokens != 0 {
		t.Fatalf("policy denial consumed a token: %d", tokens)
	}
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil)
	okRes, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolCallTool,
		Arguments: map[string]any{"name": "alpha__echo"},
	})
	if err != nil || okRes.IsError {
		t.Fatalf("first rate call: %v %+v", err, okRes)
	}
	limited, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolCallTool,
		Arguments: map[string]any{"name": "alpha__echo"},
	})
	if err != nil || limited == nil || !strings.Contains(limited.Content[0].Text, "rate limit") {
		t.Fatalf("rate limit: %v %+v", err, limited)
	}
}

type gateFunc struct {
	name string
	fn   func(context.Context, GateCall) GateDecision
}

func (g gateFunc) Name() string { return g.name }
func (g gateFunc) CheckToolCall(ctx context.Context, call GateCall) GateDecision {
	return g.fn(ctx, call)
}

func TestDiscovery_FullModeRejectsMetaNames(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	res, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: MetaToolSearchTools})
	if err != nil || out.Reason != runs.ReasonUnknownTool || res == nil || !res.IsError {
		t.Fatalf("canonical search_tools: %v %+v %+v", err, out, res)
	}
	listed, err := g.HandleToolsList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if isDeferredMetaTool(tool.Name) {
			t.Fatalf("full mode listed %s", tool.Name)
		}
	}
}

func TestBuildInstructions_FollowsEffectiveMode(t *testing.T) {
	g, ctrl := deferredFixture(t)
	addNamedTools(g, ctrl, "alpha", []Tool{{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	g.SetServerMeta(MCPServerConfig{Name: "alpha"})
	full := g.buildInstructions(DiscoveryFull)
	if !strings.Contains(full, "Call `tools/list`") || strings.Contains(full, "search_tools") {
		t.Fatalf("full instructions changed: %s", full)
	}
	deferred := g.buildInstructions(DiscoveryDeferred)
	if !strings.Contains(deferred, "search_tools") || !strings.Contains(deferred, "call_tool") || !strings.Contains(deferred, "empty query") {
		t.Fatalf("deferred instructions: %s", deferred)
	}
	g.SetCodeMode(time.Second)
	both := g.buildInstructions(DiscoveryDeferred)
	if !strings.Contains(both, "`search`") || !strings.Contains(both, "search_tools") || !strings.Contains(both, "call_tool") {
		t.Fatalf("composed instructions: %s", both)
	}
	if got := g.buildInstructions(DiscoveryFull); !strings.Contains(got, "code mode") || strings.Contains(got, "search_tools") {
		t.Fatalf("full code-mode instructions changed: %s", got)
	}

	_, deferredSession, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		ClientInfo:      ClientInfo{Name: "tester", Version: "1"},
	}, "", "", DiscoveryDeferred)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		ClientInfo:      ClientInfo{Name: "tester", Version: "1"},
	}, "", "", DiscoveryDeferred)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Instructions, "search_tools") {
		t.Fatalf("initialize instructions: %s", result.Instructions)
	}
	_ = deferredSession
	discovered := g.HandleServerDiscover(withDiscovery(context.Background(), DiscoveryDeferred))
	if !strings.Contains(discovered.Instructions, "search_tools") {
		t.Fatalf("discover instructions: %s", discovered.Instructions)
	}
	plain := g.HandleServerDiscover(context.Background())
	if plain.Instructions != g.buildInstructions(DiscoveryFull) {
		t.Fatal("stateless discover without override did not return full-mode text")
	}
}

func TestSetToolDiscovery_ListenFingerprint(t *testing.T) {
	g, ctrl := deferredFixture(t)
	addNamedTools(g, ctrl, "alpha", []Tool{
		{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "add", Description: "add", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	g.SetListChangeTiming(15*time.Millisecond, time.Second)
	ts := openListenServer(t, g)
	resp := postListenQuery(t, ts.URL, "discovery=deferred", 7)
	t.Cleanup(func() { resp.Body.Close() })
	time.Sleep(50 * time.Millisecond)
	subs := g.listeners.list()
	if len(subs) != 1 {
		t.Fatalf("subscribers = %d", len(subs))
	}
	want := fingerprintTools(g.visibleToolsFor(subs[0].accessID, subs[0].group, subs[0].principal, DiscoveryDeferred))
	if subs[0].toolFP != want {
		t.Fatalf("deferred subscriber fingerprint %s, want %s", subs[0].toolFP, want)
	}
	resp.Body.Close()

	g.SetToolDiscovery(DiscoveryFull, nil)
	base := postListen(t, ts.URL, 8, listenNotifications(true, false, false, nil), "", "")
	t.Cleanup(func() { base.Body.Close() })
	time.Sleep(30 * time.Millisecond)
	var stackDefault *listenSubscriber
	for _, sub := range g.listeners.list() {
		if sub.discovery == "" {
			stackDefault = sub
		}
	}
	if stackDefault == nil {
		t.Fatal("missing stack-default subscriber")
	}
	before := stackDefault.toolFP
	g.SetToolDiscovery(DiscoveryDeferred, []string{"alpha__echo"})
	time.Sleep(40 * time.Millisecond)
	after := ""
	for _, sub := range g.listeners.list() {
		if sub.key == stackDefault.key {
			after = sub.toolFP
		}
	}
	flipped := fingerprintTools(g.visibleToolsFor(stackDefault.accessID, stackDefault.group, stackDefault.principal, DiscoveryDeferred))
	if after == before || after != flipped {
		t.Fatalf("stack-default fingerprint before %s after %s want %s", before, after, flipped)
	}
}

func TestNotifier_CodeModeSkipsFullAndNotifiesDeferred(t *testing.T) {
	g, sink := newListChangedGateway(t, 15*time.Millisecond, time.Second)
	g.SetCodeMode(time.Second)
	g.SetToolDiscovery(DiscoveryFull, []string{"alpha__echo"})
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi"), testTool("add", "sum")}})
	g.Router().RefreshTools()
	_, full, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		ClientInfo:      ClientInfo{Name: "full", Version: "1"},
	}, "", "", DiscoveryFull)
	if err != nil {
		t.Fatal(err)
	}
	_, deferredSession, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		ClientInfo:      ClientInfo{Name: "deferred", Version: "1"},
	}, "", "", DiscoveryDeferred)
	if err != nil {
		t.Fatal(err)
	}
	sink.waitQuiet(t, 0, 40*time.Millisecond)

	ctx := withDiscovery(context.Background(), DiscoveryDeferred)
	listed, err := g.HandleToolsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The handshake tools/list handler stores fingerprintTools(result.Tools).
	// A catalog change that leaves the pinned set alone must not diverge.
	g.sessions.SetToolFingerprint(deferredSession.ID, fingerprintTools(listed.Tools))
	before := g.sessions.Get(deferredSession.ID).toolFingerprint
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi"), testTool("add", "changed")}})
	g.Router().RefreshTools()
	sink.waitQuiet(t, 0, 40*time.Millisecond)
	if got := g.sessions.Get(deferredSession.ID).toolFingerprint; got != before {
		t.Fatalf("catalog-only refresh changed fingerprint %s -> %s", before, got)
	}
	_ = full

	g.SetToolDiscovery(DiscoveryDeferred, []string{"alpha__add"})
	got := sink.waitFor(t, 1, time.Second)
	if len(got) != 1 || got[0] != deferredSession.ID {
		t.Fatalf("pinned-set notifications = %v, want %s", got, deferredSession.ID)
	}
}

func postListenQuery(t *testing.T, base, rawQuery string, id any) *http.Response {
	t.Helper()
	body := statelessBody(t, id, MethodSubscriptionsListen, map[string]any{"notifications": listenNotifications(true, false, false, nil)})
	req, err := http.NewRequest(http.MethodPost, base+"/mcp?"+rawQuery, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", StatelessProtocolVersion)
	req.Header.Set(headerMcpMethod, MethodSubscriptionsListen)
	opened, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

func TestDiscoveryRequestRejected(t *testing.T) {
	if got := unsupportedDiscoveryMessage("lazy"); got != `unsupported discovery mode "lazy" (accepted: deferred, full)` {
		t.Fatalf("message = %q", got)
	}
	if !discoveryValueRejected("lazy") || discoveryValueRejected("") || discoveryValueRejected("deferred") {
		t.Fatal("rejection predicate")
	}
}

func TestLintAlwaysExpose_WarnsOnce(t *testing.T) {
	g, ctrl := deferredFixture(t)
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	addNamedTools(g, ctrl, "alpha", []Tool{{Name: "echo", Description: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	g.SetToolDiscovery(DiscoveryDeferred, []string{"alpha__missing"})
	if !strings.Contains(logs.String(), "alpha__missing") {
		t.Fatalf("lint missing: %s", logs.String())
	}
	logs.Reset()
	g.lintAlwaysExpose()
	if logs.Len() != 0 {
		t.Fatalf("repeated lint: %s", logs.String())
	}
}
