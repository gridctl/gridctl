package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// listOrderSchema is shared so byte comparisons measure order, not schema formatting.
var listOrderSchema = json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)

func listOrderTool(name string) Tool {
	return Tool{Name: name, Description: "d-" + name, InputSchema: listOrderSchema}
}

func listOrderTools(names ...string) []Tool {
	tools := make([]Tool, len(names))
	for i, name := range names {
		tools[i] = listOrderTool(name)
	}
	return tools
}

// listOrderGateway registers servers in the given order, each with tools in
// the given (not alphabetical) order. Registration order is not list order.
func listOrderGateway(order []string, tools map[string][]Tool) *Gateway {
	g := NewGateway()
	for _, name := range order {
		c := newFakeRPCClient(name, &fakeTransport{})
		c.SetTools(tools[name])
		g.Router().AddClient(c)
	}
	return g
}

func listOrderFleet() (map[string][]Tool, []string) {
	tools := map[string][]Tool{
		"alpha": listOrderTools("t2", "t1"),
		"mid":   listOrderTools("m2", "m1"),
		"zeta":  listOrderTools("z2", "z1"),
	}
	return tools, []string{"alpha__t2", "alpha__t1", "mid__m2", "mid__m1", "zeta__z2", "zeta__z1"}
}

func mustListNames(t *testing.T, g *Gateway, ctx context.Context) ([]string, []byte) {
	t.Helper()
	result, err := g.HandleToolsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	return names, raw
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func isSubsequence(seq, of []string) bool {
	i := 0
	for _, name := range of {
		if i < len(seq) && seq[i] == name {
			i++
		}
	}
	return i == len(seq)
}

// TestListOrder_HandleToolsListBytes pins tools/list across two calls and a
// rebuilt gateway whose registration order differs.
func TestListOrder_HandleToolsListBytes(t *testing.T) {
	tools, want := listOrderFleet()
	first := listOrderGateway([]string{"zeta", "alpha", "mid"}, tools)
	ctx := context.Background()

	names, raw1 := mustListNames(t, first, ctx)
	assertNames(t, names, want)
	_, raw2 := mustListNames(t, first, ctx)
	if !bytes.Equal(raw1, raw2) {
		t.Fatalf("consecutive HandleToolsList bytes differ\n first %s\nsecond %s", raw1, raw2)
	}

	rebuilt := listOrderGateway([]string{"mid", "zeta", "alpha"}, tools)
	_, raw3 := mustListNames(t, rebuilt, ctx)
	if !bytes.Equal(raw1, raw3) {
		t.Fatalf("rebuilt gateway bytes differ\n first %s\nrebuild %s", raw1, raw3)
	}
}

func statelessToolsRaw(t *testing.T, srv *StreamableHTTPServer) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, statelessRequest(t, statelessBody(t, 1, "tools/list", nil)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Result struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.Bytes())
	}
	if envelope.Error != nil {
		t.Fatalf("tools/list error: %s", envelope.Error.Message)
	}
	if len(envelope.Result.Tools) == 0 || envelope.Result.Tools[0] != '[' {
		t.Fatalf("result.tools is not a JSON array: %s", envelope.Result.Tools)
	}
	return append([]byte(nil), envelope.Result.Tools...)
}

// TestListOrder_StatelessToolsBytes compares the raw result.tools array from
// two consecutive stateless POSTs, then from a gateway rebuilt with a
// different registration permutation.
func TestListOrder_StatelessToolsBytes(t *testing.T) {
	tools, _ := listOrderFleet()
	srv := NewStreamableHTTPServer(listOrderGateway([]string{"zeta", "alpha", "mid"}, tools), nil)
	first := statelessToolsRaw(t, srv)
	second := statelessToolsRaw(t, srv)
	if !bytes.Equal(first, second) {
		t.Fatalf("consecutive result.tools bytes differ\n first %s\nsecond %s", first, second)
	}
	rebuilt := statelessToolsRaw(t, NewStreamableHTTPServer(listOrderGateway([]string{"mid", "zeta", "alpha"}, tools), nil))
	if !bytes.Equal(first, rebuilt) {
		t.Fatalf("rebuilt result.tools bytes differ\n first %s\nrebuild %s", first, rebuilt)
	}
}

// TestListOrder_ScopedAndGroupKeepRelativeOrder checks client-profile
// filtering, a group without overrides, a renaming group (compared on
// pre-rewrite names), and a tools: whitelist.
func TestListOrder_ScopedAndGroupKeepRelativeOrder(t *testing.T) {
	tools, unscopedWant := listOrderFleet()
	g := listOrderGateway([]string{"zeta", "mid", "alpha"}, tools)
	unscoped, _ := mustListNames(t, g, context.Background())
	assertNames(t, unscoped, unscopedWant)

	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{
			"cursor": {Tools: []string{"alpha__t1", "mid__m2", "zeta__z1"}},
		},
	}))
	scoped, _ := mustListNames(t, g, WithClientAccessID(context.Background(), "cursor"))
	wantScoped := []string{"alpha__t1", "mid__m2", "zeta__z1"}
	assertNames(t, scoped, wantScoped)
	if !isSubsequence(scoped, unscoped) {
		t.Fatalf("scoped %v is not a subsequence of %v", scoped, unscoped)
	}

	grouped := listOrderGateway([]string{"zeta", "mid", "alpha"}, tools)
	grouped.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
		"dev": {Servers: []string{"alpha", "zeta"}, Exclude: []string{"alpha__t2"}},
	}))
	projected, _ := mustListNames(t, grouped, WithGroup(context.Background(), "dev"))
	wantProjected := []string{"alpha__t1", "zeta__z2", "zeta__z1"}
	assertNames(t, projected, wantProjected)
	if !isSubsequence(projected, unscoped) {
		t.Fatalf("projected %v is not a subsequence of %v", projected, unscoped)
	}

	// releaseSpec renames github__create_issue to create_issue, so the
	// projected names are not a subsequence of the unscoped names. Compare
	// the pre-rewrite (aggregated) names instead.
	github := newFakeRPCClient("github", &fakeTransport{})
	github.SetTools(listOrderTools("create_issue", "search_code", "delete_repo"))
	gitlab := newFakeRPCClient("gitlab", &fakeTransport{})
	gitlab.SetTools(listOrderTools("create_merge_request", "list_projects"))
	renamed := NewGateway()
	renamed.Router().AddClient(gitlab)
	renamed.Router().AddClient(github)
	renamed.SetGroupPolicy(NewGroupPolicy(releaseSpec()))
	base, _ := mustListNames(t, renamed, context.Background())
	wantBase := []string{
		"github__create_issue", "github__search_code", "github__delete_repo",
		"gitlab__create_merge_request", "gitlab__list_projects",
	}
	assertNames(t, base, wantBase)
	exposed, _ := mustListNames(t, renamed, WithGroup(context.Background(), "release"))
	// Map the exposure name back to the aggregated name. Other names are unchanged.
	alias := map[string]string{"create_issue": "github__create_issue"}
	preRewrite := make([]string, len(exposed))
	for i, name := range exposed {
		if canon, ok := alias[name]; ok {
			preRewrite[i] = canon
			continue
		}
		preRewrite[i] = name
	}
	wantPre := []string{"github__create_issue", "github__search_code", "gitlab__create_merge_request"}
	assertNames(t, preRewrite, wantPre)
	if !isSubsequence(preRewrite, base) {
		t.Fatalf("pre-rewrite %v is not a subsequence of %v", preRewrite, base)
	}

	zeta := newFakeRPCClient("zeta", &fakeTransport{})
	zeta.SetTools(listOrderTools("z2", "z1"))
	zeta.SetToolWhitelist([]string{"z1"})
	filtered := NewGateway()
	alpha := newFakeRPCClient("alpha", &fakeTransport{})
	alpha.SetTools(listOrderTools("t2", "t1"))
	filtered.Router().AddClient(zeta)
	filtered.Router().AddClient(alpha)
	got, _ := mustListNames(t, filtered, context.Background())
	wantFiltered := []string{"alpha__t2", "alpha__t1", "zeta__z1"}
	assertNames(t, got, wantFiltered)
	if !isSubsequence(got, unscopedWant) {
		t.Fatalf("whitelist %v is not a subsequence of %v", got, unscopedWant)
	}
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func orderListingClient(name string, prompts, resources, templates []json.RawMessage) *RPCClient {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			env, ok := result.(*listEnvelope)
			if !ok {
				return errNotListEnvelope
			}
			switch method {
			case "prompts/list":
				env.Prompts = append([]json.RawMessage(nil), prompts...)
			case "resources/list":
				env.Resources = append([]json.RawMessage(nil), resources...)
			case "resources/templates/list":
				env.ResourceTemplates = append([]json.RawMessage(nil), templates...)
			default:
				return errUnexpectedListMethod
			}
			return nil
		},
	}
	c := newFakeRPCClient(name, ft)
	c.SetDownstreamCapabilities(Capabilities{
		Prompts:   &PromptsCapability{},
		Resources: &ResourcesCapability{},
	})
	return c
}

type listOrderError string

func (e listOrderError) Error() string { return string(e) }

const (
	errNotListEnvelope      listOrderError = "list result is not a list envelope"
	errUnexpectedListMethod listOrderError = "unexpected list method"
)

func orderRegistry(skills []PromptData) AgentClient {
	return &promptProviderClient{
		AgentClient: newFakeRPCClient("registry", &fakeTransport{}),
		prompts:     skills,
	}
}

func rawNames(t *testing.T, entries []json.RawMessage, key string) []string {
	t.Helper()
	names := make([]string, len(entries))
	for i, raw := range entries {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		names[i] = jsonString(obj[key])
	}
	return names
}

func assertRawEqual(t *testing.T, a, b []json.RawMessage) {
	t.Helper()
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ab, bb) {
		t.Fatalf("entry bytes differ\n first %s\nsecond %s", ab, bb)
	}
}

// TestListOrder_PromptsResourcesTemplates pins registry-first prompt and
// resource lists, and the downstream-only template list, across two calls
// and a shuffled registration rebuild. Registry name order comes from the
// provider (the real store sorts); the gateway preserves that order and
// then appends downstream entries by server name.
func TestListOrder_PromptsResourcesTemplates(t *testing.T) {
	skills := []PromptData{
		{Name: "alpha-skill", Description: "first"},
		{Name: "zeta-skill", Description: "second"},
	}
	midPrompts := []json.RawMessage{
		mustRaw(t, map[string]string{"name": "p2", "description": "later"}),
		mustRaw(t, map[string]string{"name": "p1", "description": "earlier"}),
	}
	zetaPrompts := []json.RawMessage{
		mustRaw(t, map[string]string{"name": "q2", "description": "later"}),
		mustRaw(t, map[string]string{"name": "q1", "description": "earlier"}),
	}
	midResources := []json.RawMessage{
		mustRaw(t, map[string]string{"uri": "file://mid/b", "name": "b"}),
		mustRaw(t, map[string]string{"uri": "file://mid/a", "name": "a"}),
	}
	zetaResources := []json.RawMessage{
		mustRaw(t, map[string]string{"uri": "file://zeta/b", "name": "b"}),
		mustRaw(t, map[string]string{"uri": "file://zeta/a", "name": "a"}),
	}
	midTemplates := []json.RawMessage{
		mustRaw(t, map[string]string{"uriTemplate": "file://mid/{b}", "name": "b"}),
		mustRaw(t, map[string]string{"uriTemplate": "file://mid/{a}", "name": "a"}),
	}
	zetaTemplates := []json.RawMessage{
		mustRaw(t, map[string]string{"uriTemplate": "file://zeta/{b}", "name": "b"}),
		mustRaw(t, map[string]string{"uriTemplate": "file://zeta/{a}", "name": "a"}),
	}

	build := func(order []string) *Gateway {
		t.Helper()
		g := NewGateway()
		clients := map[string]AgentClient{
			"registry": orderRegistry(skills),
			"mid":      orderListingClient("mid", midPrompts, midResources, midTemplates),
			"zeta":     orderListingClient("zeta", zetaPrompts, zetaResources, zetaTemplates),
		}
		for _, name := range order {
			g.Router().AddClient(clients[name])
		}
		return g
	}

	ctx := context.Background()
	first := build([]string{"zeta", "registry", "mid"})
	prompts1, err := first.HandlePromptsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prompts2, err := first.HandlePromptsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertRawEqual(t, prompts1.Prompts, prompts2.Prompts)
	wantPrompts := []string{"alpha-skill", "zeta-skill", "mid__p2", "mid__p1", "zeta__q2", "zeta__q1"}
	assertNames(t, rawNames(t, prompts1.Prompts, "name"), wantPrompts)

	resources1, err := first.HandleResourcesList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resources2, err := first.HandleResourcesList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertRawEqual(t, resources1.Resources, resources2.Resources)
	wantResources := []string{
		"skills://registry/alpha-skill", "skills://registry/zeta-skill",
		"file://mid/b", "file://mid/a", "file://zeta/b", "file://zeta/a",
	}
	assertNames(t, rawNames(t, resources1.Resources, "uri"), wantResources)

	templates1 := first.HandleResourceTemplatesList(ctx)
	templates2 := first.HandleResourceTemplatesList(ctx)
	assertRawEqual(t, templates1.ResourceTemplates, templates2.ResourceTemplates)
	wantTemplates := []string{"file://mid/{b}", "file://mid/{a}", "file://zeta/{b}", "file://zeta/{a}"}
	assertNames(t, rawNames(t, templates1.ResourceTemplates, "uriTemplate"), wantTemplates)
	for _, uri := range rawNames(t, templates1.ResourceTemplates, "uriTemplate") {
		if len(uri) >= len("skills://") && uri[:len("skills://")] == "skills://" {
			t.Fatalf("registry contributed a template: %s", uri)
		}
	}

	rebuilt := build([]string{"mid", "zeta", "registry"})
	prompts3, err := rebuilt.HandlePromptsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertRawEqual(t, prompts1.Prompts, prompts3.Prompts)
	resources3, err := rebuilt.HandleResourcesList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertRawEqual(t, resources1.Resources, resources3.Resources)
	templates3 := rebuilt.HandleResourceTemplatesList(ctx)
	assertRawEqual(t, templates1.ResourceTemplates, templates3.ResourceTemplates)
}

// TestListOrder_RefreshKeepsRelativeOrder adds one tool in the middle of a
// server's own list and refreshes the router. Entries that were already
// present keep their relative order, in the server's order rather than name order.
func TestListOrder_RefreshKeepsRelativeOrder(t *testing.T) {
	tools, before := listOrderFleet()
	zeta := newFakeRPCClient("zeta", &fakeTransport{})
	zeta.SetTools(tools["zeta"])
	alpha := newFakeRPCClient("alpha", &fakeTransport{})
	alpha.SetTools(tools["alpha"])
	mid := newFakeRPCClient("mid", &fakeTransport{})
	mid.SetTools(tools["mid"])
	g := NewGateway()
	// Registration order is the reverse of name order.
	g.Router().AddClient(zeta)
	g.Router().AddClient(alpha)
	g.Router().AddClient(mid)
	got, _ := mustListNames(t, g, context.Background())
	assertNames(t, got, before)

	zeta.SetTools(listOrderTools("z2", "z9", "z1"))
	g.Router().RefreshTools()

	after, _ := mustListNames(t, g, context.Background())
	want := []string{"alpha__t2", "alpha__t1", "mid__m2", "mid__m1", "zeta__z2", "zeta__z9", "zeta__z1"}
	assertNames(t, after, want)
	if !isSubsequence(before, after) {
		t.Fatalf("pre-refresh %v is not a subsequence of %v", before, after)
	}
}
