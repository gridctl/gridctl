package a2a

import (
	"encoding/json"
	"time"
)

// ProtocolVersion is the A2A protocol version supported by this implementation.
const ProtocolVersion = "1.0"

// AgentCard represents the A2A Agent Card for discovery.
// Served at /.well-known/agent.json
type AgentCard struct {
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	URL              string            `json:"url"`
	Version          string            `json:"version,omitempty"`
	Provider         *Provider         `json:"provider,omitempty"`
	DocumentationURL string            `json:"documentationUrl,omitempty"`
	Capabilities     AgentCapabilities `json:"capabilities"`
	Skills           []Skill           `json:"skills"`
	DefaultInputModes  []string        `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string        `json:"defaultOutputModes,omitempty"`
}

// Provider describes the organization providing the agent.
type Provider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// AgentCapabilities describes what the agent supports.
// Note: Currently empty as streaming, push notifications, and state transition history
// are not implemented. This struct is retained for A2A protocol compatibility.
type AgentCapabilities struct {
}

// Skill represents a distinct capability the agent exposes.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// TaskState represents the current state of a task.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input_required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCancelled     TaskState = "cancelled"
	TaskStateRejected      TaskState = "rejected"
)

// IsTerminal returns true if the state is a terminal state.
func (s TaskState) IsTerminal() bool {
	switch s {
	case TaskStateCompleted, TaskStateFailed, TaskStateCancelled, TaskStateRejected:
		return true
	}
	return false
}

// Task represents an A2A task.
type Task struct {
	ID        string            `json:"id"`
	ContextID string            `json:"contextId,omitempty"`
	Status    TaskStatus        `json:"status"`
	Messages  []Message         `json:"messages,omitempty"`
	Artifacts []Artifact        `json:"artifacts,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

// TaskStatus represents the status of a task.
type TaskStatus struct {
	State   TaskState `json:"state"`
	Message string    `json:"message,omitempty"`
}

// Message represents a conversational message in A2A.
type Message struct {
	ID       string            `json:"messageId"`
	Role     MessageRole       `json:"role"`
	Parts    []Part            `json:"parts"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MessageRole indicates the sender of a message.
type MessageRole string

const (
	RoleUser  MessageRole = "user"
	RoleAgent MessageRole = "agent"
)

// Part represents a content part in a message.
type Part struct {
	Type     PartType        `json:"type"`
	Text     string          `json:"text,omitempty"`
	File     *FilePart       `json:"file,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// PartType indicates the type of content in a part.
type PartType string

const (
	PartTypeText PartType = "text"
	PartTypeFile PartType = "file"
	PartTypeData PartType = "data"
)

// FilePart represents file content in a message.
type FilePart struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    string `json:"bytes,omitempty"` // base64 encoded
	URI      string `json:"uri,omitempty"`
}

// Artifact represents task output artifacts.
type Artifact struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parts       []Part `json:"parts"`
	Index       int    `json:"index,omitempty"`
}

// NewTextPart creates a text content part.
func NewTextPart(text string) Part {
	return Part{Type: PartTypeText, Text: text}
}

// NewTextMessage creates a message with a single text part.
func NewTextMessage(role MessageRole, text string) Message {
	return Message{
		Role:  role,
		Parts: []Part{NewTextPart(text)},
	}
}

// A2A JSON-RPC method names.
const (
	MethodSendMessage   = "message/send"
	MethodGetTask       = "tasks/get"
	MethodListTasks     = "tasks/list"
	MethodCancelTask    = "tasks/cancel"
)

// SendMessageParams contains parameters for message/send request.
type SendMessageParams struct {
	Message   Message           `json:"message"`
	ContextID string            `json:"contextId,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// SendMessageResult is the response to message/send.
type SendMessageResult struct {
	Task    *Task    `json:"task,omitempty"`
	Message *Message `json:"message,omitempty"`
}

// GetTaskParams contains parameters for tasks/get request.
type GetTaskParams struct {
	ID            string `json:"id"`
	HistoryLength int    `json:"historyLength,omitempty"`
}

// ListTasksParams contains parameters for tasks/list request.
type ListTasksParams struct {
	ContextID string    `json:"contextId,omitempty"`
	Status    TaskState `json:"status,omitempty"`
	PageSize  int       `json:"pageSize,omitempty"`
	PageToken string    `json:"pageToken,omitempty"`
}

// ListTasksResult is the response to tasks/list.
type ListTasksResult struct {
	Tasks         []Task `json:"tasks"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// CancelTaskParams contains parameters for tasks/cancel request.
type CancelTaskParams struct {
	ID string `json:"id"`
}

// JSON-RPC types for A2A protocol.

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// A2A-specific error codes.
const (
	ErrTaskNotFound    = -32001
	ErrTaskNotCancellable = -32002
	ErrUnsupportedOperation = -32003
)

// Default timeouts for A2A operations.
const (
	// DefaultA2ATimeout is the HTTP client timeout for A2A requests.
	DefaultA2ATimeout = 60 * time.Second
)

// NewErrorResponse creates a JSON-RPC error response.
func NewErrorResponse(id *json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}
}

// NewSuccessResponse creates a JSON-RPC success response.
func NewSuccessResponse(id *json.RawMessage, result any) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}
