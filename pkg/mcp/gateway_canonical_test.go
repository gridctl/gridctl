package mcp

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/gridctl/gridctl/pkg/runs"
)

func TestCallCanonicalTool_MatchesHandleToolsCallSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{
		Content: []Content{NewTextContent("hello")},
	}, nil).Times(2)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	legacy, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "agent1__echo", Arguments: map[string]any{"m": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	got, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "agent1__echo", Arguments: map[string]any{"m": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.IsError != got.IsError || legacy.Content[0].Text != got.Content[0].Text {
		t.Fatalf("wire result changed: legacy=%+v got=%+v", legacy, got)
	}
	if out.Disposition != runs.DispositionCompleted || out.Completion != CompletionComplete {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestCallCanonicalTool_UnknownAndHidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	alpha := setupMockAgentClient(ctrl, "alpha", []Tool{{Name: "echo"}})
	beta := setupMockAgentClient(ctrl, "beta", []Tool{{Name: "secret"}})
	alpha.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	beta.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	g.Router().AddClient(alpha)
	g.Router().AddClient(beta)
	g.Router().RefreshTools()
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"cli": {Servers: []string{"alpha"}},
		},
	}))
	ctx := WithClientAccessID(WithClientID(context.Background(), "cli"), "cli")

	t.Run("out of scope is denied not unknown", func(t *testing.T) {
		res, out, err := g.CallCanonicalTool(ctx, ToolCallParams{Name: "beta__secret"})
		if err != nil || !res.IsError {
			t.Fatalf("expected denial: %v %+v", err, res)
		}
		if out.Disposition != runs.DispositionDenied || out.Reason != runs.ReasonClientScope {
			t.Fatalf("outcome = %+v", out)
		}
		if out.Completion != CompletionNotStarted {
			t.Fatalf("completion = %s", out.Completion)
		}
	})

	t.Run("missing in-scope tool is unknown", func(t *testing.T) {
		res, out, err := g.CallCanonicalTool(ctx, ToolCallParams{Name: "alpha__missing"})
		if err != nil || !res.IsError {
			t.Fatalf("expected unknown: %v %+v", err, res)
		}
		if out.Reason != runs.ReasonUnknownTool || out.Stage != runs.StageRouting {
			t.Fatalf("outcome = %+v", out)
		}
		if !strings.Contains(res.Content[0].Text, "unknown tool") {
			t.Fatalf("message = %q", res.Content[0].Text)
		}
	})

	t.Run("meta-tool rejected", func(t *testing.T) {
		res, out, err := g.CallCanonicalTool(ctx, ToolCallParams{Name: MetaToolSearch})
		if err != nil || !res.IsError {
			t.Fatalf("expected rejection: %v %+v", err, res)
		}
		if out.Reason != runs.ReasonClientScope {
			t.Fatalf("unparseable meta-tool must not disclose inventory: %+v", out)
		}
	})

	t.Run("empty halves unknown after scope", func(t *testing.T) {
		res, out, err := g.CallCanonicalTool(ctx, ToolCallParams{Name: "alpha__"})
		if err != nil || !res.IsError {
			t.Fatalf("expected rejection: %v %+v", err, res)
		}
		if out.Reason != runs.ReasonUnknownTool {
			t.Fatalf("outcome = %+v", out)
		}
	})
}

func TestCallCanonicalTool_ReplicaInventoryRecheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	r0 := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	r0.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	r1 := NewMockAgentClient(ctrl)
	r1.EXPECT().Name().Return("agent1").AnyTimes()
	r1.EXPECT().Tools().Return([]Tool{{Name: "other"}}).AnyTimes()
	r1.EXPECT().IsInitialized().Return(true).AnyTimes()
	r1.EXPECT().ServerInfo().Return(ServerInfo{Name: "agent1", Version: "1.0.0"}).AnyTimes()
	r1.EXPECT().Initialize(gomock.Any()).Return(nil).AnyTimes()
	r1.EXPECT().RefreshTools(gomock.Any()).Return(nil).AnyTimes()
	r1.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	g.Router().AddReplicaSet(NewReplicaSet("agent1", ReplicaPolicyRoundRobin, []AgentClient{r0, r1}))
	g.Router().RefreshTools()

	// Round-robin first pick is replica 0. Force the second pick.
	if _, err := g.Router().GetReplicaSet("agent1").Pick(); err != nil {
		t.Fatal(err)
	}
	res, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "agent1__echo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || out.Reason != runs.ReasonUnknownTool {
		t.Fatalf("expected unknown after replica recheck: %+v %+v", res, out)
	}
}

func TestCallCanonicalTool_StaleCatalogRemoved(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.Router().RemoveClient("agent1")
	g.Router().RefreshTools()

	res, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "agent1__echo"})
	if err != nil || !res.IsError {
		t.Fatalf("stale target authorized: %v %+v", err, res)
	}
	if out.Reason != runs.ReasonUnknownTool {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestCallCanonicalTool_GateName(t *testing.T) {
	g, _ := newGateTestGateway(t, true)
	deny := &stubGate{name: "rate-limit", decision: GateDeny("slow down")}
	g.SetCallGates([]CallGate{deny})
	res, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "github__search"})
	if err != nil || !res.IsError {
		t.Fatalf("expected gate denial: %v %+v", err, res)
	}
	if out.Disposition != runs.DispositionDenied || out.Reason != runs.ReasonGateDenied || out.Gate != "rate-limit" {
		t.Fatalf("outcome = %+v", out)
	}
	if out.Completion != CompletionNotStarted {
		t.Fatalf("completion = %s", out.Completion)
	}
}

func TestHandleToolsCall_UnchangedScopeDenial(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "beta", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default:  "deny",
		Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"alpha"}}},
	}))
	res, err := g.HandleToolsCall(WithClientAccessID(context.Background(), "cursor"), ToolCallParams{Name: "beta__echo"})
	if err != nil || !res.IsError {
		t.Fatalf("expected scope denial: %v %+v", err, res)
	}
	if !strings.Contains(res.Content[0].Text, "access scope") {
		t.Fatalf("message = %q", res.Content[0].Text)
	}
}
