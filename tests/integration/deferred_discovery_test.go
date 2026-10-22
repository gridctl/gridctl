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
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestDeferredDiscovery_Contract(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	alphaPort := freePort(t)
	betaPort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", alphaPort), "-protocol", "2026-07-28")
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", betaPort), "-protocol", "2026-07-28", "-list-changed")
	waitForPort(t, ctx, alphaPort)
	waitForPort(t, ctx, betaPort)

	gw := mcp.NewGateway()
	t.Cleanup(gw.Close)
	gw.SetListChangeTiming(40*time.Millisecond, time.Second)
	gw.SetDownstreamRefreshDebounce(40 * time.Millisecond)
	registerDeferredServer(t, ctx, gw, "alpha", alphaPort, []string{"echo", "add"})
	registerDeferredServer(t, ctx, gw, "beta", betaPort, nil)
	gw.SetToolDiscovery(mcp.DiscoveryDeferred, []string{"alpha__add"})
	gw.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{
		Default: "allow",
		Profiles: map[string]mcp.ClientProfileSpec{
			"narrow": {Servers: []string{"alpha"}},
		},
	}))
	gw.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{
		"dev": {
			Servers: []string{"alpha"},
			Overrides: map[string]mcp.GroupOverrideSpec{
				"alpha__add": {Name: "sum"},
			},
		},
	}))
	controller.ApplyCallGates(gw, &config.Stack{
		Policy: &config.PolicyConfig{Rules: []config.PolicyRule{{Match: "beta__get_time", Action: "block"}}},
		Limits: &config.LimitsConfig{RateLimits: []config.RateLimit{{Tool: "beta__echo", CallsPerMinute: 60, Burst: 1}}},
	}, nil, nil)

	mux := http.NewServeMux()
	stream := mcp.NewStreamableHTTPServer(gw, nil)
	mux.Handle("/mcp", stream)
	mux.HandleFunc("/groups/{name}/mcp", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !gw.HasGroup(name) {
			http.NotFound(w, r)
			return
		}
		stream.ServeHTTP(w, r.WithContext(mcp.WithGroup(r.Context(), name)))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	t.Run("list", func(t *testing.T) {
		handshake := deferredToolNames(t, handshakeInitialize(t, ts.URL+"/mcp", "tools/list"))
		stateless := deferredToolNames(t, deferredStatelessPost(t, ts.URL+"/mcp", "tools/list", nil, ""))
		want := []string{"search_tools", "call_tool", "alpha__add"}
		if strings.Join(handshake, ",") != strings.Join(want, ",") || strings.Join(stateless, ",") != strings.Join(want, ",") {
			t.Fatalf("handshake %v stateless %v", handshake, stateless)
		}
	})

	t.Run("search and call", func(t *testing.T) {
		page := searchTools(t, gw, context.Background(), map[string]any{})
		names := map[string]bool{}
		for _, tool := range page.Tools {
			names[tool.Name] = true
		}
		if names["alpha__get_time"] || !names["alpha__echo"] || !names["beta__get_time"] {
			t.Fatalf("search names = %v", names)
		}
		full, err := gw.HandleToolsListUnscoped()
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalVisible != len(full.Tools) {
			t.Fatalf("total_visible %d, full list %d", page.TotalVisible, len(full.Tools))
		}
		narrow := searchTools(t, gw, mcp.WithClientAccessID(context.Background(), "narrow"), map[string]any{})
		for _, tool := range narrow.Tools {
			if strings.HasPrefix(tool.Name, "beta__") {
				t.Fatalf("scoped client saw %s", tool.Name)
			}
		}

		before := mockCallCounts(t, alphaPort)["get_time"]
		hidden := callTool(t, gw, context.Background(), "alpha__get_time", nil)
		if !hidden.IsError || !strings.Contains(hidden.Content[0].Text, "Error: unknown tool") {
			t.Fatalf("hidden: %+v", hidden)
		}
		if mockCallCounts(t, alphaPort)["get_time"] != before {
			t.Fatal("hidden tool invoked the mock")
		}

		blocked := callTool(t, gw, context.Background(), "beta__get_time", nil)
		if !strings.HasPrefix(blocked.Content[0].Text, "Error: tool blocked by policy") {
			t.Fatalf("policy: %s", blocked.Content[0].Text)
		}
		if mockCallCounts(t, betaPort)["get_time"] != 0 {
			t.Fatal("policy denial invoked the mock")
		}
		echo := callTool(t, gw, context.Background(), "beta__echo", map[string]any{"message": "hi"})
		if echo.IsError || !strings.Contains(echo.Content[0].Text, "Echo: hi") {
			t.Fatalf("echo after policy: %+v", echo)
		}
		limited := callTool(t, gw, context.Background(), "beta__echo", map[string]any{"message": "again"})
		if !limited.IsError || !strings.Contains(strings.ToLower(limited.Content[0].Text), "rate") {
			t.Fatalf("rate limit: %+v", limited)
		}

		groupCtx := mcp.WithGroup(context.Background(), "dev")
		groupPage := searchTools(t, gw, groupCtx, map[string]any{})
		var groupNames []string
		for _, tool := range groupPage.Tools {
			groupNames = append(groupNames, tool.Name)
		}
		if !strings.Contains(strings.Join(groupNames, ","), "sum") || strings.Contains(strings.Join(groupNames, ","), "alpha__add") {
			t.Fatalf("group search = %v", groupNames)
		}
		sum := callTool(t, gw, groupCtx, "sum", map[string]any{"a": 2, "b": 3})
		if sum.IsError || !strings.Contains(sum.Content[0].Text, "5") {
			t.Fatalf("renamed call: %+v", sum)
		}
		denied := callTool(t, gw, groupCtx, "beta__echo", nil)
		if !denied.IsError || !strings.Contains(denied.Content[0].Text, "group") {
			t.Fatalf("group denial: %+v", denied)
		}
	})

	t.Run("paging", func(t *testing.T) {
		seen := map[string]int{}
		cursor := ""
		var first string
		for i := 0; i < 20; i++ {
			args := map[string]any{"limit": 1}
			if cursor != "" {
				args["cursor"] = cursor
			}
			page := searchTools(t, gw, context.Background(), args)
			if i == 0 {
				if page.NextCursor == "" {
					t.Fatal("limit 1 did not return nextCursor")
				}
				first = page.NextCursor
			}
			for _, tool := range page.Tools {
				seen[tool.Name]++
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		for name, n := range seen {
			if n != 1 {
				t.Fatalf("%s seen %d", name, n)
			}
		}
		foreign := callToolRaw(t, gw, context.Background(), map[string]any{"query": "nope", "cursor": first})
		if !foreign.IsError {
			t.Fatal("cursor from another query was accepted")
		}
		beforeCount := len(mustUnscoped(t, gw))
		mutated, err := gw.HandleToolsCall(context.Background(), mcp.ToolCallParams{
			Name:      "beta__mutate_tools",
			Arguments: map[string]any{"action": "add"},
		})
		if err != nil || mutated == nil || mutated.IsError {
			t.Fatalf("mutate_tools: %v %+v", err, mutated)
		}
		if err := gw.RefreshAllTools(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(mustUnscoped(t, gw)) <= beforeCount {
			t.Fatal("mutate_tools did not grow the catalog")
		}
		stale := callToolRaw(t, gw, context.Background(), map[string]any{"cursor": cursor, "limit": 1})
		if stale == nil || !stale.IsError || !strings.Contains(stale.Content[0].Text, "catalog or scope changed") {
			t.Fatalf("stale cursor: %+v", stale)
		}
		if fresh := searchTools(t, gw, context.Background(), map[string]any{"limit": 1}); fresh.Returned < 1 {
			t.Fatal("fresh first page failed")
		}
	})

	t.Run("overrides and instructions", func(t *testing.T) {
		deferredNames := deferredToolNames(t, handshakeInitialize(t, ts.URL+"/mcp?discovery=deferred", "tools/list"))
		fullNames := deferredToolNames(t, handshakeInitialize(t, ts.URL+"/mcp?discovery=full", "tools/list"))
		if !containsAll(deferredNames, "search_tools", "call_tool") || containsAll(fullNames, "search_tools") {
			t.Fatalf("handshake override deferred %v full %v", deferredNames, fullNames)
		}
		statelessDeferred := deferredToolNames(t, deferredStatelessPost(t, ts.URL+"/mcp?discovery=deferred", "tools/list", nil, ""))
		statelessFull := deferredToolNames(t, deferredStatelessPost(t, ts.URL+"/mcp?discovery=full", "tools/list", nil, ""))
		if !containsAll(statelessDeferred, "search_tools") || containsAll(statelessFull, "search_tools") {
			t.Fatalf("stateless override deferred %v full %v", statelessDeferred, statelessFull)
		}
		deferredInit := decodeInitialize(t, handshakeInitialize(t, ts.URL+"/mcp", ""))
		fullInit := decodeInitialize(t, handshakeInitialize(t, ts.URL+"/mcp?discovery=full", ""))
		if !strings.Contains(deferredInit, "search_tools") || strings.Contains(fullInit, "search_tools") {
			t.Fatalf("instructions deferred %q full %q", deferredInit, fullInit)
		}
		discDeferred := decodeDiscover(t, deferredStatelessPost(t, ts.URL+"/mcp?discovery=deferred", "server/discover", nil, ""))
		discStack := decodeDiscover(t, deferredStatelessPost(t, ts.URL+"/mcp", "server/discover", nil, ""))
		discFull := decodeDiscover(t, deferredStatelessPost(t, ts.URL+"/mcp?discovery=full", "server/discover", nil, ""))
		if !strings.Contains(discDeferred, "search_tools") || !strings.Contains(discStack, "search_tools") || strings.Contains(discFull, "search_tools") {
			t.Fatalf("discover deferred %q stack %q full %q", discDeferred, discStack, discFull)
		}
	})

	t.Run("bad discovery", func(t *testing.T) {
		assertDiscoveryRejected(t, ts.URL+"/mcp?discovery=lazy", "initialize", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"tester","version":"1"}}}`)
		assertDiscoveryRejected(t, ts.URL+"/mcp?discovery=lazy", "tools/list", statelessBody("tools/list"))
		assertDiscoveryRejected(t, ts.URL+"/mcp?discovery=lazy", "subscriptions/listen", statelessBody("subscriptions/listen"))
		assertDiscoveryRejected(t, ts.URL+"/groups/dev/mcp?discovery=lazy", "initialize", `{"jsonrpc":"2.0","id":9,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"tester","version":"1"}}}`)
	})

	t.Run("notifications", func(t *testing.T) {
		defHS := openHandshakeGET(t, ts.URL+"/mcp")
		fullHS := openHandshakeGET(t, ts.URL+"/mcp?discovery=full")
		deferred, defBuf := openIntegrationListen(t, ts.URL, 21, true, false, false, "notes")
		full, fullBuf := openDiscoveryListen(t, ts.URL, "full", 22)
		defer deferred.Body.Close()
		defer full.Body.Close()
		waitSSE(t, defBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)
		waitSSE(t, fullBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)

		beforeDefHS := countListChanged(defHS)
		beforeFullHS := countListChanged(fullHS)
		beforeDef := strings.Count(defBuf.String(), "notifications/tools/list_changed")
		beforeFull := strings.Count(fullBuf.String(), "notifications/tools/list_changed")
		mutateBeta(t, gw)
		if err := gw.RefreshAllTools(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitListChanged(t, fullHS, beforeFullHS+1, 3*time.Second)
		waitSSE(t, fullBuf, "notifications/tools/list_changed", beforeFull+1, 3*time.Second)
		time.Sleep(400 * time.Millisecond)
		if got := countListChanged(defHS); got != beforeDefHS {
			t.Fatalf("deferred handshake got %d tool events, want %d: %s", got, beforeDefHS, defHS.String())
		}
		if got := strings.Count(defBuf.String(), "notifications/tools/list_changed"); got != beforeDef {
			t.Fatalf("deferred subscriber got %d tool events, want %d: %s", got, beforeDef, defBuf.String())
		}
		if !strings.Contains(fullBuf.String(), `"io.modelcontextprotocol/subscriptionId":22`) {
			t.Fatalf("full listen event missing subscription id: %s", fullBuf.String())
		}

		gw.SetToolDiscovery(mcp.DiscoveryDeferred, []string{"alpha__echo"})
		waitListChanged(t, defHS, beforeDefHS+1, 3*time.Second)
		waitSSE(t, defBuf, "notifications/tools/list_changed", beforeDef+1, 3*time.Second)
		time.Sleep(400 * time.Millisecond)
		if got := countListChanged(fullHS); got != beforeFullHS+1 {
			t.Fatalf("full handshake events after pin change = %d, want %d: %s", got, beforeFullHS+1, fullHS.String())
		}
		if got := strings.Count(fullBuf.String(), "notifications/tools/list_changed"); got != beforeFull+1 {
			t.Fatalf("full subscriber events after pin change = %d, want %d: %s", got, beforeFull+1, fullBuf.String())
		}
		if got := countListChanged(defHS); got != beforeDefHS+1 {
			t.Fatalf("deferred handshake pin events = %d, want %d: %s", got, beforeDefHS+1, defHS.String())
		}
		if got := strings.Count(defBuf.String(), "notifications/tools/list_changed"); got != beforeDef+1 {
			t.Fatalf("deferred subscriber pin events = %d, want %d: %s", got, beforeDef+1, defBuf.String())
		}

		beforeDefHS = countListChanged(defHS)
		beforeFullHS = countListChanged(fullHS)
		beforeDef = strings.Count(defBuf.String(), "notifications/tools/list_changed")
		beforeFull = strings.Count(fullBuf.String(), "notifications/tools/list_changed")
		gw.SetToolDiscovery(mcp.DiscoveryFull, nil)
		waitListChanged(t, defHS, beforeDefHS+1, 3*time.Second)
		waitSSE(t, defBuf, "notifications/tools/list_changed", beforeDef+1, 3*time.Second)
		time.Sleep(400 * time.Millisecond)
		if got := countListChanged(fullHS); got != beforeFullHS {
			t.Fatalf("full handshake notified on mode flip: %d vs %d: %s", got, beforeFullHS, fullHS.String())
		}
		if got := strings.Count(fullBuf.String(), "notifications/tools/list_changed"); got != beforeFull {
			t.Fatalf("full subscriber notified on mode flip: %d vs %d: %s", got, beforeFull, fullBuf.String())
		}
		if got := countListChanged(defHS); got != beforeDefHS+1 {
			t.Fatalf("stack-default handshake mode-flip events = %d, want %d", got, beforeDefHS+1)
		}
		if got := strings.Count(defBuf.String(), "notifications/tools/list_changed"); got != beforeDef+1 {
			t.Fatalf("stack-default subscriber mode-flip events = %d, want %d", got, beforeDef+1)
		}
		gw.SetToolDiscovery(mcp.DiscoveryDeferred, []string{"alpha__add"})
	})

	t.Run("mrtr", func(t *testing.T) {
		session := handshakeSession(t, ts.URL+"/mcp")
		legacy := deferredHandshakeCall(t, ts.URL+"/mcp", session, map[string]any{
			"name": "call_tool",
			"arguments": map[string]any{
				"name":      "beta__ask_secret",
				"arguments": map[string]any{},
			},
		})
		if !strings.Contains(legacy, "MRTR") || !strings.Contains(legacy, `"isError":true`) {
			t.Fatalf("handshake MRTR: %s", legacy)
		}
		elicitation := map[string]any{
			"_meta": map[string]any{
				"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
				"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "tester", "version": "1"},
				"io.modelcontextprotocol/clientCapabilities": map[string]any{"elicitation": map[string]any{}},
			},
		}
		firstArgs := map[string]any{
			"name": "call_tool",
			"arguments": map[string]any{
				"name":      "beta__ask_secret",
				"arguments": map[string]any{},
			},
		}
		for k, v := range elicitation {
			firstArgs[k] = v
		}
		first := deferredStatelessPost(t, ts.URL+"/mcp", "tools/call", firstArgs, "")
		var opened struct {
			Result struct {
				ResultType   string `json:"resultType"`
				RequestState string `json:"requestState"`
			} `json:"result"`
		}
		if err := json.Unmarshal(first, &opened); err != nil {
			t.Fatal(err)
		}
		if opened.Result.ResultType != "input_required" || opened.Result.RequestState == "" {
			t.Fatalf("stateless MRTR: %s", first)
		}
		retryArgs := map[string]any{
			"name": "call_tool",
			"arguments": map[string]any{
				"name":      "beta__ask_secret",
				"arguments": map[string]any{},
			},
			"requestState":   opened.Result.RequestState,
			"inputResponses": map[string]any{"secret_word": map[string]any{"action": "accept", "content": map[string]any{"word": "swordfish"}}},
		}
		for k, v := range elicitation {
			retryArgs[k] = v
		}
		retry := deferredStatelessPost(t, ts.URL+"/mcp", "tools/call", retryArgs, "")
		if !strings.Contains(string(retry), "secret accepted") {
			t.Fatalf("call_tool retry: %s", retry)
		}
		directOpenArgs := map[string]any{
			"name":      "beta__ask_secret",
			"arguments": map[string]any{},
		}
		for k, v := range elicitation {
			directOpenArgs[k] = v
		}
		directOpen := deferredStatelessPost(t, ts.URL+"/mcp", "tools/call", directOpenArgs, "")
		var directState struct {
			Result struct {
				RequestState string `json:"requestState"`
			} `json:"result"`
		}
		if err := json.Unmarshal(directOpen, &directState); err != nil {
			t.Fatal(err)
		}
		directArgs := map[string]any{
			"name":           "beta__ask_secret",
			"arguments":      map[string]any{},
			"requestState":   directState.Result.RequestState,
			"inputResponses": map[string]any{"secret_word": map[string]any{"action": "accept", "content": map[string]any{"word": "swordfish"}}},
		}
		for k, v := range elicitation {
			directArgs[k] = v
		}
		direct := deferredStatelessPost(t, ts.URL+"/mcp", "tools/call", directArgs, "")
		if !strings.Contains(string(direct), "secret accepted") {
			t.Fatalf("direct retry: %s", direct)
		}
		mismatch := deferredStatelessPost(t, ts.URL+"/mcp", "tools/call", map[string]any{
			"name": "call_tool",
			"arguments": map[string]any{
				"name":      "alpha__echo",
				"arguments": map[string]any{"message": "x"},
			},
			"requestState":   opened.Result.RequestState,
			"inputResponses": map[string]any{"secret_word": map[string]any{"action": "accept"}},
		}, "")
		if !strings.Contains(string(mismatch), "re-issue the original call") {
			t.Fatalf("mismatched retry: %s", mismatch)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		gw.SetToolDiscovery(mcp.DiscoveryDeferred, []string{"alpha__add"})
		before := len(mustUnscoped(t, gw))
		for i := 0; i < 8; i++ {
			mutateBeta(t, gw)
		}
		waitStableUnscoped(t, gw, before+8)
		fullRaw := handshakeInitialize(t, ts.URL+"/mcp?discovery=full", "tools/list")
		defRaw := handshakeInitialize(t, ts.URL+"/mcp", "tools/list")
		t.Logf("tools/list bytes full=%d deferred=%d tools=%d", len(fullRaw), len(defRaw), len(mustUnscoped(t, gw)))
		if len(defRaw) >= len(fullRaw) {
			t.Fatalf("deferred list is not smaller: full %d deferred %d", len(fullRaw), len(defRaw))
		}
	})

	t.Run("code mode notifications", func(t *testing.T) {
		gw.SetToolDiscovery(mcp.DiscoveryDeferred, []string{"alpha__add"})
		gw.SetCodeMode(time.Second)
		time.Sleep(200 * time.Millisecond)

		_, listed, defHS := openHandshakeListAndGET(t, ts.URL+"/mcp")
		if !strings.Contains(string(listed), "search_tools") || !strings.Contains(string(listed), "execute") {
			t.Fatalf("deferred tools/list before catalog change: %s", listed)
		}
		fullHS := openHandshakeGET(t, ts.URL+"/mcp?discovery=full")
		deferred, defBuf := openIntegrationListen(t, ts.URL, 23, true, false, false, "code-notes")
		full, fullBuf := openDiscoveryListen(t, ts.URL, "full", 24)
		defer deferred.Body.Close()
		defer full.Body.Close()
		waitSSE(t, defBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)
		waitSSE(t, fullBuf, "notifications/subscriptions/acknowledged", 1, 3*time.Second)

		beforeDefHS := countListChanged(defHS)
		beforeFullHS := countListChanged(fullHS)
		beforeDef := strings.Count(defBuf.String(), "notifications/tools/list_changed")
		beforeFull := strings.Count(fullBuf.String(), "notifications/tools/list_changed")
		mutateBeta(t, gw)
		if err := gw.RefreshAllTools(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1200 * time.Millisecond)
		if got := countListChanged(defHS); got != beforeDefHS {
			t.Fatalf("catalog-only refresh notified deferred handshake: %s", defHS.String())
		}
		if got := countListChanged(fullHS); got != beforeFullHS {
			t.Fatalf("catalog-only refresh notified full handshake under code mode: %s", fullHS.String())
		}
		if got := strings.Count(defBuf.String(), "notifications/tools/list_changed"); got != beforeDef {
			t.Fatalf("catalog-only refresh notified deferred subscriber: %s", defBuf.String())
		}
		if got := strings.Count(fullBuf.String(), "notifications/tools/list_changed"); got != beforeFull {
			t.Fatalf("catalog-only refresh notified full subscriber under code mode: %s", fullBuf.String())
		}

		gw.SetToolDiscovery(mcp.DiscoveryDeferred, []string{"alpha__echo"})
		waitListChanged(t, defHS, beforeDefHS+1, 3*time.Second)
		waitSSE(t, defBuf, "notifications/tools/list_changed", beforeDef+1, 3*time.Second)
		time.Sleep(400 * time.Millisecond)
		if got := countListChanged(fullHS); got != beforeFullHS {
			t.Fatalf("full handshake under code mode notified on pin change: %s", fullHS.String())
		}
		if got := strings.Count(fullBuf.String(), "notifications/tools/list_changed"); got != beforeFull {
			t.Fatalf("full subscriber under code mode notified on pin change: %s", fullBuf.String())
		}
		if got := countListChanged(defHS); got != beforeDefHS+1 {
			t.Fatalf("deferred handshake pin events = %d, want %d: %s", got, beforeDefHS+1, defHS.String())
		}
		if got := strings.Count(defBuf.String(), "notifications/tools/list_changed"); got != beforeDef+1 {
			t.Fatalf("deferred subscriber pin events = %d, want %d: %s", got, beforeDef+1, defBuf.String())
		}
	})
}

func mutateBeta(t *testing.T, gw *mcp.Gateway) {
	t.Helper()
	mutated, err := gw.HandleToolsCall(context.Background(), mcp.ToolCallParams{
		Name:      "beta__mutate_tools",
		Arguments: map[string]any{"action": "add"},
	})
	if err != nil || mutated == nil || mutated.IsError {
		t.Fatalf("mutate_tools: %v %+v", err, mutated)
	}
}

func countListChanged(buf *lockedBuffer) int {
	return strings.Count(buf.String(), listChangedPayload)
}

func waitStableUnscoped(t *testing.T, gw *mcp.Gateway, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last int
	var stableSince time.Time
	for {
		if err := gw.RefreshAllTools(context.Background()); err != nil {
			t.Fatal(err)
		}
		n := len(mustUnscoped(t, gw))
		now := time.Now()
		if n != last {
			last = n
			stableSince = now
		}
		if n == want && now.Sub(stableSince) >= 400*time.Millisecond {
			return
		}
		if now.After(deadline) {
			t.Fatalf("catalog count %d, want %d", last, want)
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func openHandshakeGET(t *testing.T, url string) *lockedBuffer {
	t.Helper()
	session := handshakeSession(t, url)
	return openSessionGET(t, strings.Split(url, "?")[0], session)
}

func openHandshakeListAndGET(t *testing.T, url string) (string, []byte, *lockedBuffer) {
	t.Helper()
	session := handshakeSession(t, url)
	base := strings.Split(url, "?")[0]
	req, err := http.NewRequest(http.MethodPost, base, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list %d: %s", resp.StatusCode, raw)
	}
	return session, raw, openSessionGET(t, base, session)
}

func openSessionGET(t *testing.T, url, session string) *lockedBuffer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, raw)
	}
	buf := &lockedBuffer{}
	go func() { _, _ = io.Copy(buf, resp.Body) }()
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})
	time.Sleep(30 * time.Millisecond)
	return buf
}

func mustUnscoped(t *testing.T, gw *mcp.Gateway) []mcp.Tool {
	t.Helper()
	listed, err := gw.HandleToolsListUnscoped()
	if err != nil {
		t.Fatal(err)
	}
	return listed.Tools
}

func registerDeferredServer(t *testing.T, ctx context.Context, gw *mcp.Gateway, name string, port int, tools []string) {
	t.Helper()
	cfg := mcp.MCPServerConfig{
		Name:               name,
		Transport:          mcp.TransportHTTP,
		Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:           true,
		ProtocolGeneration: mcp.GenerationStateless,
		ReadyTimeout:       15 * time.Second,
		Tools:              tools,
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatal(err)
	}
}

type searchPage struct {
	Tools        []mcp.Tool `json:"tools"`
	TotalVisible int        `json:"total_visible"`
	Matched      int        `json:"matched"`
	Returned     int        `json:"returned"`
	NextCursor   string     `json:"nextCursor"`
}

func searchTools(t *testing.T, gw *mcp.Gateway, ctx context.Context, args map[string]any) searchPage {
	t.Helper()
	res := callToolRaw(t, gw, ctx, args)
	if res == nil || res.IsError {
		t.Fatalf("search_tools: %+v", res)
	}
	var page searchPage
	if err := json.Unmarshal(res.StructuredContent, &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func callTool(t *testing.T, gw *mcp.Gateway, ctx context.Context, name string, args map[string]any) *mcp.ToolCallResult {
	t.Helper()
	res, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name: mcp.MetaToolCallTool,
		Arguments: map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func callToolRaw(t *testing.T, gw *mcp.Gateway, ctx context.Context, args map[string]any) *mcp.ToolCallResult {
	t.Helper()
	res, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{Name: mcp.MetaToolSearchTools, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func openDiscoveryListen(t *testing.T, base, discovery string, id any) (*http.Response, *lockedBuffer) {
	t.Helper()
	params := map[string]any{
		"notifications": map[string]any{"toolsListChanged": true},
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
			"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "listen-client", "version": "1"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "subscriptions/listen", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/mcp?discovery="+discovery, bytes.NewReader(body))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("listen %d: %s", resp.StatusCode, raw)
	}
	buf := &lockedBuffer{}
	go func() { _, _ = io.Copy(buf, resp.Body) }()
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})
	return resp, buf
}

func handshakeSession(t *testing.T, url string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"tester","version":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize %d", resp.StatusCode)
	}
	session := resp.Header.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("missing session")
	}
	return session
}

func deferredHandshakeCall(t *testing.T, url, session string, params map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return string(raw)
}

func handshakeInitialize(t *testing.T, url, then string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"tester","version":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize %s: %d %s", url, resp.StatusCode, raw)
	}
	if then == "" {
		return raw
	}
	session := resp.Header.Get("Mcp-Session-Id")
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	follow, err := http.NewRequest(http.MethodPost, strings.Split(url, "?")[0], strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	follow.Header.Set("Content-Type", "application/json")
	follow.Header.Set("Mcp-Session-Id", session)
	listed, err := http.DefaultClient.Do(follow)
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Body.Close()
	out, _ := io.ReadAll(listed.Body)
	return out
}

func statelessBody(method string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"tester","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`, method)
}

func deferredStatelessPost(t *testing.T, url, method string, params map[string]any, _ string) []byte {
	t.Helper()
	body := statelessBody(method)
	if params != nil {
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatal(err)
		}
		p := payload["params"].(map[string]any)
		for k, v := range params {
			p[k] = v
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = string(raw)
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		if name, _ := params["name"].(string); name != "" {
			req.Header.Set("Mcp-Name", name)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, url, resp.StatusCode, raw)
	}
	return raw
}

func deferredToolNames(t *testing.T, raw []byte) []string {
	t.Helper()
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode tools: %v %s", err, raw)
	}
	var names []string
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func decodeInitialize(t *testing.T, raw []byte) string {
	t.Helper()
	var resp struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Result.Instructions
}

func decodeDiscover(t *testing.T, raw []byte) string {
	t.Helper()
	var resp struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Result.Instructions
}

func assertDiscoveryRejected(t *testing.T, url, method, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if method != "initialize" {
		req.Header.Set("MCP-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", method)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("%s status %d body %s", url, resp.StatusCode, raw)
	}
	var decoded struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Error.Code != -32602 || decoded.Error.Message != `unsupported discovery mode "lazy" (accepted: deferred, full)` {
		t.Fatalf("%s error %+v body %s", url, decoded.Error, raw)
	}
}

func containsAll(names []string, want ...string) bool {
	got := map[string]bool{}
	for _, name := range names {
		got[name] = true
	}
	for _, name := range want {
		if !got[name] {
			return false
		}
	}
	return true
}
