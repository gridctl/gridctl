package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

type recordingSink struct {
	mu  sync.Mutex
	ids []string
}

func (s *recordingSink) NotifyToolsListChanged(id string) {
	s.mu.Lock()
	s.ids = append(s.ids, id)
	s.mu.Unlock()
}

func (s *recordingSink) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...)
}

func (s *recordingSink) waitFor(t *testing.T, n int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := s.snapshot()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d notifications, want at least %d: %v", len(got), n, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *recordingSink) waitQuiet(t *testing.T, want int, quiet time.Duration) []string {
	t.Helper()
	time.Sleep(quiet + 40*time.Millisecond)
	got := s.snapshot()
	if len(got) != want {
		t.Fatalf("got %d notifications, want %d: %v", len(got), want, got)
	}
	return got
}

type listChangedStub struct {
	name    string
	tools   []Tool
	refresh func(context.Context) error
	calls   atomic.Int32
}

func (s *listChangedStub) Name() string { return s.name }
func (s *listChangedStub) Initialize(context.Context) error { return nil }
func (s *listChangedStub) IsInitialized() bool { return true }
func (s *listChangedStub) ServerInfo() ServerInfo {
	return ServerInfo{Name: s.name, Version: "1"}
}
func (s *listChangedStub) Tools() []Tool { return append([]Tool(nil), s.tools...) }
func (s *listChangedStub) RefreshTools(ctx context.Context) error {
	s.calls.Add(1)
	if s.refresh != nil {
		return s.refresh(ctx)
	}
	return nil
}
func (s *listChangedStub) CallTool(context.Context, string, map[string]any) (*ToolCallResult, error) {
	return &ToolCallResult{}, nil
}

func testTool(name, desc string) Tool {
	return Tool{Name: name, Description: desc, InputSchema: []byte(`{"type":"object"}`)}
}

func newListChangedGateway(t *testing.T, quiet, maxLatency time.Duration) (*Gateway, *recordingSink) {
	t.Helper()
	g := NewGateway()
	t.Cleanup(g.Close)
	g.SetListChangeTiming(quiet, maxLatency)
	sink := &recordingSink{}
	g.SetListChangeSink(sink)
	return g, sink
}

func initSession(t *testing.T, g *Gateway, name, accessID, group string) *Session {
	t.Helper()
	_, session, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		ClientInfo:      ClientInfo{Name: name, Version: "1"},
	}, accessID, group)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestFingerprintTools(t *testing.T) {
	a := []Tool{testTool("b", "one"), testTool("a", "two")}
	b := []Tool{testTool("a", "two"), testTool("b", "one")}
	if fingerprintTools(a) != fingerprintTools(b) {
		t.Fatal("fingerprint changed with order")
	}
	if fingerprintTools(a) == fingerprintTools([]Tool{testTool("b", "changed"), testTool("a", "two")}) {
		t.Fatal("description change did not change fingerprint")
	}
	if fingerprintTools(nil) != fingerprintTools([]Tool{}) {
		t.Fatal("nil and empty lists must hash the same")
	}
	if fingerprintTools(a) == "" {
		t.Fatal("fingerprint empty")
	}
}

func TestSessionManager_ToolFingerprint(t *testing.T) {
	m := NewSessionManager()
	s := m.Create(ClientInfo{Name: "c"}, "", "", MCPProtocolVersion)
	m.SetToolFingerprint(s.ID, "one")
	prev, ok := m.SwapToolFingerprint(s.ID, "two")
	if !ok || prev != "one" {
		t.Fatalf("swap = %q %v", prev, ok)
	}
	prev, ok = m.SwapToolFingerprint(s.ID, "three")
	if !ok || prev != "two" {
		t.Fatalf("second swap = %q %v", prev, ok)
	}
	m.Delete(s.ID)
	if _, ok := m.SwapToolFingerprint(s.ID, "x"); ok {
		t.Fatal("swap on deleted session succeeded")
	}
	m.SetToolFingerprint("missing", "x")
}

func TestListChangeNotifier_BurstAndNoop(t *testing.T) {
	g, sink := newListChangedGateway(t, 30*time.Millisecond, time.Second)
	session := initSession(t, g, "client", "", "")
	stub := &listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}}
	g.Router().AddClient(stub)
	g.Router().RefreshTools()
	g.Router().RefreshTools()
	got := sink.waitFor(t, 1, time.Second)
	if len(got) != 1 || got[0] != session.ID {
		t.Fatalf("notifications = %v, want [%s]", got, session.ID)
	}
	before := len(sink.snapshot())
	g.Router().RefreshTools()
	sink.waitQuiet(t, before, 30*time.Millisecond)
}

func TestListChangeNotifier_ZeroDelayPublishesTimer(t *testing.T) {
	g, sink := newListChangedGateway(t, time.Hour, time.Hour)
	g.listChanges.mu.Lock()
	g.listChanges.quiet = 0
	g.listChanges.mu.Unlock()
	session := initSession(t, g, "client", "", "")
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	g.Router().RefreshTools()
	got := sink.waitFor(t, 1, time.Second)
	if len(got) != 1 || got[0] != session.ID {
		t.Fatalf("notifications = %v, want [%s]", got, session.ID)
	}
}

func TestListChangeNotifier_RearmKeepsReplacement(t *testing.T) {
	g, sink := newListChangedGateway(t, time.Hour, time.Hour)
	initSession(t, g, "client", "", "")
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	g.Router().RefreshTools()

	n := g.listChanges
	n.mu.Lock()
	first := n.timer
	n.mu.Unlock()
	if first == nil {
		t.Fatal("trigger did not arm a timer")
	}
	g.Router().RefreshTools()
	n.mu.Lock()
	second := n.timer
	armed := !n.firstTrigger.IsZero()
	n.mu.Unlock()
	if second == nil || second == first {
		t.Fatal("re-arm did not replace the timer")
	}
	if !armed {
		t.Fatal("firstTrigger cleared before flush")
	}

	n.flushFired(first)
	n.mu.Lock()
	kept := n.timer == second && !n.firstTrigger.IsZero()
	n.mu.Unlock()
	if !kept {
		t.Fatal("stale flush dropped the re-armed timer")
	}
	if got := sink.snapshot(); len(got) != 0 {
		t.Fatalf("stale flush notified: %v", got)
	}

	n.flushFired(second)
	second.Stop()
	got := sink.waitFor(t, 1, time.Second)
	if len(got) != 1 {
		t.Fatalf("matching flush notifications = %v", got)
	}
	n.mu.Lock()
	cleared := n.timer == nil && n.firstTrigger.IsZero()
	n.mu.Unlock()
	if !cleared {
		t.Fatal("matching flush did not clear timer state")
	}
}

func TestListChangeNotifier_MaxLatency(t *testing.T) {
	g, sink := newListChangedGateway(t, time.Second, 40*time.Millisecond)
	initSession(t, g, "client", "", "")
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	start := time.Now()
	g.Router().RefreshTools()
	time.Sleep(15 * time.Millisecond)
	g.Router().RefreshTools()
	sink.waitFor(t, 1, 200*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("notification after %s, want before max latency bound", elapsed)
	}
}

func TestListChangeNotifier_AccessAndGroupScope(t *testing.T) {
	g, sink := newListChangedGateway(t, 20*time.Millisecond, time.Second)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"alice": {Servers: []string{"alpha"}},
			"bob":   {Servers: []string{"beta"}},
		},
	}))
	alice := initSession(t, g, "alice", "alice", "")
	bob := initSession(t, g, "bob", "bob", "")
	g.Router().AddClient(&listChangedStub{name: "beta", tools: []Tool{testTool("ping", "p")}})
	g.Router().RefreshTools()
	got := sink.waitFor(t, 1, time.Second)
	if len(got) != 1 || got[0] != bob.ID {
		t.Fatalf("access notifications = %v, want [%s]", got, bob.ID)
	}
	if got[0] == alice.ID {
		t.Fatal("excluded session notified")
	}

	g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
		"ops":   {Servers: []string{"alpha"}},
		"other": {Servers: []string{"beta"}},
	}))
	ops := initSession(t, g, "ops-client", "bob", "ops")
	other := initSession(t, g, "other-client", "bob", "other")
	before := len(sink.snapshot())
	g.Router().RefreshTools()
	sink.waitQuiet(t, before, 20*time.Millisecond)
	g.Router().RemoveClient("beta")
	got = sink.waitFor(t, before+1, time.Second)
	seen := map[string]int{}
	for _, id := range got[before:] {
		seen[id]++
	}
	if seen[ops.ID] != 0 {
		t.Fatalf("unaffected group notified: %v", got[before:])
	}
	if seen[other.ID] != 1 {
		t.Fatalf("group notifications = %v", got[before:])
	}
}

func TestListChangeNotifier_PolicySwap(t *testing.T) {
	g, sink := newListChangedGateway(t, 20*time.Millisecond, time.Second)
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	g.Router().AddClient(&listChangedStub{name: "beta", tools: []Tool{testTool("ping", "p")}})
	g.Router().RefreshTools()
	alice := initSession(t, g, "alice", "alice", "")
	bob := initSession(t, g, "bob", "bob", "")
	sink.waitQuiet(t, 0, 20*time.Millisecond)

	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "allow",
		Profiles: map[string]ClientProfileSpec{
			"alice": {Servers: []string{"alpha"}},
		},
	}))
	got := sink.waitFor(t, 1, time.Second)
	if len(got) != 1 || got[0] != alice.ID {
		t.Fatalf("policy swap notifications = %v, want [%s]", got, alice.ID)
	}
	if got[0] == bob.ID {
		t.Fatal("unchanged session notified")
	}
}

func TestListChangeNotifier_CodeModeAndCloseAndColdStart(t *testing.T) {
	t.Run("code mode", func(t *testing.T) {
		g, sink := newListChangedGateway(t, 20*time.Millisecond, time.Second)
		g.SetCodeMode(time.Second)
		initSession(t, g, "client", "", "")
		g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
		g.Router().RefreshTools()
		sink.waitQuiet(t, 0, 20*time.Millisecond)
	})

	t.Run("close", func(t *testing.T) {
		g, sink := newListChangedGateway(t, time.Second, 2*time.Second)
		initSession(t, g, "client", "", "")
		g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
		g.Router().RefreshTools()
		g.Close()
		g.Router().RefreshTools()
		time.Sleep(40 * time.Millisecond)
		if got := sink.snapshot(); len(got) != 0 {
			t.Fatalf("notifications after close: %v", got)
		}
	})

	t.Run("cold start", func(t *testing.T) {
		g, sink := newListChangedGateway(t, 20*time.Millisecond, time.Second)
		initSession(t, g, "client", "", "")
		set := NewReplicaSet("alpha", ReplicaPolicyRoundRobin, nil)
		g.Router().AddReplicaSet(set)
		set.AddReplica(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "live")}})
		sink.waitQuiet(t, 0, 20*time.Millisecond)
		g.Router().RefreshTools()
		sink.waitFor(t, 1, time.Second)
	})
}

func TestListChangeTimingSetters(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	if g.listChanges.quiet != defaultListChangeQuiet || g.listChanges.maxLatency != defaultListChangeMaxLatency {
		t.Fatalf("defaults = %s %s", g.listChanges.quiet, g.listChanges.maxLatency)
	}
	g.SetListChangeTiming(15*time.Millisecond, 40*time.Millisecond)
	if g.listChanges.quiet != 15*time.Millisecond || g.listChanges.maxLatency != 40*time.Millisecond {
		t.Fatalf("timing = %s %s", g.listChanges.quiet, g.listChanges.maxLatency)
	}
	g.SetListChangeTiming(0, 0)
	if g.listChanges.quiet != 15*time.Millisecond {
		t.Fatal("non-positive timing overwrote the current setting")
	}
	g.SetDownstreamRefreshDebounce(25 * time.Millisecond)
	if g.downstreamDebounce != 25*time.Millisecond {
		t.Fatalf("debounce = %s", g.downstreamDebounce)
	}
	g.SetDownstreamRefreshDebounce(0)
	if g.downstreamDebounce != 25*time.Millisecond {
		t.Fatal("non-positive debounce overwrote the current setting")
	}
}

type orderVerifier struct {
	mu    *sync.Mutex
	order *[]string
	drift bool
}

func (v *orderVerifier) VerifyOrPin(string, []Tool) ([]SchemaDrift, error) {
	v.mu.Lock()
	*v.order = append(*v.order, "pins")
	v.mu.Unlock()
	if v.drift {
		return []SchemaDrift{{Name: "echo", ChangeKinds: []string{"description"}}}, nil
	}
	return nil, nil
}

func TestDownstreamRefresh_DebouncePinsAndClose(t *testing.T) {
	t.Run("debounce", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		g := NewGateway()
		t.Cleanup(g.Close)
		g.SetDownstreamRefreshDebounce(30 * time.Millisecond)
		mock := NewMockAgentClient(ctrl)
		mock.EXPECT().Name().Return("srv").AnyTimes()
		mock.EXPECT().Tools().Return([]Tool{testTool("echo", "hi")}).AnyTimes()
		var n atomic.Int32
		mock.EXPECT().RefreshTools(gomock.Any()).DoAndReturn(func(context.Context) error {
			n.Add(1)
			return nil
		}).AnyTimes()
		g.Router().AddClient(mock)
		g.scheduleDownstreamToolRefresh("srv")
		g.scheduleDownstreamToolRefresh("srv")
		g.scheduleDownstreamToolRefresh("srv")
		time.Sleep(90 * time.Millisecond)
		if got := n.Load(); got != 1 {
			t.Fatalf("refreshes = %d, want 1", got)
		}
		g.downstreamMu.Lock()
		_, left := g.downstreamTimers["srv"]
		g.downstreamMu.Unlock()
		if left {
			t.Fatal("fired downstream timer was not pruned")
		}
	})

	t.Run("stale callback keeps replacement", func(t *testing.T) {
		g := NewGateway()
		t.Cleanup(g.Close)
		g.SetDownstreamRefreshDebounce(time.Hour)
		g.scheduleDownstreamToolRefresh("srv")
		g.downstreamMu.Lock()
		first := g.downstreamTimers["srv"]
		g.downstreamMu.Unlock()
		if first == nil {
			t.Fatal("schedule did not arm a timer")
		}
		g.scheduleDownstreamToolRefresh("srv")
		if !g.dropDownstreamTimer("srv", first) {
			t.Fatal("stale callback was allowed to refresh")
		}
		g.downstreamMu.Lock()
		second, ok := g.downstreamTimers["srv"]
		g.downstreamMu.Unlock()
		if !ok || second == nil || second == first {
			t.Fatal("stale callback removed the re-armed timer")
		}
	})

	t.Run("pins before router refresh", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		g, sink := newListChangedGateway(t, 15*time.Millisecond, time.Second)
		g.SetDownstreamRefreshDebounce(15 * time.Millisecond)
		var order []string
		var mu sync.Mutex
		record := func(step string) {
			mu.Lock()
			order = append(order, step)
			mu.Unlock()
		}
		g.SetSchemaVerifier(&orderVerifier{order: &order, drift: true, mu: &mu}, "block")
		g.SetServerMeta(MCPServerConfig{Name: "srv"})
		g.router.SetOnChange(func() {
			record("router")
			g.listChanges.trigger()
		})
		tools := []Tool{testTool("echo", "old")}
		var toolsMu sync.Mutex
		mock := NewMockAgentClient(ctrl)
		mock.EXPECT().Name().Return("srv").AnyTimes()
		mock.EXPECT().Tools().DoAndReturn(func() []Tool {
			toolsMu.Lock()
			defer toolsMu.Unlock()
			return append([]Tool(nil), tools...)
		}).AnyTimes()
		mock.EXPECT().RefreshTools(gomock.Any()).DoAndReturn(func(context.Context) error {
			toolsMu.Lock()
			tools = []Tool{testTool("echo", "new")}
			toolsMu.Unlock()
			return nil
		}).AnyTimes()
		g.Router().AddClient(mock)
		g.Router().RefreshTools()
		session := initSession(t, g, "client", "", "")
		sink.waitQuiet(t, 0, 15*time.Millisecond)
		mu.Lock()
		order = nil
		mu.Unlock()
		g.scheduleDownstreamToolRefresh("srv")
		got := sink.waitFor(t, 1, time.Second)
		if got[0] != session.ID {
			t.Fatalf("notified %v", got)
		}
		g.blockedMu.RLock()
		blocked := g.blockedServers["srv"]
		g.blockedMu.RUnlock()
		if !blocked {
			t.Fatal("server was not blocked before the upstream notification")
		}
		mu.Lock()
		defer mu.Unlock()
		if len(order) < 2 || order[0] != "pins" || order[1] != "router" {
			t.Fatalf("order = %v, want pins then router", order)
		}
	})

	t.Run("skip missing and partial failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		g := NewGateway()
		t.Cleanup(g.Close)
		g.SetDownstreamRefreshDebounce(15 * time.Millisecond)
		g.scheduleDownstreamToolRefresh("missing")
		time.Sleep(40 * time.Millisecond)

		bad := NewMockAgentClient(ctrl)
		good := NewMockAgentClient(ctrl)
		bad.EXPECT().Name().Return("srv").AnyTimes()
		good.EXPECT().Name().Return("srv").AnyTimes()
		bad.EXPECT().Tools().Return([]Tool{testTool("echo", "hi")}).AnyTimes()
		good.EXPECT().Tools().Return([]Tool{testTool("echo", "hi")}).AnyTimes()
		bad.EXPECT().RefreshTools(gomock.Any()).Return(errors.New("boom")).Times(1)
		var goodCalls atomic.Int32
		good.EXPECT().RefreshTools(gomock.Any()).DoAndReturn(func(context.Context) error {
			goodCalls.Add(1)
			return nil
		}).Times(1)
		g.Router().AddReplicaSet(NewReplicaSet("srv", ReplicaPolicyRoundRobin, []AgentClient{bad, good}))
		g.scheduleDownstreamToolRefresh("srv")
		deadline := time.Now().Add(time.Second)
		for goodCalls.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if goodCalls.Load() != 1 {
			t.Fatal("healthy replica was not refreshed after a sibling failure")
		}
	})

	t.Run("close unblocks refresh", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		g := NewGateway()
		g.SetDownstreamRefreshDebounce(15 * time.Millisecond)
		mock := NewMockAgentClient(ctrl)
		mock.EXPECT().Name().Return("srv").AnyTimes()
		mock.EXPECT().Tools().Return([]Tool{testTool("echo", "hi")}).AnyTimes()
		started := make(chan struct{})
		done := make(chan struct{})
		mock.EXPECT().RefreshTools(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(done)
			return ctx.Err()
		}).Times(1)
		g.Router().AddClient(mock)
		g.scheduleDownstreamToolRefresh("srv")
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("refresh did not start")
		}
		start := time.Now()
		g.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("refresh did not return when Close was called")
		}
		if time.Since(start) > 500*time.Millisecond {
			t.Fatal("Close did not unblock the refresh promptly")
		}
	})
}

func TestHandleToolsList_UsesVisibleTools(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	g.Router().RefreshTools()
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default:  "deny",
		Profiles: map[string]ClientProfileSpec{"alice": {Servers: []string{"alpha"}}},
	}))
	ctx := WithClientAccessID(context.Background(), "alice")
	result, err := g.HandleToolsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "alpha__echo" {
		t.Fatalf("tools = %#v", result.Tools)
	}
	denied, err := g.HandleToolsList(WithClientAccessID(context.Background(), "bob"))
	if err != nil {
		t.Fatal(err)
	}
	if denied.Tools == nil || len(denied.Tools) != 0 {
		t.Fatalf("denied tools = %#v", denied.Tools)
	}
}
