//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/state"
)

func buildResourcePrompt(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "resourceprompt")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "./tests/integration/fixtures/resourceprompt")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, out)
	}
	return bin
}

func registerProcess(t *testing.T, ctx context.Context, gw *mcp.Gateway, name, bin, generation string, args ...string) {
	t.Helper()
	cfg := mcp.MCPServerConfig{
		Name:               name,
		LocalProcess:       true,
		Command:            append([]string{bin}, args...),
		ProtocolGeneration: generation,
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func TestResourceAggregation_BothEras(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	registerProcess(t, ctx, gw, "docs", bin, "handshake", "-prompts", "-resources")
	registerProcess(t, ctx, gw, "bare", bin, "handshake")

	handler := mcp.NewStreamableHTTPServer(gw, nil)
	session := initializeAggregation(t, handler)
	listed := handshakeCall(t, handler, session, "prompts/list", nil)
	prompts := listed["prompts"].([]any)
	var sawReview, sawBare bool
	for _, item := range prompts {
		name, _ := item.(map[string]any)["name"].(string)
		if name == "docs__review" {
			sawReview = true
		}
		if strings.HasPrefix(name, "bare__") {
			sawBare = true
		}
	}
	if !sawReview || sawBare {
		t.Fatalf("prompts = %#v", prompts)
	}
	got := handshakeCall(t, handler, session, "prompts/get", map[string]any{
		"name":      "docs__brief",
		"arguments": map[string]string{"topic": "maps"},
	})
	raw, _ := json.Marshal(got)
	if !bytes.Contains(raw, []byte(`"blob":"AAEC"`)) || !bytes.Contains(raw, []byte("topic=maps")) {
		t.Fatalf("prompt get lost content: %s", raw)
	}

	resources := handshakeCall(t, handler, session, "resources/list", nil)
	if _, ok := resources["nextCursor"]; ok {
		t.Fatal("resources/list emitted nextCursor")
	}
	resRaw, _ := json.Marshal(resources)
	if !bytes.Contains(resRaw, []byte("file:///readme")) {
		t.Fatalf("resources = %s", resRaw)
	}
	templates := handshakeCall(t, handler, session, "resources/templates/list", nil)
	tmplRaw, _ := json.Marshal(templates)
	if !bytes.Contains(tmplRaw, []byte("file:///tmpl/{name}")) {
		t.Fatalf("templates = %s", tmplRaw)
	}
	blob := handshakeCall(t, handler, session, "resources/read", map[string]any{"uri": "file:///blob"})
	blobRaw, _ := json.Marshal(blob)
	if !bytes.Contains(blobRaw, []byte(`"blob":"AAEC"`)) || !bytes.Contains(blobRaw, []byte("application/octet-stream")) {
		t.Fatalf("blob read = %s", blobRaw)
	}
	templated := handshakeCall(t, handler, session, "resources/read", map[string]any{"uri": "file:///tmpl/a"})
	if !strings.Contains(string(mustJSON(templated)), "template") {
		t.Fatalf("template read = %#v", templated)
	}

	w, resp := aggregationStateless(t, handler, "resources/read", map[string]any{"uri": "file:///missing"})
	errObj, _ := resp["error"].(map[string]any)
	if w.Code != http.StatusOK || errObj["code"] != float64(-32602) {
		t.Fatalf("stateless not-found = %d %#v", w.Code, resp["error"])
	}

	discoverW, discover := aggregationStateless(t, handler, "server/discover", nil)
	if discoverW.Code != http.StatusOK {
		t.Fatalf("discover: %d %s", discoverW.Code, discoverW.Body.String())
	}
	discCaps := discover["result"].(map[string]any)["capabilities"].(map[string]any)
	if discCaps["prompts"] == nil || discCaps["resources"] == nil {
		t.Fatalf("capabilities = %#v", discCaps)
	}
	if res, _ := discCaps["resources"].(map[string]any); res["subscribe"] == true || res["listChanged"] == true {
		t.Fatalf("advertised subscribe/listChanged: %#v", res)
	}
}

func TestResourceAggregation_ScopeCollisionAndStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	registerProcess(t, ctx, gw, "a", bin, "handshake", "-prompts", "-resources", "-shared")
	registerProcess(t, ctx, gw, "b", bin, "handshake", "-prompts", "-resources", "-shared", "-extra")
	registerProcess(t, ctx, gw, "c", bin, "handshake", "-resources", "-fail-resources")
	gw.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{
		Default:  "deny",
		Profiles: map[string]mcp.ClientProfileSpec{"only-a": {Servers: []string{"a"}}},
	}))
	gw.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{
		"empty": {Tools: []string{"a__echo"}, Exclude: []string{"a__echo"}},
	}))

	handler := mcp.NewStreamableHTTPServer(gw, nil)
	session := initializeAggregationAs(t, handler, "only-a", "")
	listed := handshakeCall(t, handler, session, "resources/list", nil)
	raw := string(mustJSON(listed))
	if strings.Count(raw, "file:///shared") != 1 || strings.Contains(raw, "file:///extra") {
		t.Fatalf("scoped list = %s", raw)
	}
	denied := handshakePost(t, handler, session, "resources/read", map[string]any{"uri": "file:///extra"})
	if denied.Error == nil || denied.Error.Code != -32002 {
		t.Fatalf("out-of-scope read = %+v", denied.Error)
	}
	var cStatus mcp.MCPServerStatus
	for _, status := range gw.Status() {
		if status.Name == "b" && status.ResourceCollisions < 1 {
			t.Fatalf("b collisions = %d", status.ResourceCollisions)
		}
		if status.Name == "c" {
			cStatus = status
		}
	}
	if cStatus.ResourceListError == "" {
		t.Fatalf("expected list error on c, got %+v", gw.Status())
	}

	gw.UnregisterMCPServer("a")
	session = initializeAggregation(t, handler)
	after := handshakeCall(t, handler, session, "prompts/list", nil)
	if strings.Contains(string(mustJSON(after)), "a__") {
		t.Fatalf("removed server still listed: %s", mustJSON(after))
	}
}

func initializeAggregation(t *testing.T, handler http.Handler) string {
	t.Helper()
	return initializeAggregationAs(t, handler, "", "")
}

func initializeAggregationAs(t *testing.T, handler http.Handler, client, group string) string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"agg","version":"1"},"capabilities":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	if group != "" {
		req = req.WithContext(mcp.WithGroup(req.Context(), group))
	}
	req.Host = "localhost:8180"
	if client != "" {
		req.Header.Set("X-Gridctl-Client-Id", client)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initialize: %d %s", w.Code, w.Body.String())
	}
	if id := w.Header().Get("Mcp-Session-Id"); id != "" {
		return id
	}
	t.Fatal("missing session")
	return ""
}

func handshakeCall(t *testing.T, handler http.Handler, session, method string, params any) map[string]any {
	t.Helper()
	resp := handshakePost(t, handler, session, method, params)
	if resp.Error != nil {
		t.Fatalf("%s: %s", method, resp.Error.Message)
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func handshakePost(t *testing.T, handler http.Handler, session, method string, params any) struct {
	Error *struct {
		Code    int
		Message string
	}
	Result json.RawMessage
} {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": 2, "method": method}
	if params != nil {
		payload["params"] = params
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Host = "localhost:8180"
	req.Header.Set("Mcp-Session-Id", session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s decode: %v %s", method, err, w.Body.String())
	}
	return struct {
		Error *struct {
			Code    int
			Message string
		}
		Result json.RawMessage
	}{Error: func() *struct {
		Code    int
		Message string
	} { if resp.Error == nil {
		return nil
	}; return &struct {
		Code    int
		Message string
	}{resp.Error.Code, resp.Error.Message} }(), Result: resp.Result}
}

func aggregationStateless(t *testing.T, handler http.Handler, method string, params map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "agg", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Host = "localhost:8180"
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if method == "resources/read" {
		if uri, ok := params["uri"].(string); ok {
			req.Header.Set("Mcp-Name", uri)
		}
	}
	if method == "prompts/get" {
		if name, ok := params["name"].(string); ok {
			req.Header.Set("Mcp-Name", name)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestResourceAggregation_UIExtension(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name: "app", LocalProcess: true, Command: []string{bin, "-ui", "-resources"},
		ProtocolGeneration: "handshake", ProtocolExtensions: []string{"io.modelcontextprotocol/ui"},
	}); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, tool := range gw.Router().CatalogTools() {
		if strings.HasSuffix(tool.Name, "__draw") {
			saw = true
		}
	}
	if !saw {
		t.Fatal("UI tool was not registered")
	}
	handler := mcp.NewStreamableHTTPServer(gw, nil)
	session := initializeAggregation(t, handler)
	read := handshakeCall(t, handler, session, "resources/read", map[string]any{"uri": "ui://app/view"})
	raw := string(mustJSON(read))
	if !strings.Contains(raw, "text/html;profile=mcp-app") {
		t.Fatalf("ui read = %s", raw)
	}
	listed := string(mustJSON(handshakeCall(t, handler, session, "resources/list", nil)))
	if strings.Contains(listed, "ui://app/view") {
		t.Fatalf("ui resource should be omitted from the list: %s", listed)
	}
}

func TestResourceAggregation_StatusJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	registerProcess(t, ctx, gw, "docs", bin, "handshake", "-prompts", "-resources")
	handler := mcp.NewStreamableHTTPServer(gw, nil)
	session := initializeAggregation(t, handler)
	handshakeCall(t, handler, session, "prompts/list", nil)
	handshakeCall(t, handler, session, "resources/list", nil)

	apiServer := api.NewServer(gw, nil)
	ln := httptest.NewServer(apiServer.Handler())
	defer ln.Close()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gridctl", "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.HomeEnv, home)
	if err := state.Save(&state.DaemonState{StackName: "agg", PID: os.Getpid(), Port: mustPort(ln.URL), Home: home}); err != nil {
		t.Fatal(err)
	}
	gridctl := filepath.Join(t.TempDir(), "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-o", gridctl, "./cmd/gridctl")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gridctl: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(ctx, gridctl, "status", "--json")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	if !bytes.Contains(out, []byte(`"promptCount"`)) || !bytes.Contains(out, []byte(`"mcpResourceCount"`)) {
		t.Fatalf("status json missing counts: %s", out)
	}
}

func TestResourceAggregation_StatelessFields(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	bin := buildResourcePrompt(t)

	t.Run("stateless downstream", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		gw := mcp.NewGateway()
		defer gw.Close()
		registerProcess(t, ctx, gw, "docs", bin, "stateless", "-protocol", "2026-07-28", "-cache-ttl", "30000", "-cache-scope", "public", "-prompts", "-resources")
		registerProcess(t, ctx, gw, "bare", bin, "handshake")
		handler := mcp.NewStreamableHTTPServer(gw, nil)

		listed := statelessResult(t, handler, "prompts/list", nil)
		assertStatelessCache(t, listed, 30000, "public", "complete")
		if !strings.Contains(string(mustJSON(listed)), "docs__review") || strings.Contains(string(mustJSON(listed)), "bare__") {
			t.Fatalf("prompts = %s", mustJSON(listed))
		}
		if _, ok := listed["nextCursor"]; ok {
			t.Fatal("prompts/list emitted nextCursor")
		}
		got := statelessResult(t, handler, "prompts/get", map[string]any{
			"name":      "docs__brief",
			"arguments": map[string]string{"topic": "maps"},
		})
		assertStatelessCache(t, got, 30000, "public", "complete")
		if !bytes.Contains(mustJSON(got), []byte("topic=maps")) {
			t.Fatalf("prompt get = %s", mustJSON(got))
		}

		resources := statelessResult(t, handler, "resources/list", nil)
		assertStatelessCache(t, resources, 30000, "public", "complete")
		if _, ok := resources["nextCursor"]; ok {
			t.Fatal("resources/list emitted nextCursor")
		}
		templates := statelessResult(t, handler, "resources/templates/list", nil)
		assertStatelessCache(t, templates, 30000, "public", "complete")
		if !strings.Contains(string(mustJSON(templates)), "file:///tmpl/{name}") {
			t.Fatalf("templates = %s", mustJSON(templates))
		}
		read := statelessResult(t, handler, "resources/read", map[string]any{"uri": "file:///readme"})
		assertStatelessCache(t, read, 30000, "public", "complete")
		if !strings.Contains(string(mustJSON(read)), "hello") {
			t.Fatalf("read = %s", mustJSON(read))
		}
	})

	t.Run("handshake downstream", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		gw := mcp.NewGateway()
		defer gw.Close()
		registerProcess(t, ctx, gw, "old", bin, "handshake", "-prompts", "-resources")
		handler := mcp.NewStreamableHTTPServer(gw, nil)
		read := statelessResult(t, handler, "resources/read", map[string]any{"uri": "file:///readme"})
		assertStatelessCache(t, read, 0, "private", "complete")
		if !strings.Contains(string(mustJSON(read)), "hello") {
			t.Fatalf("read = %s", mustJSON(read))
		}
		listed := statelessResult(t, handler, "prompts/list", nil)
		assertStatelessCache(t, listed, 0, "private", "complete")
		if !strings.Contains(string(mustJSON(listed)), "old__review") {
			t.Fatalf("prompts = %s", mustJSON(listed))
		}
	})
}

func TestResourceAggregation_ReadErrorPassthrough(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	registerProcess(t, ctx, gw, "docs", bin, "handshake", "-resources", "-read-error", "-32001")
	handler := mcp.NewStreamableHTTPServer(gw, nil)
	session := initializeAggregation(t, handler)
	handshakeCall(t, handler, session, "resources/list", nil)
	denied := handshakePost(t, handler, session, "resources/read", map[string]any{"uri": "file:///error"})
	if denied.Error == nil || denied.Error.Code != -32001 {
		t.Fatalf("handshake read error = %+v", denied.Error)
	}
	_, resp := aggregationStateless(t, handler, "resources/read", map[string]any{"uri": "file:///error"})
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != float64(-32001) {
		t.Fatalf("stateless read error = %#v", resp["error"])
	}
}

func TestResourceAggregation_GroupMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	registerProcess(t, ctx, gw, "a", bin, "handshake", "-prompts", "-resources")
	registerProcess(t, ctx, gw, "b", bin, "handshake", "-prompts", "-resources", "-extra")
	gw.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{
		"empty": {Tools: []string{"a__echo"}, Exclude: []string{"a__echo"}},
		"some":  {Servers: []string{"a"}, Exclude: []string{"a__other"}},
	}))
	handler := mcp.NewStreamableHTTPServer(gw, nil)

	hidden := initializeAggregationAs(t, handler, "", "empty")
	hiddenResources := string(mustJSON(handshakeCall(t, handler, hidden, "resources/list", nil)))
	hiddenPrompts := string(mustJSON(handshakeCall(t, handler, hidden, "prompts/list", nil)))
	if strings.Contains(hiddenResources, "file:///") || strings.Contains(hiddenPrompts, "a__") || strings.Contains(hiddenPrompts, "b__") {
		t.Fatalf("empty group leaked resources=%s prompts=%s", hiddenResources, hiddenPrompts)
	}
	hiddenRead := handshakePost(t, handler, hidden, "resources/read", map[string]any{"uri": "file:///readme"})
	if hiddenRead.Error == nil || hiddenRead.Error.Code != -32002 {
		t.Fatalf("hidden read = %+v", hiddenRead.Error)
	}

	visible := initializeAggregationAs(t, handler, "", "some")
	visibleResources := string(mustJSON(handshakeCall(t, handler, visible, "resources/list", nil)))
	visiblePrompts := string(mustJSON(handshakeCall(t, handler, visible, "prompts/list", nil)))
	if !strings.Contains(visibleResources, "file:///readme") || strings.Contains(visibleResources, "file:///extra") {
		t.Fatalf("some group resources = %s", visibleResources)
	}
	if !strings.Contains(visiblePrompts, "a__review") || strings.Contains(visiblePrompts, "b__") {
		t.Fatalf("some group prompts = %s", visiblePrompts)
	}
	extra := handshakePost(t, handler, visible, "resources/read", map[string]any{"uri": "file:///extra"})
	if extra.Error == nil || extra.Error.Code != -32002 {
		t.Fatalf("hidden server read = %+v", extra.Error)
	}
	if !strings.Contains(string(mustJSON(handshakeCall(t, handler, visible, "resources/read", map[string]any{"uri": "file:///readme"}))), "hello") {
		t.Fatal("visible read failed")
	}
}

func TestResourceAggregation_ListErrorClears(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	registerProcess(t, ctx, gw, "flaky", bin, "handshake", "-resources", "-fail-resource-lists", "1")
	waitForListError(t, gw, "flaky")
	handler := mcp.NewStreamableHTTPServer(gw, nil)
	session := initializeAggregation(t, handler)
	listed := string(mustJSON(handshakeCall(t, handler, session, "resources/list", nil)))
	if !strings.Contains(listed, "file:///readme") {
		t.Fatalf("recovered list = %s", listed)
	}
	for _, status := range gw.Status() {
		if status.Name == "flaky" && status.ResourceListError != "" {
			t.Fatalf("list error stuck at %q", status.ResourceListError)
		}
	}
}

func TestResourceAggregation_StatelessUIExtension(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bin := buildResourcePrompt(t)
	gw := mcp.NewGateway()
	defer gw.Close()
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name: "app", LocalProcess: true,
		Command:            []string{bin, "-protocol", "2026-07-28", "-ui", "-resources"},
		ProtocolGeneration: "stateless", ProtocolExtensions: []string{"io.modelcontextprotocol/ui"},
	}); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, tool := range gw.Router().CatalogTools() {
		if strings.HasSuffix(tool.Name, "__draw") {
			saw = true
		}
	}
	if !saw {
		t.Fatal("stateless UI tool was not registered")
	}
}

func statelessResult(t *testing.T, handler http.Handler, method string, params map[string]any) map[string]any {
	t.Helper()
	w, resp := aggregationStateless(t, handler, method, params)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
	}
	if errObj, ok := resp["error"]; ok && errObj != nil {
		t.Fatalf("%s error: %#v", method, errObj)
	}
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("%s result = %#v", method, resp)
	}
	return result
}

func assertStatelessCache(t *testing.T, result map[string]any, ttl float64, scope, resultType string) {
	t.Helper()
	if result["resultType"] != resultType || result["cacheScope"] != scope || result["ttlMs"] != ttl {
		t.Fatalf("cache fields = resultType:%#v ttlMs:%#v cacheScope:%#v, want %s %v %s", result["resultType"], result["ttlMs"], result["cacheScope"], resultType, ttl, scope)
	}
}

func waitForListError(t *testing.T, gw *mcp.Gateway, name string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, status := range gw.Status() {
			if status.Name == name && status.ResourceListError == "rpc" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("resourceListError was not set on %s: %+v", name, gw.Status())
}

func mustPort(rawURL string) int {
	// httptest.Server.URL is http://127.0.0.1:port
	i := strings.LastIndex(rawURL, ":")
	n := 0
	for _, c := range rawURL[i+1:] {
		n = n*10 + int(c-'0')
	}
	return n
}
