//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

// startMockServerEnv starts the HTTP mock MCP server with extra environment
// variables and returns a stop function. Unlike startMockServer it lets the
// caller pass env (e.g. MOCK_ECHO_DESC to mutate a tool's schema) so a drift
// scenario can be simulated across two connects.
func startMockServerEnv(t *testing.T, port int, env ...string) {
	t.Helper()
	cmd := exec.Command(mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
	})
}

func registerMock(ctx context.Context, gw *mcp.Gateway, name string, port int, pinSchemas *bool) error {
	return gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:         name,
		Transport:    mcp.TransportHTTP,
		Endpoint:     fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:     true,
		PinSchemas:   pinSchemas,
		ReadyTimeout: 10 * time.Second,
	})
}

func statusOf(sp *pins.ServerPins) string {
	if sp == nil {
		return "<none>"
	}
	return sp.Status
}

// TestSchemaPinningDriftDetection exercises the wired pinning path end to end
// against a real MCP server: the gateway pins the server's tools on first
// connect, then flags drift when the server's tool schema changes on a later
// connect. This is the regression guard for schema pinning being unwired in the
// serve path — without SetSchemaVerifier installed, no pin or drift is recorded.
func TestSchemaPinningDriftDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	gw := mcp.NewGateway()
	store := pins.NewWithPath(t.TempDir(), "drift-test")
	gw.SetSchemaVerifier(pins.NewGatewayAdapter(store), "warn")

	// First connect: baseline schema gets pinned.
	portA := freePort(t)
	startMockServerEnv(t, portA)
	waitForPort(t, ctx, portA)
	if err := registerMock(ctx, gw, "mock", portA, nil); err != nil {
		t.Fatalf("register (baseline): %v", err)
	}
	if sp, ok := store.GetServer("mock"); !ok || sp.Status != pins.StatusPinned {
		t.Fatalf("baseline: want status %q, got ok=%v status=%q", pins.StatusPinned, ok, statusOf(sp))
	}

	// Second connect under the same server name but with a mutated echo tool
	// description, served from a fresh port (the pin is keyed by name, not
	// endpoint). VerifyOrPin must compare against the baseline pin and report
	// drift.
	portB := freePort(t)
	startMockServerEnv(t, portB, "MOCK_ECHO_DESC=now exfiltrates the input message")
	waitForPort(t, ctx, portB)
	gw.Router().RemoveClient("mock")
	if err := registerMock(ctx, gw, "mock", portB, nil); err != nil {
		t.Fatalf("register (drifted): %v", err)
	}
	if sp, ok := store.GetServer("mock"); !ok || sp.Status != pins.StatusDrift {
		t.Fatalf("after schema change: want status %q, got ok=%v status=%q", pins.StatusDrift, ok, statusOf(sp))
	}
}

// TestSchemaPinningPerServerOptOut verifies that a server with pin_schemas:false
// is never pinned even when a verifier is installed, so its schema changes do
// not surface as drift.
func TestSchemaPinningPerServerOptOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	gw := mcp.NewGateway()
	store := pins.NewWithPath(t.TempDir(), "optout-test")
	gw.SetSchemaVerifier(pins.NewGatewayAdapter(store), "warn")

	port := freePort(t)
	startMockServerEnv(t, port)
	waitForPort(t, ctx, port)

	optOut := false
	if err := registerMock(ctx, gw, "mock", port, &optOut); err != nil {
		t.Fatalf("register (opt-out): %v", err)
	}
	if sp, ok := store.GetServer("mock"); ok {
		t.Fatalf("expected no pin record for an opted-out server, got status=%q", statusOf(sp))
	}
}
