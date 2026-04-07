//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker"
)

// sanitizeName converts a test name (which may contain '/' from subtests) into
// a string safe for use as a Docker stack or network name.
func sanitizeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == ' ' {
			return '-'
		}
		return r
	}, strings.ToLower(s))
}

// writeStackYAML marshals a config.Stack to YAML and writes it to path.
func writeStackYAML(t *testing.T, path string, stack *config.Stack) {
	t.Helper()
	data, err := yaml.Marshal(stack)
	if err != nil {
		t.Fatalf("marshal stack YAML: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write stack YAML: %v", err)
	}
}

// sleepCmd is a minimal command that keeps a container alive for test stacks.
var sleepCmd = []string{"sh", "-c", "while true; do sleep 1; done"}

// TestHotReload_AddServer verifies that adding an MCP server via config reload
// starts a new container and reports the server in ReloadResult.Added.
func TestHotReload_AddServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	stackPath := t.TempDir() + "/stack.yaml"

	topo := &config.Stack{
		Version: "1",
		Name:    stackName,
		Network: config.Network{Name: netName, Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd},
		},
	}
	writeStackYAML(t, stackPath, topo)

	if _, err := rt.Up(ctx, topo, runtime.UpOptions{BasePort: 20100}); err != nil {
		t.Fatalf("Up: %v", err)
	}
	defer rt.Down(ctx, stackName) //nolint:errcheck

	handler := reload.NewHandler(stackPath, topo, mcp.NewGateway(), rt, 0, 20110, nil, nil)

	updated := &config.Stack{
		Version: topo.Version,
		Name:    topo.Name,
		Network: topo.Network,
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd},
			{Name: "server2", Image: "alpine:latest", Port: 8081, Command: sleepCmd},
		},
	}
	writeStackYAML(t, stackPath, updated)

	result, err := handler.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected reload success, got: %s", result.Message)
	}
	if len(result.Added) != 1 || result.Added[0] != "mcp-server:server2" {
		t.Errorf("expected Added=[mcp-server:server2], got: %v", result.Added)
	}
	if len(result.Removed) != 0 {
		t.Errorf("expected no Removed, got: %v", result.Removed)
	}

	statuses, err := rt.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 2 {
		t.Errorf("expected 2 running containers after add, got %d", len(statuses))
	}
}

// TestHotReload_RemoveServer verifies that removing an MCP server via config
// reload stops the container and reports the server in ReloadResult.Removed.
func TestHotReload_RemoveServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	stackPath := t.TempDir() + "/stack.yaml"

	topo := &config.Stack{
		Version: "1",
		Name:    stackName,
		Network: config.Network{Name: netName, Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd},
			{Name: "server2", Image: "alpine:latest", Port: 8081, Command: sleepCmd},
		},
	}
	writeStackYAML(t, stackPath, topo)

	if _, err := rt.Up(ctx, topo, runtime.UpOptions{BasePort: 20200}); err != nil {
		t.Fatalf("Up: %v", err)
	}
	defer rt.Down(ctx, stackName) //nolint:errcheck

	handler := reload.NewHandler(stackPath, topo, mcp.NewGateway(), rt, 0, 20210, nil, nil)

	updated := &config.Stack{
		Version: topo.Version,
		Name:    topo.Name,
		Network: topo.Network,
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd},
		},
	}
	writeStackYAML(t, stackPath, updated)

	result, err := handler.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected reload success, got: %s", result.Message)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "mcp-server:server2" {
		t.Errorf("expected Removed=[mcp-server:server2], got: %v", result.Removed)
	}
	if len(result.Added) != 0 {
		t.Errorf("expected no Added, got: %v", result.Added)
	}

	statuses, err := rt.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 1 {
		t.Errorf("expected 1 running container after remove, got %d", len(statuses))
	}
}

// TestHotReload_ModifyServer verifies that modifying an MCP server's config via
// reload replaces the container and reports the server in ReloadResult.Modified.
func TestHotReload_ModifyServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	stackPath := t.TempDir() + "/stack.yaml"

	topo := &config.Stack{
		Version: "1",
		Name:    stackName,
		Network: config.Network{Name: netName, Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd},
		},
	}
	writeStackYAML(t, stackPath, topo)

	if _, err := rt.Up(ctx, topo, runtime.UpOptions{BasePort: 20300}); err != nil {
		t.Fatalf("Up: %v", err)
	}
	defer rt.Down(ctx, stackName) //nolint:errcheck

	handler := reload.NewHandler(stackPath, topo, mcp.NewGateway(), rt, 0, 20310, nil, nil)

	// Changing env triggers the "modified" path in diff.go.
	updated := &config.Stack{
		Version: topo.Version,
		Name:    topo.Name,
		Network: topo.Network,
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd, Env: map[string]string{"TEST_VAR": "changed"}},
		},
	}
	writeStackYAML(t, stackPath, updated)

	result, err := handler.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected reload success, got: %s", result.Message)
	}
	if len(result.Modified) != 1 || result.Modified[0] != "mcp-server:server1" {
		t.Errorf("expected Modified=[mcp-server:server1], got: %v", result.Modified)
	}

	statuses, err := rt.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statuses) != 1 {
		t.Errorf("expected 1 running container after modify, got %d", len(statuses))
	}
}

// TestHotReload_NetworkChangeRejected verifies that changing the network
// configuration returns a failed result requiring a full restart.
func TestHotReload_NetworkChangeRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	stackPath := t.TempDir() + "/stack.yaml"

	topo := &config.Stack{
		Version: "1",
		Name:    stackName,
		Network: config.Network{Name: netName, Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd},
		},
	}
	writeStackYAML(t, stackPath, topo)

	if _, err := rt.Up(ctx, topo, runtime.UpOptions{BasePort: 20400}); err != nil {
		t.Fatalf("Up: %v", err)
	}
	defer rt.Down(ctx, stackName) //nolint:errcheck

	handler := reload.NewHandler(stackPath, topo, mcp.NewGateway(), rt, 0, 20410, nil, nil)

	updated := &config.Stack{
		Version:    topo.Version,
		Name:       topo.Name,
		Network:    config.Network{Name: netName + "-changed", Driver: "bridge"},
		MCPServers: topo.MCPServers,
	}
	writeStackYAML(t, stackPath, updated)

	result, err := handler.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload returned unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("expected reload to fail for network change, but got success")
	}
	if !strings.Contains(result.Message, "network") {
		t.Errorf("expected message to mention 'network', got: %q", result.Message)
	}
}

// TestHotReload_Idempotent verifies that reloading with no config changes is a
// no-op and produces empty Added/Removed/Modified slices on repeated calls.
func TestHotReload_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	stackPath := t.TempDir() + "/stack.yaml"

	topo := &config.Stack{
		Version: "1",
		Name:    stackName,
		Network: config.Network{Name: netName, Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 8080, Command: sleepCmd},
		},
	}
	writeStackYAML(t, stackPath, topo)

	if _, err := rt.Up(ctx, topo, runtime.UpOptions{BasePort: 20500}); err != nil {
		t.Fatalf("Up: %v", err)
	}
	defer rt.Down(ctx, stackName) //nolint:errcheck

	handler := reload.NewHandler(stackPath, topo, mcp.NewGateway(), rt, 0, 20510, nil, nil)

	for i, label := range []string{"first", "second"} {
		result, err := handler.Reload(ctx)
		if err != nil {
			t.Fatalf("Reload (%s): %v", label, err)
		}
		if !result.Success {
			t.Fatalf("expected %s reload to succeed, got: %s", label, result.Message)
		}
		total := len(result.Added) + len(result.Removed) + len(result.Modified)
		if total != 0 {
			t.Errorf("reload %d (%s): expected no changes, got added=%v removed=%v modified=%v",
				i+1, label, result.Added, result.Removed, result.Modified)
		}
	}
}
