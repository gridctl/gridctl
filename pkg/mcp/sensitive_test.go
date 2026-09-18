package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

type sensitiveObserverSpy struct {
	legacy atomicCounter
	rich   atomicCounter
	safe   []SensitiveToolCallObservation
}

type atomicCounter struct {
	sync.Mutex
	n int
}

type sensitiveCounterSpy struct{ calls atomicCounter }

func (c *sensitiveCounterSpy) Count(string) int { c.calls.increment(); return 1 }

type legacySensitiveObserver struct{ called chan struct{} }

func (o legacySensitiveObserver) ObserveToolCall(string, int, map[string]any, *ToolCallResult) {
	o.called <- struct{}{}
}

func (c *atomicCounter) increment() { c.Lock(); c.n++; c.Unlock() }
func (c *atomicCounter) count() int { c.Lock(); defer c.Unlock(); return c.n }

func (o *sensitiveObserverSpy) ObserveToolCall(string, int, map[string]any, *ToolCallResult) {
	o.legacy.increment()
}

func (o *sensitiveObserverSpy) ObserveToolCallWithClient(context.Context, ToolCallObservation) ToolCallSummary {
	o.rich.increment()
	return ToolCallSummary{}
}

func (o *sensitiveObserverSpy) ObserveSensitiveToolCall(obs SensitiveToolCallObservation) {
	o.safe = append(o.safe, obs)
}

func sensitiveSentinel(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "gca2a_t1_" + base64.RawURLEncoding.EncodeToString(b)
}

func TestGateway_SensitiveObserverExcludesPayload(t *testing.T) {
	g := NewGateway()
	sentinel := sensitiveSentinel(t)
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "agent", []Tool{{Name: "send"}})
	client.EXPECT().CallTool(gomock.Any(), "send", gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent(sentinel)}}, nil,
	)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.serverMeta["agent"] = MCPServerConfig{sensitiveCalls: true}
	obs := &sensitiveObserverSpy{}
	g.SetToolCallObserver(obs)
	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{
		Name: "agent__send", Arguments: map[string]any{"data": map[string]any{"secret": sentinel}},
	})
	if err != nil || result.Content[0].Text != sentinel {
		t.Fatal("caller delivery changed", err)
	}
	if obs.legacy.count() != 0 || obs.rich.count() != 0 || len(obs.safe) != 1 {
		t.Fatal("sensitive call reached raw observer")
	}
	if strings.Contains(fmt.Sprint(obs.safe), sentinel) || obs.safe[0].Usage.InputTokens == 0 || obs.safe[0].Usage.OutputTokens == 0 {
		t.Fatal("observation must retain only numeric usage")
	}
}

func TestGateway_SensitiveLegacyOnlyObserverSkipped(t *testing.T) {
	g := NewGateway()
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "agent", []Tool{{Name: "send"}})
	client.EXPECT().CallTool(gomock.Any(), "send", gomock.Any()).Return(&ToolCallResult{}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.serverMeta["agent"] = MCPServerConfig{sensitiveCalls: true}
	observer := legacySensitiveObserver{called: make(chan struct{}, 1)}
	g.SetToolCallObserver(observer)
	if _, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "agent__send"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-observer.called:
		t.Fatal("legacy callback received sensitive call")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestGateway_OrdinaryErrorDeliveryPreserved(t *testing.T) {
	secret := sensitiveSentinel(t)
	var logs bytes.Buffer
	g := NewGateway()
	g.SetLogger(slog.New(slog.NewJSONHandler(&logs, nil)))
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "agent", []Tool{{Name: "send"}})
	client.EXPECT().CallTool(gomock.Any(), "send", gomock.Any()).Return(nil, errors.New(secret))
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "agent__send"})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, secret) {
		t.Fatal("ordinary caller error changed")
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatal("ordinary error leaked into diagnostics")
	}
}

func TestSensitiveExecution_Propagation(t *testing.T) {
	ctx := withSensitiveExecution(context.Background())
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { markSensitiveExecution(withSensitiveExecution(child)) })
		wg.Go(func() { _ = isSensitiveExecution(ctx) })
	}
	wg.Wait()
	if !isSensitiveExecution(ctx) || isSensitiveExecution(withSensitiveExecution(context.Background())) {
		t.Fatal("sensitivity must propagate within an execution only")
	}
}

func TestSafeCallError(t *testing.T) {
	for _, original := range []error{context.Canceled, context.DeadlineExceeded,
		errCapabilityUnavailable, errCapabilityCapacity, errCapabilityInProgress,
		errCapabilitySuperseded, errCapabilityUncertain, errCapabilityConflict, errCapabilityRandom} {
		err := safeCallError(fmt.Errorf("%s: %w", sensitiveSentinel(t), original))
		if !errors.Is(err, original) || err.Error() != original.Error() {
			t.Fatal("cancellation identity was not preserved safely")
		}
	}
	if err := safeCallError(errors.New(sensitiveSentinel(t))); err.Error() != "sensitive_call_failed" {
		t.Fatal("unsafe error projection")
	}
}

type markingCaller struct{ secret string }

func (c markingCaller) CallTool(ctx context.Context, _ string, _ map[string]any) (*ToolCallResult, error) {
	markSensitiveExecution(ctx)
	return &ToolCallResult{Content: []Content{NewTextContent(c.secret)}}, nil
}

func TestCodeMode_SensitiveExceptionsKeepCallerDelivery(t *testing.T) {
	secret := sensitiveSentinel(t)
	for _, code := range []string{
		fmt.Sprintf(`mcp.callTool("agent", "send", {}); throw %q;`, secret),
		fmt.Sprintf(`throw %q + ;`, secret),
	} {
		cm := NewCodeMode(time.Second)
		var logs bytes.Buffer
		cm.SetLogger(slog.New(slog.NewJSONHandler(&logs, nil)))
		result, err := cm.handleExecute(context.Background(), ToolCallParams{Arguments: map[string]any{"code": code}}, markingCaller{secret}, []Tool{{Name: "agent__send"}})
		if err != nil || !result.IsError {
			t.Fatal("expected script error", err)
		}
		if strings.Contains(logs.String(), secret) {
			t.Fatal("code exception leaked capability to diagnostics")
		}
	}
}
