package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
)

func policyProbeGateway(t *testing.T) (*Gateway, *whitelistProbeClient) {
	t.Helper()
	c := &whitelistProbeClient{}
	c.SetTools([]Tool{
		{Name: "list_paths"},
		{Name: "read_secret"},
		{Name: "purge", Annotations: &ToolAnnotations{DestructiveHint: boolPtr(true)}},
	})
	c.SetInitialized(ServerInfo{Name: "alpha"})
	g := NewGateway()
	g.Router().AddClient(c)
	g.Router().RefreshTools()
	g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
		"grp": {Servers: []string{"alpha"}},
	}))
	return g, c
}

// scriptedPolicyGate is a first-party-shaped gate for dispatch tests.
// pkg/mcp tests cannot import pkg/toolpolicy (that package imports mcp).
// The compiled policy is covered in pkg/toolpolicy; this gate proves the
// dispatch boundary: inventory first, then the gate, then no downstream call.
type scriptedPolicyGate struct {
	block            map[string]string
	blockDestructive bool
}

func (g *scriptedPolicyGate) Name() string { return "policy" }

func (g *scriptedPolicyGate) CheckToolCall(_ context.Context, call GateCall) GateDecision {
	if msg, ok := g.block[call.PrefixedTool]; ok {
		return GateDenyWithReason(msg, runs.ReasonPolicyBlocked, "1: "+call.PrefixedTool)
	}
	if g.blockDestructive && call.Annotations != nil && call.Annotations.DestructiveHint != nil && *call.Annotations.DestructiveHint {
		return GateDenyWithReason("Error: tool blocked by policy (block_destructive: destructiveHint is true); do not retry", runs.ReasonPolicyBlocked, "block_destructive")
	}
	return GateAllow()
}

func installScripted(g *Gateway, block map[string]string) {
	g.SetCallGates([]CallGate{&scriptedPolicyGate{block: block}})
}

func TestDispatch_PolicyBlockedOnEveryPath(t *testing.T) {
	g, c := policyProbeGateway(t)
	installScripted(g, map[string]string{
		"alpha__read_secret": "Error: tool blocked by policy (rule 1: alpha__*); do not retry",
	})
	blocked := ToolCallParams{Name: "alpha__read_secret", Arguments: map[string]any{"path": "secret-value"}}
	allowed := ToolCallParams{Name: "alpha__list_paths"}
	want := "Error: tool blocked by policy (rule 1: alpha__*)"

	assertBlocked := func(t *testing.T, res *ToolCallResult, err error, before int) {
		t.Helper()
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("blocked call: err=%v res=%+v", err, res)
		}
		if !strings.HasPrefix(res.Content[0].Text, want) || !strings.HasSuffix(res.Content[0].Text, "do not retry") {
			t.Fatalf("text = %q", res.Content[0].Text)
		}
		if strings.Contains(res.Content[0].Text, "secret-value") {
			t.Fatal("denial included an argument")
		}
		if len(c.called) != before {
			t.Fatalf("blocked call reached downstream: %v", c.called)
		}
	}
	assertAllowed := func(t *testing.T, res *ToolCallResult, err error, before int) {
		t.Helper()
		if err != nil || res == nil || res.IsError {
			t.Fatalf("allowed call failed: err=%v res=%+v", err, res)
		}
		if len(c.called) != before+1 || c.called[len(c.called)-1] != "list_paths" {
			t.Fatalf("allowed call did not reach list_paths: %v", c.called)
		}
	}

	t.Run("direct", func(t *testing.T) {
		before := len(c.called)
		res, err := g.HandleToolsCall(context.Background(), blocked)
		assertBlocked(t, res, err, before)
		res, err = g.HandleToolsCall(context.Background(), allowed)
		assertAllowed(t, res, err, before)
	})

	t.Run("group alias sees canonical name", func(t *testing.T) {
		g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
			"grp": {
				Servers: []string{"alpha"},
				Overrides: map[string]GroupOverrideSpec{
					"alpha__read_secret": {Name: "steal"},
				},
			},
		}))
		before := len(c.called)
		ctx := WithGroup(context.Background(), "grp")
		res, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "steal", Arguments: map[string]any{"path": "secret-value"}})
		assertBlocked(t, res, err, before)
		g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
			"grp": {Servers: []string{"alpha"}},
		}))
	})

	t.Run("group session", func(t *testing.T) {
		before := len(c.called)
		ctx := WithGroup(context.Background(), "grp")
		res, err := g.HandleToolsCall(ctx, blocked)
		assertBlocked(t, res, err, before)
		res, err = g.HandleToolsCall(ctx, allowed)
		assertAllowed(t, res, err, before)
	})

	t.Run("code mode execute", func(t *testing.T) {
		g.SetCodeMode(5 * time.Second)
		before := len(c.called)
		denied, err := g.HandleToolsCall(context.Background(), ToolCallParams{
			Name: MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "read_secret", {path: "secret-value"}); })()`,
			},
		})
		if err != nil || denied == nil || !denied.IsError || !strings.Contains(denied.Content[0].Text, want) {
			t.Fatalf("execute blocked = err=%v res=%+v", err, denied)
		}
		if len(c.called) != before {
			t.Fatalf("execute blocked reached downstream: %v", c.called)
		}
		ok, err := g.HandleToolsCall(context.Background(), ToolCallParams{
			Name: MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "list_paths", {}); })()`,
			},
		})
		if err != nil || ok == nil || ok.IsError {
			t.Fatalf("execute allowed failed: err=%v res=%+v", err, ok)
		}
		if len(c.called) != before+1 || c.called[len(c.called)-1] != "list_paths" {
			t.Fatalf("execute allowed did not reach list_paths: %v", c.called)
		}
	})

	t.Run("code mode search lists blocked tool", func(t *testing.T) {
		g.SetCodeMode(5 * time.Second)
		before := len(c.called)
		res, err := g.HandleToolsCall(context.Background(), ToolCallParams{
			Name:      MetaToolSearch,
			Arguments: map[string]any{"query": "read_secret"},
		})
		if err != nil || res == nil || res.IsError {
			t.Fatalf("search failed: err=%v res=%+v", err, res)
		}
		if !strings.Contains(res.Content[0].Text, "alpha__read_secret") {
			t.Fatalf("search hid a blocked tool: %s", res.Content[0].Text)
		}
		if len(c.called) != before {
			t.Fatalf("search reached downstream: %v", c.called)
		}
	})

	t.Run("canonical", func(t *testing.T) {
		sink := &capturingSink{}
		g.SetRunSink(sink)
		before := len(c.called)
		res, out, err := g.CallCanonicalTool(context.Background(), blocked)
		assertBlocked(t, res, err, before)
		if out.Disposition != runs.DispositionDenied || out.Stage != runs.StageGate || out.Reason != runs.ReasonPolicyBlocked || out.Gate != "policy" {
			t.Fatalf("outcome = %+v", out)
		}
		if out.Completion != CompletionNotStarted {
			t.Fatalf("completion = %s", out.Completion)
		}
		res, out, err = g.CallCanonicalTool(context.Background(), allowed)
		assertAllowed(t, res, err, before)
		if out.Disposition != runs.DispositionCompleted {
			t.Fatalf("allowed outcome = %+v", out)
		}
	})
}

func TestDispatch_PolicyDoesNotConsumeRateToken(t *testing.T) {
	g, c := policyProbeGateway(t)
	// A one-token gate behind policy proves the denial does not reach it.
	// The production installer order is covered by controller.ApplyCallGates
	// and the integration suite; this slice is the hand-built unit stand-in.
	rate := &countingGate{name: "rate-limits", allow: 1}
	g.SetCallGates([]CallGate{
		&scriptedPolicyGate{block: map[string]string{
			"alpha__read_secret": "Error: tool blocked by policy (rule 1: alpha__read_secret); do not retry",
		}},
		rate,
	})

	res, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "alpha__read_secret"})
	if err != nil || res == nil || !res.IsError || out.Reason != runs.ReasonPolicyBlocked {
		t.Fatalf("policy denial = err=%v res=%+v out=%+v", err, res, out)
	}
	if len(c.called) != 0 {
		t.Fatal("policy denial reached downstream")
	}
	res, out, err = g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "alpha__list_paths"})
	if err != nil || res == nil || res.IsError || out.Reason != runs.ReasonOK {
		t.Fatalf("first allow = err=%v res=%+v out=%+v", err, res, out)
	}
	res, out, err = g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "alpha__list_paths"})
	if err != nil || res == nil || !res.IsError || out.Reason != runs.ReasonGateDenied || out.Gate != "rate-limits" {
		t.Fatalf("second allow = err=%v res=%+v out=%+v", err, res, out)
	}
}

func TestDispatch_PolicyHiddenToolRejectedBeforeGate(t *testing.T) {
	g, c := policyProbeGateway(t)
	c.SetToolWhitelist([]string{"list_paths"})
	g.Router().RefreshTools()
	installScripted(g, map[string]string{
		"alpha__read_secret": "Error: tool blocked by policy (rule 1: alpha__read_secret); do not retry",
	})
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	res, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "alpha__read_secret"})
	if err != nil || res == nil || !res.IsError || res.Content[0].Text != "Error: unknown tool" {
		t.Fatalf("hidden = err=%v res=%+v", err, res)
	}
	if out.Reason != runs.ReasonUnknownTool || out.Stage != runs.StageRouting {
		t.Fatalf("outcome = %+v", out)
	}
	if len(c.called) != 0 {
		t.Fatal("hidden call reached downstream")
	}
	if strings.Contains(logs.String(), "tool call blocked by policy") {
		t.Fatal("policy gate ran for a hidden tool")
	}
}

func TestDispatch_PolicyDenialLogOmitsArguments(t *testing.T) {
	g, _ := policyProbeGateway(t)
	installScripted(g, map[string]string{
		"alpha__read_secret": "Error: tool blocked by policy (rule 1: alpha__read_secret); do not retry",
	})
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	_, _, err := g.CallCanonicalTool(context.Background(), ToolCallParams{
		Name:      "alpha__read_secret",
		Arguments: map[string]any{"path": "secret-value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	line := logs.String()
	if !strings.Contains(line, "tool call blocked by policy") || !strings.Contains(line, "alpha__read_secret") {
		t.Fatalf("log = %s", line)
	}
	if strings.Contains(line, "secret-value") || strings.Contains(line, "path") {
		t.Fatalf("log included arguments: %s", line)
	}
}

func TestDispatch_PolicySwapAppliesOnNextCall(t *testing.T) {
	g, c := policyProbeGateway(t)
	installScripted(g, map[string]string{
		"alpha__read_secret": "Error: tool blocked by policy (rule 1: alpha__read_secret); do not retry",
	})
	res, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "alpha__read_secret"})
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("first = err=%v res=%+v", err, res)
	}
	g.SetCallGates(nil)
	res, err = g.HandleToolsCall(context.Background(), ToolCallParams{Name: "alpha__read_secret"})
	if err != nil || res == nil || res.IsError {
		t.Fatalf("swapped allow failed: err=%v res=%+v", err, res)
	}
	if len(c.called) != 1 || c.called[0] != "read_secret" {
		t.Fatalf("called = %v", c.called)
	}
}

func TestDispatch_PolicyDenialDoesNotColdStart(t *testing.T) {
	g := NewGateway()
	set := NewReplicaSet("alpha", ReplicaPolicyRoundRobin, nil)
	set.SetToolCache([]Tool{{Name: "read_secret"}})
	g.Router().AddReplicaSet(set)
	g.Router().RefreshTools()
	installScripted(g, map[string]string{
		"alpha__read_secret": "Error: tool blocked by policy (rule 1: alpha__read_secret); do not retry",
	})
	spawner := &policyColdStartSpawner{}
	g.autoscalers["alpha"] = NewAutoscaler("alpha", set, spawner, AutoscalePolicy{Max: 2, IdleToZero: true}, slog.New(slog.DiscardHandler))

	res, out, err := g.CallCanonicalTool(context.Background(), ToolCallParams{Name: "alpha__read_secret"})
	if err != nil || res == nil || !res.IsError || out.Reason != runs.ReasonPolicyBlocked {
		t.Fatalf("denial = err=%v res=%+v out=%+v", err, res, out)
	}
	if spawner.spawned != 0 {
		t.Fatalf("policy denial cold-started: spawned %d", spawner.spawned)
	}
}

type policyColdStartSpawner struct {
	spawned int
}

func (s *policyColdStartSpawner) Spawn(context.Context) (AgentClient, error) {
	s.spawned++
	return nil, context.Canceled
}

func (s *policyColdStartSpawner) Reap(context.Context, *Replica) error { return nil }

// countingGate allows n calls, then denies with the rate-limit reason shape.
type countingGate struct {
	name  string
	allow int
	seen  int
}

func (g *countingGate) Name() string { return g.name }

func (g *countingGate) CheckToolCall(context.Context, GateCall) GateDecision {
	g.seen++
	if g.seen <= g.allow {
		return GateAllow()
	}
	return GateDeny("Rate limit exceeded.")
}

func TestRouter_ToolAnnotationsIndexedAtRefresh(t *testing.T) {
	c := &whitelistProbeClient{}
	hint := boolPtr(true)
	c.SetTools([]Tool{
		{Name: "purge", Annotations: &ToolAnnotations{DestructiveHint: hint}},
		{Name: "list_paths"},
	})
	c.SetToolWhitelist([]string{"list_paths"})
	r := NewRouter()
	r.AddClient(c)
	r.RefreshTools()
	if r.ToolAnnotations("alpha__purge") != nil {
		t.Fatal("hidden tool must not be in the dispatch index")
	}
	if r.ToolAnnotations("alpha__list_paths") != nil {
		t.Fatal("tool without annotations must return nil")
	}
	c.SetToolWhitelist(nil)
	r.RefreshTools()
	got := r.ToolAnnotations("alpha__purge")
	if got == nil || got.DestructiveHint == nil || !*got.DestructiveHint {
		t.Fatalf("annotations = %+v", got)
	}
}
