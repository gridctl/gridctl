// Package limits enforces the stack.yaml `limits:` block: dollar budget caps
// with calendar-aligned windows and token-bucket rate limits, both scoped to
// one client, server, or tool. It implements the gateway's CallGate seam for
// pre-call checks and CostSettler for post-call spend settlement, and owns a
// small durable ledger so budget spend survives daemon restarts.
//
// Enforcement is check-then-settle: a call is admitted against spend already
// recorded, and its own cost lands after it completes. Concurrent or
// in-flight calls can therefore overshoot a cap by their own cost; the next
// call after the cap is reached is denied. Budgets govern attributed cost
// only — a call whose model cannot be priced settles nothing.
package limits

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
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

// microPerUSD converts dollars to the int64 micro-USD unit used by counters,
// matching pkg/metrics' costScale convention.
const microPerUSD = 1_000_000

// flushDebounce is how long the flusher coalesces settlements before
// writing the ledger. Spend newer than this can be lost on a crash (not on
// a clean shutdown, which flushes).
const flushDebounce = 2 * time.Second

// budgetEntry is one compiled budget with its live window state.
type budgetEntry struct {
	scope  string
	key    string // match key (normalized for client scope)
	rawKey string // as configured, for messages and status
	period Period

	maxMicro      int64
	warnAtPercent int

	mu          sync.Mutex
	windowStart time.Time
	spentMicro  int64
	warned      bool // one WARN per window at the warn threshold
	overLogged  bool // one WARN per window when the cap is crossed
}

// ledgerKey identifies this entry's row in the durable ledger. Period is
// part of the key so a period change starts a fresh counter.
func (e *budgetEntry) ledgerKey() string {
	return e.scope + "|" + e.rawKey + "|" + string(e.period)
}

// roll resets the window state when now has moved past the stored window.
// A computed window BEFORE the stored one (a backward clock correction, an
// NTP step across midnight) keeps the current counters: rolling backward
// would refill the cap twice, once now and once when midnight re-crosses.
// Callers must hold e.mu. Returns true when the window rolled.
func (e *budgetEntry) roll(now time.Time) bool {
	ws := windowStart(e.period, now)
	if !ws.After(e.windowStart) {
		return false
	}
	e.windowStart = ws
	e.spentMicro = 0
	e.warned = false
	e.overLogged = false
	return true
}

// rateEntry is one compiled rate limit and its token bucket.
type rateEntry struct {
	scope     string
	key       string
	rawKey    string
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
// nil-safe and permissive. Entries are immutable after compile; only the
// per-entry window state mutates, under per-entry locks.
type Policy struct {
	budgets []*budgetEntry
	rates   []*rateEntry

	ledgerPath string
	logger     *slog.Logger
	now        func() time.Time

	// orphanRows are ledger rows loaded from disk that match no compiled
	// entry (their config entry was removed). They are preserved on flush,
	// bounded by orphanMaxAge, so removing and re-adding a budget within
	// its window never refills spent budget.
	orphanRows map[string]ledgerEntry

	// retired forwards settlements to the successor policy after a hot
	// reload, closing the window where an in-flight call read the old
	// settler pointer but executes after the counter carry-over.
	retired atomic.Pointer[Policy]

	dirty     chan struct{}
	stop      chan struct{}
	done      chan struct{}
	startOnce sync.Once
	started   atomic.Bool
	stopOnce  sync.Once
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
// policy (no limits, byte-identical legacy behavior). When budgets exist and
// ledgerPath is non-empty, prior spend is loaded from the ledger: entries
// whose stored window matches the current one resume, stale windows reset.
// A corrupt or missing ledger logs a WARN and starts fresh; it never fails.
func NewPolicy(cfg *config.LimitsConfig, ledgerPath string, logger *slog.Logger) *Policy {
	if cfg == nil || (len(cfg.Budgets) == 0 && len(cfg.RateLimits) == 0) {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	p := &Policy{
		ledgerPath: ledgerPath,
		logger:     logger,
		now:        time.Now,
		dirty:      make(chan struct{}, 1),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	// Duplicate scopes are rejected by validation, but client keys can
	// collide only after normalization ("Claude Code" vs "claude-code"),
	// so dedupe again on the compiled match key: two entries on one scope
	// would double-settle every call.
	seenBudgets := make(map[string]bool, len(cfg.Budgets))
	for _, b := range cfg.Budgets {
		scope, key, ok := b.ScopeKey()
		if !ok {
			continue // validation rejects this; skip defensively
		}
		matchKey := key
		if scope == scopeClient {
			matchKey = mcp.NormalizeClientID(key)
		}
		if seenBudgets[scope+"|"+matchKey] {
			logger.Warn("limits: duplicate budget scope after client normalization; keeping the first",
				"scope", scope, "key", key)
			continue
		}
		seenBudgets[scope+"|"+matchKey] = true
		// Floor at one micro-USD: a positive cap below the counter's
		// resolution would otherwise compile to zero and deny at zero spend.
		maxMicro := int64(math.Round(b.MaxUSD * microPerUSD))
		if maxMicro < 1 {
			maxMicro = 1
		}
		p.budgets = append(p.budgets, &budgetEntry{
			scope:         scope,
			key:           matchKey,
			rawKey:        key,
			period:        Period(b.Period),
			maxMicro:      maxMicro,
			warnAtPercent: b.WarnAtPercent,
		})
	}
	seenRates := make(map[string]bool, len(cfg.RateLimits))
	for _, r := range cfg.RateLimits {
		scope, key, ok := r.ScopeKey()
		if !ok {
			continue
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
	now := p.now()
	for _, e := range p.budgets {
		e.windowStart = windowStart(e.period, now)
	}
	if len(p.budgets) > 0 && p.ledgerPath != "" {
		p.loadLedger(now)
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

func (e *budgetEntry) matches(call mcp.GateCall, normClient string) bool {
	return scopeMatches(e.scope, e.key, call, normClient)
}

func (e *rateEntry) matches(call mcp.GateCall, normClient string) bool {
	return scopeMatches(e.scope, e.key, call, normClient)
}

// Gates returns the policy's pre-call gates in canonical order: rate limits
// before budgets, so a rate-limited caller gets the cheaper check's message.
// A nil policy returns nil.
func (p *Policy) Gates() []mcp.CallGate {
	if p == nil {
		return nil
	}
	var gates []mcp.CallGate
	if len(p.rates) > 0 {
		gates = append(gates, &rateGate{p})
	}
	if len(p.budgets) > 0 {
		gates = append(gates, &budgetGate{p})
	}
	return gates
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

// budgetGate implements mcp.CallGate over the policy's budget entries.
type budgetGate struct{ p *Policy }

func (g *budgetGate) Name() string { return "budgets" }

func (g *budgetGate) CheckToolCall(_ context.Context, call mcp.GateCall) mcp.GateDecision {
	normClient := mcp.NormalizeClientID(call.ClientAccessID)
	now := g.p.now()
	for _, e := range g.p.budgets {
		if !e.matches(call, normClient) {
			continue
		}
		e.mu.Lock()
		if e.roll(now) {
			g.p.markDirty()
		}
		spent, maxM := e.spentMicro, e.maxMicro
		end := windowEnd(e.period, e.windowStart)
		e.mu.Unlock()
		if spent >= maxM {
			return mcp.GateDeny(fmt.Sprintf(
				"Budget exceeded for %s %q: $%.2f of $%.2f %s cap. Resets %s (local). Do not retry until the cap resets.",
				e.scope, e.rawKey,
				float64(spent)/microPerUSD, float64(maxM)/microPerUSD,
				e.period, end.Format("2006-01-02T15:04")))
		}
	}
	return mcp.GateAllow()
}

// SettleToolCallCost implements mcp.CostSettler: it adds the call's priced
// cost to every matching budget window. Runs synchronously on the dispatch
// path, so it is a few short critical sections and a channel nudge; the
// ledger write happens on the flusher goroutine.
func (p *Policy) SettleToolCallCost(ctx context.Context, call mcp.GateCall, costUSD float64) {
	if p == nil {
		return
	}
	// After a hot reload, in-flight calls that captured this policy's
	// settler pointer forward their cost to the successor so no spend is
	// dropped during the swap.
	if next := p.retired.Load(); next != nil {
		next.SettleToolCallCost(ctx, call, costUSD)
		return
	}
	if len(p.budgets) == 0 || costUSD <= 0 {
		return
	}
	micro := int64(math.Round(costUSD * microPerUSD))
	if micro == 0 {
		return
	}
	normClient := mcp.NormalizeClientID(call.ClientAccessID)
	now := p.now()
	settled := false
	for _, e := range p.budgets {
		if !e.matches(call, normClient) {
			continue
		}
		e.mu.Lock()
		e.roll(now)
		e.spentMicro += micro
		warnNow := !e.warned && e.warnAtPercent > 0 &&
			e.spentMicro*100 >= e.maxMicro*int64(e.warnAtPercent)
		if warnNow {
			e.warned = true
		}
		overNow := !e.overLogged && e.spentMicro >= e.maxMicro
		if overNow {
			e.overLogged = true
		}
		spent, maxM := e.spentMicro, e.maxMicro
		e.mu.Unlock()
		settled = true
		if warnNow && spent < maxM {
			p.logger.Warn("budget warn threshold crossed",
				"scope", e.scope, "key", e.rawKey, "period", string(e.period),
				"spent_usd", float64(spent)/microPerUSD,
				"max_usd", float64(maxM)/microPerUSD,
				"warn_at_percent", e.warnAtPercent)
		}
		if overNow {
			p.logger.Warn("budget exceeded; further matching calls will be denied",
				"scope", e.scope, "key", e.rawKey, "period", string(e.period),
				"spent_usd", float64(spent)/microPerUSD,
				"max_usd", float64(maxM)/microPerUSD)
		}
	}
	if settled {
		p.markDirty()
	}
}

// CarryOver adopts state from a retiring policy so a hot reload never
// resets enforcement: budget windows carry for entries whose scope, key,
// and period are unchanged (cap changes deliberately keep the counter, and
// spend merges by maximum so a settlement that raced the swap is never
// lost), and rate limiters are reused for entries whose scope, key, rate,
// and burst are unchanged (an unrelated stack edit must not refill a
// drained bucket). It then marks the old policy retired, forwarding any
// late settlements here.
func (p *Policy) CarryOver(old *Policy) {
	if p == nil || old == nil {
		return
	}
	prevBudgets := make(map[string]*budgetEntry, len(old.budgets))
	for _, e := range old.budgets {
		prevBudgets[e.ledgerKey()] = e
	}
	for _, e := range p.budgets {
		o, ok := prevBudgets[e.ledgerKey()]
		if !ok {
			continue
		}
		o.mu.Lock()
		ws, spent, warned, over := o.windowStart, o.spentMicro, o.warned, o.overLogged
		o.mu.Unlock()
		e.mu.Lock()
		if ws.After(e.windowStart) {
			e.windowStart = ws
			e.spentMicro = spent
			e.warned = warned
			e.overLogged = over
		} else if ws.Equal(e.windowStart) {
			// Same window: take the larger spend (the ledger seeded this
			// entry at load; the in-memory old counter may be newer).
			if spent > e.spentMicro {
				e.spentMicro = spent
			}
			e.warned = e.warned || warned
			e.overLogged = e.overLogged || over
		}
		e.mu.Unlock()
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

	old.retired.Store(p)
}
