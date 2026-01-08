package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"agentlab/pkg/dockerclient"
	"agentlab/pkg/mcp"
	"agentlab/pkg/runtime"

	"github.com/docker/docker/api/types/container"
)

// Server provides the combined API server for agentlab.
type Server struct {
	gateway      *mcp.Gateway
	mcpHandler   *mcp.Handler
	sseServer    *mcp.SSEServer
	staticFS     fs.FS
	dockerClient dockerclient.DockerClient
	topologyName string
}

// NewServer creates a new API server.
func NewServer(gateway *mcp.Gateway, staticFS fs.FS) *Server {
	return &Server{
		gateway:    gateway,
		mcpHandler: mcp.NewHandler(gateway),
		sseServer:  mcp.NewSSEServer(gateway),
		staticFS:   staticFS,
	}
}

// SetDockerClient sets the Docker client for container operations.
func (s *Server) SetDockerClient(cli dockerclient.DockerClient) {
	s.dockerClient = cli
}

// SetTopologyName sets the topology name for container lookups.
func (s *Server) SetTopologyName(name string) {
	s.topologyName = name
}

// Handler returns the main HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// MCP endpoints - both POST (JSON-RPC) and SSE
	mux.Handle("/mcp", s.mcpHandler)                       // POST JSON-RPC
	mux.Handle("/sse", s.sseServer)                        // GET SSE connection
	mux.HandleFunc("/message", s.sseServer.HandleMessage)  // POST message for SSE

	// API endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/mcp-servers", s.handleMCPServers)
	mux.HandleFunc("/api/tools", s.handleTools)

	// Agent control endpoints (pattern: /api/agents/{name}/action)
	mux.HandleFunc("/api/agents/", s.handleAgentAction)

	// Static files (UI) - served at root
	if s.staticFS != nil {
		fileServer := http.FileServer(http.FS(s.staticFS))
		mux.Handle("/", spaHandler(fileServer, s.staticFS))
	}

	return corsMiddleware(mux)
}

// handleStatus returns the overall gateway status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := struct {
		Gateway    ServerInfo        `json:"gateway"`
		MCPServers []MCPServerStatus `json:"mcp-servers"`
		Agents     []AgentStatus     `json:"agents"`
		Resources  []ResourceStatus  `json:"resources"`
	}{
		Gateway: ServerInfo{
			Name:    s.gateway.ServerInfo().Name,
			Version: s.gateway.ServerInfo().Version,
		},
		MCPServers: s.getMCPServerStatuses(),
		Agents:     s.getAgentStatuses(),
		Resources:  s.getResourceStatuses(),
	}

	writeJSON(w, status)
}

// handleMCPServers returns information about registered MCP servers.
func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, s.gateway.Status())
}

// handleTools returns all aggregated tools.
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, _ := s.gateway.HandleToolsList()
	writeJSON(w, result)
}

// ServerInfo mirrors the mcp.ServerInfo type for API responses.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPServerStatus mirrors the mcp.MCPServerStatus type for API responses.
type MCPServerStatus struct {
	Name        string   `json:"name"`
	Endpoint    string   `json:"endpoint"`
	Initialized bool     `json:"initialized"`
	ToolCount   int      `json:"toolCount"`
	Tools       []string `json:"tools"`
}

func (s *Server) getMCPServerStatuses() []MCPServerStatus {
	mcpStatuses := s.gateway.Status()
	statuses := make([]MCPServerStatus, len(mcpStatuses))
	for i, ms := range mcpStatuses {
		statuses[i] = MCPServerStatus{
			Name:        ms.Name,
			Endpoint:    ms.Endpoint,
			Initialized: ms.Initialized,
			ToolCount:   ms.ToolCount,
			Tools:       ms.Tools,
		}
	}
	return statuses
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// corsMiddleware adds CORS headers to responses.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// spaHandler wraps the file server to handle SPA routing.
func spaHandler(fileServer http.Handler, staticFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		} else if path[0] == '/' {
			path = path[1:]
		}

		// Check if file exists
		if _, err := fs.Stat(staticFS, path); err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}

// ResourceStatus contains status information for a resource container.
type ResourceStatus struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

// AgentStatus contains status information for an agent container.
type AgentStatus struct {
	Name        string `json:"name"`
	Image       string `json:"image"`
	Status      string `json:"status"`
	ContainerID string `json:"containerId,omitempty"`
}

// getResourceStatuses returns status of all resource containers.
func (s *Server) getResourceStatuses() []ResourceStatus {
	if s.dockerClient == nil || s.topologyName == "" {
		return []ResourceStatus{}
	}

	ctx := context.Background()
	containers, err := runtime.ListManagedContainers(ctx, s.dockerClient, s.topologyName)
	if err != nil {
		return []ResourceStatus{}
	}

	var resources []ResourceStatus
	for _, c := range containers {
		// Only include resource containers (not MCP servers)
		if resName, ok := c.Labels[runtime.LabelResource]; ok {
			status := "stopped"
			if c.State == "running" {
				status = "running"
			} else if c.State != "exited" {
				status = c.State
			}

			resources = append(resources, ResourceStatus{
				Name:   resName,
				Image:  c.Image,
				Status: status,
			})
		}
	}

	return resources
}

// getAgentStatuses returns status of all agent containers.
func (s *Server) getAgentStatuses() []AgentStatus {
	if s.dockerClient == nil || s.topologyName == "" {
		return []AgentStatus{}
	}

	ctx := context.Background()
	containers, err := runtime.ListManagedContainers(ctx, s.dockerClient, s.topologyName)
	if err != nil {
		return []AgentStatus{}
	}

	var agents []AgentStatus
	for _, c := range containers {
		// Only include agent containers
		if agentName, ok := c.Labels[runtime.LabelAgent]; ok {
			status := "stopped"
			if c.State == "running" {
				status = "running"
			} else if c.State != "exited" {
				status = c.State
			}

			agents = append(agents, AgentStatus{
				Name:        agentName,
				Image:       c.Image,
				Status:      status,
				ContainerID: c.ID[:12],
			})
		}
	}

	return agents
}

// handleAgentAction routes agent control requests.
// URL pattern: /api/agents/{name}/{action}
func (s *Server) handleAgentAction(w http.ResponseWriter, r *http.Request) {
	// Parse URL path: /api/agents/{name}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path: expected /api/agents/{name}/{action}", http.StatusBadRequest)
		return
	}

	agentName := parts[0]
	action := parts[1]

	switch action {
	case "logs":
		s.handleAgentLogs(w, r, agentName)
	case "restart":
		s.handleAgentRestart(w, r, agentName)
	case "stop":
		s.handleAgentStop(w, r, agentName)
	default:
		http.Error(w, "Unknown action: "+action, http.StatusBadRequest)
	}
}

// handleAgentLogs returns container logs for an agent.
func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request, agentName string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.dockerClient == nil || s.topologyName == "" {
		http.Error(w, "Docker client not configured", http.StatusServiceUnavailable)
		return
	}

	// Get number of lines from query param (default 100)
	lines := 100
	if linesParam := r.URL.Query().Get("lines"); linesParam != "" {
		if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
			lines = n
		}
	}

	// Find container by name
	containerName := runtime.ContainerName(s.topologyName, agentName)
	exists, containerID, err := runtime.ContainerExists(r.Context(), s.dockerClient, containerName)
	if err != nil {
		http.Error(w, "Failed to find container: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Container not found: "+agentName, http.StatusNotFound)
		return
	}

	// Get container logs
	logsReader, err := s.dockerClient.ContainerLogs(r.Context(), containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       strconv.Itoa(lines),
		Timestamps: true,
	})
	if err != nil {
		http.Error(w, "Failed to get logs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer logsReader.Close()

	// Read and parse logs
	var logLines []string
	scanner := bufio.NewScanner(logsReader)
	for scanner.Scan() {
		line := scanner.Text()
		// Docker logs have an 8-byte header we need to skip
		if len(line) > 8 {
			line = line[8:]
		}
		logLines = append(logLines, line)
	}

	writeJSON(w, logLines)
}

// handleAgentRestart restarts an agent container.
func (s *Server) handleAgentRestart(w http.ResponseWriter, r *http.Request, agentName string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.dockerClient == nil || s.topologyName == "" {
		http.Error(w, "Docker client not configured", http.StatusServiceUnavailable)
		return
	}

	// Find container by name
	containerName := runtime.ContainerName(s.topologyName, agentName)
	exists, containerID, err := runtime.ContainerExists(r.Context(), s.dockerClient, containerName)
	if err != nil {
		http.Error(w, "Failed to find container: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Container not found: "+agentName, http.StatusNotFound)
		return
	}

	// Restart container
	timeout := 10
	if err := s.dockerClient.ContainerRestart(r.Context(), containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		http.Error(w, "Failed to restart container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "restarted", "agent": agentName})
}

// handleAgentStop stops an agent container.
func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request, agentName string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.dockerClient == nil || s.topologyName == "" {
		http.Error(w, "Docker client not configured", http.StatusServiceUnavailable)
		return
	}

	// Find container by name
	containerName := runtime.ContainerName(s.topologyName, agentName)
	exists, containerID, err := runtime.ContainerExists(r.Context(), s.dockerClient, containerName)
	if err != nil {
		http.Error(w, "Failed to find container: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Container not found: "+agentName, http.StatusNotFound)
		return
	}

	// Stop container
	if err := runtime.StopContainer(r.Context(), s.dockerClient, containerID, 10); err != nil {
		http.Error(w, "Failed to stop container: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "stopped", "agent": agentName})
}
