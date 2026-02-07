package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// maxRequestBodySize is the maximum allowed size for incoming request bodies (1MB).
// Matches mcp.MaxRequestBodySize; duplicated to avoid cross-package dependency.
const maxRequestBodySize = 1 * 1024 * 1024

// TaskHandler is a function that processes A2A messages for a local agent.
type TaskHandler func(ctx context.Context, task *Task, msg *Message) (*Task, error)

// LocalAgent represents an A2A agent served by this gateway.
type LocalAgent struct {
	Card    AgentCard
	Handler TaskHandler
}

// Handler provides HTTP handlers for A2A protocol endpoints.
type Handler struct {
	mu          sync.RWMutex
	baseURL     string
	localAgents map[string]*LocalAgent // name -> local agent
	tasks       map[string]*Task       // taskID -> task
}

// NewHandler creates a new A2A HTTP handler.
func NewHandler(baseURL string) *Handler {
	return &Handler{
		baseURL:     baseURL,
		localAgents: make(map[string]*LocalAgent),
		tasks:       make(map[string]*Task),
	}
}

// RegisterLocalAgent registers an agent that this gateway serves.
func (h *Handler) RegisterLocalAgent(name string, agent *LocalAgent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Set the agent's URL if not already set
	if agent.Card.URL == "" {
		agent.Card.URL = fmt.Sprintf("%s/a2a/%s", h.baseURL, name)
	}

	h.localAgents[name] = agent
}

// UnregisterLocalAgent removes a local agent.
func (h *Handler) UnregisterLocalAgent(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.localAgents, name)
}

// GetLocalAgent returns a local agent by name.
func (h *Handler) GetLocalAgent(name string) *LocalAgent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.localAgents[name]
}

// ListLocalAgents returns all registered local agents.
func (h *Handler) ListLocalAgents() []AgentCard {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cards := make([]AgentCard, 0, len(h.localAgents))
	for _, agent := range h.localAgents {
		cards = append(cards, agent.Card)
	}
	return cards
}

// ServeHTTP routes A2A HTTP requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route based on path:
	// GET  /.well-known/agent.json - List all agent cards
	// GET  /a2a/{agent} - Get specific agent card
	// POST /a2a/{agent} - JSON-RPC endpoint for agent
	switch {
	case path == "/.well-known/agent.json" && r.Method == http.MethodGet:
		h.handleAgentCardList(w, r)
	case strings.HasPrefix(path, "/a2a/"):
		agentName := strings.TrimPrefix(path, "/a2a/")
		agentName = strings.Split(agentName, "/")[0] // Get first path segment
		if agentName == "" {
			h.handleAgentsList(w, r)
		} else {
			switch r.Method {
			case http.MethodGet:
				h.handleAgentCard(w, r, agentName)
			case http.MethodPost:
				h.handleAgentRPC(w, r, agentName)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		}
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleAgentCardList returns all agent cards (for discovery).
func (h *Handler) handleAgentCardList(w http.ResponseWriter, r *http.Request) {
	cards := h.ListLocalAgents()

	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"agents": cards,
	}
	_ = json.NewEncoder(w).Encode(response)
}

// handleAgentsList returns a summary of all agents.
func (h *Handler) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	type AgentSummary struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		URL         string `json:"url"`
		SkillCount  int    `json:"skillCount"`
	}

	summaries := make([]AgentSummary, 0, len(h.localAgents))
	for name, agent := range h.localAgents {
		summaries = append(summaries, AgentSummary{
			Name:        name,
			Description: agent.Card.Description,
			URL:         agent.Card.URL,
			SkillCount:  len(agent.Card.Skills),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summaries)
}

// handleAgentCard returns a specific agent's card.
func (h *Handler) handleAgentCard(w http.ResponseWriter, r *http.Request, agentName string) {
	agent := h.GetLocalAgent(agentName)
	if agent == nil {
		http.Error(w, "Agent not found: "+agentName, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agent.Card)
}

// handleAgentRPC handles JSON-RPC requests for a specific agent.
func (h *Handler) handleAgentRPC(w http.ResponseWriter, r *http.Request, agentName string) {
	agent := h.GetLocalAgent(agentName)
	if agent == nil {
		h.writeError(w, nil, MethodNotFound, "Agent not found: "+agentName)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, nil, ParseError, "Failed to read request body")
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeError(w, nil, ParseError, "Invalid JSON")
		return
	}

	resp := h.handleMethod(r.Context(), agent, &req)
	h.writeResponse(w, resp)
}

// handleMethod routes the JSON-RPC method to the appropriate handler.
func (h *Handler) handleMethod(ctx context.Context, agent *LocalAgent, req *Request) Response {
	switch req.Method {
	case MethodSendMessage:
		return h.handleSendMessage(ctx, agent, req)
	case MethodGetTask:
		return h.handleGetTask(ctx, req)
	case MethodListTasks:
		return h.handleListTasks(ctx, req)
	case MethodCancelTask:
		return h.handleCancelTask(ctx, req)
	default:
		return NewErrorResponse(req.ID, MethodNotFound, "Unknown method: "+req.Method)
	}
}

// handleSendMessage handles the message/send method.
func (h *Handler) handleSendMessage(ctx context.Context, agent *LocalAgent, req *Request) Response {
	var params SendMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid SendMessage params")
	}

	// Generate message ID if not set
	if params.Message.ID == "" {
		params.Message.ID = uuid.New().String()
	}

	// Create or get task
	task := h.createTask(params.ContextID)
	task.Messages = append(task.Messages, params.Message)
	task.Status = TaskStatus{State: TaskStateWorking}

	// If there's a handler, invoke it
	if agent.Handler != nil {
		updatedTask, err := agent.Handler(ctx, task, &params.Message)
		if err != nil {
			task.Status = TaskStatus{
				State:   TaskStateFailed,
				Message: err.Error(),
			}
		} else if updatedTask != nil {
			task = updatedTask
		}
	} else {
		// No handler - mark as completed with acknowledgment
		task.Status = TaskStatus{State: TaskStateCompleted}
		task.Messages = append(task.Messages, NewTextMessage(RoleAgent, "Message received"))
	}

	h.updateTask(task)

	result := SendMessageResult{Task: task}
	return NewSuccessResponse(req.ID, result)
}

// handleGetTask handles the tasks/get method.
func (h *Handler) handleGetTask(ctx context.Context, req *Request) Response {
	var params GetTaskParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid GetTask params")
	}

	task := h.getTask(params.ID)
	if task == nil {
		return NewErrorResponse(req.ID, ErrTaskNotFound, "Task not found: "+params.ID)
	}

	// Optionally limit message history
	if params.HistoryLength > 0 && len(task.Messages) > params.HistoryLength {
		task.Messages = task.Messages[len(task.Messages)-params.HistoryLength:]
	}

	return NewSuccessResponse(req.ID, task)
}

// handleListTasks handles the tasks/list method.
func (h *Handler) handleListTasks(ctx context.Context, req *Request) Response {
	var params ListTasksParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid ListTasks params")
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	var tasks []Task
	for _, task := range h.tasks {
		// Filter by context ID if specified
		if params.ContextID != "" && task.ContextID != params.ContextID {
			continue
		}
		// Filter by status if specified
		if params.Status != "" && task.Status.State != params.Status {
			continue
		}
		tasks = append(tasks, *task)
	}

	// Apply pagination
	if params.PageSize > 0 && len(tasks) > params.PageSize {
		tasks = tasks[:params.PageSize]
	}

	result := ListTasksResult{Tasks: tasks}
	return NewSuccessResponse(req.ID, result)
}

// handleCancelTask handles the tasks/cancel method.
func (h *Handler) handleCancelTask(ctx context.Context, req *Request) Response {
	var params CancelTaskParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid CancelTask params")
	}

	task := h.getTask(params.ID)
	if task == nil {
		return NewErrorResponse(req.ID, ErrTaskNotFound, "Task not found: "+params.ID)
	}

	if task.Status.State.IsTerminal() {
		return NewErrorResponse(req.ID, ErrTaskNotCancellable, "Task is already in terminal state")
	}

	task.Status = TaskStatus{State: TaskStateCancelled}
	task.UpdatedAt = time.Now()
	h.updateTask(task)

	return NewSuccessResponse(req.ID, task)
}

// createTask creates a new task.
func (h *Handler) createTask(contextID string) *Task {
	h.mu.Lock()
	defer h.mu.Unlock()

	task := &Task{
		ID:        uuid.New().String(),
		ContextID: contextID,
		Status:    TaskStatus{State: TaskStateSubmitted},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	h.tasks[task.ID] = task
	return task
}

// getTask retrieves a task by ID.
func (h *Handler) getTask(taskID string) *Task {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.tasks[taskID]
}

// updateTask updates a task in the store.
func (h *Handler) updateTask(task *Task) {
	h.mu.Lock()
	defer h.mu.Unlock()
	task.UpdatedAt = time.Now()
	h.tasks[task.ID] = task
}

// CleanupTasks removes terminal tasks older than maxAge. Returns count removed.
func (h *Handler) CleanupTasks(maxAge time.Duration) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, task := range h.tasks {
		if task.Status.State.IsTerminal() && task.UpdatedAt.Before(cutoff) {
			delete(h.tasks, id)
			removed++
		}
	}
	return removed
}

// TaskCount returns the number of tracked tasks.
func (h *Handler) TaskCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.tasks)
}

// writeError writes a JSON-RPC error response.
func (h *Handler) writeError(w http.ResponseWriter, id *json.RawMessage, code int, message string) {
	h.writeResponse(w, NewErrorResponse(id, code, message))
}

// writeResponse writes a JSON-RPC response.
func (h *Handler) writeResponse(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
