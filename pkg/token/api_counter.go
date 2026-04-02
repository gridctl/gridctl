package token

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	// anthropicEndpoint is the base URL for Anthropic API requests.
	// Overridable via withEndpoint for testing.
	anthropicEndpoint = "https://api.anthropic.com"

	// countTokensPath is the path for the token counting endpoint.
	countTokensPath = "/v1/messages/count_tokens" //nolint:gosec // URL path, not a credential

	// countTokensModel is the model used for token counting requests.
	// The model affects tokenization; claude-3-5-haiku is stable and low-cost.
	countTokensModel = "claude-3-5-haiku-20241022" //nolint:gosec // model name, not a credential
)

// APICounter counts tokens via Anthropic's count_tokens endpoint.
//
// Note: CountJSON passes a JSON-marshaled string as the message content.
// This counts the JSON bytes as tokens, which is an approximation — the real
// context includes tool definitions and system prompts not visible here.
// It is still significantly more accurate than the 4-bytes/token heuristic.
//
// This counter is Anthropic-specific: it returns incorrect results when the
// gateway is routing to non-Anthropic models (Gemini, local endpoints, etc.).
// Use gateway.tokenizer: embedded for model-agnostic deployments.
type APICounter struct {
	apiKey   string
	client   *http.Client
	fallback *TiktokenCounter
	endpoint string // base URL; overridable for testing
}

// NewAPICounter creates an APICounter using the given Anthropic API key.
// The HTTP client is initialized with a 5-second timeout. A TiktokenCounter
// is also initialized as a fallback for network or API errors.
func NewAPICounter(apiKey string) (*APICounter, error) {
	fallback, err := NewTiktokenCounter()
	if err != nil {
		return nil, fmt.Errorf("token: failed to initialize APICounter fallback: %w", err)
	}
	return &APICounter{
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 5 * time.Second},
		fallback: fallback,
		endpoint: anthropicEndpoint,
	}, nil
}

// withEndpoint returns a copy of the counter with the base URL overridden.
// Used in tests to point the counter at a local httptest.Server.
func (c *APICounter) withEndpoint(url string) *APICounter {
	copy := *c
	copy.endpoint = url
	return &copy
}

// Count returns the token count for the given text by calling the Anthropic
// count_tokens endpoint. On any error (network, non-200, parse), it falls
// back to the embedded TiktokenCounter and logs a warning.
func (c *APICounter) Count(text string) int {
	if text == "" {
		return 0
	}

	count, err := c.callAPI(text)
	if err != nil {
		slog.Default().Warn("token: api counter failed, using embedded fallback", "error", err)
		return c.fallback.Count(text)
	}
	return count
}

// callAPI performs the count_tokens HTTP request and returns the token count.
func (c *APICounter) callAPI(text string) (int, error) {
	body, err := json.Marshal(map[string]any{
		"model": countTokensModel,
		"messages": []map[string]string{
			{"role": "user", "content": text},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint+countTokensPath, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var result struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	return result.InputTokens, nil
}
