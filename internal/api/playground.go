package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime/agent"
	"gopkg.in/yaml.v3"
)

// playgroundChatRequest is the body for POST /api/playground/chat.
type playgroundChatRequest struct {
	AgentID   string          `json:"agentId"`
	Message   string          `json:"message"`
	SessionID string          `json:"sessionId"`
	AuthMode  agent.AuthMode  `json:"authMode"`
	Model     string          `json:"model"`
	OllamaURL string          `json:"ollamaUrl,omitempty"` // optional override for Path C
}

// playgroundAuthResponse is returned by POST /api/playground/auth.
type playgroundAuthResponse struct {
	Providers map[string]providerAuth `json:"providers"`
	Ollama    ollamaAuth              `json:"ollama"`
}

// providerAuth describes auth availability for one LLM provider.
// KeyName is the vault/env key name found (nil if none). CLIPath is the
// absolute path to a detected CLI binary (nil if not on PATH).
type providerAuth struct {
	APIKey  bool    `json:"apiKey"`
	KeyName *string `json:"keyName"`
	CLIPath *string `json:"cliPath"`
}

// ollamaAuth describes Ollama reachability.
type ollamaAuth struct {
	Reachable bool   `json:"reachable"`
	Endpoint  string `json:"endpoint"`
}

// handlePlaygroundAuth detects available auth methods for each LLM provider.
// POST /api/playground/auth
func (s *Server) handlePlaygroundAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := playgroundAuthResponse{
		Providers: make(map[string]providerAuth),
	}

	// findKey checks vault (if available and unlocked) then env for the first
	// matching key name. Returns a pointer to the matched key name, or nil.
	findKey := func(keyNames ...string) *string {
		if s.vaultStore != nil && !s.vaultStore.IsLocked() {
			for _, k := range keyNames {
				if v, ok := s.vaultStore.Get(k); ok && v != "" {
					name := k
					return &name
				}
			}
		} else {
			for _, k := range keyNames {
				if os.Getenv(k) != "" {
					name := k
					return &name
				}
			}
		}
		return nil
	}

	// lookupCLI returns a pointer to the absolute CLI path, or nil if not found.
	lookupCLI := func(name string) *string {
		if p, err := exec.LookPath(name); err == nil {
			return &p
		}
		return nil
	}

	// Anthropic: API key + claude CLI
	anthropicKey := findKey("ANTHROPIC_API_KEY")
	claudePath := lookupCLI("claude")
	resp.Providers["anthropic"] = providerAuth{
		APIKey:  anthropicKey != nil,
		KeyName: anthropicKey,
		CLIPath: claudePath,
	}

	// OpenAI: API key only (no first-party CLI to detect)
	openaiKey := findKey("OPENAI_API_KEY")
	resp.Providers["openai"] = providerAuth{
		APIKey:  openaiKey != nil,
		KeyName: openaiKey,
	}

	// Gemini: check both GEMINI_API_KEY and GOOGLE_API_KEY + gemini CLI
	geminiKey := findKey("GEMINI_API_KEY", "GOOGLE_API_KEY")
	geminiPath := lookupCLI("gemini")
	resp.Providers["gemini"] = providerAuth{
		APIKey:  geminiKey != nil,
		KeyName: geminiKey,
		CLIPath: geminiPath,
	}

	// Ollama: probe the /api/tags endpoint with a short timeout
	ollamaEndpoint := os.Getenv("OLLAMA_HOST")
	if ollamaEndpoint == "" {
		ollamaEndpoint = "http://localhost:11434"
	}
	resp.Ollama = ollamaAuth{Endpoint: ollamaEndpoint}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaEndpoint+"/api/tags", nil); err == nil {
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
			resp.Ollama.Reachable = res.StatusCode == http.StatusOK
		}
	}

	writeJSON(w, resp)
}

// handlePlaygroundChat starts an inference session and begins streaming events.
// POST /api/playground/chat
func (s *Server) handlePlaygroundChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req playgroundChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		writeJSONError(w, "sessionId is required", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		writeJSONError(w, "message is required", http.StatusBadRequest)
		return
	}

	// Resolve agent config and system prompt
	systemPrompt, err := s.resolveAgentSystemPrompt(req.AgentID)
	if err != nil {
		writeJSONError(w, "failed to resolve agent: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get or create session
	session := s.playgroundSessions.GetOrCreate(req.SessionID)

	// Build LLM client
	llmClient, err := s.buildLLMClient(req.AuthMode, req.Model, req.OllamaURL)
	if err != nil {
		writeJSONError(w, "unsupported auth mode or model: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get tools for this agent
	tools, err := s.agentTools(req.AgentID)
	if err != nil {
		tools = []agent.Tool{} // non-fatal — proceed without tools
	}

	// Start inference in a goroutine; reject if already active
	ctx, cancel := context.WithCancel(context.Background())
	if !session.StartInference(cancel) {
		cancel()
		llmClient.Close()
		writeJSONError(w, "session already has active inference", http.StatusConflict)
		return
	}

	// Add user message to history before starting goroutine
	session.AddMessage("user", req.Message)
	historySnapshot := session.History()

	go func() {
		defer session.FinishInference()
		defer llmClient.Close()

		finalResponse, toolTurns, err := llmClient.Stream(ctx, systemPrompt, historySnapshot, tools, &gatewayToolCaller{gateway: s.gateway}, session.WriteChan())
		if err != nil && ctx.Err() == nil {
			// Ensure error is visible via SSE if not already sent
			session.Send(agent.LLMEvent{Type: agent.EventTypeError, Data: agent.ErrorData{Message: err.Error()}})
			session.Send(agent.LLMEvent{Type: agent.EventTypeDone})
		}
		// Persist intermediate tool-use and tool-result turns first so subsequent
		// requests see the full conversation context including tool interactions.
		for _, m := range toolTurns {
			session.AddTurn(m)
		}
		if finalResponse != "" {
			session.AddMessage("assistant", finalResponse)
		}
	}()

	writeJSON(w, map[string]string{"sessionId": req.SessionID, "status": "processing"})
}

// handlePlaygroundStream streams SSE events for a session.
// GET /api/playground/stream?sessionId=xxx
func (s *Server) handlePlaygroundStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeJSONError(w, "sessionId query parameter required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Get or create session so the channel is ready before chat starts
	session := s.playgroundSessions.GetOrCreate(sessionID)
	events := session.Events()

	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handlePlaygroundSessionDelete destroys a session and cleans up resources.
// DELETE /api/playground/session/{id}
func (s *Server) handlePlaygroundSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/playground/session/")
	if id == "" {
		writeJSONError(w, "session ID required", http.StatusBadRequest)
		return
	}

	s.playgroundSessions.Delete(id)
	w.WriteHeader(http.StatusNoContent)
}

// handlePlayground routes all /api/playground/ requests.
func (s *Server) handlePlayground(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/playground")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "auth":
		s.handlePlaygroundAuth(w, r)
	case path == "chat":
		s.handlePlaygroundChat(w, r)
	case path == "stream":
		s.handlePlaygroundStream(w, r)
	case path == "agent" && r.Method == http.MethodPatch:
		s.handlePlaygroundAgentPatch(w, r)
	case strings.HasPrefix(path, "session/"):
		s.handlePlaygroundSessionDelete(w, r)
	default:
		writeJSONError(w, "not found", http.StatusNotFound)
	}
}

// resolveAgentSystemPrompt loads the stack and expands the agent's system prompt.
// It uses config.ExpandString to resolve ${vault:KEY} and ${VAR} references.
func (s *Server) resolveAgentSystemPrompt(agentID string) (string, error) {
	if s.stackFile == "" || agentID == "" {
		return "", nil
	}

	// Load stack without vault expansion to get raw template values
	stack, err := config.LoadStack(s.stackFile)
	if err != nil {
		return "", fmt.Errorf("loading stack: %w", err)
	}

	for _, a := range stack.Agents {
		if a.Name != agentID {
			continue
		}
		if a.Prompt == "" {
			return "", nil
		}
		// Expand ${vault:KEY} and ${VAR} references using config.ExpandString
		var resolver config.Resolver
		if s.vaultStore != nil {
			resolver = config.VaultResolver(s.vaultStore)
		} else {
			resolver = config.EnvResolver()
		}
		expanded, _, _ := config.ExpandString(a.Prompt, resolver)
		return expanded, nil
	}
	return "", nil
}

// agentTools returns the tools available to an agent via the MCP gateway.
func (s *Server) agentTools(agentID string) ([]agent.Tool, error) {
	if s.gateway == nil {
		return nil, nil
	}
	result, err := s.gateway.HandleToolsListForAgent(agentID)
	if err != nil {
		return nil, err
	}
	tools := make([]agent.Tool, 0, len(result.Tools))
	for _, t := range result.Tools {
		serverName, _, _ := mcp.ParsePrefixedTool(t.Name)
		tools = append(tools, agent.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			ServerName:  serverName,
		})
	}
	return tools, nil
}

// buildLLMClient constructs the appropriate LLM client based on auth mode and model.
func (s *Server) buildLLMClient(authMode agent.AuthMode, model, ollamaURL string) (agent.LLMClient, error) {
	switch authMode {
	case agent.AuthModeAPIKey, "": // default to API key
		return s.buildAPIKeyClient(model)
	case agent.AuthModeLocalLLM:
		return agent.NewLocalLLMClient(ollamaURL, model), nil
	default:
		return nil, fmt.Errorf("unsupported auth mode: %s", authMode)
	}
}

// buildAPIKeyClient resolves API keys from vault and creates the appropriate provider client.
func (s *Server) buildAPIKeyClient(model string) (agent.LLMClient, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "claude-3-5-sonnet-latest"
	}

	provider := inferProviderFromModel(model)

	getKey := func(vaultKey, envKey string) (string, error) {
		if s.vaultStore != nil && !s.vaultStore.IsLocked() {
			if k, ok := s.vaultStore.Get(vaultKey); ok && k != "" {
				return k, nil
			}
		}
		if k := os.Getenv(envKey); k != "" {
			return k, nil
		}
		return "", fmt.Errorf("%s not found in vault or environment", vaultKey)
	}

	switch provider {
	case "anthropic":
		key, err := getKey("ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY")
		if err != nil {
			return nil, err
		}
		return agent.NewAnthropicClient(key, model), nil
	case "openai":
		key, err := getKey("OPENAI_API_KEY", "OPENAI_API_KEY")
		if err != nil {
			return nil, err
		}
		return agent.NewLocalLLMClientWithKey("https://api.openai.com/v1", model, key), nil
	default:
		return nil, fmt.Errorf("cannot determine provider for model %q", model)
	}
}

// inferProviderFromModel maps a model name to its provider.
func inferProviderFromModel(model string) string {
	switch {
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "gpt-"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"), strings.HasPrefix(model, "o4"):
		return "openai"
	case strings.HasPrefix(model, "gemini"):
		return "gemini"
	default:
		return "anthropic" // safe default
	}
}

// gatewayToolCaller wraps *mcp.Gateway to implement agent.ToolCaller.
type gatewayToolCaller struct {
	gateway *mcp.Gateway
}

func (g *gatewayToolCaller) CallTool(ctx context.Context, name string, args map[string]any) (agent.ToolCallResponse, error) {
	result, err := g.gateway.CallTool(ctx, name, args)
	if err != nil {
		return agent.ToolCallResponse{IsError: true}, err
	}
	serverName, _, _ := mcp.ParsePrefixedTool(name)
	return agent.ToolCallResponse{
		Content:    extractTextFromContent(result.Content),
		ServerName: serverName,
		IsError:    result.IsError,
	}, nil
}

// marshalStackYAML serializes the stack to YAML bytes.
func marshalStackYAML(stack *config.Stack) ([]byte, error) {
	return yaml.Marshal(stack)
}

// patchAgentRequest is the body for PATCH /api/playground/agent.
type patchAgentRequest struct {
	AgentID string               `json:"agentId"`
	Prompt  string               `json:"prompt"`
	Uses    []config.ToolSelector `json:"uses"`
}

// handlePlaygroundAgentPatch updates an agent's prompt and uses in the stack YAML.
// PATCH /api/playground/agent
func (s *Server) handlePlaygroundAgentPatch(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "no stack file configured", http.StatusServiceUnavailable)
		return
	}

	var req patchAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.AgentID == "" {
		writeJSONError(w, "agentId is required", http.StatusBadRequest)
		return
	}

	stack, err := config.LoadStack(s.stackFile)
	if err != nil {
		writeJSONError(w, "failed to load stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	found := false
	for i := range stack.Agents {
		if stack.Agents[i].Name != req.AgentID {
			continue
		}
		stack.Agents[i].Prompt = req.Prompt
		if req.Uses != nil {
			stack.Agents[i].Uses = req.Uses
		}
		found = true
		break
	}

	if !found {
		writeJSONError(w, "agent not found: "+req.AgentID, http.StatusNotFound)
		return
	}

	data, err := marshalStackYAML(stack)
	if err != nil {
		writeJSONError(w, "failed to serialize stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(s.stackFile, data, 0o644); err != nil {
		writeJSONError(w, "failed to write stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

// extractTextFromContent concatenates text content from MCP tool result content blocks.
func extractTextFromContent(content []mcp.Content) string {
	var sb strings.Builder
	for _, c := range content {
		if c.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}
