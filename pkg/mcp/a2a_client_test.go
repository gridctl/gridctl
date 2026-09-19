package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Unit-only snapshot/storage double. Adapter acceptance also requires the real
// pin store through gateway construction in tests/integration.
type a2aUnitPins struct {
	mu       sync.Mutex
	approved map[string]PinSnapshot
}

type a2aUnitSnapshot struct {
	gen     uint64
	hash    string
	records []Tool
}

func (s a2aUnitSnapshot) Generation() uint64 { return s.gen }
func (s a2aUnitSnapshot) Hash() string       { return s.hash }
func (s a2aUnitSnapshot) Records() []Tool {
	tools := make([]Tool, len(s.records))
	for i, tool := range s.records {
		tools[i] = tool
		tools[i].InputSchema = append([]byte(nil), tool.InputSchema...)
	}
	return tools
}

func (s *a2aUnitPins) BuildCardSnapshot(gen uint64, card, endpoint, dialect, profile string, body []byte, tools []Tool) (PinSnapshot, error) {
	identity := sha256.Sum256([]byte(strings.Join([]string{card, endpoint, dialect, profile}, "\x00")))
	digest := sha256.Sum256(body)
	records := append([]Tool(nil), tools...)
	records = append(records, Tool{Name: "_agent_card", Description: "sha256:" + hex.EncodeToString(digest[:])}, Tool{Name: "_agent_identity", Description: "sha256:" + hex.EncodeToString(identity[:])})
	encoded, err := json.Marshal(records)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return a2aUnitSnapshot{gen: gen, hash: hex.EncodeToString(hash[:]), records: records}, nil
}

func (s *a2aUnitPins) VerifyCard(_ context.Context, name string, snapshot PinSnapshot) (CardPinDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.approved == nil {
		s.approved = make(map[string]PinSnapshot)
	}
	if s.approved[name] == nil {
		s.approved[name] = snapshot
	}
	return CardPinDecision{Trusted: snapshotRecord(s.approved[name], "_agent_card") == snapshotRecord(snapshot, "_agent_card")}, nil
}

func (s *a2aUnitPins) CheckCard(ctx context.Context, name string, snapshot PinSnapshot) (CardPinDecision, error) {
	return s.VerifyCard(ctx, name, snapshot)
}

func (s *a2aUnitPins) ApproveCard(_ context.Context, name string, snapshot PinSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approved[name] = snapshot
	return nil
}

type a2aUnitFixture struct {
	server       *httptest.Server
	version      string
	gets, calls  atomic.Int64
	cardRevision atomic.Int64
	mu           sync.Mutex
	sessions     []string
	discovery    string
	request      map[string]any
	onRPC        func(http.ResponseWriter, *http.Request, map[string]any)
	noCache      bool
}

func newA2AUnitFixture(t *testing.T, version string, noCache bool) *a2aUnitFixture {
	t.Helper()
	f := &a2aUnitFixture{version: version, noCache: noCache}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			f.gets.Add(1)
			f.mu.Lock()
			f.discovery = r.Header.Get("X-Amzn-Bedrock-AgentCore-Runtime-Session-Id")
			f.mu.Unlock()
			if f.noCache {
				w.Header().Set("Cache-Control", "no-store")
			}
			card := map[string]any{
				"name": "fixture", "description": fmt.Sprintf("revision %d", f.cardRevision.Load()),
				"defaultInputModes": []string{"text/plain", "application/json"}, "defaultOutputModes": []string{"text/plain", "application/json"},
				"skills": []any{map[string]any{"id": "chat", "description": "Talk to the fixture"}},
			}
			if version == "1.0" {
				card["supportedInterfaces"] = []any{map[string]any{"url": f.server.URL, "protocolBinding": "JSONRPC", "protocolVersion": "1.0.0"}}
			} else {
				card["url"], card["protocolVersion"] = f.server.URL, "0.3.0"
			}
			_ = json.NewEncoder(w).Encode(card)
			return
		}
		f.calls.Add(1)
		if r.Header.Get("A2A-Version") != version {
			t.Error("incorrect dialect header")
		}
		var req map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.sessions = append(f.sessions, r.Header.Get("X-Amzn-Bedrock-AgentCore-Runtime-Session-Id"))
		f.request = req
		callback := f.onRPC
		f.mu.Unlock()
		if callback != nil {
			callback(w, r, req)
			return
		}
		f.respond(w, req, "input-required", "task", "context")
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *a2aUnitFixture) respond(w http.ResponseWriter, req map[string]any, state, task, conversation string) {
	wireState := state
	if f.version == "1.0" {
		wireState = "TASK_STATE_" + strings.ToUpper(strings.ReplaceAll(state, "-", "_"))
	}
	result := map[string]any{"id": task, "contextId": conversation, "status": map[string]any{"state": wireState}}
	if f.version == "0.3" {
		result["kind"] = "task"
	} else if req["method"] == "SendMessage" {
		result = map[string]any{"task": result}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
}

func (f *a2aUnitFixture) client(t *testing.T, profile string, limit int) *A2AClient {
	t.Helper()
	pins := &a2aUnitPins{}
	client, err := newA2AClient(context.Background(), "agent", A2AClientConfig{Card: f.server.URL, Dialect: "auto", Profile: profile}, NewCapabilityStore(), NewCardTrustService(pins), pins, limit)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func a2aUnitCall(t *testing.T, c *A2AClient, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := c.CallTool(context.Background(), name, args)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || len(result.Content) != 1 || !result.atomicResult {
		t.Fatal("expected one atomic envelope")
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &envelope); err != nil {
		t.Fatal(err)
	}
	if _, failed := envelope["error"]; failed != result.IsError {
		t.Fatal("error envelope and IsError disagree")
	}
	return envelope
}

func TestA2AClient_LifecycleAndAuthority(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			fixture := newA2AUnitFixture(t, version, true)
			c := fixture.client(t, "", 0)
			if c.Name() != "agent" || !c.IsInitialized() || c.ServerInfo().Name != "agent" || len(c.Tools()) != 4 || c.Ping(context.Background()) != nil {
				t.Fatal("initialization did not publish the expected client")
			}
			if len(c.PinSnapshot().Records()) != 6 {
				t.Fatal("missing unfiltered hidden trust records")
			}
			c.SetToolWhitelist([]string{"send"})
			if err := c.RefreshTools(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(c.Tools()) != 1 || len(c.PinSnapshot().Records()) != 6 {
				t.Fatal("whitelist changed trust inventory")
			}
			c.SetToolWhitelist(nil)
			first := a2aUnitCall(t, c, "send", map[string]any{"message": "hello"})
			contextHandle, taskHandle := first["context_handle"], first["task_handle"]
			if _, valid := capabilityDigest(fmt.Sprint(contextHandle), "c"); !valid {
				t.Fatal("missing context authority")
			}
			if _, valid := capabilityDigest(fmt.Sprint(taskHandle), "t"); !valid {
				t.Fatal("missing task authority")
			}
			beforeGET, beforeRPC := fixture.gets.Load(), fixture.calls.Load()
			for _, args := range []map[string]any{
				{"task_handle": "task"}, {"task_handle": contextHandle}, {"task_handle": ""}, {},
			} {
				if got := a2aUnitCall(t, c, "task_get", args); got["error"] != "capability_unavailable" {
					t.Fatal("invalid authority did not fail uniformly")
				}
			}
			if fixture.gets.Load() != beforeGET || fixture.calls.Load() != beforeRPC {
				t.Fatal("invalid capability caused outbound I/O")
			}
			get := a2aUnitCall(t, c, "task_get", map[string]any{"task_handle": taskHandle})
			if get["task_handle"] != taskHandle || get["context_handle"] != nil {
				t.Fatal("get leaked parent authority")
			}
			resume := a2aUnitCall(t, c, "skill-chat", map[string]any{"message": "continue", "context_handle": contextHandle, "task_handle": taskHandle})
			if resume["task_handle"] != taskHandle || resume["context_handle"] != contextHandle {
				t.Fatal("resume did not preserve supplied authority")
			}
			fixture.mu.Lock()
			params := fixture.request["params"].(map[string]any)
			message := params["message"].(map[string]any)
			if message["taskId"] != "task" || message["contextId"] != "context" || message["metadata"].(map[string]any)["skill_id"] != "chat" {
				t.Error("resume did not use stored routing and advisory skill metadata")
			}
			fixture.onRPC = func(w http.ResponseWriter, _ *http.Request, req map[string]any) {
				fixture.respond(w, req, "canceled", "task", "context")
			}
			fixture.mu.Unlock()
			cancel := a2aUnitCall(t, c, "task_cancel", map[string]any{"task_handle": taskHandle})
			if cancel["state"] != "canceled" || cancel["context_handle"] != nil {
				t.Fatal("cancel did not return the remote observation")
			}
			if err := c.Close(); err != nil || c.Ping(context.Background()) == nil {
				t.Fatal("closed client remained healthy")
			}
		})
	}
}

func TestA2AClient_DriftAndApprovalRetireAuthority(t *testing.T) {
	fixture := newA2AUnitFixture(t, "1.0", true)
	c := fixture.client(t, "", 0)
	first := a2aUnitCall(t, c, "send", map[string]any{"message": "hello"})
	fixture.cardRevision.Add(1)
	before := fixture.calls.Load()
	got := a2aUnitCall(t, c, "task_get", map[string]any{"task_handle": first["task_handle"]})
	if got["error"] == nil || fixture.calls.Load() != before {
		t.Fatal("card drift permitted RPC")
	}
	pending := c.PinSnapshot()
	if err := c.trust.Approve(context.Background(), c.Name(), ""); err == nil {
		t.Fatal("unbound approval succeeded")
	}
	gets := fixture.gets.Load()
	if err := c.trust.Approve(context.Background(), c.Name(), pending.Hash()); err != nil {
		t.Fatal(err)
	}
	if fixture.gets.Load() != gets+1 {
		t.Fatal("approval reused cached discovery")
	}
	gets = fixture.gets.Load()
	got = a2aUnitCall(t, c, "task_get", map[string]any{"task_handle": first["task_handle"]})
	if got["error"] != "capability_unavailable" || fixture.gets.Load() != gets || fixture.calls.Load() != before {
		t.Fatal("approval rebound old authority")
	}
	got = a2aUnitCall(t, c, "send", map[string]any{"message": "new work"})
	if got["task_handle"] == nil || got["task_handle"] == first["task_handle"] {
		t.Fatal("approval did not permit fresh authority")
	}
}

func TestA2AClient_PendingRegistrationPublishesOnlyOnApproval(t *testing.T) {
	fixture := newA2AUnitFixture(t, "1.0", true)
	first := fixture.client(t, "", 0)
	old := a2aUnitCall(t, first, "send", map[string]any{"message": "first"})
	fixture.cardRevision.Add(1)
	next, err := newA2AClient(context.Background(), first.name, first.cfg, first.store, first.trust, first.builder, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = next.Close() })
	var publications atomic.Int64
	next.toolsChanged = func() { publications.Add(1) }
	if err := next.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(next.Tools()) != 0 || publications.Load() != 0 || next.Ping(context.Background()) == nil {
		t.Fatal("pending registration published candidate tools")
	}
	before := fixture.calls.Load()
	if got := a2aUnitCall(t, next, "send", map[string]any{"message": "candidate"}); got["error"] != "card_approval_required" || fixture.calls.Load() != before {
		t.Fatal("unapproved registration became callable")
	}
	if err := next.trust.Approve(context.Background(), next.Name(), next.PinSnapshot().Hash()); err != nil {
		t.Fatal(err)
	}
	if len(next.Tools()) != 4 || publications.Load() != 1 || next.Ping(context.Background()) != nil {
		t.Fatal("approval did not synchronously publish tools and authority")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if got := a2aUnitCall(t, next, "task_get", map[string]any{"task_handle": old["task_handle"]}); got["error"] != "capability_unavailable" {
		t.Fatal("replacement retained old authority")
	}
	if got := a2aUnitCall(t, next, "send", map[string]any{"message": "fresh"}); got["task_handle"] == nil {
		t.Fatal("closing replaced instance retired the replacement")
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	gets := fixture.gets.Load()
	if err := next.Initialize(context.Background()); err == nil || fixture.gets.Load() != gets {
		t.Fatal("closed client restarted discovery")
	}
}

func TestA2AClient_BedrockSessionsAndGenericCollisions(t *testing.T) {
	for _, profile := range []string{"", "bedrock"} {
		t.Run(profile, func(t *testing.T) {
			fixture := newA2AUnitFixture(t, "0.3", false)
			c := fixture.client(t, profile, 0)
			first := a2aUnitCall(t, c, "send", map[string]any{"message": "one"})
			second := a2aUnitCall(t, c, "send", map[string]any{"message": "two"})
			if fixture.gets.Load() != 1 {
				t.Fatal("fresh cache did not avoid card GET")
			}
			if profile == "" {
				if second["error"] != "protocol_conflict" {
					t.Fatal("generic roots aliased known remote IDs")
				}
			} else if second["task_handle"] == nil || second["task_handle"] == first["task_handle"] {
				t.Fatal("distinct Bedrock sessions failed to isolate identical remote IDs")
			}
			_ = a2aUnitCall(t, c, "task_get", map[string]any{"task_handle": first["task_handle"]})
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if profile == "" {
				if fixture.discovery != "" || strings.Join(fixture.sessions, "") != "" {
					t.Fatal("generic endpoint received session headers")
				}
			} else if fixture.discovery == "" || fixture.sessions[0] == fixture.discovery || fixture.sessions[0] == fixture.sessions[1] || fixture.sessions[0] != fixture.sessions[2] {
				t.Fatal("discovery, root, or continuation sessions were mixed")
			}
		})
	}
}

func TestA2AClient_ResultBudgetRefusesBeforeIO(t *testing.T) {
	fixture := newA2AUnitFixture(t, "1.0", true)
	c := fixture.client(t, "", 1)
	gets := fixture.gets.Load()
	got := a2aUnitCall(t, c, "send", map[string]any{"message": "hello"})
	if got["error"] != "result_budget_too_small" || fixture.calls.Load() != 0 || fixture.gets.Load() != gets {
		t.Fatal("minimal result budget was not reserved before outbound I/O")
	}
}

func TestA2AClient_ConcurrentCancelSupersedesResume(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		for _, order := range []string{"cancel-first", "send-first"} {
			t.Run(version+"/"+order, func(t *testing.T) {
				fixture := newA2AUnitFixture(t, version, false)
				c := fixture.client(t, "bedrock", 0)
				first := a2aUnitCall(t, c, "send", map[string]any{"message": "start"})
				sendEntered, cancelEntered := make(chan struct{}), make(chan struct{})
				releaseSend, releaseCancel := make(chan struct{}), make(chan struct{})
				fixture.mu.Lock()
				fixture.onRPC = func(w http.ResponseWriter, r *http.Request, req map[string]any) {
					var release <-chan struct{}
					state := "input-required"
					switch req["method"] {
					case "SendMessage", "message/send":
						close(sendEntered)
						release = releaseSend
					case "CancelTask", "tasks/cancel":
						close(cancelEntered)
						release, state = releaseCancel, "canceled"
					default:
						fixture.respond(w, req, "canceled", "task", "context")
						return
					}
					select {
					case <-release:
						fixture.respond(w, req, state, "task", "context")
					case <-r.Context().Done():
					}
				}
				fixture.mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				type outcome struct {
					result *ToolCallResult
					err    error
				}
				resumeDone, cancelDone := make(chan outcome, 1), make(chan outcome, 1)
				resumeArgs := map[string]any{"message": "resume", "context_handle": first["context_handle"], "task_handle": first["task_handle"]}
				taskArgs := map[string]any{"task_handle": first["task_handle"]}
				go func() {
					result, err := c.CallTool(ctx, "send", resumeArgs)
					resumeDone <- outcome{result, err}
				}()
				select {
				case <-sendEntered:
				case <-ctx.Done():
					t.Fatal("resume did not reach remote")
				}
				for _, tool := range []string{"send", "task_get"} {
					args := taskArgs
					if tool == "send" {
						args = resumeArgs
					}
					if got := a2aUnitCall(t, c, tool, args); got["error"] != "operation_in_progress" {
						t.Fatal("overlap did not fail fast")
					}
				}
				go func() {
					result, err := c.CallTool(ctx, "task_cancel", taskArgs)
					cancelDone <- outcome{result, err}
				}()
				select {
				case <-cancelEntered:
				case <-ctx.Done():
					t.Fatal("verified cancel did not overlap the blocked resume")
				}
				if got := a2aUnitCall(t, c, "task_cancel", taskArgs); got["error"] != "operation_in_progress" {
					t.Fatal("second cancel was admitted")
				}
				var sendResult, cancelResult outcome
				if order == "cancel-first" {
					close(releaseCancel)
					cancelResult = <-cancelDone
					close(releaseSend)
					sendResult = <-resumeDone
				} else {
					close(releaseSend)
					sendResult = <-resumeDone
					close(releaseCancel)
					cancelResult = <-cancelDone
				}
				if sendResult.err != nil || sendResult.result == nil || !sendResult.result.IsError || !strings.Contains(sendResult.result.Content[0].Text, "operation_superseded") {
					t.Fatal("older resume published an observation or authority")
				}
				if cancelResult.err != nil || cancelResult.result == nil || cancelResult.result.IsError || !strings.Contains(cancelResult.result.Content[0].Text, `"state":"canceled"`) {
					t.Fatal("acknowledged cancellation was not delivered")
				}
				if got := a2aUnitCall(t, c, "send", resumeArgs); got["error"] != "operation_uncertain" {
					t.Fatal("overlap skipped post-drain reconciliation")
				}
				if got := a2aUnitCall(t, c, "task_get", taskArgs); got["state"] != "canceled" {
					t.Fatal("get did not reconcile canceled work")
				}
				if got := a2aUnitCall(t, c, "send", resumeArgs); got["error"] != "capability_unavailable" {
					t.Fatal("terminal task was revived")
				}
			})
		}
	}
}

func TestA2AClient_ResponseInjectionPreservesExistingAuthority(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			fixture := newA2AUnitFixture(t, version, false)
			c := fixture.client(t, "", 0)
			first := a2aUnitCall(t, c, "send", map[string]any{"message": "first"})
			fixture.mu.Lock()
			fixture.onRPC = func(w http.ResponseWriter, _ *http.Request, req map[string]any) {
				fixture.respond(w, req, "completed", "foreign-task", "foreign-context")
			}
			fixture.mu.Unlock()
			for _, tool := range []string{"task_get", "task_cancel", "send"} {
				args := map[string]any{"task_handle": first["task_handle"]}
				if tool == "send" {
					// Cancel's conflicting mutation makes this root uncertain. Use
					// a read below to reconcile before checking resume injection.
					fixture.mu.Lock()
					fixture.onRPC = nil
					fixture.mu.Unlock()
					_ = a2aUnitCall(t, c, "task_get", args)
					fixture.mu.Lock()
					fixture.onRPC = func(w http.ResponseWriter, _ *http.Request, req map[string]any) {
						fixture.respond(w, req, "completed", "foreign-task", "foreign-context")
					}
					fixture.mu.Unlock()
					args["context_handle"], args["message"] = first["context_handle"], "resume"
				}
				got := a2aUnitCall(t, c, tool, args)
				if got["error"] != "protocol_conflict" || got["task_handle"] != nil || got["context_handle"] != nil {
					t.Fatal("conflicting identities were admitted")
				}
			}
			fixture.mu.Lock()
			fixture.onRPC = nil
			fixture.mu.Unlock()
			got := a2aUnitCall(t, c, "task_get", map[string]any{"task_handle": first["task_handle"]})
			if got["task_handle"] != first["task_handle"] || got["state"] != "input-required" {
				t.Fatal("response injection damaged existing authority")
			}
		})
	}
}

func TestA2AClient_RemoteErrorsAreLocalAndSingleAttempt(t *testing.T) {
	for _, tc := range []struct {
		status, code int
		message      string
		retryable    bool
	}{
		{401, 0, "", false}, {403, 0, "", false}, {424, 0, "", false}, {424, -32055, "remote failure", false},
		{409, -32054, "Session operation in progress, please retry", true}, {409, -32054, "Conflict", false},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.message), func(t *testing.T) {
			fixture := newA2AUnitFixture(t, "0.3", false)
			c := fixture.client(t, "bedrock", 0)
			fixture.mu.Lock()
			fixture.onRPC = func(w http.ResponseWriter, _ *http.Request, req map[string]any) {
				w.WriteHeader(tc.status)
				if tc.code == 0 {
					_, _ = w.Write([]byte("private remote body"))
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "error": map[string]any{"code": tc.code, "message": tc.message, "data": "private remote body"}})
				}
			}
			fixture.mu.Unlock()
			got := a2aUnitCall(t, c, "send", map[string]any{"message": "work"})
			wantCategory := "http_failed"
			if tc.code != 0 {
				wantCategory = "rpc_failed"
			}
			if got["error"] != wantCategory || got["http_status"] != float64(tc.status) || got["retryable"] != tc.retryable || fixture.calls.Load() != 1 {
				t.Fatal("remote failure lost safe status or retried")
			}
			if strings.Contains(fmt.Sprint(got), "private remote body") || tc.message != "" && strings.Contains(fmt.Sprint(got), tc.message) {
				t.Fatal("downstream error text escaped")
			}
			c.store.mu.Lock()
			defer c.store.mu.Unlock()
			if c.store.used != (capabilityCounts{}) {
				t.Fatal("known failure retained undelivered capacity")
			}
		})
	}
}

func TestA2AClient_RandomAndCapacityRefusalBeforeDiscovery(t *testing.T) {
	for _, failure := range []string{"random", "roots", "tasks", "tombstones"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newA2AUnitFixture(t, "1.0", true)
			c := fixture.client(t, "", 0)
			c.store.mu.Lock()
			want := "capability_capacity_exhausted"
			switch failure {
			case "random":
				c.store.random = failingCapabilityRandom{}
				want = "capability_random_unavailable"
			case "roots":
				c.store.globalLimit.roots = 0
			case "tasks":
				c.store.globalLimit.tasks = 0
			case "tombstones":
				c.store.globalLimit.tombstones = 1
			}
			c.store.mu.Unlock()
			gets := fixture.gets.Load()
			if got := a2aUnitCall(t, c, "send", map[string]any{"message": "hello"}); got["error"] != want {
				t.Fatal("failed resource reservation was not reported")
			}
			if fixture.calls.Load() != 0 || fixture.gets.Load() != gets {
				t.Fatal("reservation failure caused outbound I/O")
			}
			c.store.mu.Lock()
			defer c.store.mu.Unlock()
			if c.store.used != (capabilityCounts{}) {
				t.Fatal("failed reservation leaked capacity")
			}
		})
	}
}

func TestA2AClient_CanceledFreshSendHasNoRecovery(t *testing.T) {
	fixture := newA2AUnitFixture(t, "1.0", false)
	c := fixture.client(t, "", 0)
	now := time.Now()
	c.store.mu.Lock()
	c.store.now = func() time.Time { return now }
	c.store.mu.Unlock()
	entered := make(chan struct{})
	fixture.mu.Lock()
	fixture.onRPC = func(_ http.ResponseWriter, r *http.Request, _ map[string]any) {
		close(entered)
		<-r.Context().Done()
	}
	fixture.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := c.CallTool(ctx, "send", map[string]any{"message": "blocking"})
		if result != nil {
			done <- errors.New("canceled request delivered authority")
			return
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("send did not reach remote")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation identity")
	}
	if fixture.calls.Load() != 1 {
		t.Fatal("local cancellation sent another RPC")
	}
	c.store.mu.Lock()
	if c.store.used.roots != 1 || c.store.used.tasks != 1 || len(c.authority.contexts) != 0 || len(c.authority.tasks) != 0 {
		t.Error("ambiguous fresh work did not retain a capability-free lease")
	}
	now = now.Add(10 * time.Minute)
	c.store.sweepLocked()
	if c.store.used != (capabilityCounts{}) {
		t.Error("uncertainty lease did not reclaim undelivered capacity")
	}
	c.store.mu.Unlock()
}

func TestA2AClient_DirectMessageNumbersAndUnusedCapacity(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			fixture := newA2AUnitFixture(t, version, false)
			c := fixture.client(t, "", 0)
			fixture.mu.Lock()
			fixture.onRPC = func(w http.ResponseWriter, _ *http.Request, req map[string]any) {
				parts := []any{map[string]any{"text": ""}, map[string]any{"data": map[string]any{"large": json.Number("9007199254740993")}}}
				message := map[string]any{"messageId": "message", "contextId": "context", "role": "ROLE_AGENT", "parts": parts}
				var result any
				if version == "0.3" {
					message["role"], message["kind"] = "agent", "message"
					parts[0].(map[string]any)["kind"], parts[1].(map[string]any)["kind"] = "text", "data"
					result = message
				} else {
					result = map[string]any{"message": message}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
			}
			fixture.mu.Unlock()
			result, err := c.CallTool(context.Background(), "send", map[string]any{"message": "hello", "data": map[string]any{"large": json.Number("9007199254740993")}})
			if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, `"large":9007199254740993`) || !strings.Contains(result.Content[0].Text, `"text":""`) {
				t.Fatal("direct message changed accepted application content")
			}
			if strings.Contains(result.Content[0].Text, "task_handle") || strings.Contains(result.Content[0].Text, "messageId") || strings.Contains(result.Content[0].Text, "contextId") {
				t.Fatal("direct message exposed protocol IDs or unused authority")
			}
			c.store.mu.Lock()
			defer c.store.mu.Unlock()
			if c.store.used != (capabilityCounts{roots: 1, tombstones: 1}) {
				t.Fatal("direct message retained unused task/tombstone reservations")
			}
		})
	}
}
