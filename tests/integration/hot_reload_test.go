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

// TestHotReload_ToolWhitelistPersists verifies that a change to the tools:
// field on an external URL server is detected as Modified by the reload
// handler, survives a simulated daemon restart (fresh Handler over the same
// stack file), and is picked up on the subsequent Reload without mutating
// any other server. This is the persistence guarantee the sidebar Tools
// Editor relies on — once the YAML is updated, the whitelist stays even if
// the daemon process exits and comes back up.
func TestHotReload_ToolWhitelistPersists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	stackPath := t.TempDir() + "/stack.yaml"

	topo := &config.Stack{
		Version: "1",
		Name:    stackName,
		Network: config.Network{Name: netName, Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "ext", URL: "https://example.com/mcp", Transport: "http"},
		},
	}
	writeStackYAML(t, stackPath, topo)

	// External servers skip container creation, so the test does not require
	// Docker. That keeps the whitelist-persistence check runnable in any CI.
	handler := reload.NewHandler(stackPath, topo, mcp.NewGateway(), nil, 0, 20610, nil, nil)
	handler.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []reload.ReplicaRuntime, stackPath string) error {
		return nil
	})

	// Update only the tools: field. Every other field on every other server
	// must remain untouched after the reload.
	updated := &config.Stack{
		Version:    topo.Version,
		Name:       topo.Name,
		Network:    topo.Network,
		MCPServers: []config.MCPServer{{Name: "ext", URL: "https://example.com/mcp", Transport: "http", Tools: []string{"a", "b"}}},
	}
	writeStackYAML(t, stackPath, updated)

	result, err := handler.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected reload success, got: %s", result.Message)
	}
	if len(result.Modified) != 1 || result.Modified[0] != "mcp-server:ext" {
		t.Fatalf("expected Modified=[mcp-server:ext], got: %v", result.Modified)
	}

	// Simulate a daemon restart: drop the handler and create a fresh one
	// against the same stack file. The new handler starts with an empty
	// currentCfg, so it should read the persisted whitelist on its first
	// reload and classify the server as Added (cold load), not Modified.
	freshHandler := reload.NewHandler(stackPath, nil, mcp.NewGateway(), nil, 0, 20611, nil, nil)

	var registered []config.MCPServer
	freshHandler.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []reload.ReplicaRuntime, stackPath string) error {
		registered = append(registered, server)
		return nil
	})
	freshResult, err := freshHandler.Reload(ctx)
	if err != nil {
		t.Fatalf("fresh Reload: %v", err)
	}
	if !freshResult.Success {
		t.Fatalf("expected fresh reload success, got: %s", freshResult.Message)
	}
	if len(registered) != 1 {
		t.Fatalf("expected exactly one server registration, got %d", len(registered))
	}
	if got := registered[0].Tools; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("expected whitelist [a b] to survive daemon restart, got %v", got)
	}
}
