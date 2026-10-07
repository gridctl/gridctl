package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

const (
	defaultListChangeQuiet      = 250 * time.Millisecond
	defaultListChangeMaxLatency = 2 * time.Second
	defaultDownstreamDebounce   = 500 * time.Millisecond
	downstreamRefreshTimeout    = 10 * time.Second
)

// toolsListChangedPayload is the exact SSE data for a northbound
// notification. The field order is fixed so every client sees one byte string.
var toolsListChangedPayload = []byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`)

// ListChangeSink delivers one tools-list change to a handshake session.
// The transport appends the event to that session's history and enqueues it
// for an open GET stream. A missing session is skipped.
type ListChangeSink interface {
	NotifyToolsListChanged(sessionID string)
}

// listChangeNotifier coalesces tool-map and policy triggers into one
// notification per affected session. trigger only arms a timer so it is
// safe to call while the caller holds pendingMu.
type listChangeNotifier struct {
	g          *Gateway
	ctx        context.Context
	quiet      time.Duration
	maxLatency time.Duration

	mu           sync.Mutex
	timer        *time.Timer
	firstTrigger time.Time
	closed       bool
	sink         ListChangeSink
}

func newListChangeNotifier(g *Gateway, ctx context.Context) *listChangeNotifier {
	return &listChangeNotifier{
		g:          g,
		ctx:        ctx,
		quiet:      defaultListChangeQuiet,
		maxLatency: defaultListChangeMaxLatency,
	}
}

func (n *listChangeNotifier) setSink(sink ListChangeSink) {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.sink = sink
	n.mu.Unlock()
}

func (n *listChangeNotifier) setTiming(quiet, maxLatency time.Duration) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if quiet > 0 {
		n.quiet = quiet
	}
	if maxLatency > 0 {
		n.maxLatency = maxLatency
	}
}

func (n *listChangeNotifier) trigger() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || n.ctx.Err() != nil {
		return
	}
	now := time.Now()
	if n.firstTrigger.IsZero() {
		n.firstTrigger = now
	}
	delay := n.quiet
	if remaining := n.maxLatency - now.Sub(n.firstTrigger); remaining < delay {
		if remaining < 0 {
			delay = 0
		} else {
			delay = remaining
		}
	}
	if n.timer != nil {
		n.timer.Stop()
	}
	n.timer = time.AfterFunc(delay, n.flush)
}

func (n *listChangeNotifier) close() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	if n.timer != nil {
		n.timer.Stop()
		n.timer = nil
	}
}

func (n *listChangeNotifier) aborted() bool {
	n.mu.Lock()
	closed := n.closed
	n.mu.Unlock()
	return closed || n.ctx.Err() != nil
}

func (n *listChangeNotifier) flush() {
	n.mu.Lock()
	if n.closed || n.ctx.Err() != nil {
		n.mu.Unlock()
		return
	}
	n.firstTrigger = time.Time{}
	n.timer = nil
	sink := n.sink
	n.mu.Unlock()

	if n.g.codeModeOn() || n.aborted() {
		return
	}
	for _, sess := range n.g.sessions.List() {
		if n.aborted() {
			return
		}
		fp := fingerprintTools(n.g.visibleToolsFor(sess.AccessID, sess.Group))
		prev, ok := n.g.sessions.SwapToolFingerprint(sess.ID, fp)
		if !ok || prev == "" || prev == fp {
			continue
		}
		if n.aborted() {
			return
		}
		if sink == nil {
			continue
		}
		sink.NotifyToolsListChanged(sess.ID)
		n.g.logger.Debug("list_changed sent", "session", sess.ID)
	}
}

// SetListChangeSink installs the transport that receives northbound
// notifications. Passing nil disables delivery; fingerprints are still updated.
func (g *Gateway) SetListChangeSink(sink ListChangeSink) {
	if g == nil || g.listChanges == nil {
		return
	}
	g.listChanges.setSink(sink)
}

// SetListChangeTiming overrides the northbound quiet window and maximum
// latency. Non-positive values keep the current setting. Production uses
// 250 milliseconds and 2 seconds.
func (g *Gateway) SetListChangeTiming(quiet, maxLatency time.Duration) {
	if g == nil || g.listChanges == nil {
		return
	}
	g.listChanges.setTiming(quiet, maxLatency)
}

// SetDownstreamRefreshDebounce overrides the per-server southbound debounce.
// Non-positive values keep the current setting. Production uses 500 milliseconds.
func (g *Gateway) SetDownstreamRefreshDebounce(d time.Duration) {
	if g == nil || d <= 0 {
		return
	}
	g.downstreamMu.Lock()
	g.downstreamDebounce = d
	g.downstreamMu.Unlock()
}

func (g *Gateway) codeModeOn() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.codeMode != nil
}

// visibleToolsFor is the non-code-mode body of HandleToolsList: the
// aggregated list after client-profile and group filtering. Policies are
// snapshotted so the caller does not hold g.mu across the router read.
func (g *Gateway) visibleToolsFor(accessID, group string) []Tool {
	g.mu.RLock()
	policy := g.clientPolicy
	groups := g.groupPolicy
	g.mu.RUnlock()

	tools := g.router.AggregatedTools()
	if policy != nil {
		tools = policy.Filter(accessID, tools)
	}
	if group != "" {
		tools = groups.FilterAndRewrite(group, tools)
	}
	return tools
}

// fingerprintTools hashes a session's visible list. Nil and empty lists
// hash the same so initialize and a later empty tools/list agree. Order
// does not matter; a description change does.
func fingerprintTools(tools []Tool) string {
	cp := append([]Tool{}, tools...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	raw, err := json.Marshal(cp)
	if err != nil {
		sum := sha256.Sum256([]byte("marshal-error"))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (g *Gateway) scheduleDownstreamToolRefresh(name string) {
	g.downstreamMu.Lock()
	defer g.downstreamMu.Unlock()
	if g.downstreamClosed || g.lifeCtx == nil || g.lifeCtx.Err() != nil {
		return
	}
	delay := g.downstreamDebounce
	if delay <= 0 {
		delay = defaultDownstreamDebounce
	}
	if g.downstreamTimers == nil {
		g.downstreamTimers = make(map[string]*time.Timer)
	}
	if t, ok := g.downstreamTimers[name]; ok {
		t.Stop()
	}
	g.downstreamTimers[name] = time.AfterFunc(delay, func() {
		g.refreshServerToolsFromNotification(g.lifeCtx, name)
	})
}

func (g *Gateway) stopDownstreamRefreshTimers() {
	g.downstreamMu.Lock()
	defer g.downstreamMu.Unlock()
	g.downstreamClosed = true
	for name, t := range g.downstreamTimers {
		t.Stop()
		delete(g.downstreamTimers, name)
	}
}

// refreshServerToolsFromNotification reloads one server off the reader
// goroutine, verifies pins, then refreshes the router so the upstream
// notification fires only after a block is in place.
func (g *Gateway) refreshServerToolsFromNotification(ctx context.Context, name string) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	set := g.router.GetReplicaSet(name)
	if set == nil {
		return
	}
	for _, rep := range set.Replicas() {
		if ctx.Err() != nil {
			return
		}
		if rep == nil || !rep.Healthy() {
			continue
		}
		client := rep.Client()
		if client == nil {
			continue
		}
		repCtx, cancel := context.WithTimeout(ctx, downstreamRefreshTimeout)
		err := client.RefreshTools(repCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			g.logger.Warn("downstream tool refresh failed",
				"server", name,
				"replica", rep.ID(),
				"error", err)
		}
		if ctx.Err() != nil {
			return
		}
	}
	if ctx.Err() != nil {
		return
	}
	set = g.router.GetReplicaSet(name)
	if set == nil {
		return
	}
	if client := set.Client(); client != nil {
		if err := g.verifyClientPins(ctx, name, client); err != nil {
			g.logger.Warn("downstream tool refresh pin verification failed",
				"server", name,
				"error", err)
		}
	}
	if ctx.Err() != nil || g.router.GetReplicaSet(name) == nil {
		return
	}
	g.router.RefreshTools()
}
