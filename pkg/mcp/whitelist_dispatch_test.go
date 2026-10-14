package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
)

// whitelistProbeClient embeds ClientBase so Tools() applies the whitelist
// the same way a real client does. The gomock client does not.
type whitelistProbeClient struct {
	ClientBase
	called []string
}

func (p *whitelistProbeClient) Name() string                       { return "alpha" }
func (p *whitelistProbeClient) Initialize(context.Context) error   { return nil }
func (p *whitelistProbeClient) RefreshTools(context.Context) error { return nil }
func (p *whitelistProbeClient) CallTool(_ context.Context, name string, _ map[string]any) (*ToolCallResult, error) {
	p.called = append(p.called, name)
	return &ToolCallResult{Content: []Content{NewTextContent("reached " + name)}}, nil
}

func newWhitelistGateway(t *testing.T) (*Gateway, *whitelistProbeClient) {
	t.Helper()
	c := &whitelistProbeClient{}
	c.SetTools([]Tool{
		{Name: "list_paths"},
		{Name: "read_secret"},
	})
	c.SetToolWhitelist([]string{"list_paths"})
	c.SetInitialized(ServerInfo{Name: "alpha"})
	g := NewGateway()
	g.Router().AddClient(c)
	g.Router().RefreshTools()
	g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
		"grp": {Servers: []string{"alpha"}},
	}))
	return g, c
}

func assertHiddenRejected(t *testing.T, res *ToolCallResult, err error, called []string, before int) {
	t.Helper()
	if err != nil {
		t.Fatalf("hidden call: %v", err)
	}
	if res == nil || !res.IsError || !res.unknownTool {
		t.Fatalf("hidden call = %+v", res)
	}
	if res.Content[0].Text != "Error: unknown tool" {
		t.Fatalf("hidden text = %q", res.Content[0].Text)
	}
	if len(called) != before {
		t.Fatalf("hidden call reached downstream: %v", called)
	}
}

func assertVisibleReached(t *testing.T, res *ToolCallResult, err error, called []string, before int) {
	t.Helper()
	if err != nil || res == nil || res.IsError {
		t.Fatalf("visible call failed: err=%v res=%+v", err, res)
	}
	if len(called) != before+1 || called[len(called)-1] != "list_paths" {
		t.Fatalf("visible call did not reach list_paths: %v", called)
	}
}

func TestDispatch_WhitelistHiddenToolRejectedOnEveryPath(t *testing.T) {
	g, c := newWhitelistGateway(t)
	hidden := ToolCallParams{Name: "alpha__read_secret"}
	visible := ToolCallParams{Name: "alpha__list_paths"}

	t.Run("direct", func(t *testing.T) {
		before := len(c.called)
		res, err := g.HandleToolsCall(context.Background(), hidden)
		assertHiddenRejected(t, res, err, c.called, before)
		res, err = g.HandleToolsCall(context.Background(), visible)
		assertVisibleReached(t, res, err, c.called, before)
	})

	t.Run("group session", func(t *testing.T) {
		before := len(c.called)
		ctx := WithGroup(context.Background(), "grp")
		res, err := g.HandleToolsCall(ctx, hidden)
		assertHiddenRejected(t, res, err, c.called, before)
		res, err = g.HandleToolsCall(ctx, visible)
		assertVisibleReached(t, res, err, c.called, before)
	})

	t.Run("code mode on direct prefixed name", func(t *testing.T) {
		g.SetCodeMode(5 * time.Second)
		before := len(c.called)
		res, err := g.HandleToolsCall(context.Background(), hidden)
		assertHiddenRejected(t, res, err, c.called, before)
		res, err = g.HandleToolsCall(context.Background(), visible)
		assertVisibleReached(t, res, err, c.called, before)
	})

	t.Run("code mode execute", func(t *testing.T) {
		g.SetCodeMode(5 * time.Second)
		before := len(c.called)
		denied, err := g.HandleToolsCall(context.Background(), ToolCallParams{
			Name: MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "read_secret", {}); })()`,
			},
		})
		if err != nil || denied == nil || !denied.IsError || !strings.Contains(denied.Content[0].Text, "access denied") {
			t.Fatalf("execute hidden = err=%v res=%+v", err, denied)
		}
		if len(c.called) != before {
			t.Fatalf("execute hidden reached downstream: %v", c.called)
		}
		ok, err := g.HandleToolsCall(context.Background(), ToolCallParams{
			Name: MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "list_paths", {}); })()`,
			},
		})
		if err != nil || ok == nil || ok.IsError {
			t.Fatalf("execute visible failed: err=%v res=%+v", err, ok)
		}
		if len(c.called) != before+1 || c.called[len(c.called)-1] != "list_paths" {
			t.Fatalf("execute visible did not reach list_paths: %v", c.called)
		}
	})

	t.Run("code mode search stays callable", func(t *testing.T) {
		g.SetCodeMode(5 * time.Second)
		before := len(c.called)
		res, err := g.HandleToolsCall(context.Background(), ToolCallParams{
			Name:      MetaToolSearch,
			Arguments: map[string]any{"query": "read_secret"},
		})
		if err != nil || res == nil || res.IsError {
			t.Fatalf("search failed: err=%v res=%+v", err, res)
		}
		if strings.Contains(res.Content[0].Text, "alpha__read_secret") {
			t.Fatalf("search exposed hidden tool: %s", res.Content[0].Text)
		}
		if len(c.called) != before {
			t.Fatalf("search reached downstream: %v", c.called)
		}
	})

	t.Run("canonical", func(t *testing.T) {
		sink := &capturingSink{}
		g.SetRunSink(sink)
		before := len(c.called)
		res, out, err := g.CallCanonicalTool(context.Background(), hidden)
		assertHiddenRejected(t, res, err, c.called, before)
		if out.Disposition != runs.DispositionRoutingFailed || out.Stage != runs.StageRouting || out.Reason != runs.ReasonUnknownTool {
			t.Fatalf("outcome = %+v", out)
		}
		if out.Completion != CompletionNotStarted {
			t.Fatalf("completion = %s", out.Completion)
		}
		a := sink.last()
		if a == nil || a.server != "" || a.tool != "" || !a.finished {
			t.Fatalf("run record resolved a server: %+v", a)
		}
		res, _, err = g.CallCanonicalTool(context.Background(), visible)
		assertVisibleReached(t, res, err, c.called, before)
	})
}

func TestDispatch_WhitelistHiddenToolRejectedUnderServersOnlyProfile(t *testing.T) {
	g, c := newWhitelistGateway(t)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"cli": {Servers: []string{"alpha"}},
		},
	}))
	ctx := WithClientAccessID(context.Background(), "cli")
	res, out, err := g.CallCanonicalTool(ctx, ToolCallParams{Name: "alpha__read_secret"})
	assertHiddenRejected(t, res, err, c.called, 0)
	if out.Reason != runs.ReasonUnknownTool || out.Disposition != runs.DispositionRoutingFailed {
		t.Fatalf("servers-only profile disclosed scope instead of unknown: %+v", out)
	}
}

func TestDispatch_WhitelistHiddenToolRejectedWhenNamedByProfileOrGroup(t *testing.T) {
	t.Run("profile tools", func(t *testing.T) {
		g, c := newWhitelistGateway(t)
		g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
			Default: "deny",
			Profiles: map[string]ClientProfileSpec{
				"cli": {Tools: []string{"alpha__read_secret", "alpha__list_paths"}},
			},
		}))
		ctx := WithClientAccessID(context.Background(), "cli")
		res, out, err := g.CallCanonicalTool(ctx, ToolCallParams{Name: "alpha__read_secret"})
		assertHiddenRejected(t, res, err, c.called, 0)
		if out.Reason != runs.ReasonUnknownTool {
			t.Fatalf("named hidden tool was not unknown: %+v", out)
		}
		res, _, err = g.CallCanonicalTool(ctx, ToolCallParams{Name: "alpha__list_paths"})
		assertVisibleReached(t, res, err, c.called, 0)
	})

	t.Run("group tools", func(t *testing.T) {
		g, c := newWhitelistGateway(t)
		g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
			"grp": {Tools: []string{"alpha__read_secret", "alpha__list_paths"}},
		}))
		ctx := WithGroup(context.Background(), "grp")
		res, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "alpha__read_secret"})
		assertHiddenRejected(t, res, err, c.called, 0)
		res, err = g.HandleToolsCall(ctx, ToolCallParams{Name: "alpha__list_paths"})
		assertVisibleReached(t, res, err, c.called, 0)
	})
}

func TestDispatch_WhitelistHiddenToolDoesNotConsumeGate(t *testing.T) {
	g, c := newWhitelistGateway(t)
	gate := &stubGate{name: "rate-limit", decision: GateAllow()}
	g.SetCallGates([]CallGate{gate})
	res, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "alpha__read_secret"})
	assertHiddenRejected(t, res, err, c.called, 0)
	if len(gate.calls) != 0 {
		t.Fatalf("hidden tool consumed a gate token: %+v", gate.calls)
	}
	res, err = g.HandleToolsCall(context.Background(), ToolCallParams{Name: "alpha__list_paths"})
	assertVisibleReached(t, res, err, c.called, 0)
	if len(gate.calls) != 1 {
		t.Fatalf("visible tool gate calls = %d, want 1", len(gate.calls))
	}
}

func TestDispatch_HiddenAndUnknownToolShareHandshakeText(t *testing.T) {
	g, c := newWhitelistGateway(t)
	cases := []string{"alpha__read_secret", "missing__tool", "notprefixed", "alpha__"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			before := len(c.called)
			res, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: name})
			assertHiddenRejected(t, res, err, c.called, before)
		})
	}
}

func TestDispatch_WhitelistHiddenToolDoesNotColdStart(t *testing.T) {
	t.Run("configured whitelist before first spawn", func(t *testing.T) {
		gw := NewGateway()
		t.Cleanup(gw.Close)
		sp := newGatewayFakeSpawner(t)
		if err := gw.RegisterAutoscaler(context.Background(),
			MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}, Tools: []string{"echo"}},
			ReplicaPolicyRoundRobin, sp,
			AutoscalePolicy{Min: 0, Max: 2, TargetInFlight: 1, IdleToZero: true},
		); err != nil {
			t.Fatal(err)
		}
		res, err := gw.HandleToolsCall(context.Background(), ToolCallParams{Name: "svc__add"})
		if err != nil || res == nil || !res.IsError || res.Content[0].Text != "Error: unknown tool" {
			t.Fatalf("hidden cold call = err=%v res=%+v", err, res)
		}
		if got := sp.spawns.Load(); got != 0 {
			t.Fatalf("hidden tool spawned %d replicas, want 0", got)
		}
		res, err = gw.HandleToolsCall(context.Background(), ToolCallParams{Name: "svc__echo"})
		if err != nil || res == nil || res.IsError {
			t.Fatalf("visible cold start failed: err=%v res=%+v", err, res)
		}
		if got := sp.spawns.Load(); got != 1 {
			t.Fatalf("visible cold start spawns = %d, want 1", got)
		}
	})

	t.Run("scaled to zero with filtered cache", func(t *testing.T) {
		gw := NewGateway()
		t.Cleanup(gw.Close)
		sp := newGatewayFakeSpawner(t)
		if err := gw.RegisterAutoscaler(context.Background(),
			MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}},
			ReplicaPolicyRoundRobin, sp,
			AutoscalePolicy{Min: 0, Max: 2, TargetInFlight: 1, IdleToZero: true},
		); err != nil {
			t.Fatal(err)
		}
		set := gw.Router().GetReplicaSet("svc")
		set.SetToolCache([]Tool{{Name: "echo"}})
		gw.Router().RefreshTools()
		res, err := gw.HandleToolsCall(context.Background(), ToolCallParams{Name: "svc__add"})
		if err != nil || res == nil || !res.IsError || res.Content[0].Text != "Error: unknown tool" {
			t.Fatalf("cached hidden call = err=%v res=%+v", err, res)
		}
		if got := sp.spawns.Load(); got != 0 {
			t.Fatalf("cached hidden tool spawned %d replicas, want 0", got)
		}
		res, err = gw.HandleToolsCall(context.Background(), ToolCallParams{Name: "svc__echo"})
		if err != nil || res == nil || res.IsError {
			t.Fatalf("cached visible cold start failed: err=%v res=%+v", err, res)
		}
		if got := sp.spawns.Load(); got != 1 {
			t.Fatalf("cached visible spawns = %d, want 1", got)
		}
	})
}

func TestDispatch_VisibleToolColdStartsThenRepeats(t *testing.T) {
	gw := NewGateway()
	t.Cleanup(gw.Close)
	sp := newGatewayFakeSpawner(t)
	if err := gw.RegisterAutoscaler(context.Background(),
		MCPServerConfig{Name: "svc", LocalProcess: true, Command: []string{"x"}},
		ReplicaPolicyRoundRobin, sp,
		AutoscalePolicy{Min: 0, Max: 2, TargetInFlight: 1, IdleToZero: true},
	); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, err := gw.HandleToolsCall(context.Background(), ToolCallParams{Name: "svc__echo"})
		if err != nil || res == nil || res.IsError {
			t.Fatalf("call %d failed: err=%v res=%+v", i, err, res)
		}
	}
	if got := sp.spawns.Load(); got != 1 {
		t.Fatalf("spawns = %d, want 1", got)
	}
}
