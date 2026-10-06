package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

func TestAggregateRawListCacheMeta(t *testing.T) {
	ttl := func(n int64) *int64 { return &n }
	public := RawListPage{Entries: []json.RawMessage{[]byte(`{}`)}, TTLMs: ttl(30000), CacheScope: CacheScopePublic}
	handshake := RawListPage{Entries: []json.RawMessage{[]byte(`{}`)}}

	registryOnly := aggregateRawListCacheMeta(true, nil)
	if registryOnly.TTLMs == nil || *registryOnly.TTLMs != skillResourceTTLMs || registryOnly.CacheScope != CacheScopePrivate {
		t.Fatalf("registry-only = %+v", registryOnly)
	}
	one := aggregateRawListCacheMeta(true, []RawListPage{public})
	if one.TTLMs == nil || *one.TTLMs != 30000 || one.CacheScope != CacheScopePrivate {
		t.Fatalf("registry plus public downstream = %+v", one)
	}
	mixed := aggregateRawListCacheMeta(true, []RawListPage{public, handshake})
	if mixed.TTLMs == nil || *mixed.TTLMs != 0 {
		t.Fatalf("handshake contributor = %+v", mixed)
	}
	onlyPublic := aggregateRawListCacheMeta(false, []RawListPage{public})
	if onlyPublic.CacheScope != CacheScopePublic || onlyPublic.TTLMs == nil || *onlyPublic.TTLMs != 30000 {
		t.Fatalf("public downstream only = %+v", onlyPublic)
	}
}

func TestCompileURITemplate(t *testing.T) {
	re, err := compileURITemplate("file:///{name}.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("file:///a.txt") || re.MatchString("file:///a/b.txt") {
		t.Fatalf("segment match failed: %s", re)
	}
	plus, err := compileURITemplate("ui://app/{+path}")
	if err != nil {
		t.Fatal(err)
	}
	if !plus.MatchString("ui://app/a/b") || plus.MatchString("ui://other/a") {
		t.Fatalf("reserved match failed: %s", plus)
	}
	literal, err := compileURITemplate("file:///a.txt?x=1")
	if err != nil {
		t.Fatal(err)
	}
	if !literal.MatchString("file:///a.txt?x=1") || literal.MatchString("file:///aXtxt?x=1") {
		t.Fatalf("literal quote failed: %s", literal)
	}
}

func TestRouter_ResourceCollisionAndResolve(t *testing.T) {
	r := NewRouter()
	if fresh := r.SetResourceIndex("b", []string{"file:///same"}, []string{"file:///{name}"}); len(fresh) != 0 {
		t.Fatalf("first publisher should not collide: %+v", fresh)
	}
	fresh := r.SetResourceIndex("a", []string{"file:///same"}, []string{"file:///{name}"})
	if len(fresh) != 2 {
		t.Fatalf("collisions = %+v, want uri and template", fresh)
	}
	owner, ok := r.ResolveResource("file:///same")
	if !ok || owner != "a" {
		t.Fatalf("exact owner = %q %v, want a", owner, ok)
	}
	if got, _ := r.ResourceOwner("file:///same"); got != "a" {
		t.Fatalf("list owner = %q", got)
	}
	if r.TemplateOwner("b", "file:///{name}") {
		t.Fatal("losing template should be omitted")
	}
	tmplOwner, ok := r.ResolveResource("file:///only-template")
	if !ok || tmplOwner != "a" {
		t.Fatalf("template owner = %q %v", tmplOwner, ok)
	}
	status := r.ResourceStatus("b")
	if status.Collisions != 2 {
		t.Fatalf("loser collisions = %d, want 2", status.Collisions)
	}
	again := r.SetResourceIndex("a", []string{"file:///same"}, nil)
	if len(again) != 0 {
		t.Fatalf("collision logged twice: %+v", again)
	}
	r.RemoveClient("a")
	if _, ok := r.ResolveResource("file:///same"); !ok {
		// b still publishes it
		t.Fatal("remaining publisher should own the URI after the winner leaves")
	}
}

func TestRouter_UIResourceIndex(t *testing.T) {
	r := NewRouter()
	r.SetUIResourceIndex("app", []string{"ui://app/canvas"})
	server, ok := r.ResolveResource("ui://app/canvas")
	if !ok || server != "app" {
		t.Fatalf("ui index = %q %v", server, ok)
	}
	r.SetResourceIndex("other", []string{"ui://app/canvas"}, nil)
	server, _ = r.ResolveResource("ui://app/canvas")
	if server != "other" {
		t.Fatalf("exact match should beat ui index, got %q", server)
	}
	r.RemoveClient("other")
	r.RemoveClient("app")
	if _, ok := r.ResolveResource("ui://app/canvas"); ok {
		t.Fatal("removed server URI still resolved")
	}
}

func TestUIResourceURIs(t *testing.T) {
	meta := json.RawMessage(`{"ui":{"resourceUri":"ui://app/main"},"ui/resourceUri":"ui://app/legacy"}`)
	got := uiResourceURIs(meta)
	if len(got) != 2 || got[0] != "ui://app/main" || got[1] != "ui://app/legacy" {
		t.Fatalf("uris = %#v", got)
	}
	if uiResourceURIs(nil) != nil {
		t.Fatal("absent _meta should not index")
	}
}

func TestAllowsServer(t *testing.T) {
	policy := NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"only-a": {Servers: []string{"a"}},
			"tool-a": {Tools: []string{"a__x"}},
			"open":   {},
		},
	})
	if !policy.AllowsServer("only-a", "a") || policy.AllowsServer("only-a", "b") {
		t.Fatal("server allow-list")
	}
	if !policy.AllowsServer("tool-a", "a") || policy.AllowsServer("tool-a", "b") {
		t.Fatal("tool half should admit the server")
	}
	if !policy.AllowsServer("open", "b") {
		t.Fatal("empty profile should allow every server")
	}
	if policy.AllowsServer("stranger", "a") {
		t.Fatal("unlisted client should follow default deny")
	}
	if !(*ClientAccessPolicy)(nil).AllowsServer("anyone", "a") {
		t.Fatal("nil policy allows all")
	}
}

func TestServerIsMemberExclude(t *testing.T) {
	policy := NewGroupPolicy(GroupsSpec{
		"none":  {Tools: []string{"a__read"}, Exclude: []string{"a__read"}},
		"some":  {Servers: []string{"a"}, Exclude: []string{"a__write"}},
		"other": {Servers: []string{"b"}},
	})
	if policy.ServerIsMember("none", "a", []string{"a__read"}) {
		t.Fatal("excluded tool must not expose the server")
	}
	if !policy.ServerIsMember("some", "a", []string{"a__read", "a__write"}) {
		t.Fatal("a remaining member tool should expose the server")
	}
	if policy.ServerIsMember("other", "a", []string{"a__read"}) {
		t.Fatal("unrelated server")
	}
}

func TestHandshakeResourcesReadNotFoundCode(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	session := initializeStreamable(t, srv)
	resp := streamablePost(t, srv, session, "resources/read", map[string]any{"uri": "file:///missing"})
	if resp.Error == nil || resp.Error.Code != ErrCodeResourceNotFound {
		t.Fatalf("error = %+v, want %d", resp.Error, ErrCodeResourceNotFound)
	}
	data, _ := json.Marshal(resp.Error.Data)
	if !strings.Contains(string(data), "file:///missing") {
		t.Fatalf("data = %s", data)
	}
}

func TestInitializeDeclaresUIExtension(t *testing.T) {
	var saw bool
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, params any, result any) error {
			if method != "initialize" {
				return nil
			}
			raw, _ := json.Marshal(params)
			if strings.Contains(string(raw), UIExtensionID) && strings.Contains(string(raw), UIExtensionMIME) {
				saw = true
			}
			out, _ := json.Marshal(InitializeResult{ProtocolVersion: MCPProtocolVersion, ServerInfo: ServerInfo{Name: "s", Version: "1"}})
			return json.Unmarshal(out, result)
		},
	}
	r := newFakeRPCClient("s", ft)
	r.SetProtocolExtensions([]string{UIExtensionID})
	r.SetGenerationPin(GenerationHandshake)
	if err := r.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !saw {
		t.Fatal("handshake initialize omitted the UI extension")
	}
}

func TestInputRequiredRelayMessageNoun(t *testing.T) {
	prompt := inputRequiredRelayMessage("prompt")
	resource := inputRequiredRelayMessage("resource")
	if !strings.HasPrefix(prompt, "prompt requires") || strings.Contains(prompt, "resource requires") {
		t.Fatalf("prompt message = %q", prompt)
	}
	if !strings.HasPrefix(resource, "resource requires") || !strings.Contains(resource, "MRTR") {
		t.Fatalf("resource message = %q", resource)
	}
}

func TestRouter_ServerToolsPickAndListMutations(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := NewRouter()
	client := setupMockAgentClient(ctrl, "docs", []Tool{{Name: "echo"}})
	r.AddClient(client)
	r.RefreshTools()
	names := r.ServerToolNames("docs")
	if len(names) != 1 || names[0] != "docs__echo" {
		t.Fatalf("tools = %#v", names)
	}
	if _, err := r.PickServer("docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PickServer("missing"); err == nil {
		t.Fatal("expected pick error")
	}
	if fresh := r.ReplaceServerURIs("docs", []string{"file:///a"}); len(fresh) != 0 {
		t.Fatalf("fresh uris = %+v", fresh)
	}
	if fresh := r.ReplaceServerTemplates("docs", []string{"file:///tmpl/{name}"}); len(fresh) != 0 {
		t.Fatalf("fresh templates = %+v", fresh)
	}
	if owner, ok := r.ResolveResource("file:///a"); !ok || owner != "docs" {
		t.Fatalf("uri owner = %q %v", owner, ok)
	}
	r.SetResourceListError("docs", "rpc")
	if r.ResourceStatus("docs").ListError != "rpc" {
		t.Fatal("error not recorded")
	}
	r.SetResourceListError("docs", "")
	if r.ResourceStatus("docs").ListError != "" {
		t.Fatal("error not cleared")
	}
}

func TestFanOutListCallerCancelSkipsStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := &listingClient{
		AgentClient: setupMockAgentClient(ctrl, "docs", nil),
		resources: func(ctx context.Context) (RawListPage, error) {
			return RawListPage{}, ctx.Err()
		},
	}
	g.Router().AddClient(client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.HandleResourcesList(ctx); err != nil {
		t.Fatal(err)
	}
	if got := g.router.ResourceStatus("docs").ListError; got != "" {
		t.Fatalf("caller cancel recorded as %q", got)
	}

	client.resources = func(context.Context) (RawListPage, error) {
		return RawListPage{}, errors.New("boom")
	}
	if _, err := g.HandleResourcesList(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := g.router.ResourceStatus("docs").ListError; got != "transport" {
		t.Fatalf("list error = %q, want transport", got)
	}
}

func TestRefreshResourceIndexFansOutConcurrently(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	started := make(chan string, 2)
	release := make(chan struct{})
	block := func(kind string) func(context.Context) (RawListPage, error) {
		return func(ctx context.Context) (RawListPage, error) {
			started <- kind
			select {
			case <-release:
				return RawListPage{}, nil
			case <-ctx.Done():
				return RawListPage{}, ctx.Err()
			}
		}
	}
	g.Router().AddClient(&listingClient{
		AgentClient: setupMockAgentClient(ctrl, "docs", nil),
		resources:   block("resources"),
		templates:   block("templates"),
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.refreshResourceIndex(context.Background())
	}()
	seen := map[string]bool{}
	timeout := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case kind := <-started:
			seen[kind] = true
		case <-timeout:
			t.Fatal("resource and template fan-outs did not overlap")
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not finish")
	}
}

type listingClient struct {
	AgentClient
	resources func(context.Context) (RawListPage, error)
	templates func(context.Context) (RawListPage, error)
}

func (c *listingClient) DownstreamCapabilities() Capabilities {
	return Capabilities{Resources: &ResourcesCapability{}}
}

func (c *listingClient) ListResources(ctx context.Context) (RawListPage, error) {
	if c.resources == nil {
		return RawListPage{}, nil
	}
	return c.resources(ctx)
}

func (c *listingClient) ListResourceTemplates(ctx context.Context) (RawListPage, error) {
	if c.templates == nil {
		return RawListPage{}, nil
	}
	return c.templates(ctx)
}
