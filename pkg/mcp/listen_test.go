package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func listenNotifications(tools, prompts, resources bool, uris []string) map[string]any {
	notes := map[string]any{}
	if tools {
		notes["toolsListChanged"] = true
	}
	if prompts {
		notes["promptsListChanged"] = true
	}
	if resources {
		notes["resourcesListChanged"] = true
	}
	if uris != nil {
		notes["resourceSubscriptions"] = uris
	}
	return notes
}

func openListenServer(t *testing.T, g *Gateway) *httptest.Server {
	t.Helper()
	mcpSrv := NewStreamableHTTPServer(g, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if group := r.Header.Get("X-Test-Group"); group != "" {
			r = r.WithContext(WithGroup(r.Context(), group))
		}
		mcpSrv.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func postListen(t *testing.T, url string, id any, notes map[string]any, clientID, group string) *http.Response {
	t.Helper()
	body := statelessBody(t, id, MethodSubscriptionsListen, map[string]any{"notifications": notes})
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", StatelessProtocolVersion)
	req.Header.Set(headerMcpMethod, MethodSubscriptionsListen)
	if clientID != "" {
		req.Header.Set(ClientAccessIDHeader, clientID)
	}
	if group != "" {
		req.Header.Set("X-Test-Group", group)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readSSEData(t *testing.T, r io.Reader, n int, timeout time.Duration) [][]byte {
	t.Helper()
	type result struct {
		data [][]byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(r)
		var data [][]byte
		var lines []string
		for len(data) < n {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				trimmed := strings.TrimRight(line, "\r\n")
				if trimmed == "" {
					if len(lines) > 0 {
						data = append(data, []byte(strings.Join(lines, "\n")))
						lines = nil
					}
				} else if value, ok := sseDataField([]byte(trimmed)); ok {
					lines = append(lines, value)
				}
			}
			if err != nil {
				ch <- result{data: data, err: err}
				return
			}
		}
		ch <- result{data: data}
	}()
	select {
	case got := <-ch:
		if len(got.data) < n {
			t.Fatalf("got %d SSE events, want %d: %v", len(got.data), n, got.err)
		}
		return got.data
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %d SSE events", n)
		return nil
	}
}

func TestListen_AckPreservesRequestID(t *testing.T) {
	if sdk.MetaKeySubscriptionID != metaKeySubscriptionID {
		t.Fatalf("subscription id key = %q, sdk %q", metaKeySubscriptionID, sdk.MetaKeySubscriptionID)
	}
	g := NewGateway()
	t.Cleanup(g.Close)
	g.SetListChangeTiming(time.Hour, time.Hour)
	ts := openListenServer(t, g)
	for _, id := range []any{1, "sub-7"} {
		resp := postListen(t, ts.URL, id, listenNotifications(true, false, false, nil), "", "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("id %v: status %d", id, resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("id %v: content-type %q", id, resp.Header.Get("Content-Type"))
		}
		if resp.Header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("id %v: cache-control %q", id, resp.Header.Get("Cache-Control"))
		}
		events := readSSEData(t, resp.Body, 1, 2*time.Second)
		var note struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(events[0], &note); err != nil {
			t.Fatal(err)
		}
		if note.Method != NotificationSubscriptionsAcknowledged {
			t.Fatalf("method = %q", note.Method)
		}
		var raw struct {
			Meta          map[string]json.RawMessage `json:"_meta"`
			Notifications map[string]bool            `json:"notifications"`
		}
		if err := json.Unmarshal(note.Params, &raw); err != nil {
			t.Fatal(err)
		}
		wantID, _ := json.Marshal(id)
		if string(raw.Meta[metaKeySubscriptionID]) != string(wantID) {
			t.Fatalf("subscription id = %s, want %s", raw.Meta[metaKeySubscriptionID], wantID)
		}
		if !raw.Notifications["toolsListChanged"] || raw.Notifications["promptsListChanged"] || raw.Notifications["resourcesListChanged"] {
			t.Fatalf("honored = %#v", raw.Notifications)
		}
		var ack sdk.SubscriptionsAcknowledgedParams
		if err := json.Unmarshal(note.Params, &ack); err != nil {
			t.Fatal(err)
		}
		if !ack.Notifications.ToolsListChanged || ack.Notifications.PromptsListChanged || ack.Notifications.ResourcesListChanged || len(ack.Notifications.ResourceSubscriptions) != 0 {
			t.Fatalf("sdk notifications = %+v", ack.Notifications)
		}
		gotID, _ := json.Marshal(ack.Meta[sdk.MetaKeySubscriptionID])
		if string(gotID) != string(wantID) {
			t.Fatalf("sdk subscription id = %s, want %s", gotID, wantID)
		}
	}
}

func TestListen_InvalidAndEmptyAndLimits(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	srv := NewStreamableHTTPServer(g, nil)

	t.Run("missing notifications", func(t *testing.T) {
		w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, MethodSubscriptionsListen, map[string]any{})))
		if w.Code != http.StatusOK || resp.Error == nil || resp.Error.Code != jsonrpc.InvalidParams {
			t.Fatalf("expected 200 -32602, got %d %+v", w.Code, resp.Error)
		}
	})

	t.Run("nothing honored", func(t *testing.T) {
		body := statelessBody(t, 9, MethodSubscriptionsListen, map[string]any{
			"notifications": map[string]any{"resourceSubscriptions": []string{"file:///x"}},
		})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, statelessRequest(t, body))
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
			t.Fatalf("status %d type %q body %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
		}
		raw := w.Body.String()
		if !strings.Contains(raw, "event: message") {
			t.Fatalf("events were not SSE message events: %s", raw)
		}
		events := readSSEData(t, strings.NewReader(raw), 2, time.Second)
		if !strings.Contains(string(events[0]), NotificationSubscriptionsAcknowledged) {
			t.Fatalf("first event = %s", events[0])
		}
		if !strings.Contains(string(events[0]), `"notifications":{}`) {
			t.Fatalf("ack should honor nothing: %s", events[0])
		}
		var final jsonrpc.Response
		if err := json.Unmarshal(events[1], &final); err != nil {
			t.Fatal(err)
		}
		if string(*final.ID) != "9" || !strings.Contains(string(final.Result), `"resultType":"complete"`) {
			t.Fatalf("final = %s", events[1])
		}
	})

	t.Run("limits", func(t *testing.T) {
		ts := openListenServer(t, g)
		var open []*http.Response
		defer func() {
			for _, resp := range open {
				resp.Body.Close()
			}
		}()
		for i := 0; i < maxListenStreamsPerClient; i++ {
			resp := postListen(t, ts.URL, i+1, listenNotifications(true, false, false, nil), "alice", "")
			if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
				t.Fatalf("stream %d: %d %s", i, resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			open = append(open, resp)
		}
		over := postListen(t, ts.URL, 99, listenNotifications(true, false, false, nil), "alice", "")
		defer over.Body.Close()
		assertListenLimit(t, over, maxListenStreamsPerClient)
		if !strings.Contains(logs.String(), "listen stream rejected") || !strings.Contains(logs.String(), "limit=16") {
			t.Fatalf("limit log = %s", logs.String())
		}
		open[0].Body.Close()
		open = open[1:]
		time.Sleep(20 * time.Millisecond)
		again := postListen(t, ts.URL, 100, listenNotifications(true, false, false, nil), "alice", "")
		if again.StatusCode != http.StatusOK || !strings.HasPrefix(again.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("slot not freed: %d %s", again.StatusCode, again.Header.Get("Content-Type"))
		}
		open = append(open, again)

		// Fill the overall cap from the 16 already open for alice.
		need := maxListenStreams - len(open)
		for i := 0; i < need; i++ {
			resp := postListen(t, ts.URL, i+1, listenNotifications(true, false, false, nil), "c"+strconv.Itoa(i/maxListenStreamsPerClient), "")
			if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("overall stream %d: %d %s", i, resp.StatusCode, body)
			}
			open = append(open, resp)
		}
		overall := postListen(t, ts.URL, 257, listenNotifications(true, false, false, nil), "overflow", "")
		defer overall.Body.Close()
		assertListenLimit(t, overall, maxListenStreams)
	})
}

func assertListenLimit(t *testing.T, resp *http.Response, limit int) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var decoded jsonrpc.Response
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Error == nil || decoded.Error.Code != jsonrpc.InternalError || !strings.Contains(decoded.Error.Message, strconv.Itoa(limit)) {
		t.Fatalf("limit error = %+v", decoded.Error)
	}
}

type declaringStub struct {
	listChangedStub
	caps Capabilities
}

func (s *declaringStub) DownstreamCapabilities() Capabilities { return s.caps }

func TestListen_BurstOneEvent(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	g.SetListChangeTiming(50*time.Millisecond, time.Second)
	ts := openListenServer(t, g)
	resp := postListen(t, ts.URL, 1, listenNotifications(true, false, false, nil), "alice", "")
	defer resp.Body.Close()
	readSSEData(t, resp.Body, 1, 2*time.Second)

	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	for i := 0; i < 5; i++ {
		g.Router().RefreshTools()
	}
	events := readSSEData(t, resp.Body, 1, 2*time.Second)
	if !strings.Contains(string(events[0]), MethodToolsListChanged) {
		t.Fatalf("event = %s", events[0])
	}
	assertNoListChanged(t, resp.Body, 180*time.Millisecond)
}

func TestListen_ScopeGroupRegistryAndClose(t *testing.T) {
	g := NewGateway()
	t.Cleanup(g.Close)
	g.SetListChangeTiming(30*time.Millisecond, time.Second)
	g.Router().AddClient(&declaringStub{
		listChangedStub: listChangedStub{name: "meta", tools: []Tool{testTool("meta", "m")}},
		caps: Capabilities{
			Prompts:   &PromptsCapability{ListChanged: true},
			Resources: &ResourcesCapability{ListChanged: true},
		},
	})
	g.Router().RefreshTools()
	time.Sleep(80 * time.Millisecond)

	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"alice": {Servers: []string{"alpha"}},
		},
	}))
	g.SetGroupPolicy(NewGroupPolicy(GroupsSpec{
		"release": {Servers: []string{"alpha"}},
		"other":   {Servers: []string{"beta"}},
	}))
	ts := openListenServer(t, g)

	visible := postListen(t, ts.URL, 1, listenNotifications(true, true, true, nil), "alice", "")
	defer visible.Body.Close()
	hidden := postListen(t, ts.URL, 2, listenNotifications(true, true, true, nil), "bob", "")
	defer hidden.Body.Close()
	grouped := postListen(t, ts.URL, 3, listenNotifications(true, true, true, nil), "alice", "release")
	defer grouped.Body.Close()
	other := postListen(t, ts.URL, 4, listenNotifications(true, true, true, nil), "alice", "other")
	defer other.Body.Close()
	toolsOnly := postListen(t, ts.URL, "tools", listenNotifications(true, false, false, nil), "alice", "")
	defer toolsOnly.Body.Close()
	for _, body := range []io.Reader{visible.Body, hidden.Body, grouped.Body, other.Body, toolsOnly.Body} {
		readSSEData(t, body, 1, 2*time.Second)
	}

	g.Router().AddClient(&listChangedStub{name: "alpha", tools: []Tool{testTool("echo", "hi")}})
	g.Router().RefreshTools()

	got := readSSEData(t, visible.Body, 1, 2*time.Second)
	if !strings.Contains(string(got[0]), MethodToolsListChanged) {
		t.Fatalf("visible event = %s", got[0])
	}
	toolsGot := readSSEData(t, toolsOnly.Body, 1, 2*time.Second)
	if !strings.Contains(string(toolsGot[0]), MethodToolsListChanged) {
		t.Fatalf("tools-only event = %s", toolsGot[0])
	}
	groupedGot := readSSEData(t, grouped.Body, 1, 2*time.Second)
	if !strings.Contains(string(groupedGot[0]), MethodToolsListChanged) {
		t.Fatalf("group event = %s", groupedGot[0])
	}
	assertNoListChanged(t, hidden.Body, 150*time.Millisecond)
	assertNoListChanged(t, other.Body, 150*time.Millisecond)

	g.NotifyRegistryChanged()
	reg := readSSEData(t, visible.Body, 2, 2*time.Second)
	joined := string(reg[0]) + string(reg[1])
	if strings.Count(joined, MethodPromptsListChanged) != 1 || strings.Count(joined, MethodResourcesListChanged) != 1 {
		t.Fatalf("registry events = %s", joined)
	}
	assertNoListChanged(t, toolsOnly.Body, 150*time.Millisecond)

	g.Close()
	final := readSSEData(t, visible.Body, 1, 2*time.Second)
	if !strings.Contains(string(final[0]), `"resultType":"complete"`) {
		t.Fatalf("close result = %s", final[0])
	}
}

func assertNoListChanged(t *testing.T, body io.Reader, quiet time.Duration) {
	t.Helper()
	got := make(chan []byte, 1)
	go func() {
		reader := bufio.NewReader(body)
		var lines []string
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				trimmed := strings.TrimRight(line, "\r\n")
				if trimmed == "" && len(lines) > 0 {
					got <- []byte(strings.Join(lines, "\n"))
					return
				}
				if value, ok := sseDataField([]byte(trimmed)); ok {
					lines = append(lines, value)
				}
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case event := <-got:
		t.Fatalf("unexpected event: %s", event)
	case <-time.After(quiet):
	}
}
