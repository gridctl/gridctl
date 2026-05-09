// Package gateway is the LLM-side passthrough provider. It wraps a set
// of model-prefix → underlying provider routes and delegates each
// ChatRequest to the route that matches the request's Model.
//
// The "gateway" name parallels the MCP gateway: just as Gateway.CallTool
// fronts heterogeneous downstream MCP servers behind one entry point,
// llm.gateway.Provider fronts heterogeneous LLM vendors behind one
// agent.ChatModel. The runtime holds a single provider; provider
// selection is a configuration concern, not an architectural one.
//
// Routes are matched by Model prefix in the order they were registered;
// the first match wins. Providers are intentionally registered with
// short prefixes ("claude-", "gpt-", "gemini-") so a single instance
// can serve mixed-provider skills out of the box.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent"
)

// Route binds a model-name prefix to an underlying provider. Prefix
// matching is intentional rather than exact-match — the runtime can
// register "claude-" once and serve every Claude model the user asks
// for, including new IDs not known at build time.
type Route struct {
	Prefix   string
	Provider agent.ChatModel
}

// Provider implements agent.ChatModel by routing to one of N
// underlying providers based on ChatRequest.Model. Construct via New.
type Provider struct {
	routes   []Route
	fallback agent.ChatModel
}

// Option configures a Provider during construction.
type Option func(*Provider)

// WithRoute registers a (prefix, provider) route. Order matters: the
// first matching prefix wins on each ChatRequest.
func WithRoute(prefix string, provider agent.ChatModel) Option {
	return func(p *Provider) {
		p.routes = append(p.routes, Route{Prefix: prefix, Provider: provider})
	}
}

// WithFallback sets a provider used when no prefix matches. Without a
// fallback, unmatched requests return an error.
func WithFallback(provider agent.ChatModel) Option {
	return func(p *Provider) { p.fallback = provider }
}

// New constructs a routing Provider. At least one route or a fallback
// must be configured; an empty router returns an error so misconfigured
// callers fail at construction rather than on first request.
func New(opts ...Option) (*Provider, error) {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	if len(p.routes) == 0 && p.fallback == nil {
		return nil, errors.New("agent/llm/gateway: at least one route or a fallback is required")
	}
	return p, nil
}

// Generate dispatches the request to the matching underlying provider.
func (p *Provider) Generate(ctx context.Context, req agent.ChatRequest) (agent.ChatResponse, error) {
	prov, err := p.resolve(req.Model)
	if err != nil {
		return agent.ChatResponse{}, err
	}
	return prov.Generate(ctx, req)
}

// Stream dispatches the request to the matching underlying provider.
func (p *Provider) Stream(ctx context.Context, req agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	prov, err := p.resolve(req.Model)
	if err != nil {
		return nil, err
	}
	return prov.Stream(ctx, req)
}

// Routes returns a copy of the registered routes. Useful for
// diagnostics; the slice is safe to mutate.
func (p *Provider) Routes() []Route {
	out := make([]Route, len(p.routes))
	copy(out, p.routes)
	return out
}

// resolve picks the provider for a model ID. Returns an error when no
// route matches and no fallback is configured.
func (p *Provider) resolve(model string) (agent.ChatModel, error) {
	if model == "" {
		return nil, errors.New("agent/llm/gateway: model is required")
	}
	for _, r := range p.routes {
		if strings.HasPrefix(model, r.Prefix) {
			return r.Provider, nil
		}
	}
	if p.fallback != nil {
		return p.fallback, nil
	}
	return nil, fmt.Errorf("agent/llm/gateway: no route matches model %q", model)
}
