// Package anthropic implements agent.ChatModel against the Anthropic
// Messages API. The package uses only net/http and encoding/json.
//
// Wire reference:
//   - https://docs.anthropic.com/en/api/messages
//   - https://docs.anthropic.com/en/api/messages-streaming
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gridctl/gridctl/pkg/agent"
)

// DefaultBaseURL is the production Anthropic API endpoint. Override
// via Option WithBaseURL for tests or proxies.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultAPIVersion is the value for the anthropic-version header. The
// default is the most recent stable version that supports tool_use,
// prompt caching, and the streaming-message format the package parses.
const DefaultAPIVersion = "2023-06-01"

// DefaultMaxTokens is the value used for ChatRequest.MaxTokens when
// the caller did not set one. Anthropic requires a value; gridctl's
// default keeps a single round-trip well under the model context
// window for typical agentic loops.
const DefaultMaxTokens = 4096

// Provider implements agent.ChatModel against the Anthropic Messages
// API. Construct with New; do not zero-value.
type Provider struct {
	apiKey     string
	baseURL    string
	apiVersion string
	httpClient *http.Client
	logger     *slog.Logger
}

// Option configures a Provider during construction.
type Option func(*Provider)

// WithBaseURL overrides the API base URL. Useful for tests, proxies,
// or pointing at an Anthropic-compatible third-party endpoint.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// WithAPIVersion overrides the anthropic-version header value.
// Defaults to DefaultAPIVersion.
func WithAPIVersion(v string) Option {
	return func(p *Provider) { p.apiVersion = v }
}

// WithHTTPClient overrides the underlying HTTP client. Tests inject
// a roundtripper here to capture requests and stub responses.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) { p.httpClient = c }
}

// WithLogger overrides the slog.Logger. Defaults to slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(p *Provider) { p.logger = l }
}

// New constructs a Provider authenticated with the given API key. The
// key is required; an empty key returns an error so misconfiguration
// surfaces at construction rather than on first request.
func New(apiKey string, opts ...Option) (*Provider, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic: api key is required")
	}
	p := &Provider{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		apiVersion: DefaultAPIVersion,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// messageRequest is the on-wire request body. Field names match the
// Anthropic API exactly; do not rename without checking the wire spec.
type messageRequest struct {
	Model         string         `json:"model"`
	Messages      []wireMessage  `json:"messages"`
	System        string         `json:"system,omitempty"`
	MaxTokens     int            `json:"max_tokens"`
	Temperature   *float64       `json:"temperature,omitempty"`
	Tools         []wireTool     `json:"tools,omitempty"`
	StopSequences []string       `json:"stop_sequences,omitempty"`
	Stream        bool           `json:"stream,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// wireMessage is the on-wire message envelope. Anthropic groups text
// and tool_use as content blocks within a single message; the package
// helpers in messages.go translate agent.Message into this shape.
type wireMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

// contentBlock is one item in wireMessage.Content. The Type field
// discriminates the active fields: "text", "tool_use", "tool_result".
type contentBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// Content carries the tool result body. Anthropic accepts either
	// a string or a content-block array. The package always emits a
	// string; deserializing is unused on this side.
	Content string `json:"content,omitempty"`
}

// messageResponse is the on-wire non-streaming response body.
type messageResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      wireUsage      `json:"usage"`
}

// wireUsage is the on-wire usage block. Anthropic reports cache tokens
// separately from input tokens; the package preserves the split when
// translating to agent.Usage.
type wireUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// errorResponse is the on-wire error envelope. Anthropic returns it
// for 4xx and 5xx alike; the type and message fields drive the error
// surfaced to callers.
type errorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate dispatches a synchronous Messages request and translates
// the response into agent.ChatResponse.
func (p *Provider) Generate(ctx context.Context, req agent.ChatRequest) (agent.ChatResponse, error) {
	wire, err := p.buildRequest(req, false)
	if err != nil {
		return agent.ChatResponse{}, err
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("anthropic: build http request: %w", err)
	}
	p.setHeaders(httpReq, false)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("anthropic: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agent.ChatResponse{}, parseError(resp)
	}

	var raw messageResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return agent.ChatResponse{}, fmt.Errorf("anthropic: decode response: %w", err)
	}

	out := translateResponse(raw)
	p.logger.LogAttrs(ctx, slog.LevelDebug, "anthropic.generate",
		slog.String("model", out.Model),
		slog.Int("input_tokens", out.Usage.InputTokens),
		slog.Int("output_tokens", out.Usage.OutputTokens),
		slog.String("stop_reason", string(out.StopReason)),
	)
	return out, nil
}

// Stream dispatches a streaming Messages request and returns a reader
// that emits agent.ChatChunk values as the SSE stream progresses.
func (p *Provider) Stream(ctx context.Context, req agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	wire, err := p.buildRequest(req, true)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build http request: %w", err)
	}
	p.setHeaders(httpReq, true)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: http request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, parseError(resp)
	}

	chunks, err := readStream(resp.Body, p.logger)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	return agent.StreamReaderFromSlice(chunks), nil
}

// buildRequest validates a ChatRequest and constructs the on-wire
// envelope. It is shared by Generate and Stream.
func (p *Provider) buildRequest(req agent.ChatRequest, stream bool) (*messageRequest, error) {
	if req.Model == "" {
		return nil, errors.New("anthropic: model is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("anthropic: messages is required")
	}
	if req.Temperature < 0 {
		return nil, errors.New("anthropic: temperature must be non-negative")
	}

	wireMsgs, err := translateMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	out := &messageRequest{
		Model:         req.Model,
		Messages:      wireMsgs,
		System:        req.System,
		MaxTokens:     maxTokens,
		StopSequences: req.StopSequences,
		Stream:        stream,
	}
	if req.Temperature > 0 {
		t := req.Temperature
		out.Temperature = &t
	}
	if len(req.Tools) > 0 {
		out.Tools = translateTools(req.Tools)
	}
	return out, nil
}

// setHeaders applies authentication and protocol headers to the
// outbound request. Stream-mode adds Accept: text/event-stream so
// the API confirms SSE intent.
func (p *Provider) setHeaders(r *http.Request, stream bool) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("x-api-key", p.apiKey)
	r.Header.Set("anthropic-version", p.apiVersion)
	if stream {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
}

// parseError reads a non-200 response body and translates it into a Go
// error preserving the provider type and message. Best-effort: on
// malformed bodies it falls back to the HTTP status text.
func parseError(resp *http.Response) error {
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var er errorResponse
	if err := json.Unmarshal(raw, &er); err == nil && er.Error.Message != "" {
		return fmt.Errorf("anthropic: %s (%s, http %d)", er.Error.Message, er.Error.Type, resp.StatusCode)
	}
	if len(raw) > 0 {
		return fmt.Errorf("anthropic: http %d: %s", resp.StatusCode, string(raw))
	}
	return fmt.Errorf("anthropic: http %d %s", resp.StatusCode, resp.Status)
}

// translateResponse converts an on-wire messageResponse into an
// agent.ChatResponse. Tool-use blocks become ToolCall entries; text
// blocks are concatenated into Content.
func translateResponse(raw messageResponse) agent.ChatResponse {
	var content string
	var calls []agent.ToolCall

	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			calls = append(calls, agent.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: input,
			})
		}
	}

	return agent.ChatResponse{
		Model:      raw.Model,
		Content:    content,
		ToolCalls:  calls,
		StopReason: translateStopReason(raw.StopReason),
		Usage: agent.Usage{
			InputTokens:      raw.Usage.InputTokens,
			OutputTokens:     raw.Usage.OutputTokens,
			CacheReadTokens:  raw.Usage.CacheReadInputTokens,
			CacheWriteTokens: raw.Usage.CacheCreationInputTokens,
		},
	}
}

// translateStopReason maps Anthropic stop reasons into the
// gridctl-shaped vocabulary. Unknown reasons fall through to
// agent.StopReasonEnd so the caller can still reason about completion;
// the slog warning surfaces the unmapped value.
func translateStopReason(s string) agent.StopReason {
	switch s {
	case "end_turn":
		return agent.StopReasonEnd
	case "max_tokens":
		return agent.StopReasonMaxTokens
	case "tool_use":
		return agent.StopReasonToolUse
	case "stop_sequence":
		return agent.StopReasonStopSequence
	case "":
		return agent.StopReasonEnd
	default:
		return agent.StopReasonEnd
	}
}
