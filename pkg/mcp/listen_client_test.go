package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadListenEvents_TaggedDispatchAndIgnore(t *testing.T) {
	var tools, prompts, resources atomic.Int32
	client := NewClient("down", "http://127.0.0.1")
	client.SetEra(EraStateless)
	client.setListChangedHandler(func() { tools.Add(1) })
	client.setPromptsChangedHandler(func() { prompts.Add(1) })
	client.setResourcesChangedHandler(func() { resources.Add(1) })

	pr, pw := io.Pipe()
	stream := &listenStream{body: pr, reqID: json.RawMessage(`7`)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.readListenEvents(ctx, stream, func() bool { return true })
	}()

	write := func(payload string) {
		t.Helper()
		if _, err := io.WriteString(pw, "event: message\ndata: "+payload+"\n\n"); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"jsonrpc":"2.0","method":"notifications/subscriptions/acknowledged","params":{"_meta":{"io.modelcontextprotocol/subscriptionId":7},"notifications":{"toolsListChanged":true}}}`)
	write(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed","params":{"_meta":{"io.modelcontextprotocol/subscriptionId":7}}}`)
	write(`{"jsonrpc":"2.0","method":"notifications/prompts/list_changed","params":{"_meta":{"io.modelcontextprotocol/subscriptionId":9}}}`)
	write(`{"jsonrpc":"2.0","method":"notifications/resources/list_changed","params":{}}`)
	write(`{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{}}`)
	write(`{"jsonrpc":"2.0","method":"notifications/prompts/list_changed","params":{"_meta":{"io.modelcontextprotocol/subscriptionId":7}}}`)
	write(`{"jsonrpc":"2.0","method":"notifications/resources/list_changed","params":{"_meta":{"io.modelcontextprotocol/subscriptionId":7}}}`)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (tools.Load() < 1 || prompts.Load() < 1 || resources.Load() < 1) {
		time.Sleep(5 * time.Millisecond)
	}
	if tools.Load() != 1 || prompts.Load() != 1 || resources.Load() != 1 {
		t.Fatalf("dispatch tools=%d prompts=%d resources=%d", tools.Load(), prompts.Load(), resources.Load())
	}
	cancel()
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not return on cancel")
	}
}

func TestListenLoop_PromptsOnlyReconnectAndStop(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	var methods []string
	var attempts atomic.Int32
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Method != MethodSubscriptionsListen {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
			return
		}
		n := attempts.Add(1)
		mu.Lock()
		bodies = append(bodies, string(body))
		methods = append(methods, r.Header.Get(headerMcpMethod))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		ack := listenAckPayload(req.ID, listenKinds{prompts: true})
		_, _ = w.Write([]byte("event: message\ndata: " + string(ack) + "\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		if n == 1 {
			return
		}
		<-r.Context().Done()
		close(hold)
	}))
	t.Cleanup(srv.Close)

	g := NewGateway()
	t.Cleanup(g.Close)
	g.setListenBackoff(20*time.Millisecond, 40*time.Millisecond)
	client := NewClient("prompts", srv.URL)
	client.SetEra(EraStateless)
	client.setProtocolVersion(StatelessProtocolVersion)
	client.SetDownstreamCapabilities(Capabilities{Prompts: &PromptsCapability{ListChanged: true}})
	g.Router().AddReplicaSet(NewReplicaSet("prompts", "", []AgentClient{client}))
	g.pendingMu.Lock()
	g.regGen["prompts"] = 1
	g.pendingMu.Unlock()
	g.startListenLoop("prompts", 1)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && attempts.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if attempts.Load() < 2 {
		t.Fatalf("reconnect attempts = %d", attempts.Load())
	}
	mu.Lock()
	got := bodies[0]
	method := methods[0]
	mu.Unlock()
	if !strings.Contains(got, `"promptsListChanged":true`) || strings.Contains(got, "toolsListChanged") || strings.Contains(got, "resourcesListChanged") {
		t.Fatalf("listen request = %s", got)
	}
	if method != MethodSubscriptionsListen {
		t.Fatalf("Mcp-Method = %q", method)
	}

	g.pendingMu.Lock()
	g.regGen["prompts"]++
	g.cancelListenLoopLocked("prompts")
	g.pendingMu.Unlock()
	select {
	case <-hold:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled loop did not drop the stream")
	}
	seen := attempts.Load()
	time.Sleep(80 * time.Millisecond)
	if attempts.Load() != seen {
		t.Fatalf("loop continued after cancel: %d -> %d", seen, attempts.Load())
	}
}
