package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"agentlab/pkg/a2a"
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
	a2aGateway   *a2a.Gateway
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

// SetA2AGateway sets the A2A gateway for agent-to-agent communication.
func (s *Server) SetA2AGateway(a2aGateway *a2a.Gateway) {
	s.a2aGateway = a2aGateway
}

// A2AGateway returns the A2A gateway.
func (s *Server) A2AGateway() *a2a.Gateway {
	return s.a2aGateway
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

	// A2A endpoints
	if s.a2aGateway != nil {
		mux.HandleFunc("/.well-known/agent.json", s.handleA2AAgentCards)
		mux.Handle("/a2a/", s.a2aGateway.Handler())
	}

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
// Agents are returned as a unified list that merges container and A2A status.
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

// handleA2AAgentCards returns all agent cards for A2A discovery.
func (s *Server) handleA2AAgentCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.a2aGateway == nil {
		http.Error(w, "A2A not enabled", http.StatusNotFound)
		return
	}

	cards := s.a2aGateway.Handler().ListLocalAgents()

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"agents": cards,
	}
	_ = json.NewEncoder(w).Encode(response)
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
	Name         string   `json:"name"`
	Transport    string   `json:"transport"`
	Endpoint     string   `json:"endpoint"`
	Initialized  bool     `json:"initialized"`
	ToolCount    int      `json:"toolCount"`
	Tools        []string `json:"tools"`
	External     bool     `json:"external"`
	LocalProcess bool     `json:"localProcess"`
}

func (s *Server) getMCPServerStatuses() []MCPServerStatus {
	mcpStatuses := s.gateway.Status()
	statuses := make([]MCPServerStatus, len(mcpStatuses))
	for i, ms := range mcpStatuses {
		statuses[i] = MCPServerStatus{
			Name:         ms.Name,
			Transport:    string(ms.Transport),
			Endpoint:     ms.Endpoint,
			Initialized:  ms.Initialized,
			ToolCount:    ms.ToolCount,
			Tools:        ms.Tools,
			External:     ms.External,
			LocalProcess: ms.LocalProcess,
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

// AgentStatus contains unified status for all agents (local containers and remote A2A).
// This merges container state with A2A protocol state into a single representation.
type AgentStatus struct {
	// Core identification
	Name   string `json:"name"`
	Status string `json:"status"` // "running", "stopped", "error", "unavailable"

	// Variant: "local" (container-based) or "remote" (A2A only)
	Variant string `json:"variant"`

	// Container fields (populated for local/container-based agents)
	Image       string   `json:"image,omitempty"`
	ContainerID string   `json:"containerId,omitempty"`
	Uses        []string `json:"uses,omitempty"`

	// A2A fields (populated when agent has A2A capability)
	HasA2A      bool     `json:"hasA2A"`
	Role        string   `json:"role,omitempty"`        // "local" or "remote"
	URL         string   `json:"url,omitempty"`         // A2A endpoint URL
	Endpoint    string   `json:"endpoint,omitempty"`    // A2A RPC endpoint
	SkillCount  int      `json:"skillCount,omitempty"`  // Number of A2A skills
	Skills      []string `json:"skills,omitempty"`      // A2A skill names
	Description string   `json:"description,omitempty"` // Agent description
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

// containerAgentInfo holds container-specific info for an agent.
type containerAgentInfo struct {
	Name        string
	Image       string
	Status      string
	ContainerID string
	Uses        []string
}

// getContainerAgents returns a map of container agent info keyed by name.
func (s *Server) getContainerAgents() map[string]containerAgentInfo {
	result := make(map[string]containerAgentInfo)

	if s.dockerClient == nil || s.topologyName == "" {
		return result
	}

	ctx := context.Background()
	containers, err := runtime.ListManagedContainers(ctx, s.dockerClient, s.topologyName)
	if err != nil {
		return result
	}

	for _, c := range containers {
		// Only include agent containers
		if agentName, ok := c.Labels[runtime.LabelAgent]; ok {
			status := "stopped"
			if c.State == "running" {
				status = "running"
			} else if c.State != "exited" {
				status = c.State
			}

			// Get the agent's uses/dependencies from the gateway
			uses := s.gateway.GetAgentAllowedServers(agentName)

			result[agentName] = containerAgentInfo{
				Name:        agentName,
				Image:       c.Image,
				Status:      status,
				ContainerID: c.ID[:12],
				Uses:        uses,
			}
		}
	}

	return result
}

// getAgentStatuses returns unified status for all agents (local + remote).
// It merges container state with A2A protocol state.
func (s *Server) getAgentStatuses() []AgentStatus {
	// Get container agents as a map for quick lookup
	containerAgents := s.getContainerAgents()

	// Get A2A agent statuses
	var a2aStatuses []a2a.A2AAgentStatus
	if s.a2aGateway != nil {
		a2aStatuses = s.a2aGateway.Status()
	}

	// Build unified list
	var unified []AgentStatus
	seen := make(map[string]bool)

	// Process A2A agents first (they may have container counterparts)
	for _, a2aAgent := range a2aStatuses {
		agent := AgentStatus{
			Name:        a2aAgent.Name,
			HasA2A:      true,
			Role:        a2aAgent.Role,
			URL:         a2aAgent.URL,
			Endpoint:    a2aAgent.Endpoint,
			SkillCount:  a2aAgent.SkillCount,
			Skills:      a2aAgent.Skills,
			Description: a2aAgent.Description,
		}

		if a2aAgent.Role == "local" {
			// Local A2A agent - merge with container info if available
			agent.Variant = "local"
			if container, ok := containerAgents[a2aAgent.Name]; ok {
				agent.Image = container.Image
				agent.Status = container.Status
				agent.ContainerID = container.ContainerID
				agent.Uses = container.Uses
			} else {
				// Container not found - might be starting or crashed
				if a2aAgent.Available {
					agent.Status = "running"
				} else {
					agent.Status = "unavailable"
				}
			}
		} else {
			// Remote A2A agent - no container, derive status from availability
			agent.Variant = "remote"
			if a2aAgent.Available {
				agent.Status = "running"
			} else {
				agent.Status = "unavailable"
			}
		}

		unified = append(unified, agent)
		seen[a2aAgent.Name] = true
	}

	// Add container-only agents (not A2A enabled)
	for name, container := range containerAgents {
		if !seen[name] {
			unified = append(unified, AgentStatus{
				Name:        name,
				Variant:     "local",
				Image:       container.Image,
				Status:      container.Status,
				ContainerID: container.ContainerID,
				Uses:        container.Uses,
				HasA2A:      false,
			})
		}
	}

	return unified
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
