//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

type a2aAdapterFixture struct {
	server                *httptest.Server
	version               string
	gets, calls, revision atomic.Int64
	contentBytes          atomic.Int64
	echoData              atomic.Bool
	mu                    sync.Mutex
	tasks                 map[string]map[string]any
	lastData              map[string]any
	remotePrefix          string
	sessions              []string
	onRPC                 func(http.ResponseWriter, *http.Request, string, string, map[string]any) bool
	onCard                func(http.ResponseWriter, *http.Request) bool
}

func newA2AAdapterFixture(t *testing.T, version string) *a2aAdapterFixture {
	t.Helper()
	f := &a2aAdapterFixture{version: version, tasks: make(map[string]map[string]any), remotePrefix: rand.Text()}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if session := r.Header.Get("X-Amzn-Bedrock-AgentCore-Runtime-Session-Id"); session != "" {
			f.mu.Lock()
			f.sessions = append(f.sessions, session)
			f.mu.Unlock()
		}
		if r.Method == http.MethodGet {
			f.gets.Add(1)
			f.mu.Lock()
			onCard := f.onCard
			f.mu.Unlock()
			if onCard != nil && onCard(w, r) {
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			skills := []any{map[string]any{"id": "chat", "description": "Conversational fixture"}}
			if f.revision.Load() != 0 {
				skills = append(skills, map[string]any{"id": "new", "description": "New card skill"})
			}
			card := map[string]any{"name": "fixture", "description": fmt.Sprintf("revision %d", f.revision.Load()), "skills": skills,
				"defaultInputModes": []string{"text/plain", "application/json"}, "defaultOutputModes": []string{"text/plain", "application/json"}}
			if version == "1.0" {
				card["supportedInterfaces"] = []any{map[string]any{"url": "http://" + r.Host + "/rpc", "protocolBinding": "JSONRPC", "protocolVersion": "1.0.0"}}
			} else {
				card["url"], card["protocolVersion"] = "http://"+r.Host+"/rpc", "0.3.0"
			}
			_ = json.NewEncoder(w).Encode(card)
			return
		}
		n := f.calls.Add(1)
		if r.Header.Get("A2A-Version") != version {
			t.Error("wrong A2A dialect header")
		}
		var req struct {
			ID     string         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if decoder.Decode(&req) != nil {
			t.Error("invalid fixture RPC")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		onRPC := f.onRPC
		f.mu.Unlock()
		if onRPC != nil && onRPC(w, r, req.ID, req.Method, req.Params) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var task map[string]any
		switch req.Method {
		case "SendMessage", "message/send":
			if (req.Method == "SendMessage") != (version == "1.0") {
				t.Error("dialect method mismatch")
			}
			message := req.Params["message"].(map[string]any)
			for _, part := range message["parts"].([]any) {
				if data, ok := part.(map[string]any)["data"].(map[string]any); ok {
					f.lastData = data
				}
			}
			taskID, _ := message["taskId"].(string)
			contextID, _ := message["contextId"].(string)
			if taskID != "" {
				task = f.tasks[taskID]
			} else {
				taskID = fmt.Sprintf("%s-task-%d", f.remotePrefix, n)
				if contextID == "" {
					contextID = fmt.Sprintf("%s-context-%d", f.remotePrefix, n)
				}
				task = map[string]any{"id": taskID, "contextId": contextID}
				f.tasks[taskID] = task
			}
			task["status"] = map[string]any{"state": "input-required"}
		case "GetTask", "tasks/get", "CancelTask", "tasks/cancel":
			taskID, _ := req.Params["id"].(string)
			task = f.tasks[taskID]
			if task != nil && (req.Method == "CancelTask" || req.Method == "tasks/cancel") {
				task["status"] = map[string]any{"state": "canceled"}
			}
		default:
			t.Error("unsupported fixture operation")
		}
		if task == nil {
			t.Error("adapter requested unknown remote work")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wireTask := make(map[string]any, len(task))
		for key, value := range task {
			wireTask[key] = value
		}
		state := task["status"].(map[string]any)["state"].(string)
		if version == "1.0" {
			wireTask["status"] = map[string]any{"state": "TASK_STATE_" + strings.ToUpper(strings.ReplaceAll(state, "-", "_"))}
		} else {
			wireTask["kind"] = "task"
		}
		if size := f.contentBytes.Load(); size > 0 {
			part := map[string]any{"text": strings.Repeat("x", int(size))}
			if version == "0.3" {
				part["kind"] = "text"
			}
			wireTask["artifacts"] = []any{map[string]any{"artifactId": "artifact", "parts": []any{part}}}
		}
		if f.echoData.Load() && f.lastData != nil {
			part := map[string]any{"data": f.lastData}
			if version == "0.3" {
				part["kind"] = "data"
			}
			wireTask["artifacts"] = []any{map[string]any{"artifactId": "artifact", "parts": []any{part}}}
		}
		var result any = wireTask
		if req.Method == "SendMessage" {
			result = map[string]any{"task": wireTask}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(f.server.Close)
	return f
}

func a2aAdapterEnvelope(t *testing.T, result *mcp.ToolCallResult) map[string]any {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatal("missing A2A envelope")
	}
	var envelope map[string]any
	if json.Unmarshal([]byte(result.Content[0].Text), &envelope) != nil {
		t.Fatal("A2A envelope was not atomic JSON")
	}
	if result.RequestState != "" || result.ResultType != "" && result.ResultType != mcp.ResultTypeComplete {
		t.Fatal("remote task became MCP continuation")
	}
	return envelope
}

func TestA2AAdapter_RealStorageRESTAndApproval(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			fixture := newA2AAdapterFixture(t, version)
			gateway := mcp.NewGateway()
			defer gateway.Close()
			store := pins.NewWithPath(t.TempDir(), "adapter")
			if err := gateway.SetCardPinStorage(t.Context(), store); err != nil {
				t.Fatal(err)
			}
			cfg := mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: fixture.server.URL + "/card", Dialect: version}}
			if err := gateway.RegisterMCPServer(t.Context(), cfg); err != nil {
				t.Fatal(err)
			}
			if len(gateway.Router().AggregatedTools()) != 4 {
				t.Fatal("registration did not generate the expected callable tools")
			}
			persisted, ok := store.GetServer("agent")
			if !ok || len(persisted.Tools) != 6 || persisted.Tools["_agent_card"] == nil {
				t.Fatal("first-use card trust did not persist before registration")
			}
			apiServer := api.NewServer(gateway, nil)
			auth := rand.Text()
			apiServer.SetAuth("bearer", auth, "")
			apiServer.SetCardPinStore(store)
			listener := httptest.NewServer(apiServer.Handler())
			defer listener.Close()
			request := func(path string, input any) (int, []byte) {
				t.Helper()
				body, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, listener.URL+path, bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+auth)
				req.Header.Set("Content-Type", "application/json")
				resp, err := listener.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if path == "/api/tools/call" && resp.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("credential-bearing REST response was cacheable")
				}
				data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				if err != nil {
					t.Fatal(err)
				}
				return resp.StatusCode, data
			}
			call := func(tool, label string, args map[string]any) map[string]any {
				t.Helper()
				input := map[string]any{"name": "agent__" + tool, "arguments": args}
				if label != "" {
					input["client"] = label
				}
				status, body := request("/api/tools/call", input)
				if status != http.StatusOK {
					t.Fatalf("REST call status %d", status)
				}
				var response struct {
					Result *mcp.ToolCallResult `json:"result"`
				}
				if json.Unmarshal(body, &response) != nil {
					t.Fatal("invalid REST envelope")
				}
				return a2aAdapterEnvelope(t, response.Result)
			}
			first := call("send", "same-label", map[string]any{"message": "hello", "return_immediately": true})
			if first["task_handle"] == nil || first["context_handle"] == nil || first["state"] != "input-required" {
				t.Fatal("send did not deliver usable capabilities")
			}
			beforeRPC, beforeGET := fixture.calls.Load(), fixture.gets.Load()
			for _, tool := range []string{"task_get", "task_cancel"} {
				got := call(tool, "same-label", map[string]any{"task_handle": "task-1"})
				if got["error"] != "capability_unavailable" {
					t.Fatal("same-label raw ID was authorized")
				}
			}
			if fixture.calls.Load() != beforeRPC || fixture.gets.Load() != beforeGET {
				t.Fatal("invalid authority triggered discovery or RPC")
			}
			got := call("task_get", "transferred-label", map[string]any{"task_handle": first["task_handle"]})
			if got["task_handle"] != first["task_handle"] || got["context_handle"] != nil {
				t.Fatal("bearer transfer failed or disclosed parent authority")
			}
			got = call("skill-chat", "", map[string]any{"message": "continue", "task_handle": first["task_handle"], "context_handle": first["context_handle"]})
			if got["task_handle"] != first["task_handle"] {
				t.Fatal("empty-label authorized continuation failed")
			}
			fixture.revision.Add(1)
			beforeRPC = fixture.calls.Load()
			got = call("task_get", "same-label", map[string]any{"task_handle": first["task_handle"]})
			if got["error"] == nil || fixture.calls.Load() != beforeRPC || len(gateway.Router().AggregatedTools()) != 4 {
				t.Fatal("card drift admitted a call or candidate tools")
			}
			snapshot, err := gateway.CardTrust().Snapshot(t.Context(), "agent")
			if err != nil {
				t.Fatal(err)
			}
			if status, _ := request("/api/pins/agent/approve", map[string]any{}); status != http.StatusBadRequest {
				t.Fatal("unbound A2A approval succeeded")
			}
			if status, _ := request("/api/pins/agent/approve", map[string]any{"expected_server_hash": snapshot.Hash()}); status != http.StatusOK {
				t.Fatalf("approval status %d", status)
			}
			if len(gateway.Router().AggregatedTools()) != 5 {
				t.Fatal("approved tool catalog was not published")
			}
			got = call("task_get", "same-label", map[string]any{"task_handle": first["task_handle"]})
			if got["error"] != "capability_unavailable" {
				t.Fatal("approval rebound old capability")
			}
			fresh := call("skill-new", "", map[string]any{"message": "new work"})
			got = call("task_cancel", "", map[string]any{"task_handle": fresh["task_handle"]})
			if got["state"] != "canceled" || got["context_handle"] != nil {
				t.Fatal("remote cancel observation was not delivered")
			}
		})
	}
}

func TestA2AAdapter_GatewayBudgetIsAtomicAndLive(t *testing.T) {
	fixture := newA2AAdapterFixture(t, "1.0")
	gateway := mcp.NewGateway()
	defer gateway.Close()
	store := pins.NewWithPath(t.TempDir(), "budget")
	if err := gateway.SetCardPinStorage(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterMCPServer(t.Context(), mcp.MCPServerConfig{Name: "agent", A2A: true, OutputFormat: "yaml", A2AConfig: &mcp.A2AClientConfig{Card: fixture.server.URL}}); err != nil {
		t.Fatal(err)
	}
	fixture.contentBytes.Store(1 << 20)
	gateway.SetMaxToolResultBytes(512)
	result, err := gateway.CallTool(context.Background(), "agent__send", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	envelope := a2aAdapterEnvelope(t, result)
	if result.IsError || len(result.Content[0].Text) > 512 || envelope["content_truncated"] != true || envelope["task_handle"] == nil || envelope["context_handle"] == nil {
		t.Fatal("configured gateway limit broke atomic capability delivery")
	}
	beforeRPC, beforeGET := fixture.calls.Load(), fixture.gets.Load()
	gateway.SetMaxToolResultBytes(1)
	result, err = gateway.CallTool(t.Context(), "agent__send", map[string]any{"message": "refuse"})
	if err != nil {
		t.Fatal(err)
	}
	if got := a2aAdapterEnvelope(t, result); got["error"] != "result_budget_too_small" || fixture.calls.Load() != beforeRPC || fixture.gets.Load() != beforeGET {
		t.Fatal("changed gateway limit was not enforced before outbound I/O")
	}
}

func TestA2AAdapter_ApprovalClearsVerifiedSchemaBlock(t *testing.T) {
	f := newA2AAdapterFixture(t, "1.0")
	g := mcp.NewGateway()
	defer g.Close()
	store := pins.NewWithPath(t.TempDir(), "schema-approval")
	if err := g.SetCardPinStorage(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	g.SetSchemaVerifier(pins.NewGatewayAdapter(store), "block")
	cfg := mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: f.server.URL}}
	if err := g.RegisterMCPServer(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := g.CardTrust().Snapshot(t.Context(), "agent")
	if err != nil {
		t.Fatal(err)
	}
	old := snapshot.Records()
	for i := range old {
		if old[i].Name == "send" {
			old[i].Description = "Previously generated send description"
		}
	}
	// Persist the same approved card with an older generated-tool definition,
	// as an adapter upgrade can encounter. This is the real pin store.
	if err := store.Approve("agent", old); err != nil {
		t.Fatal(err)
	}
	g.UnregisterMCPServer("agent")
	if err := g.RegisterMCPServer(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	before := f.calls.Load()
	result, err := g.CallTool(t.Context(), "agent__send", map[string]any{"message": "blocked"})
	if err != nil || result == nil || !result.IsError || f.calls.Load() != before {
		t.Fatal("generated-tool drift did not block")
	}
	snapshot, err = g.CardTrust().Snapshot(t.Context(), "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.CardTrust().Approve(t.Context(), "agent", snapshot.Hash()); err != nil {
		t.Fatal(err)
	}
	result, err = g.CallTool(t.Context(), "agent__send", map[string]any{"message": "approved"})
	if err != nil || result == nil || result.IsError || f.calls.Load() != before+1 {
		t.Fatal("hash-bound approval left a verified schema block")
	}
}

func TestA2AAdapter_RealHandshakeAndStatelessCapabilities(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			fixture := newA2AAdapterFixture(t, version)
			gateway := mcp.NewGateway()
			defer gateway.Close()
			store := pins.NewWithPath(t.TempDir(), "mcp")
			if err := gateway.SetCardPinStorage(t.Context(), store); err != nil {
				t.Fatal(err)
			}
			if err := gateway.RegisterMCPServer(t.Context(), mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: fixture.server.URL}}); err != nil {
				t.Fatal(err)
			}
			gateway.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{"readers": {Tools: []string{"agent__task_get"}, Overrides: map[string]mcp.GroupOverrideSpec{"agent__task_get": {Name: "read"}}}}))
			listener := httptest.NewServer(api.NewServer(gateway, nil).Handler())
			defer listener.Close()
			route := "/mcp"
			post := func(session string, modern bool, method string, params map[string]any) (string, json.RawMessage) {
				t.Helper()
				if modern {
					params["_meta"] = map[string]any{
						"io.modelcontextprotocol/protocolVersion":    statelessVersion,
						"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "same-label", "version": "1"},
						"io.modelcontextprotocol/clientCapabilities": map[string]any{},
					}
				}
				body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, listener.URL+route, bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				if modern {
					req.Header.Set("MCP-Protocol-Version", statelessVersion)
					req.Header.Set("Mcp-Method", method)
					if name, ok := params["name"].(string); ok {
						req.Header.Set("Mcp-Name", name)
					}
				} else if session != "" {
					req.Header.Set("Mcp-Session-Id", session)
					req.Header.Set("MCP-Protocol-Version", "2025-11-25")
				}
				resp, err := listener.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("MCP HTTP status %d", resp.StatusCode)
				}
				if method == "tools/call" && resp.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("MCP call response can be stored")
				}
				var wire struct {
					Result json.RawMessage `json:"result"`
					Error  json.RawMessage `json:"error"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&wire) != nil || len(wire.Error) != 0 {
					t.Fatal("MCP request failed at the protocol boundary")
				}
				return resp.Header.Get("Mcp-Session-Id"), wire.Result
			}
			initialize := func() string {
				session, _ := post("", false, "initialize", map[string]any{"protocolVersion": "2025-11-25", "clientInfo": map[string]any{"name": "same-label", "version": "1"}, "capabilities": map[string]any{}})
				if session == "" {
					t.Fatal("handshake did not establish an MCP session")
				}
				return session
			}
			firstSession, secondSession := initialize(), initialize()
			call := func(session string, modern bool, tool string, args map[string]any) map[string]any {
				t.Helper()
				_, body := post(session, modern, "tools/call", map[string]any{"name": "agent__" + tool, "arguments": args})
				var result mcp.ToolCallResult
				if json.Unmarshal(body, &result) != nil {
					t.Fatal("invalid MCP tool result")
				}
				if modern && result.ResultType != mcp.ResultTypeComplete {
					t.Fatal("stateless invocation did not complete independently of remote work")
				}
				return a2aAdapterEnvelope(t, &result)
			}
			first := call(firstSession, false, "send", map[string]any{"message": "hello"})
			if first["task_handle"] == nil {
				t.Fatal("handshake caller did not receive task authority")
			}
			for _, modern := range []bool{false, true} {
				call(secondSession, modern, "send", map[string]any{"message": "exact data", "data": map[string]any{"n": json.Number("9007199254740993"), "fraction": json.Number("0.1234567890123456789")}})
				fixture.mu.Lock()
				exact := fixture.lastData["n"] == json.Number("9007199254740993") && fixture.lastData["fraction"] == json.Number("0.1234567890123456789")
				fixture.mu.Unlock()
				if !exact {
					t.Fatal("MCP ingress rounded A2A application numbers")
				}
				beforeGET, beforeRPC := fixture.gets.Load(), fixture.calls.Load()
				for _, tool := range []string{"task_get", "task_cancel", "send"} {
					args := map[string]any{"task_handle": "task-1"}
					if tool == "send" {
						args["message"], args["context_handle"] = "impersonate", "context-1"
					}
					if got := call(secondSession, modern, tool, args); got["error"] != "capability_unavailable" {
						t.Fatal("same-name MCP caller obtained routing authority")
					}
				}
				if fixture.calls.Load() != beforeRPC || fixture.gets.Load() != beforeGET {
					t.Fatal("same-name invalid capability dispatched I/O")
				}
				if got := call(secondSession, modern, "task_get", map[string]any{"task_handle": first["task_handle"]}); got["task_handle"] != first["task_handle"] || got["context_handle"] != nil {
					t.Fatal("deliberate bearer transfer did not work across MCP sessions")
				}
			}
			modern := call("", true, "skill-chat", map[string]any{"message": "new conversation"})
			if modern["task_handle"] == nil || modern["state"] != "input-required" {
				t.Fatal("stateless invocation lost remote interrupted state")
			}
			if got := call(firstSession, false, "task_cancel", map[string]any{"task_handle": modern["task_handle"]}); got["state"] != "canceled" {
				t.Fatal("cross-era bearer cancellation failed")
			}
			route = "/groups/readers/mcp"
			groupSession := initialize()
			for _, modern := range []bool{false, true} {
				if got := call(groupSession, modern, "read", map[string]any{"task_handle": first["task_handle"]}); got["task_handle"] != first["task_handle"] {
					t.Fatal("real grouped MCP alias lost bearer authority")
				}
				beforeGET, beforeRPC := fixture.gets.Load(), fixture.calls.Load()
				if got := call(groupSession, modern, "read", map[string]any{"task_handle": "raw-task-id"}); got["error"] != "capability_unavailable" {
					t.Fatal("real grouped MCP alias bypassed capability guard")
				}
				if fixture.gets.Load() != beforeGET || fixture.calls.Load() != beforeRPC {
					t.Fatal("invalid group capability caused outbound I/O")
				}
			}
		})
	}
}
