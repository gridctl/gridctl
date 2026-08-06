// Package limits enforces the stack.yaml `limits:` block: token-bucket rate
// limits scoped to one client, server, or tool. It implements the gateway's
// CallGate seam for pre-call checks. Enforcement is entirely in-memory; a
// bucket's state does not survive a daemon restart.
package limits

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"

	"golang.org/x/time/rate"
)

// Scope kinds, matching the config block's one-of keys.
const (
	scopeClient = "client"
	scopeServer = "server"
	scopeTool   = "tool"
)

// rateEntry is one compiled rate limit and its token bucket.
type rateEntry struct {
	scope     string
	key       string // match key (normalized for client scope)
	rawKey    string // as configured, for messages and status
	perMinute int
	burst     int
	limiter   *rate.Limiter
}

// carryKey identifies a rate entry across policy rebuilds. Rate and burst
// are part of the key: a changed rate deliberately gets a fresh bucket.
func (e *rateEntry) carryKey() string {
	return fmt.Sprintf("%s|%s|%d|%d", e.scope, e.rawKey, e.perMinute, e.burst)
}

// Policy is the compiled, enforcement-ready form of a config.LimitsConfig.
// A nil *Policy means no limits block was configured; every method is
// nil-safe and permissive. Entries are immutable after compile.
type Policy struct {
	rates []*rateEntry
}

// DefaultBurst returns the bucket capacity for a rate limit that does not
// set one: a few seconds of the sustained rate, never below five.
func DefaultBurst(callsPerMinute int) int {
	b := callsPerMinute / 6
	if b < 5 {
		b = 5
	}
	return b
}

// NewPolicy compiles the limits block. A nil or empty config returns a nil
// policy (no limits, byte-identical legacy behavior).
func NewPolicy(cfg *config.LimitsConfig, logger *slog.Logger) *Policy {
	if cfg == nil || len(cfg.RateLimits) == 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	p := &Policy{}
	// Duplicate scopes are rejected by validation, but client keys can
	// collide only after normalization ("Claude Code" vs "claude-code"),
	// so dedupe again on the compiled match key.
	seenRates := make(map[string]bool, len(cfg.RateLimits))
	for _, r := range cfg.RateLimits {
		scope, key, ok := r.ScopeKey()
		if !ok {
			continue // validation rejects this; skip defensively
		}
		matchKey := key
		if scope == scopeClient {
			matchKey = mcp.NormalizeClientID(key)
		}
		if seenRates[scope+"|"+matchKey] {
			logger.Warn("limits: duplicate rate-limit scope after client normalization; keeping the first",
				"scope", scope, "key", key)
			continue
		}
		seenRates[scope+"|"+matchKey] = true
		burst := r.Burst
		if burst <= 0 {
			burst = DefaultBurst(r.CallsPerMinute)
		}
		p.rates = append(p.rates, &rateEntry{
			scope:     scope,
			key:       matchKey,
			rawKey:    key,
			perMinute: r.CallsPerMinute,
			burst:     burst,
			limiter:   rate.NewLimiter(rate.Limit(float64(r.CallsPerMinute)/60.0), burst),
		})
	}
	if len(p.rates) == 0 {
		return nil
	}
	return p
}

// scopeMatches reports whether an entry with the given scope and match key
// applies to the call. normClient is the call's normalized client access ID.
func scopeMatches(scope, key string, call mcp.GateCall, normClient string) bool {
	switch scope {
	case scopeClient:
		return normClient != "" && key == normClient
	case scopeServer:
		return call.ServerName != "" && key == call.ServerName
	default: // scopeTool
		return key == call.PrefixedTool
	}
}

func (e *rateEntry) matches(call mcp.GateCall, normClient string) bool {
	return scopeMatches(e.scope, e.key, call, normClient)
}

// Gates returns the policy's pre-call gates. A nil policy returns nil.
func (p *Policy) Gates() []mcp.CallGate {
	if p == nil || len(p.rates) == 0 {
		return nil
	}
	return []mcp.CallGate{&rateGate{p}}
}

// rateGate implements mcp.CallGate over the policy's rate entries.
type rateGate struct{ p *Policy }

func (g *rateGate) Name() string { return "rate-limits" }

func (g *rateGate) CheckToolCall(_ context.Context, call mcp.GateCall) mcp.GateDecision {
	normClient := mcp.NormalizeClientID(call.ClientAccessID)
	for _, e := range g.p.rates {
		if !e.matches(call, normClient) {
			continue
		}
		if !e.limiter.Allow() {
			return mcp.GateDeny(fmt.Sprintf(
				"Rate limit exceeded for %s %q: %d calls/min. Retry after ~%s.",
				e.scope, e.rawKey, e.perMinute, e.retryAfter()))
		}
	}
	return mcp.GateAllow()
}

// retryAfter estimates how long until one token is available, without
// consuming or reserving anything: the deficit below one token divided by
// the refill rate. Clamped to a one-second floor so the hint never reads
// as "retry immediately" right after a denial.
func (e *rateEntry) retryAfter() time.Duration {
	deficit := 1.0 - e.limiter.Tokens()
	if deficit < 0 {
		deficit = 0
	}
	perSecond := float64(e.perMinute) / 60.0
	d := time.Duration(deficit / perSecond * float64(time.Second))
	return max(d.Round(time.Second), time.Second)
}

// CarryOver adopts state from a retiring policy so a hot reload never
// resets enforcement: rate limiters are reused for entries whose scope,
// key, rate, and burst are unchanged (an unrelated stack edit must not
// refill a drained bucket).
func (p *Policy) CarryOver(old *Policy) {
	if p == nil || old == nil {
		return
	}
	prevRates := make(map[string]*rateEntry, len(old.rates))
	for _, e := range old.rates {
		prevRates[e.carryKey()] = e
	}
	for _, e := range p.rates {
		if o, ok := prevRates[e.carryKey()]; ok {
			e.limiter = o.limiter
		}
	}
}
