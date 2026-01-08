package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client communicates with a remote A2A agent.
type Client struct {
	name       string
	endpoint   string
	httpClient *http.Client
	requestID  atomic.Int64

	mu          sync.RWMutex
	agentCard   *AgentCard
	available   bool
	authType    string
	authToken   string
	authHeader  string
}

// NewClient creates a new A2A client.
func NewClient(name, endpoint string) *Client {
	return &Client{
		name:     name,
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		authHeader: "Authorization",
	}
}

// SetAuth configures authentication for the client.
func (c *Client) SetAuth(authType, token, headerName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authType = authType
	c.authToken = token
	if headerName != "" {
		c.authHeader = headerName
	}
}

// Name returns the local name alias for this agent.
func (c *Client) Name() string {
	return c.name
}

// Endpoint returns the agent's endpoint URL.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// AgentCard returns the cached agent card.
func (c *Client) AgentCard() *AgentCard {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agentCard
}

// IsAvailable returns true if the agent is reachable.
func (c *Client) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.available
}

// FetchAgentCard retrieves the agent card from /.well-known/agent.json
func (c *Client) FetchAgentCard(ctx context.Context) (*AgentCard, error) {
	cardURL := c.endpoint
	// If endpoint doesn't end with agent.json, assume it's base URL
	if !strings.HasSuffix(cardURL, "agent.json") {
		cardURL = strings.TrimSuffix(cardURL, "/") + "/.well-known/agent.json"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", cardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.mu.Lock()
		c.available = false
		c.mu.Unlock()
		return nil, fmt.Errorf("fetching agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.mu.Lock()
		c.available = false
		c.mu.Unlock()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("decoding agent card: %w", err)
	}

	c.mu.Lock()
	c.agentCard = &card
	c.available = true
	c.mu.Unlock()

	return &card, nil
}

// SendMessage sends a message to the remote agent.
func (c *Client) SendMessage(ctx context.Context, params SendMessageParams) (*SendMessageResult, error) {
	var result SendMessageResult
	if err := c.call(ctx, MethodSendMessage, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetTask retrieves task status.
func (c *Client) GetTask(ctx context.Context, taskID string, historyLength int) (*Task, error) {
	params := GetTaskParams{ID: taskID, HistoryLength: historyLength}
	var task Task
	if err := c.call(ctx, MethodGetTask, params, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks lists tasks matching the criteria.
func (c *Client) ListTasks(ctx context.Context, params ListTasksParams) (*ListTasksResult, error) {
	var result ListTasksResult
	if err := c.call(ctx, MethodListTasks, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelTask cancels a running task.
func (c *Client) CancelTask(ctx context.Context, taskID string) (*Task, error) {
	params := CancelTaskParams{ID: taskID}
	var task Task
	if err := c.call(ctx, MethodCancelTask, params, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// call performs a JSON-RPC call to the A2A endpoint.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	// Build JSON-RPC request
	id := c.requestID.Add(1)
	idBytes, _ := json.Marshal(id)

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(idBytes),
		"method":  method,
	}
	if params != nil {
		reqBody["params"] = params
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	// Use the URL from the agent card for RPC calls, or fall back to endpoint
	rpcURL := c.endpoint
	c.mu.RLock()
	if c.agentCard != nil && c.agentCard.URL != "" {
		rpcURL = c.agentCard.URL
	}
	authType := c.authType
	authToken := c.authToken
	authHeader := c.authHeader
	c.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add auth header if configured
	if authType == "bearer" && authToken != "" {
		req.Header.Set(authHeader, "Bearer "+authToken)
	} else if authType == "api_key" && authToken != "" {
		req.Header.Set(authHeader, authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.mu.Lock()
		c.available = false
		c.mu.Unlock()
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse JSON-RPC response
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if result != nil && len(rpcResp.Result) > 0 {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("unmarshaling result: %w", err)
		}
	}

	c.mu.Lock()
	c.available = true
	c.mu.Unlock()

	return nil
}

// Ping checks if the agent is reachable by fetching its agent card.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.FetchAgentCard(ctx)
	return err
}
