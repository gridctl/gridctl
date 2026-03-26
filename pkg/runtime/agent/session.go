// Package agent manages test flight sessions for the Visual Agent Builder playground.
// Each session represents one playground conversation and owns the event channel
// that the SSE stream handler reads from.
package agent

import (
	"context"
	"encoding/json"
	"sync"
)

// AuthMode specifies how the playground authenticates with the LLM provider.
type AuthMode string

const (
	AuthModeAPIKey   AuthMode = "API_KEY"
	AuthModeCLIProxy AuthMode = "CLI_PROXY"
	AuthModeLocalLLM AuthMode = "LOCAL_LLM"
)

// EventType identifies the type of a streaming event.
type EventType string

const (
	EventTypeToken         EventType = "token"
	EventTypeToolCallStart EventType = "tool_call_start"
	EventTypeToolCallEnd   EventType = "tool_call_end"
	EventTypeMetrics       EventType = "metrics"
	EventTypeDone          EventType = "done"
	EventTypeError         EventType = "error"
)

// LLMEvent is a single event emitted during LLM inference and forwarded over SSE.
type LLMEvent struct {
	Type EventType `json:"type"`
	Data any       `json:"data,omitempty"`
}

// TokenData carries a streamed text token.
type TokenData struct {
	Text string `json:"text"`
}

// ToolCallStartData carries tool call initiation data.
type ToolCallStartData struct {
	ToolName   string `json:"toolName"`
	ServerName string `json:"serverName"`
	Input      any    `json:"input"`
}

// ToolCallEndData carries tool call completion data.
type ToolCallEndData struct {
	ToolName   string `json:"toolName"`
	Output     any    `json:"output"`
	DurationMs int64  `json:"durationMs"`
}

// MetricsData carries per-turn token usage metrics.
type MetricsData struct {
	TokensIn         int     `json:"tokens_in"`
	TokensOut        int     `json:"tokens_out"`
	FormatSavingsPct float64 `json:"format_savings_pct"`
}

// ErrorData carries error information.
type ErrorData struct {
	Message string `json:"message"`
}

// ToolCallBlock represents a single tool invocation within an assistant message.
type ToolCallBlock struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON string of the tool arguments
}

// Message is a single turn in the conversation history.
// Simple text turns use Role+Content. Tool-use assistant turns additionally
// populate ToolCalls. Tool-result turns use Role=="tool" with ToolCallID.
// RawParam holds a provider-specific serialized message param for turns that
// require accurate round-trip reconstruction (e.g. Anthropic tool-use turns).
type Message struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []ToolCallBlock `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	RawParam   json.RawMessage `json:"rawParam,omitempty"`
}

// Tool is a minimal MCP tool definition for LLM clients.
// ServerName is parsed from the prefixed tool name (server__tool format).
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
	ServerName  string          `json:"serverName"`
}

// ToolCallResponse is the result of a tool call execution.
type ToolCallResponse struct {
	Content    string
	ServerName string
	IsError    bool
}

// ToolCaller executes MCP tool calls through the gateway.
type ToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (ToolCallResponse, error)
}

// LLMClient abstracts over LLM provider implementations.
type LLMClient interface {
	// Stream runs the full agentic loop (potentially multiple LLM calls due to tool use).
	// It emits events to the channel, returns the final text response, and returns any
	// intermediate tool turns (assistant tool-use + tool-result messages) for history
	// persistence. The caller is responsible for persisting all turns to session history.
	// The caller must not close events — Stream only writes to it.
	Stream(ctx context.Context, systemPrompt string, history []Message, tools []Tool, caller ToolCaller, events chan<- LLMEvent) (finalResponse string, toolTurns []Message, err error)
	// Close releases any held resources.
	Close() error
}

// TestFlightSession manages a single playground conversation.
// All fields are protected by mu.
type TestFlightSession struct {
	mu      sync.Mutex
	ID      string
	history []Message
	events  chan LLMEvent
	cancel  context.CancelFunc
	active  bool // true while inference is running
}

// NewSession creates a new TestFlightSession with a buffered event channel.
func NewSession(id string) *TestFlightSession {
	return &TestFlightSession{
		ID:     id,
		events: make(chan LLMEvent, 512),
	}
}

// Events returns the receive-only event channel for the SSE stream handler.
func (s *TestFlightSession) Events() <-chan LLMEvent {
	return s.events
}

// WriteChan returns the write-only event channel for inference goroutines.
func (s *TestFlightSession) WriteChan() chan<- LLMEvent {
	return s.events
}

// Send emits an event; drops it if the buffer is full rather than blocking.
func (s *TestFlightSession) Send(e LLMEvent) {
	select {
	case s.events <- e:
	default:
	}
}

// History returns a snapshot of the conversation history.
func (s *TestFlightSession) History() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := make([]Message, len(s.history))
	copy(h, s.history)
	return h
}

// AddMessage appends a plain text message to the conversation history.
func (s *TestFlightSession) AddMessage(role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, Message{Role: role, Content: content})
}

// AddTurn appends a structured message (including tool-use and tool-result turns)
// to the conversation history.
func (s *TestFlightSession) AddTurn(m Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, m)
}

// ResetHistory clears conversation history and creates a fresh event channel.
func (s *TestFlightSession) ResetHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	s.events = make(chan LLMEvent, 512)
}

// StartInference marks the session as active and stores the cancel func.
// Returns false if inference is already running.
func (s *TestFlightSession) StartInference(cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return false
	}
	s.active = true
	s.cancel = cancel
	return true
}

// FinishInference marks inference as complete.
func (s *TestFlightSession) FinishInference() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = false
	s.cancel = nil
}

// Cancel cancels any active inference.
func (s *TestFlightSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.active = false
}

// SessionRegistry manages active TestFlightSessions.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*TestFlightSession
}

// NewSessionRegistry creates a new empty SessionRegistry.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[string]*TestFlightSession),
	}
}

// GetOrCreate returns the session for id, creating it if it does not exist.
func (r *SessionRegistry) GetOrCreate(id string) *TestFlightSession {
	r.mu.RLock()
	s, ok := r.sessions[id]
	r.mu.RUnlock()
	if ok {
		return s
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok = r.sessions[id]; ok {
		return s
	}
	s = NewSession(id)
	r.sessions[id] = s
	return s
}

// Get returns a session by ID.
func (r *SessionRegistry) Get(id string) (*TestFlightSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Delete removes and cancels the session with the given ID.
func (r *SessionRegistry) Delete(id string) {
	r.mu.Lock()
	s, ok := r.sessions[id]
	delete(r.sessions, id)
	r.mu.Unlock()
	if ok {
		s.Cancel()
	}
}
