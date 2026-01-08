package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"agentlab/internal/api"
	"agentlab/pkg/config"
	"agentlab/pkg/mcp"
	"agentlab/pkg/runtime"
	"agentlab/pkg/state"

	"github.com/spf13/cobra"
)

var (
	deployVerbose     bool
	deployNoCache     bool
	deployPort        int
	deployForeground  bool
	deployDaemonChild bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy <topology.yaml>",
	Short: "Start MCP servers defined in a topology file",
	Long: `Reads a topology YAML file and starts all defined MCP servers and resources.

Creates a Docker network, pulls/builds images as needed, and starts containers.
The MCP gateway runs as a background daemon by default.

Use --foreground (-f) to run in foreground with verbose output.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploy(args[0])
	},
}

func init() {
	deployCmd.Flags().BoolVarP(&deployVerbose, "verbose", "v", false, "Print full topology as JSON")
	deployCmd.Flags().BoolVar(&deployNoCache, "no-cache", false, "Force rebuild of source-based images")
	deployCmd.Flags().IntVarP(&deployPort, "port", "p", 8080, "Port for MCP gateway")
	deployCmd.Flags().BoolVarP(&deployForeground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	deployCmd.Flags().BoolVar(&deployDaemonChild, "daemon-child", false, "Internal flag for daemon process")
	_ = deployCmd.Flags().MarkHidden("daemon-child")
}

func runDeploy(topologyPath string) error {
	// Convert to absolute path for daemon child
	absPath, err := filepath.Abs(topologyPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	topologyPath = absPath

	// Load topology
	topo, err := config.LoadTopology(topologyPath)
	if err != nil {
		return fmt.Errorf("failed to load topology: %w", err)
	}

	// Check if already running
	existingState, _ := state.Load(topo.Name)
	if existingState != nil && state.IsRunning(existingState) {
		return fmt.Errorf("topology '%s' is already running on port %d (PID: %d)\nUse 'agentlab destroy %s' to stop it first",
			topo.Name, existingState.Port, existingState.PID, topologyPath)
	}

	// If we're the daemon child, run the gateway
	if deployDaemonChild {
		return runDeployDaemonChild(topologyPath, topo)
	}

	// Print info
	if deployForeground || deployVerbose {
		fmt.Printf("Loading topology from %s\n", topologyPath)
		fmt.Printf("Topology '%s' loaded successfully\n", topo.Name)
		fmt.Printf("  Version: %s\n", topo.Version)
		if len(topo.Networks) > 0 {
			fmt.Printf("  Networks: %d\n", len(topo.Networks))
		} else {
			fmt.Printf("  Network: %s (%s)\n", topo.Network.Name, topo.Network.Driver)
		}
		fmt.Printf("  MCP Servers: %d\n", len(topo.MCPServers))
		fmt.Printf("  Resources: %d\n", len(topo.Resources))
	}

	if deployVerbose {
		fmt.Println("\nFull topology (JSON):")
		data, _ := json.MarshalIndent(topo, "", "  ")
		fmt.Println(string(data))
	}

	// Start containers
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	ctx := context.Background()
	opts := runtime.UpOptions{
		NoCache:  deployNoCache,
		BasePort: 9000,
	}
	result, err := rt.Up(ctx, topo, opts)
	if err != nil {
		return fmt.Errorf("failed to start topology: %w", err)
	}

	// If foreground mode, run gateway directly
	if deployForeground {
		return runGateway(ctx, rt, topo, result, deployPort, true)
	}

	// Daemon mode: fork child process
	pid, err := forkDeployDaemon(topologyPath, deployPort)
	if err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for daemon to be ready
	time.Sleep(500 * time.Millisecond)

	// Verify daemon started
	st, err := state.Load(topo.Name)
	if err != nil {
		return fmt.Errorf("daemon may have failed to start - check logs at %s", state.LogPath(topo.Name))
	}

	fmt.Printf("Topology '%s' started successfully\n", topo.Name)
	fmt.Printf("  Gateway: http://localhost:%d\n", st.Port)
	fmt.Printf("  PID: %d\n", pid)
	fmt.Printf("  Logs: %s\n", state.LogPath(topo.Name))
	fmt.Printf("\nUse 'agentlab destroy %s' to stop\n", topologyPath)

	return nil
}

// runDeployDaemonChild runs the gateway as a daemon child process
func runDeployDaemonChild(topologyPath string, topo *config.Topology) error {
	// Create runtime
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	// Containers should already be running, but we need the result
	// Re-query container info
	ctx := context.Background()
	result, err := getRunningContainers(ctx, rt, topo)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	// Write state file before starting server
	st := &state.DaemonState{
		TopologyName: topo.Name,
		TopologyFile: topologyPath,
		PID:          os.Getpid(),
		Port:         deployPort,
		StartedAt:    time.Now(),
	}
	if err := state.Save(st); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Run gateway (blocks until shutdown)
	return runGateway(ctx, rt, topo, result, deployPort, false)
}

// getRunningContainers retrieves info about already-running containers
func getRunningContainers(ctx context.Context, rt *runtime.Runtime, topo *config.Topology) (*runtime.UpResult, error) {
	// Get container statuses
	statuses, err := rt.Status(ctx, topo.Name)
	if err != nil {
		return nil, err
	}

	// Build result from statuses
	result := &runtime.UpResult{}
	for _, status := range statuses {
		if status.Type == "mcp-server" {
			// Find the MCP server config to get port info
			var containerPort int
			for _, s := range topo.MCPServers {
				if s.Name == status.MCPServerName {
					containerPort = s.Port
					break
				}
			}

			// Get host port from container
			hostPort, _ := runtime.GetContainerHostPort(ctx, rt.DockerClient(), status.ID, containerPort)

			result.MCPServers = append(result.MCPServers, runtime.MCPServerInfo{
				Name:          status.MCPServerName,
				ContainerID:   status.ID,
				ContainerName: status.Name,
				ContainerPort: containerPort,
				HostPort:      hostPort,
			})
		}
	}

	return result, nil
}

// runGateway runs the MCP gateway (blocking)
func runGateway(ctx context.Context, rt *runtime.Runtime, topo *config.Topology, result *runtime.UpResult, port int, verbose bool) error {
	// Create MCP gateway
	gateway := mcp.NewGateway()
	gateway.SetDockerClient(rt.DockerClient())

	// Build a map from MCP server name to config for transport lookup
	serverConfigs := make(map[string]config.MCPServer)
	for _, s := range topo.MCPServers {
		serverConfigs[s.Name] = s
	}

	// Register MCP servers with the gateway
	if verbose {
		fmt.Println("\nRegistering MCP servers with gateway...")
	}
	for _, server := range result.MCPServers {
		serverCfg := serverConfigs[server.Name]
		transport := mcp.TransportHTTP // default
		if serverCfg.Transport == "stdio" {
			transport = mcp.TransportStdio
		}

		var cfg mcp.MCPServerConfig
		if transport == mcp.TransportStdio {
			cfg = mcp.MCPServerConfig{
				Name:        server.Name,
				Transport:   transport,
				ContainerID: server.ContainerID,
			}
		} else {
			cfg = mcp.MCPServerConfig{
				Name:      server.Name,
				Transport: transport,
				Endpoint:  fmt.Sprintf("http://localhost:%d/mcp", server.HostPort),
			}
		}

		if err := gateway.RegisterMCPServer(ctx, cfg); err != nil {
			if verbose {
				fmt.Printf("  Warning: failed to register MCP server %s: %v\n", server.Name, err)
			}
		}
	}

	// Get embedded web files
	webFS, err := WebFS()
	if err != nil && verbose {
		fmt.Printf("Warning: no embedded web UI: %v\n", err)
	}

	// Start API server with Docker client for container operations
	server := api.NewServer(gateway, webFS)
	server.SetDockerClient(rt.DockerClient())
	server.SetTopologyName(topo.Name)
	addr := fmt.Sprintf(":%d", port)

	// Handle shutdown gracefully
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	// Start server
	serverErr := make(chan error, 1)
	go func() {
		if err := http.ListenAndServe(addr, server.Handler()); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Give the server a moment to fail if port is in use
	select {
	case err := <-serverErr:
		// Clean up state file on startup failure
		_ = state.Delete(topo.Name)
		return fmt.Errorf("failed to start server on port %d: %w", port, err)
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
	}

	if verbose {
		fmt.Printf("\nMCP Gateway running:\n")
		fmt.Printf("  POST /mcp         - JSON-RPC endpoint\n")
		fmt.Printf("  GET  /sse         - SSE endpoint (for Claude Desktop)\n")
		fmt.Printf("  POST /message     - SSE message endpoint\n")
		fmt.Printf("\nWeb UI available at http://localhost%s/\n", addr)
		fmt.Printf("API endpoints:\n")
		fmt.Printf("  GET  /api/status      - Gateway status\n")
		fmt.Printf("  GET  /api/mcp-servers - List MCP servers\n")
		fmt.Printf("  GET  /api/tools       - List tools\n")
		fmt.Println("\nPress Ctrl+C to stop...")
	}

	// Wait for shutdown signal or server error
	select {
	case <-done:
		if verbose {
			fmt.Println("\nShutting down...")
		}
		// Clean up state file on graceful shutdown
		_ = state.Delete(topo.Name)
	case err := <-serverErr:
		_ = state.Delete(topo.Name)
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// forkDeployDaemon starts the daemon child process
func forkDeployDaemon(topologyPath string, port int) (int, error) {
	// Get current executable
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("getting executable: %w", err)
	}

	// Ensure log directory exists
	if err := state.EnsureLogDir(); err != nil {
		return 0, fmt.Errorf("creating log directory: %w", err)
	}

	// Get topology name for log file
	topo, err := config.LoadTopology(topologyPath)
	if err != nil {
		return 0, fmt.Errorf("loading topology: %w", err)
	}

	// Open log file
	logFile, err := os.OpenFile(state.LogPath(topo.Name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, fmt.Errorf("opening log file: %w", err)
	}

	// Build command with --daemon-child flag
	cmd := exec.Command(exe, "deploy", topologyPath,
		"--daemon-child",
		"--port", strconv.Itoa(port))

	// Detach from terminal
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	// Redirect stdio to log file
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Inherit environment
	cmd.Env = os.Environ()

	// Start child process
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("starting daemon: %w", err)
	}

	// Don't wait - let it run in background
	return cmd.Process.Pid, nil
}
