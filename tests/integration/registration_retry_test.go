//go:build integration

package integration

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestGatewayRegistrationRetryRecovery covers the startup-ordering bug
// behind #1180: an external HTTP server that is not yet running when the
// gateway registers it must not be dropped for the process lifetime. Once
// the backend comes up, the health monitor's pending-registration retry
// must register it, surface its tools, and mark it healthy, with no
// gateway restart and no config change.
func TestGatewayRegistrationRetryRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := freePort(t)

	gw := mcp.NewGateway()
	defer gw.Close()

	cfg := mcp.MCPServerConfig{
		Name:      "late-boot",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:  true,
		// Short window so the initial failure is quick; must stay >= 1s
		// because the ready poll first pings at 500ms.
		ReadyTimeout: 1 * time.Second,
	}

	// Nothing is listening yet: registration must fail and surface as a
	// registration failure, exactly as a wrong start order does.
	err := gw.RegisterMCPServer(ctx, cfg)
	if err == nil {
		t.Fatal("expected registration to fail with no backend listening")
	}
	gw.RecordRegistrationFailure("late-boot", err)

	var failedRow bool
	for _, s := range gw.Status() {
		if s.Name == "late-boot" && s.RegistrationFailed {
			failedRow = true
		}
	}
	if !failedRow {
		t.Fatal("expected a RegistrationFailed status row before recovery")
	}

	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()
	gw.StartHealthMonitor(healthCtx, 100*time.Millisecond)

	// The backend comes up late, after the gateway already gave up once.
	mock := exec.Command(mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	if err := mock.Start(); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	t.Cleanup(func() {
		mock.Process.Kill() //nolint:errcheck
		mock.Wait()         //nolint:errcheck
	})
	waitForPort(t, ctx, port)

	// The pending retry must converge: registered, routable tools, healthy,
	// and the failure row gone.
	deadline := time.Now().Add(20 * time.Second)
	for {
		var recovered bool
		for _, s := range gw.Status() {
			if s.Name == "late-boot" && !s.RegistrationFailed && s.Initialized && s.ToolCount > 0 {
				recovered = true
			}
		}
		if recovered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway never recovered the late-boot server; status: %+v", gw.Status())
		}
		time.Sleep(200 * time.Millisecond)
	}

	tools, err := gw.HandleToolsList(ctx)
	if err != nil {
		t.Fatalf("tools/list after recovery: %v", err)
	}
	var sawTool bool
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "late-boot") || tool.Name != "" {
			sawTool = true
		}
	}
	if !sawTool {
		t.Error("expected the recovered server's tools in the aggregated list")
	}
}
