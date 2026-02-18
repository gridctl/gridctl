package a2a

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
)

// Gateway manages A2A agents - both local (server role) and remote (client role).
type Gateway struct {
	mu sync.RWMutex

	// Handler for local agents (server role)
	handler *Handler

	// Remote agents (client role)
	remoteAgents map[string]*Client // name -> client

	logger *slog.Logger
}

// NewGateway creates a new A2A gateway.
func NewGateway(baseURL string, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = logging.NewDiscardLogger()
	}
	return &Gateway{
		handler:      NewHandler(baseURL),
		remoteAgents: make(map[string]*Client),
		logger:       logger,
	}
}

// Handler returns the HTTP handler for A2A endpoints.
func (g *Gateway) Handler() *Handler {
	return g.handler
}

// TaskCount returns the number of tracked A2A tasks.
func (g *Gateway) TaskCount() int {
	return g.handler.TaskCount()
}

// StartCleanup starts periodic cleanup of terminal A2A tasks.
func (g *Gateway) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.handler.CleanupTasks(1 * time.Hour)
			}
		}
	}()
}

// RegisterLocalAgent registers an agent that this gateway serves (server role).
func (g *Gateway) RegisterLocalAgent(name string, card AgentCard, handler TaskHandler) {
	g.handler.RegisterLocalAgent(name, &LocalAgent{
		Card:    card,
		Handler: handler,
	})
}

// UnregisterLocalAgent removes a local agent.
func (g *Gateway) UnregisterLocalAgent(name string) {
	g.handler.UnregisterLocalAgent(name)
}

// RegisterRemoteAgent registers an external A2A agent (client role).
func (g *Gateway) RegisterRemoteAgent(ctx context.Context, name, endpoint string, authType, authToken, authHeader string) error {
	client := NewClient(name, endpoint)

	// Configure auth if provided
	if authType != "" {
		client.SetAuth(authType, authToken, authHeader)
	}

	// Fetch and validate agent card
	card, err := client.FetchAgentCard(ctx)
	if err != nil {
		return fmt.Errorf("fetching agent card from %s: %w", endpoint, err)
	}

	g.mu.Lock()
	g.remoteAgents[name] = client
	g.mu.Unlock()

	g.logger.Info("registered remote A2A agent",
		"name", name,
		"card_name", card.Name,
		"skills", len(card.Skills))
	return nil
}

// UnregisterRemoteAgent removes a remote agent.
func (g *Gateway) UnregisterRemoteAgent(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.remoteAgents, name)
}

// GetRemoteAgent returns a remote agent client by name.
func (g *Gateway) GetRemoteAgent(name string) *Client {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.remoteAgents[name]
}

// ListRemoteAgents returns all registered remote agents.
func (g *Gateway) ListRemoteAgents() []*Client {
	g.mu.RLock()
	defer g.mu.RUnlock()

	clients := make([]*Client, 0, len(g.remoteAgents))
	for _, client := range g.remoteAgents {
		clients = append(clients, client)
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].name < clients[j].name })
	return clients
}

// SendMessage routes a message to the appropriate agent.
func (g *Gateway) SendMessage(ctx context.Context, targetAgent string, params SendMessageParams) (*SendMessageResult, error) {
	// Check local agents first
	if agent := g.handler.GetLocalAgent(targetAgent); agent != nil {
		// Create task for local agent
		task := g.handler.createTask(params.ContextID)
		task.Messages = append(task.Messages, params.Message)

		if agent.Handler != nil {
			updatedTask, err := agent.Handler(ctx, task, &params.Message)
			if err != nil {
				task.Status = TaskStatus{State: TaskStateFailed, Message: err.Error()}
			} else if updatedTask != nil {
				task = updatedTask
			}
		}

		g.handler.updateTask(task)
		return &SendMessageResult{Task: task}, nil
	}

	// Check remote agents
	g.mu.RLock()
	client, ok := g.remoteAgents[targetAgent]
	g.mu.RUnlock()

	if ok {
		return client.SendMessage(ctx, params)
	}

	return nil, fmt.Errorf("unknown A2A agent: %s", targetAgent)
}

// GetTask retrieves a task by ID.
func (g *Gateway) GetTask(ctx context.Context, taskID string) (*Task, error) {
	// Check local tasks first
	if task := g.handler.getTask(taskID); task != nil {
		return task, nil
	}

	// For remote tasks, we'd need to know which agent owns it
	// For now, return not found
	return nil, fmt.Errorf("task not found: %s", taskID)
}

// A2AAgentStatus contains status information for an A2A agent.
type A2AAgentStatus struct {
	Name        string   `json:"name"`
	Role        string   `json:"role"` // "local" or "remote"
	URL         string   `json:"url,omitempty"`
	Endpoint    string   `json:"endpoint,omitempty"`
	Available   bool     `json:"available"`
	SkillCount  int      `json:"skillCount"`
	Skills      []string `json:"skills"`
	Description string   `json:"description,omitempty"`
}

// Status returns status of all A2A agents.
func (g *Gateway) Status() []A2AAgentStatus {
	var statuses []A2AAgentStatus

	// Local agents
	for _, card := range g.handler.ListLocalAgents() {
		skillNames := make([]string, len(card.Skills))
		for i, skill := range card.Skills {
			skillNames[i] = skill.Name
		}

		statuses = append(statuses, A2AAgentStatus{
			Name:        card.Name,
			Role:        "local",
			URL:         card.URL,
			Available:   true, // Local agents are always available
			SkillCount:  len(card.Skills),
			Skills:      skillNames,
			Description: card.Description,
		})
	}

	// Remote agents - collect names and sort for deterministic output
	g.mu.RLock()
	defer g.mu.RUnlock()

	remoteNames := make([]string, 0, len(g.remoteAgents))
	for name := range g.remoteAgents {
		remoteNames = append(remoteNames, name)
	}
	sort.Strings(remoteNames)

	for _, name := range remoteNames {
		client := g.remoteAgents[name]
		status := A2AAgentStatus{
			Name:      name,
			Role:      "remote",
			Endpoint:  client.Endpoint(),
			Available: client.IsAvailable(),
		}

		if card := client.AgentCard(); card != nil {
			status.URL = card.URL
			status.Description = card.Description
			status.SkillCount = len(card.Skills)
			skillNames := make([]string, len(card.Skills))
			for i, skill := range card.Skills {
				skillNames[i] = skill.Name
			}
			status.Skills = skillNames
		}

		statuses = append(statuses, status)
	}

	return statuses
}

// AggregatedSkills returns all skills from all agents (local and remote).
func (g *Gateway) AggregatedSkills() []Skill {
	var skills []Skill

	// Local agent skills
	for _, card := range g.handler.ListLocalAgents() {
		for _, skill := range card.Skills {
			// Prefix skill ID with agent name for disambiguation
			prefixedSkill := skill
			prefixedSkill.ID = card.Name + "/" + skill.ID
			skills = append(skills, prefixedSkill)
		}
	}

	// Remote agent skills - sort by name for deterministic output
	g.mu.RLock()
	defer g.mu.RUnlock()

	remoteNames := make([]string, 0, len(g.remoteAgents))
	for name := range g.remoteAgents {
		remoteNames = append(remoteNames, name)
	}
	sort.Strings(remoteNames)

	for _, name := range remoteNames {
		client := g.remoteAgents[name]
		if card := client.AgentCard(); card != nil {
			for _, skill := range card.Skills {
				prefixedSkill := skill
				prefixedSkill.ID = name + "/" + skill.ID
				skills = append(skills, prefixedSkill)
			}
		}
	}

	return skills
}
