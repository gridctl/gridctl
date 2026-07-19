//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/mcpauth"
)

// oauthDebug mirrors the mock server's /debug/oauth introspection payload.
type oauthDebug struct {
	TokensIssued int      `json:"tokens_issued"`
	Refreshes    int      `json:"refreshes"`
	Revoked      []string `json:"revoked"`
}

func fetchOAuthDebug(t *testing.T, base string) oauthDebug {
	t.Helper()
	resp, err := http.Get(base + "/debug/oauth")
	if err != nil {
		t.Fatalf("fetching oauth debug state: %v", err)
	}
	defer resp.Body.Close()
	var d oauthDebug
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatalf("decoding oauth debug state: %v", err)
	}
	return d
}

// startCallbackServer serves the broker's OAuth callback on a local port,
// standing in for the gateway's HTTP listener.
func startCallbackServer(t *testing.T, broker *mcpauth.Broker, port int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(mcpauth.CallbackPath, broker.CallbackHandler())
	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: mux}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Close() })
}

// TestOAuthBrokerFullFlow drives the complete downstream OAuth lifecycle
// against a real OAuth-protected mock MCP server: needs-auth registration,
// headless authorization (an HTTP client plays the browser by following
// the auto-approving AS redirect to the broker callback), re-registration,
// authenticated tool calls, refresh rotation, restart persistence, and
// logout revocation.
func TestOAuthBrokerFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	// A 5s access TTL sits inside the broker's 30s expiry skew, so every
	// use of the grant forces a refresh: rotation gets exercised without
	// sleeping in the test.
	startMockServer(t, mockHTTPServerBin,
		"-port", fmt.Sprintf("%d", port), "-oauth", "-oauth-access-ttl", "5")
	waitForPort(t, ctx, port)

	storeDir := t.TempDir()
	store, err := mcpauth.NewTokenStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	cbPort := freePort(t)
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d%s", cbPort, mcpauth.CallbackPath)
	broker := mcpauth.NewBroker(store, redirectURL, nil)
	startCallbackServer(t, broker, cbPort)

	gw := mcp.NewGateway()
	broker.SetStateSink(gw)
	registrar := controller.NewServerRegistrar(gw, false)
	registrar.SetAuthBroker(broker)

	serverCfg := config.MCPServer{
		Name: "protected",
		URL:  base + "/mcp",
		Auth: &config.ServerAuth{Type: "oauth"},
	}

	// Phase 1: registration before authorization lands in needs-auth, not
	// in the registration-failure (error) bucket, and does not block.
	if err := registrar.RegisterOne(ctx, serverCfg, nil, ""); err == nil {
		t.Fatal("expected registration to fail before authorization")
	}
	st, ok := gw.ServerAuthState("protected")
	if !ok || st.Status != mcp.AuthStatusNeedsAuth {
		t.Fatalf("expected needs_auth state, got %+v (ok=%v)", st, ok)
	}
	for _, row := range gw.Status() {
		if row.Name == "protected" {
			if row.RegistrationFailed {
				t.Fatal("needs-auth server must not report RegistrationFailed")
			}
			if row.AuthStatus != mcp.AuthStatusNeedsAuth {
				t.Fatalf("status row AuthStatus = %q", row.AuthStatus)
			}
		}
	}

	// Phase 2: headless authorization. The default HTTP client follows the
	// AS's 302 to the broker callback, completing the code exchange.
	authorizeURL, stateToken, err := broker.BeginAuthorization(ctx, "protected", time.Minute)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	resp, err := http.Get(authorizeURL)
	if err != nil {
		t.Fatalf("driving authorization redirect: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback returned %d", resp.StatusCode)
	}
	if err := broker.Wait(ctx, stateToken); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if st, _ := gw.ServerAuthState("protected"); st.Status != mcp.AuthStatusAuthorized {
		t.Fatalf("expected authorized state, got %+v", st)
	}

	// Phase 3: registration now succeeds and tool calls carry the Bearer
	// token end to end.
	if err := registrar.RegisterOne(ctx, serverCfg, nil, ""); err != nil {
		t.Fatalf("re-registration after authorization: %v", err)
	}
	client := mcp.NewClient("protected", base+"/mcp")
	client.SetHeaderSource(broker.HeaderSource("protected"))
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize with brokered token: %v", err)
	}
	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "brokered"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "brokered") {
		t.Fatalf("unexpected tool result: %+v", result)
	}

	// Phase 4: the short TTL forced refreshes; the rotated refresh token
	// must be persisted (single-use rotation dies otherwise).
	dbg := fetchOAuthDebug(t, base)
	if dbg.Refreshes == 0 {
		t.Fatal("expected at least one refresh with a 5s access TTL")
	}
	resource := strings.ToLower(base) + "/mcp"
	grant, found, err := store.Grant(resource)
	if err != nil || !found {
		t.Fatalf("grant missing after refresh: %v", err)
	}
	if grant.Token.RefreshToken == "" || !strings.HasPrefix(grant.Token.RefreshToken, "refresh-") {
		t.Fatalf("rotated refresh token not persisted: %q", grant.Token.RefreshToken)
	}

	// Phase 5: a fresh store + broker over the same directory (daemon
	// restart) still sees the authorization without any passphrase.
	store2, err := mcpauth.NewTokenStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	broker2 := mcpauth.NewBroker(store2, redirectURL, nil)
	if err := broker2.Configure("protected", base+"/mcp", nil); err != nil {
		t.Fatal(err)
	}
	info, err := broker2.ServerStatus("protected")
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != mcp.AuthStatusAuthorized {
		t.Fatalf("restart lost authorization: %+v", info)
	}

	// Phase 6: logout revokes (best effort) and deletes the grant.
	if err := broker.Logout(ctx, "protected"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, found, _ := store.Grant(resource); found {
		t.Fatal("grant must be deleted on logout")
	}
	if dbg := fetchOAuthDebug(t, base); len(dbg.Revoked) == 0 {
		t.Fatal("logout must attempt revocation")
	}
}

// TestOAuthDCRRefusedStaticClientFallback covers Slack-class authorization
// servers that refuse dynamic client registration: without static
// credentials the error names the config fix; with them the flow works.
func TestOAuthDCRRefusedStaticClientFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	startMockServer(t, mockHTTPServerBin,
		"-port", fmt.Sprintf("%d", port), "-oauth", "-oauth-no-dcr")
	waitForPort(t, ctx, port)

	store, err := mcpauth.NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cbPort := freePort(t)
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d%s", cbPort, mcpauth.CallbackPath)
	broker := mcpauth.NewBroker(store, redirectURL, nil)
	startCallbackServer(t, broker, cbPort)

	// Without static credentials: the error names the stack.yaml fix.
	if err := broker.Configure("slackish", base+"/mcp", nil); err != nil {
		t.Fatal(err)
	}
	_, _, err = broker.BeginAuthorization(ctx, "slackish", time.Minute)
	if !errors.Is(err, mcpauth.ErrDCRUnsupported) {
		t.Fatalf("expected ErrDCRUnsupported, got %v", err)
	}
	if !strings.Contains(err.Error(), "auth.client_id") {
		t.Fatalf("error must name the static-client fallback: %v", err)
	}

	// With a pre-registered client the flow completes end to end.
	if err := broker.Configure("slackish", base+"/mcp", &mcp.ServerAuthConfig{
		Type: "oauth", ClientID: "static-client",
	}); err != nil {
		t.Fatal(err)
	}
	authorizeURL, stateToken, err := broker.BeginAuthorization(ctx, "slackish", time.Minute)
	if err != nil {
		t.Fatalf("BeginAuthorization with static client: %v", err)
	}
	resp, err := http.Get(authorizeURL)
	if err != nil {
		t.Fatalf("driving authorization redirect: %v", err)
	}
	resp.Body.Close()
	if err := broker.Wait(ctx, stateToken); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	name, value, err := broker.HeaderSource("slackish").AuthHeader(ctx)
	if err != nil {
		t.Fatalf("AuthHeader: %v", err)
	}
	if name != "Authorization" || !strings.HasPrefix(value, "Bearer mock-access-") {
		t.Fatalf("unexpected header %q: %q", name, value)
	}
}
