package mcp

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"
)

func discoveryGateway(t *testing.T) *Gateway {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()
	alpha := setupMockAgentClient(ctrl, "alpha", []Tool{
		{Name: "echo", Description: "repeat a payload", InputSchema: []byte(`{"type":"object","properties":{"message":{"type":"string"}}}`)},
		{Name: "sum", Description: "add numbers", InputSchema: []byte(`{"type":"object","properties":{"left":{"type":"number"}}}`)},
	})
	beta := setupMockAgentClient(ctrl, "beta", []Tool{
		{Name: "secret", Description: "classified", InputSchema: []byte(`{"type":"object","properties":{}}`)},
	})
	g.Router().AddClient(alpha)
	g.Router().AddClient(beta)
	g.Router().RefreshTools()
	return g
}

func TestDiscoverTools_CodeModeDoesNotChangeMembership(t *testing.T) {
	g := discoveryGateway(t)
	ctx := context.Background()
	before, err := g.DiscoverTools(ctx, DiscoverOptions{Query: ""})
	if err != nil {
		t.Fatal(err)
	}
	g.SetCodeMode(0)
	after, err := g.DiscoverTools(ctx, DiscoverOptions{Query: ""})
	if err != nil {
		t.Fatal(err)
	}
	if before.Matched != after.Matched || len(before.Tools) != len(after.Tools) {
		t.Fatalf("code mode changed discovery: before=%d after=%d", before.Matched, after.Matched)
	}
	for _, tool := range after.Tools {
		if tool.Name == MetaToolSearch || tool.Name == MetaToolExecute {
			t.Fatalf("discovery returned meta-tool %q", tool.Name)
		}
	}
}

func TestDiscoverTools_GeneratedDescriptionMatch(t *testing.T) {
	g := discoveryGateway(t)
	ctx := WithClientAccessID(context.Background(), "cli")
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"cli": {Servers: []string{"alpha"}},
		},
	}))
	for _, q := range []string{"mcp", "server", "call"} {
		res, err := g.DiscoverTools(ctx, DiscoverOptions{Query: q})
		if err != nil {
			t.Fatal(err)
		}
		if res.Matched != 2 || res.TotalVisible != 2 {
			t.Fatalf("query %q matched %d visible %d, want 2/2", q, res.Matched, res.TotalVisible)
		}
		for _, tool := range res.Tools {
			if strings.HasPrefix(tool.Name, "beta__") {
				t.Fatalf("hidden tool leaked for %q: %s", q, tool.Name)
			}
		}
	}
	res, err := g.DiscoverTools(ctx, DiscoverOptions{Query: "mcp", Server: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 2 {
		t.Fatalf("server filter matched %d", res.Matched)
	}
}

func TestDiscoverTools_ExactAndEmpty(t *testing.T) {
	g := discoveryGateway(t)
	ctx := context.Background()

	leaf, err := g.DiscoverTools(ctx, DiscoverOptions{Name: "alpha__echo"})
	if err != nil || leaf.Returned != 1 || leaf.Tools[0].Name != "alpha__echo" {
		t.Fatalf("leaf: %v %+v", err, leaf)
	}

	if _, err := g.DiscoverTools(ctx, DiscoverOptions{Name: "alpha__missing"}); err != ErrDiscoverNotFound {
		t.Fatalf("missing name: %v", err)
	}
	if _, err := g.DiscoverTools(ctx, DiscoverOptions{Server: "missing"}); err != ErrDiscoverNotFound {
		t.Fatalf("missing server: %v", err)
	}

	none, err := g.DiscoverTools(ctx, DiscoverOptions{Server: "alpha", Query: "zzzz"})
	if err != nil || none.Matched != 0 || none.TotalVisible != 2 || none.Returned != 0 || none.Truncated {
		t.Fatalf("no matches: %+v err=%v", none, err)
	}
	if none.Tools == nil {
		t.Fatal("empty tools must be a non-nil slice")
	}

	limited, err := g.DiscoverTools(ctx, DiscoverOptions{Query: "", Limit: 1})
	if err != nil || limited.Returned != 1 || !limited.Truncated || limited.Matched < 2 {
		t.Fatalf("limit: %+v err=%v", limited, err)
	}
	if limited.Tools[0].Name != "alpha__echo" {
		t.Fatalf("sort = %s", limited.Tools[0].Name)
	}
}

func TestDiscoverTools_DefaultDenyHidesCounts(t *testing.T) {
	g := discoveryGateway(t)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{Default: "deny"}))
	res, err := g.DiscoverTools(WithClientAccessID(context.Background(), "cli"), DiscoverOptions{Query: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalVisible != 0 || res.Matched != 0 || res.Returned != 0 {
		t.Fatalf("hidden tools counted: %+v", res)
	}
	if _, err := g.DiscoverTools(WithClientAccessID(context.Background(), "cli"), DiscoverOptions{Name: "alpha__echo"}); err != ErrDiscoverNotFound {
		t.Fatalf("hidden name: %v", err)
	}
}

func TestDiscoverTools_NoDispatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "alpha", []Tool{{Name: "echo", Description: "x"}})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	sink := &capturingSink{}
	g.SetRunSink(sink)
	deny := &stubGate{name: "rate-limit", decision: GateDeny("no")}
	g.SetCallGates([]CallGate{deny})

	if _, err := g.DiscoverTools(context.Background(), DiscoverOptions{Query: "echo"}); err != nil {
		t.Fatal(err)
	}
	if sink.last() != nil {
		t.Fatal("discovery recorded a run attempt")
	}
	if len(deny.calls) != 0 {
		t.Fatal("discovery consumed a call gate")
	}
}
