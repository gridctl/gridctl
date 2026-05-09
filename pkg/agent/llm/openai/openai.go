// Package openai implements agent.ChatModel against the OpenAI Chat
// Completions API. The package uses only net/http and encoding/json.
//
// Wire reference:
//   - https://platform.openai.com/docs/api-reference/chat/create
//   - https://platform.openai.com/docs/api-reference/chat/streaming
package openai

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

// DefaultBaseURL is the production OpenAI API endpoint. Override via
// Option WithBaseURL for tests, proxies, or OpenAI-compatible
// endpoints (Azure OpenAI, vLLM, llama.cpp, Ollama in OpenAI mode).
const DefaultBaseURL = "https://api.openai.com"

// Provider implements agent.ChatModel against the OpenAI Chat
// Completions API. Construct with New; do not zero-value.
type Provider struct {
	apiKey       string
	organization string
	baseURL      string
	httpClient   *http.Client
	logger       *slog.Logger
}

// Option configures a Provider during construction.
type Option func(*Provider)

// WithBaseURL overrides the API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// WithOrganization sets the OpenAI-Organization header.
func WithOrganization(org string) Option {
	return func(p *Provider) { p.organization = org }
}

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) { p.httpClient = c }
}

// WithLogger overrides the slog.Logger.
func WithLogger(l *slog.Logger) Option {
	return func(p *Provider) { p.logger = l }
}

// New constructs a Provider authenticated with the given API key.
func New(apiKey string, opts ...Option) (*Provider, error) {
	if apiKey == "" {
		return nil, errors.New("openai: api key is required")
	}
	p := &Provider{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []wireMessage  `json:"messages"`
	Tools         []wireTool     `json:"tools,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	MaxTokens     int            `json:"max_completion_tokens,omitempty"`
	Stop          []string       `json:"stop,omitempty"`
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []choice     `json:"choices"`
	Usage   responseUsage `json:"usage"`
}

type choice struct {
	Index        int            `json:"index"`
	Message      responseMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type responseMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
}

type responseUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Generate dispatches a synchronous Chat Completions request.
func (p *Provider) Generate(ctx context.Context, req agent.ChatRequest) (agent.ChatResponse, error) {
	wire, err := p.buildRequest(req, false)
	if err != nil {
		return agent.ChatResponse{}, err
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("openai: build http request: %w", err)
	}
	p.setHeaders(httpReq, false)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("openai: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agent.ChatResponse{}, parseError(resp)
	}

	var raw chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return agent.ChatResponse{}, fmt.Errorf("openai: decode response: %w", err)
	}

	out := translateResponse(raw)
	p.logger.LogAttrs(ctx, slog.LevelDebug, "openai.generate",
		slog.String("model", out.Model),
		slog.Int("input_tokens", out.Usage.InputTokens),
		slog.Int("output_tokens", out.Usage.OutputTokens),
		slog.String("stop_reason", string(out.StopReason)),
	)
	return out, nil
}

// Stream dispatches a streaming Chat Completions request.
func (p *Provider) Stream(ctx context.Context, req agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	wire, err := p.buildRequest(req, true)
	if err != nil {
		return nil, err
	}
	wire.StreamOptions = &streamOptions{IncludeUsage: true}

	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build http request: %w", err)
	}
	p.setHeaders(httpReq, true)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: http request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, parseError(resp)
	}

	chunks, err := readStream(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	return agent.StreamReaderFromSlice(chunks), nil
}

func (p *Provider) buildRequest(req agent.ChatRequest, stream bool) (*chatRequest, error) {
	if req.Model == "" {
		return nil, errors.New("openai: model is required")
	}
	if len(req.Messages) == 0 && req.System == "" {
		return nil, errors.New("openai: messages is required")
	}
	if req.Temperature < 0 {
		return nil, errors.New("openai: temperature must be non-negative")
	}

	wireMsgs, err := translateMessages(req.System, req.Messages)
	if err != nil {
		return nil, err
	}

	out := &chatRequest{
		Model:    req.Model,
		Messages: wireMsgs,
		Stop:     req.StopSequences,
		Stream:   stream,
	}
	if req.Temperature > 0 {
		t := req.Temperature
		out.Temperature = &t
	}
	if req.MaxTokens > 0 {
		out.MaxTokens = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		out.Tools = translateTools(req.Tools)
	}
	return out, nil
}

func (p *Provider) setHeaders(r *http.Request, stream bool) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.apiKey)
	if p.organization != "" {
		r.Header.Set("OpenAI-Organization", p.organization)
	}
	if stream {
		r.Header.Set("Accept", "text/event-stream")
	} else {
		r.Header.Set("Accept", "application/json")
	}
}

func parseError(resp *http.Response) error {
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var er errorResponse
	if err := json.Unmarshal(raw, &er); err == nil && er.Error.Message != "" {
		return fmt.Errorf("openai: %s (%s, http %d)", er.Error.Message, er.Error.Type, resp.StatusCode)
	}
	if len(raw) > 0 {
		return fmt.Errorf("openai: http %d: %s", resp.StatusCode, string(raw))
	}
	return fmt.Errorf("openai: http %d %s", resp.StatusCode, resp.Status)
}

func translateResponse(raw chatResponse) agent.ChatResponse {
	if len(raw.Choices) == 0 {
		return agent.ChatResponse{Model: raw.Model}
	}
	c := raw.Choices[0]

	calls := make([]agent.ToolCall, 0, len(c.Message.ToolCalls))
	for _, tc := range c.Message.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		calls = append(calls, agent.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(args),
		})
	}

	usage := agent.Usage{
		InputTokens:  raw.Usage.PromptTokens,
		OutputTokens: raw.Usage.CompletionTokens,
	}
	if raw.Usage.PromptTokensDetails != nil {
		usage.CacheReadTokens = raw.Usage.PromptTokensDetails.CachedTokens
		// OpenAI-compatible cache reads are reported under prompt_tokens
		// inclusive; subtract so InputTokens reflects fresh tokens only,
		// matching the gridctl-shaped split.
		if usage.CacheReadTokens > 0 && usage.InputTokens >= usage.CacheReadTokens {
			usage.InputTokens -= usage.CacheReadTokens
		}
	}

	return agent.ChatResponse{
		Model:      raw.Model,
		Content:    c.Message.Content,
		ToolCalls:  calls,
		StopReason: translateStopReason(c.FinishReason),
		Usage:      usage,
	}
}

func translateStopReason(s string) agent.StopReason {
	switch s {
	case "stop":
		return agent.StopReasonEnd
	case "length":
		return agent.StopReasonMaxTokens
	case "tool_calls", "function_call":
		return agent.StopReasonToolUse
	case "content_filter":
		return agent.StopReasonError
	case "":
		return agent.StopReasonEnd
	default:
		return agent.StopReasonEnd
	}
}
