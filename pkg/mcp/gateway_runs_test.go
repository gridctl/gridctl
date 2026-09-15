package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/gridctl/gridctl/pkg/runs"
)

type capturingSink struct {
	mu       sync.Mutex
	attempts []*capturingAttempt
}

func (s *capturingSink) Begin(ctx context.Context, requestedName string) RunAttempt {
	a := &capturingAttempt{ctx: ctx, requested: requestedName}
	s.mu.Lock()
	s.attempts = append(s.attempts, a)
	s.mu.Unlock()
	return a
}

func (s *capturingSink) last() *capturingAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.attempts) == 0 {
		return nil
	}
	return s.attempts[len(s.attempts)-1]
}

type capturingAttempt struct {
	ctx         context.Context
	requested   string
	disposition string
	stage       string
	reason      string
	server      string
	tool        string
	finished    bool
	downstream  time.Duration
}

func (a *capturingAttempt) Context() context.Context { return a.ctx }
func (a *capturingAttempt) SetOutcome(d, s, r string) {
	a.disposition, a.stage, a.reason = d, s, r
}
func (a *capturingAttempt) SetResolved(server, tool string, _ int) {
	a.server, a.tool = server, tool
}
func (a *capturingAttempt) SetDownstreamDuration(d time.Duration) { a.downstream = d }
func (a *capturingAttempt) SetTraceID(string)                     {}
func (a *capturingAttempt) SetLabels(string, string)              {}
func (a *capturingAttempt) SetPreviousAttemptID(string)           {}
func (a *capturingAttempt) Finish()                               { a.finished = true }

func TestHandleToolsCall_RecordsCompletion(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	sink := &capturingSink{}
	g.SetRunSink(sink)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{
		Content: []Content{NewTextContent("ok")},
	}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "agent1__echo"})
	if err != nil || result.IsError {
		t.Fatalf("call failed: %v %+v", err, result)
	}
	a := sink.last()
	if a == nil || !a.finished {
		t.Fatal("expected finished record")
	}
	if a.disposition != runs.DispositionCompleted || a.server != "agent1" || a.tool != "echo" {
		t.Fatalf("attempt = %+v", a)
	}
	if a.downstream <= 0 && a.downstream != 0 {
		t.Fatal("downstream duration should be set")
	}
}

func TestHandleToolsCall_RecordsRoutingFailure(t *testing.T) {
	g := NewGateway()
	sink := &capturingSink{}
	g.SetRunSink(sink)
	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "missing__tool"})
	if err != nil || !result.IsError {
		t.Fatalf("expected error result")
	}
	a := sink.last()
	if a.disposition != runs.DispositionRoutingFailed || a.reason != runs.ReasonUnknownTool || !a.finished {
		t.Fatalf("attempt = %+v", a)
	}
}

func TestHandleToolsCall_RecordsTransportError(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	sink := &capturingSink{}
	g.SetRunSink(sink)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(nil, context.Canceled)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "agent1__echo"})
	if err != nil || !result.IsError {
		t.Fatalf("expected in-band error, got %v %+v", err, result)
	}
	a := sink.last()
	if a.disposition != runs.DispositionCancelled {
		t.Fatalf("disposition = %s", a.disposition)
	}
}

func TestHandleToolsCall_DisabledSinkNoRecord(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{
		Content: []Content{NewTextContent("ok")},
	}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	if _, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "agent1__echo"}); err != nil {
		t.Fatal(err)
	}
}

func TestHandleToolsCall_DoesNotRecordPayloads(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	sink := &capturingSink{}
	g.SetRunSink(sink)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{
		Content: []Content{NewTextContent("secret-result")},
		IsError: true,
	}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	_, _ = g.HandleToolsCall(context.Background(), ToolCallParams{
		Name:      "agent1__echo",
		Arguments: map[string]any{"token": "secret-arg"},
	})
	a := sink.last()
	if a.disposition != runs.DispositionToolError {
		t.Fatalf("disposition = %s", a.disposition)
	}
}

func TestHandleToolsCall_InputRequired(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	sink := &capturingSink{}
	g.SetRunSink(sink)
	registerMRTRServer(t, g, ctrl, "srv", "blob")
	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "srv__ask"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultType != ResultTypeInputRequired {
		t.Fatalf("resultType = %s", result.ResultType)
	}
	a := sink.last()
	if a.disposition != runs.DispositionInputRequired {
		t.Fatalf("disposition = %s", a.disposition)
	}
}

func TestHandleToolsCall_RecordsCodeModeParentChild(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetCodeMode(30 * time.Second)
	sink := &capturingSink{}
	g.SetRunSink(sink)
	client := setupMockAgentClient(ctrl, "myserver", []Tool{{Name: "get_data"}})
	client.EXPECT().CallTool(gomock.Any(), "get_data", gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("ok")}, IsError: true}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": `try { mcp.callTool("myserver", "get_data", {}); } catch (e) {} "done";`},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = result
	sink.mu.Lock()
	n := len(sink.attempts)
	sink.mu.Unlock()
	if n < 2 {
		t.Fatalf("want parent+child records, got %d", n)
	}
	var outer, inner *capturingAttempt
	for _, a := range sink.attempts {
		if a.requested == MetaToolExecute {
			outer = a
		}
		if a.server == "myserver" {
			inner = a
		}
	}
	if outer == nil || inner == nil {
		t.Fatalf("missing outer/inner: %+v", sink.attempts)
	}
	if outer.disposition != runs.DispositionCompleted {
		t.Fatalf("outer disposition = %s", outer.disposition)
	}
	if inner.disposition != runs.DispositionToolError {
		t.Fatalf("inner disposition = %s", inner.disposition)
	}
}

func TestHandleToolsCall_RecordsGroupDenial(t *testing.T) {
	g, _ := newGroupGateway(t)
	sink := &capturingSink{}
	g.SetRunSink(sink)
	result, err := g.HandleToolsCall(groupCtx("release"), ToolCallParams{Name: "github__delete_repo"})
	if err != nil || !result.IsError {
		t.Fatalf("expected group denial")
	}
	a := sink.last()
	if a.disposition != runs.DispositionDenied || a.stage != runs.StageGroup {
		t.Fatalf("attempt = %+v", a)
	}
}

func TestHandleToolsCall_RecordsClientScopeDenial(t *testing.T) {
	g := newScopeTestGateway(t)
	sink := &capturingSink{}
	g.SetRunSink(sink)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{
			"cursor": {Servers: []string{"github"}},
		},
	}))
	result, err := g.HandleToolsCall(ctxWithAccess("cursor"), ToolCallParams{Name: "gitlab__list-issues"})
	if err != nil || !result.IsError {
		t.Fatalf("expected denial")
	}
	a := sink.last()
	if a.disposition != runs.DispositionDenied || a.stage != runs.StageScope {
		t.Fatalf("attempt = %+v", a)
	}
}

func TestHandleToolsCall_MRTRMismatchRecorded(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	sink := &capturingSink{}
	g.SetRunSink(sink)
	registerMRTRServer(t, g, ctrl, "srv", "state")
	ctx := withMRTRRelay(context.Background(), &mrtrRelay{ExpectedServer: "other", RequestState: "state"})
	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "srv__ask"})
	if err != nil || !result.IsError {
		t.Fatalf("expected mismatch error")
	}
	a := sink.last()
	if a.disposition != runs.DispositionRetryRejected {
		t.Fatalf("disposition = %s", a.disposition)
	}
}
