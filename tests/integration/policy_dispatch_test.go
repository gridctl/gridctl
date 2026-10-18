//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/toolpolicy"
)

func mockCallCounts(t *testing.T, port int) map[string]int {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/calls", port))
	if err != nil {
		t.Fatalf("call counts: %v", err)
	}
	defer resp.Body.Close()
	var counts map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&counts); err != nil {
		t.Fatalf("decode counts: %v", err)
	}
	if counts == nil {
		counts = map[string]int{}
	}
	return counts
}

func policyRules() *config.PolicyConfig {
	return &config.PolicyConfig{
		Rules: []config.PolicyRule{
			{Match: "alpha__*", Action: "block"},
			{Match: "alpha__echo", Action: "allow"},
		},
	}
}

// TestPolicyDispatch_EveryPath blocks alpha__add and allows alpha__echo on
// every tools/call entry point. The mock records zero tools/call requests
// for the blocked name.
func TestPolicyDispatch_EveryPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-destructive-tool")
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:      "alpha",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:  true,
	}); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	gw.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{
		"grp": {Servers: []string{"alpha"}},
	}))
	listed, err := gw.HandleToolsList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	if !names["alpha__add"] || !names["alpha__echo"] || !names["alpha__purge"] {
		t.Fatalf("tools/list before code mode = %v", names)
	}
	gw.SetCodeMode(15 * time.Second)
	if _, pol := controller.ApplyCallGates(gw, &config.Stack{Policy: policyRules()}, nil, nil); pol == nil {
		t.Fatal("policy gate was not installed")
	}

	store := pins.NewWithPath(t.TempDir(), "policy-pins")
	gw.SetSchemaVerifier(pins.NewGatewayAdapter(store), "warn")
	// Registration already happened; pin by re-registering so the verifier
	// sees the tool definitions, then confirm a later gate swap does not drift.
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:      "alpha",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:  true,
	}); err != nil {
		t.Fatalf("re-register for pins: %v", err)
	}
	controller.ApplyCallGates(gw, &config.Stack{Policy: policyRules()}, nil, nil)
	if sp, ok := store.GetServer("alpha"); !ok || sp.Status != pins.StatusPinned {
		t.Fatalf("pin status after policy install = ok=%v %+v", ok, sp)
	}

	apiServer := api.NewServer(gw, nil)
	apiServer.SetPolicyStatusFunc(func() toolpolicy.StatusReport {
		return policyReport(gw, policyRules())
	})
	ts := httptest.NewServer(apiServer.Handler())
	t.Cleanup(ts.Close)

	post := func(t *testing.T, path, session string, body map[string]any, headers map[string]string) string {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d body %s", path, resp.StatusCode, buf)
		}
		return string(buf)
	}
	initialize := func(t *testing.T, path string) string {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"clientInfo":      map[string]any{"name": "tester", "version": "1"},
			},
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		session := resp.Header.Get("Mcp-Session-Id")
		if session == "" {
			t.Fatalf("no session on %s (status %d)", path, resp.StatusCode)
		}
		return session
	}

	assertBlocked := func(t *testing.T, body string) {
		t.Helper()
		if !strings.Contains(body, "Error: tool blocked by policy (rule 1: alpha__*)") || !strings.Contains(body, `"isError":true`) {
			t.Fatalf("expected in-band policy denial, got %s", body)
		}
		if strings.Contains(body, "-32602") || strings.Contains(body, "Unknown tool") {
			t.Fatalf("policy denial used the unknown-tool code: %s", body)
		}
	}
	assertAllowed := func(t *testing.T, body string) {
		t.Helper()
		if !strings.Contains(body, "Echo:") || strings.Contains(body, `"isError":true`) {
			t.Fatalf("expected echo success, got %s", body)
		}
	}

	before := mockCallCounts(t, port)

	t.Run("catalog keeps blocked tools", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/tools/catalog?include=all")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(raw), "alpha__add") || !strings.Contains(string(raw), "alpha__echo") {
			t.Fatalf("catalog dropped a blocked tool: %s", raw)
		}
	})

	t.Run("handshake", func(t *testing.T) {
		session := initialize(t, "/mcp")
		blocked := post(t, "/mcp", session, map[string]any{
			"jsonrpc": "2.0", "id": 3, "method": "tools/call",
			"params": map[string]any{"name": "alpha__add", "arguments": map[string]any{"a": 1, "b": 2}},
		}, nil)
		assertBlocked(t, blocked)
		allowed := post(t, "/mcp", session, map[string]any{
			"jsonrpc": "2.0", "id": 4, "method": "tools/call",
			"params": map[string]any{"name": "alpha__echo", "arguments": map[string]any{"message": "hi"}},
		}, nil)
		assertAllowed(t, allowed)
	})

	t.Run("stateless", func(t *testing.T) {
		body := post(t, "/mcp", "", map[string]any{
			"jsonrpc": "2.0", "id": 5, "method": "tools/call",
			"params": map[string]any{
				"name":      "alpha__add",
				"arguments": map[string]any{"a": 1, "b": 2},
				"_meta": map[string]any{
					"io.modelcontextprotocol/protocolVersion":    statelessVersion,
					"io.modelcontextprotocol/clientCapabilities": map[string]any{},
				},
			},
		}, map[string]string{
			"MCP-Protocol-Version": statelessVersion,
			"Mcp-Method":           "tools/call",
			"Mcp-Name":             "alpha__add",
		})
		assertBlocked(t, body)
	})

	t.Run("group", func(t *testing.T) {
		session := initialize(t, "/groups/grp/mcp")
		body := post(t, "/groups/grp/mcp", session, map[string]any{
			"jsonrpc": "2.0", "id": 6, "method": "tools/call",
			"params": map[string]any{"name": "alpha__add", "arguments": map[string]any{"a": 1, "b": 2}},
		}, nil)
		assertBlocked(t, body)
	})

	t.Run("code mode execute", func(t *testing.T) {
		session := initialize(t, "/mcp")
		body := post(t, "/mcp", session, map[string]any{
			"jsonrpc": "2.0", "id": 7, "method": "tools/call",
			"params": map[string]any{
				"name": "execute",
				"arguments": map[string]any{
					"code": `(async () => { return await mcp.callTool("alpha", "add", {a: 1, b: 2}); })()`,
				},
			},
		}, nil)
		if !strings.Contains(body, "Error: tool blocked by policy (rule 1: alpha__*)") {
			t.Fatalf("execute did not surface the policy denial: %s", body)
		}
	})

	t.Run("rest", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/tools/call", strings.NewReader(`{"name":"alpha__add","arguments":{"a":1,"b":2}}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d %s", resp.StatusCode, raw)
		}
		var env struct {
			Outcome mcp.CallOutcome `json:"outcome"`
			Result  *struct{}       `json:"result"`
			Error   *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		if env.Outcome.Disposition != "denied" || env.Outcome.Stage != "gate" || env.Outcome.Reason != "policy_blocked" || env.Outcome.Gate != "policy" || env.Outcome.Completion != "not_started" {
			t.Fatalf("outcome = %+v", env.Outcome)
		}
		if env.Error == nil || env.Error.Code != "policy_blocked" || env.Error.Message != "tool blocked by policy" {
			t.Fatalf("error = %+v", env.Error)
		}
		if strings.Contains(string(raw), "alpha__*") {
			t.Fatalf("safe message leaked the rule: %s", raw)
		}
	})

	t.Run("cli", func(t *testing.T) {
		root, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		bin := filepath.Join(dir, "gridctl")
		build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/gridctl")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
		home := filepath.Join(dir, "home")
		if err := os.MkdirAll(filepath.Join(home, ".gridctl", "state"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(state.HomeEnv, home)
		if err := state.Save(&state.DaemonState{
			StackName: "policy-demo",
			PID:       os.Getpid(),
			Port:      mustPort(ts.URL),
			Home:      home,
		}); err != nil {
			t.Fatal(err)
		}
		run := func(args ...string) (string, string, int) {
			t.Helper()
			cmd := exec.CommandContext(ctx, bin, args...)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			return stdout.String(), stderr.String(), code
		}
		stdout, stderr, code := run("call", "alpha__add", `{"a":1,"b":2}`)
		if code != 1 || !strings.Contains(stdout+stderr, "tool blocked by policy") {
			t.Fatalf("human exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		stdout, stderr, code = run("call", "alpha__add", `{"a":1,"b":2}`, "--format", "json")
		if code != 1 || !strings.Contains(stdout, `"policy_blocked"`) || !strings.Contains(stdout, `"policy"`) {
			t.Fatalf("json exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
	})

	after := mockCallCounts(t, port)
	if after["add"] != before["add"] {
		t.Fatalf("blocked name reached the mock: before=%v after=%v", before, after)
	}
	if after["echo"] <= before["echo"] {
		t.Fatalf("allowed call did not reach the mock: before=%v after=%v", before, after)
	}
	if sp, ok := store.GetServer("alpha"); !ok || sp.Status != pins.StatusPinned {
		t.Fatalf("pins drifted after policy denials: ok=%v %+v", ok, sp)
	}
}

func TestPolicyDispatch_BlockDestructive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-destructive-tool")
	waitForPort(t, ctx, port)
	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name: "alpha", Transport: mcp.TransportHTTP,
		Endpoint: fmt.Sprintf("http://127.0.0.1:%d/mcp", port), External: true,
	}); err != nil {
		t.Fatal(err)
	}
	controller.ApplyCallGates(gw, &config.Stack{Policy: &config.PolicyConfig{BlockDestructive: true}}, nil, nil)
	res, out, err := gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__purge"})
	if err != nil || res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "block_destructive") || out.Reason != "policy_blocked" {
		t.Fatalf("purge = err=%v res=%+v out=%+v", err, res, out)
	}
	if mockCallCounts(t, port)["purge"] != 0 {
		t.Fatal("destructive tool reached the mock")
	}
	res, _, err = gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__echo", Arguments: map[string]any{"message": "ok"}})
	if err != nil || res == nil || res.IsError {
		t.Fatalf("unannotated tool denied: err=%v res=%+v", err, res)
	}
	controller.ApplyCallGates(gw, &config.Stack{Policy: &config.PolicyConfig{
		BlockDestructive: true,
		Rules:            []config.PolicyRule{{Match: "alpha__purge", Action: "allow"}},
	}}, nil, nil)
	res, _, err = gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__purge"})
	if err != nil || res == nil || res.IsError {
		t.Fatalf("explicit allow lost: err=%v res=%+v", err, res)
	}
}

func TestPolicyDispatch_RateLimitNotConsumed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)
	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name: "alpha", Transport: mcp.TransportHTTP,
		Endpoint: fmt.Sprintf("http://127.0.0.1:%d/mcp", port), External: true,
	}); err != nil {
		t.Fatal(err)
	}
	stack := &config.Stack{
		Policy: policyRules(),
		Limits: &config.LimitsConfig{RateLimits: []config.RateLimit{{Server: "alpha", CallsPerMinute: 60, Burst: 1}}},
	}
	controller.ApplyCallGates(gw, stack, nil, nil)
	res, out, err := gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__add", Arguments: map[string]any{"a": 1, "b": 1}})
	if err != nil || out.Reason != "policy_blocked" {
		t.Fatalf("policy = err=%v out=%+v res=%+v", err, out, res)
	}
	res, out, err = gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__echo", Arguments: map[string]any{"message": "one"}})
	if err != nil || res == nil || res.IsError || out.Reason != "ok" {
		t.Fatalf("first allow = err=%v out=%+v res=%+v", err, out, res)
	}
	_, out, err = gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__echo", Arguments: map[string]any{"message": "two"}})
	if err != nil || out.Reason != "gate_denied" || out.Gate != "rate-limits" {
		t.Fatalf("second allow = err=%v out=%+v", err, out)
	}
}

func TestPolicyDispatch_ExposeAllKeepsBlock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name: "alpha", Transport: mcp.TransportHTTP, Endpoint: endpoint, External: true, Tools: []string{"echo"},
	}); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	spec := fmt.Sprintf(`version: "1"
name: policy-demo
network:
  name: net
  driver: bridge
mcp-servers:
  - name: alpha
    url: %s
    transport: http
    tools: [echo]
policy:
  rules:
    - match: alpha__*
      action: block
    - match: alpha__echo
      action: allow
`, endpoint)
	if err := os.WriteFile(stackPath, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	current := &config.Stack{
		Version: "1",
		Name:    "policy-demo",
		Network: config.Network{Name: "net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{
			Name: "alpha", URL: endpoint, Transport: "http", Tools: []string{"echo"},
		}},
		Policy: policyRules(),
	}
	rh := reload.NewHandler(stackPath, current, gw, runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil)
	rh.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, _ []reload.ReplicaRuntime, _ string) error {
		return gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name: server.Name, Transport: mcp.TransportHTTP, Endpoint: server.URL, External: true, Tools: server.Tools,
		})
	})
	rh.SetOnConfigApplied(func(newCfg *config.Stack) {
		controller.ApplyCallGates(gw, newCfg, nil, nil)
	})

	res, out, err := gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__add"})
	if err != nil || res == nil || res.Content[0].Text != "Error: unknown tool" || out.Reason != "unknown_tool" {
		t.Fatalf("hidden before expose = err=%v res=%+v out=%+v", err, res, out)
	}

	apiServer := api.NewServer(gw, nil)
	apiServer.SetStackFile(stackPath)
	apiServer.SetReloadHandler(rh)
	body := `{"tools":[]}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "/api/mcp-servers/alpha/tools", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:8180"
	req.SetPathValue("name", "alpha")
	w := httptest.NewRecorder()
	apiServer.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status %d %s", w.Code, w.Body.String())
	}
	saved, err := os.ReadFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "policy:") || strings.Contains(string(saved), "tools:") {
		t.Fatalf("stack after expose-all = %s", saved)
	}
	res, out, err = gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__add", Arguments: map[string]any{"a": 1, "b": 2}})
	if err != nil || res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "tool blocked by policy") || out.Reason != "policy_blocked" {
		t.Fatalf("expose-all re-enabled the tool: err=%v res=%+v out=%+v", err, res, out)
	}
	if mockCallCounts(t, port)["add"] != 0 {
		t.Fatal("exposed blocked tool reached the mock")
	}
}

func TestPolicyDispatch_StatusReportsApprovalAndHiddenDestructive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-destructive-tool")
	waitForPort(t, ctx, port)
	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name: "alpha", Transport: mcp.TransportHTTP,
		Endpoint: fmt.Sprintf("http://127.0.0.1:%d/mcp", port), External: true,
		Tools: []string{"echo"},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.PolicyConfig{
		BlockDestructive: true,
		Rules:            []config.PolicyRule{{Match: "alpha__echo", Action: "require_approval"}},
	}
	controller.ApplyCallGates(gw, &config.Stack{Policy: cfg}, nil, nil)
	apiServer := api.NewServer(gw, nil)
	apiServer.SetPolicyStatusFunc(func() toolpolicy.StatusReport { return policyReport(gw, cfg) })
	ts := httptest.NewServer(apiServer.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/api/policy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got toolpolicy.StatusReport
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	echo := got.Tools["alpha__echo"]
	if !got.Configured || echo.Action != "require_approval" || echo.Source != "rule" || !echo.Exposed {
		t.Fatalf("echo row = %+v report=%+v", echo, got)
	}
	purge := got.Tools["alpha__purge"]
	if purge.Action != "block" || purge.Source != "block_destructive" || purge.Exposed {
		t.Fatalf("hidden destructive row = %+v", purge)
	}
	res, out, err := gw.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "alpha__echo", Arguments: map[string]any{"message": "x"}})
	if err != nil || res == nil || !strings.Contains(res.Content[0].Text, "requires approval") || out.Reason != "policy_blocked" {
		t.Fatalf("approval denial = err=%v res=%+v out=%+v", err, res, out)
	}
}

func policyReport(gw *mcp.Gateway, cfg *config.PolicyConfig) toolpolicy.StatusReport {
	pol := toolpolicy.NewPolicy(cfg, nil)
	if pol == nil || gw == nil || gw.Router() == nil {
		return toolpolicy.EmptyStatus()
	}
	exposed := map[string]bool{}
	for _, tool := range gw.Router().AggregatedTools() {
		exposed[tool.Name] = true
	}
	var entries []toolpolicy.CatalogEntry
	for _, tool := range gw.Router().AllCatalogTools() {
		entries = append(entries, toolpolicy.CatalogEntry{
			Name:        tool.Name,
			Annotations: tool.Annotations,
			Exposed:     exposed[tool.Name],
		})
	}
	return pol.StatusWithCatalog(entries)
}
