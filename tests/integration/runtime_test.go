//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"agentlab/pkg/config"
	"agentlab/pkg/runtime"
)

// TestFullTopologyLifecycle tests the complete lifecycle of a topology.
// This test requires a running Docker daemon.
func TestFullTopologyLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Skip if Docker is not available
	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create a simple topology
	topo := &config.Topology{
		Version: "1",
		Name:    "integration-test",
		Network: config.Network{
			Name:   "integration-test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name:  "test-server",
				Image: "alpine:latest",
				Port:  8080,
				// Override command to keep container running
				Command: []string{"sh", "-c", "while true; do sleep 1; done"},
			},
		},
	}

	// Start the topology
	result, err := rt.Up(ctx, topo, runtime.UpOptions{BasePort: 19000})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	// Verify containers are running
	if len(result.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(result.MCPServers))
	}

	// Check status
	statuses, err := rt.Status(ctx, "integration-test")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(statuses) != 1 {
		t.Errorf("expected 1 container in status, got %d", len(statuses))
	}

	// Stop the topology
	if err := rt.Down(ctx, "integration-test"); err != nil {
		t.Fatalf("Down() error = %v", err)
	}

	// Verify containers are gone
	statuses, err = rt.Status(ctx, "integration-test")
	if err != nil {
		t.Fatalf("Status() after Down() error = %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected 0 containers after Down(), got %d", len(statuses))
	}
}

// TestTopologyWithResources tests a topology with both MCP servers and resources.
func TestTopologyWithResources(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topo := &config.Topology{
		Version: "1",
		Name:    "integration-resources",
		Network: config.Network{
			Name:   "integration-resources-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name:    "app-server",
				Image:   "alpine:latest",
				Port:    8080,
				Command: []string{"sh", "-c", "while true; do sleep 1; done"},
			},
		},
		Resources: []config.Resource{
			{
				Name:  "db",
				Image: "alpine:latest",
				Env: map[string]string{
					"TEST_VAR": "test-value",
				},
			},
		},
	}

	// Start topology
	_, err = rt.Up(ctx, topo, runtime.UpOptions{BasePort: 19100})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	// Verify both containers are running
	statuses, err := rt.Status(ctx, "integration-resources")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(statuses) != 2 {
		t.Errorf("expected 2 containers, got %d", len(statuses))
	}

	// Cleanup
	if err := rt.Down(ctx, "integration-resources"); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
}

// TestMultipleNetworks tests advanced network mode with multiple networks.
func TestMultipleNetworks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rt, err := runtime.New()
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	topo := &config.Topology{
		Version: "1",
		Name:    "integration-multinetwork",
		Networks: []config.Network{
			{Name: "public-net", Driver: "bridge"},
			{Name: "private-net", Driver: "bridge"},
		},
		MCPServers: []config.MCPServer{
			{
				Name:    "frontend",
				Image:   "alpine:latest",
				Port:    8080,
				Network: "public-net",
				Command: []string{"sh", "-c", "while true; do sleep 1; done"},
			},
			{
				Name:    "backend",
				Image:   "alpine:latest",
				Port:    8081,
				Network: "private-net",
				Command: []string{"sh", "-c", "while true; do sleep 1; done"},
			},
		},
	}

	// Start topology
	result, err := rt.Up(ctx, topo, runtime.UpOptions{BasePort: 19200})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	if len(result.MCPServers) != 2 {
		t.Errorf("expected 2 MCP servers, got %d", len(result.MCPServers))
	}

	// Cleanup
	if err := rt.Down(ctx, "integration-multinetwork"); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
}

// TestDockerNotAvailable verifies graceful handling when Docker is not available.
func TestDockerNotAvailable(t *testing.T) {
	// This test only makes sense if we can simulate Docker being unavailable
	// For now, we just test the error path by temporarily setting DOCKER_HOST to invalid
	origHost := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "tcp://invalid:99999")
	defer os.Setenv("DOCKER_HOST", origHost)

	_, err := runtime.New()
	if err == nil {
		t.Error("expected error when Docker is not available")
	}
}
