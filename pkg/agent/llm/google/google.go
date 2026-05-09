// Package google implements agent.ChatModel against the Google
// Gemini Generative Language API. The package uses only net/http and
// encoding/json.
//
// Wire reference:
//   - https://ai.google.dev/api/generate-content
//   - https://ai.google.dev/api/generate-content#stream
package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gridctl/gridctl/pkg/agent"
)

// DefaultBaseURL is the production Generative Language API endpoint.
const DefaultBaseURL = "https://generativelanguage.googleapis.com"

// DefaultAPIVersion targets the v1beta surface, which is where
// function-calling and SSE streaming both live; the v1 surface lags.
const DefaultAPIVersion = "v1beta"

// Provider implements agent.ChatModel against the Gemini API.
type Provider struct {
	apiKey     string
	baseURL    string
	apiVersion string
	httpClient *http.Client
	logger     *slog.Logger
}

// Option configures a Provider during construction.
type Option func(*Provider)

// WithBaseURL overrides the API base URL.
func WithBaseURL(u string) Option {
	return func(p *Provider) { p.baseURL = u }
}

// WithAPIVersion overrides the API version (default v1beta).
func WithAPIVersion(v string) Option {
	return func(p *Provider) { p.apiVersion = v }
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
		return nil, errors.New("google: api key is required")
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

type generateRequest struct {
	Contents          []wireContent      `json:"contents"`
	Tools             []wireToolset      `json:"tools,omitempty"`
	SystemInstruction *wireContent       `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type generationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type wireContent struct {
	Role  string     `json:"role,omitempty"`
	Parts []wirePart `json:"parts"`
}

type wirePart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *functionCallPart       `json:"functionCall,omitempty"`
	FunctionResponse *functionResponsePart   `json:"functionResponse,omitempty"`
}

type functionCallPart struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionResponsePart struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response,omitempty"`
}

type generateResponse struct {
	Candidates    []candidate    `json:"candidates"`
	UsageMetadata usageMetadata  `json:"usageMetadata"`
	ModelVersion  string         `json:"modelVersion"`
	PromptFeedback *promptFeedback `json:"promptFeedback,omitempty"`
}

type candidate struct {
	Content      wireContent `json:"content"`
	FinishReason string      `json:"finishReason"`
	Index        int         `json:"index"`
}

type usageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
}

type promptFeedback struct {
	BlockReason string `json:"blockReason,omitempty"`
}

type errorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Generate dispatches a synchronous generateContent request.
func (p *Provider) Generate(ctx context.Context, req agent.ChatRequest) (agent.ChatResponse, error) {
	wire, err := p.buildRequest(req)
	if err != nil {
		return agent.ChatResponse{}, err
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("google: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s/models/%s:generateContent?key=%s",
		p.baseURL, p.apiVersion, url.PathEscape(req.Model), url.QueryEscape(p.apiKey))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("google: build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return agent.ChatResponse{}, fmt.Errorf("google: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agent.ChatResponse{}, parseError(resp, req.Model)
	}

	var raw generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return agent.ChatResponse{}, fmt.Errorf("google: decode response: %w", err)
	}

	out := translateResponse(raw, req.Model)
	p.logger.LogAttrs(ctx, slog.LevelDebug, "google.generate",
		slog.String("model", out.Model),
		slog.Int("input_tokens", out.Usage.InputTokens),
		slog.Int("output_tokens", out.Usage.OutputTokens),
		slog.String("stop_reason", string(out.StopReason)),
	)
	return out, nil
}

// Stream dispatches a streaming streamGenerateContent request.
func (p *Provider) Stream(ctx context.Context, req agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	wire, err := p.buildRequest(req)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("google: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s/models/%s:streamGenerateContent?alt=sse&key=%s",
		p.baseURL, p.apiVersion, url.PathEscape(req.Model), url.QueryEscape(p.apiKey))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("google: build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google: http request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, parseError(resp, req.Model)
	}

	chunks, err := readStream(resp.Body, req.Model)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	return agent.StreamReaderFromSlice(chunks), nil
}

func (p *Provider) buildRequest(req agent.ChatRequest) (*generateRequest, error) {
	if req.Model == "" {
		return nil, errors.New("google: model is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("google: messages is required")
	}
	if req.Temperature < 0 {
		return nil, errors.New("google: temperature must be non-negative")
	}

	contents, err := translateMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	out := &generateRequest{Contents: contents}
	if req.System != "" {
		out.SystemInstruction = &wireContent{
			Parts: []wirePart{{Text: req.System}},
		}
	}
	if len(req.Tools) > 0 {
		out.Tools = translateTools(req.Tools)
	}

	cfg := &generationConfig{
		MaxOutputTokens: req.MaxTokens,
		StopSequences:   req.StopSequences,
	}
	if req.Temperature > 0 {
		t := req.Temperature
		cfg.Temperature = &t
	}
	if cfg.Temperature != nil || cfg.MaxOutputTokens > 0 || len(cfg.StopSequences) > 0 {
		out.GenerationConfig = cfg
	}
	return out, nil
}

func parseError(resp *http.Response, model string) error {
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var er errorResponse
	if err := json.Unmarshal(raw, &er); err == nil && er.Error.Message != "" {
		return fmt.Errorf("google: %s (%s, http %d, model %s)", er.Error.Message, er.Error.Status, resp.StatusCode, model)
	}
	if len(raw) > 0 {
		return fmt.Errorf("google: http %d (model %s): %s", resp.StatusCode, model, string(raw))
	}
	return fmt.Errorf("google: http %d %s (model %s)", resp.StatusCode, resp.Status, model)
}

func translateResponse(raw generateResponse, model string) agent.ChatResponse {
	if len(raw.Candidates) == 0 {
		stop := agent.StopReasonEnd
		if raw.PromptFeedback != nil && raw.PromptFeedback.BlockReason != "" {
			stop = agent.StopReasonError
		}
		return agent.ChatResponse{
			Model:      effectiveModel(raw.ModelVersion, model),
			StopReason: stop,
			Usage:      translateUsage(raw.UsageMetadata),
		}
	}

	c := raw.Candidates[0]
	var content string
	var calls []agent.ToolCall
	for i, part := range c.Content.Parts {
		if part.Text != "" {
			content += part.Text
		}
		if part.FunctionCall != nil {
			args := part.FunctionCall.Args
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			calls = append(calls, agent.ToolCall{
				ID:        fmt.Sprintf("%s-%d", part.FunctionCall.Name, i),
				Name:      part.FunctionCall.Name,
				Arguments: args,
			})
		}
	}

	return agent.ChatResponse{
		Model:      effectiveModel(raw.ModelVersion, model),
		Content:    content,
		ToolCalls:  calls,
		StopReason: translateStopReason(c.FinishReason, len(calls) > 0),
		Usage:      translateUsage(raw.UsageMetadata),
	}
}

func translateUsage(u usageMetadata) agent.Usage {
	out := agent.Usage{
		InputTokens:  u.PromptTokenCount,
		OutputTokens: u.CandidatesTokenCount,
	}
	if u.CachedContentTokenCount > 0 {
		out.CacheReadTokens = u.CachedContentTokenCount
		if out.InputTokens >= u.CachedContentTokenCount {
			out.InputTokens -= u.CachedContentTokenCount
		}
	}
	return out
}

func translateStopReason(s string, hasFunctionCalls bool) agent.StopReason {
	if hasFunctionCalls {
		// Gemini doesn't always return a dedicated tool_use stop
		// reason; the heuristic of "function calls present" lets the
		// runtime branch as if it had been reported explicitly.
		return agent.StopReasonToolUse
	}
	switch s {
	case "STOP", "":
		return agent.StopReasonEnd
	case "MAX_TOKENS":
		return agent.StopReasonMaxTokens
	case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII":
		return agent.StopReasonError
	default:
		return agent.StopReasonEnd
	}
}

func effectiveModel(reported, requested string) string {
	if reported != "" {
		return reported
	}
	return requested
}
