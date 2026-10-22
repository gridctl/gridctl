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
	// toolsDirty is set by trigger and means the visible tool list may
	// have changed. Prompt and resource changes live in changed so a
	// prompts-only flush does not emit tool events.
	toolsDirty bool
	changed    map[string]notedChange
}

// notedChange is one server's prompt or resource change waiting for the
// next flush. snap is set when tools were captured at trigger time, which
// unregister needs because the router no longer has the server's tools
// when the flush runs.
type notedChange struct {
	kinds listenKinds
	tools []string
	snap  bool
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
	n.arm(func() { n.toolsDirty = true })
}

// triggerPrompts records that server's prompt list may have changed.
// The name "registry" is visible to every subscriber.
func (n *listChangeNotifier) triggerPrompts(server string) {
	n.arm(func() { n.noteChange(server, true, false) })
}

// triggerResources records that server's resource list may have changed.
func (n *listChangeNotifier) triggerResources(server string) {
	n.arm(func() { n.noteChange(server, false, true) })
}

func (n *listChangeNotifier) noteChange(server string, prompts, resources bool) {
	n.noteServerChange(server, prompts, resources, nil, false)
}

// noteRemoved records a prompt or resource change for a server that is
// leaving the router. tools is the prefixed tool list captured before
// removal, so a group subscriber is still told when that server was a
// member. A later note without a snapshot clears it: the server is
// present again and live tool names are the membership source.
func (n *listChangeNotifier) noteRemoved(server string, prompts, resources bool, tools []string) {
	copied := append([]string(nil), tools...)
	n.arm(func() { n.noteServerChange(server, prompts, resources, copied, true) })
}

func (n *listChangeNotifier) noteServerChange(server string, prompts, resources bool, tools []string, snap bool) {
	if server == "" || (!prompts && !resources) {
		return
	}
	if n.changed == nil {
		n.changed = make(map[string]notedChange)
	}
	k := n.changed[server]
	if prompts {
		k.kinds.prompts = true
	}
	if resources {
		k.kinds.resources = true
	}
	if snap {
		k.tools = append([]string(nil), tools...)
		k.snap = true
	} else {
		k.tools = nil
		k.snap = false
	}
	n.changed[server] = k
}

// arm marks a change and (re)arms the coalescing timer. mark runs under
// the notifier mutex. Safe to call while the caller holds pendingMu.
func (n *listChangeNotifier) arm(mark func()) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || n.ctx.Err() != nil {
		return
	}
	if mark != nil {
		mark()
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
	// AfterFunc can run the callback before it returns. Publish the
	// pointer first so that callback does not race the assignment, and
	// so a stale callback cannot clear the timer this call just armed.
	n.timer = armTimer(delay, n.flushFired)
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
	n.mu.Unlock()
	// Unlock before cancelling subscribers: their writers must not need
	// this mutex to finish, and CloseAll takes the registry mutex.
	if n.g != nil && n.g.listeners != nil {
		n.g.listeners.CloseAll()
	}
	n.mu.Lock()
}

func (n *listChangeNotifier) aborted() bool {
	n.mu.Lock()
	closed := n.closed
	n.mu.Unlock()
	return closed || n.ctx.Err() != nil
}

func (n *listChangeNotifier) flushFired(fired *time.Timer) {
	n.mu.Lock()
	if n.closed || n.ctx.Err() != nil {
		n.mu.Unlock()
		return
	}
	if fired == nil || n.timer != fired {
		n.mu.Unlock()
		return
	}
	n.firstTrigger = time.Time{}
	n.timer = nil
	sink := n.sink
	toolsDirty := n.toolsDirty
	n.toolsDirty = false
	changes := n.changed
	n.changed = nil
	n.mu.Unlock()

	if n.aborted() {
		return
	}
	// Tool events are decided per session and per subscriber. A full-mode
	// session under code mode keeps the constant pair and is not
	// fingerprinted. Every other view, including deferred under code mode,
	// is fingerprinted from the composed projection.
	if toolsDirty {
		codeMode := n.g.codeModeOn()
		for _, sess := range n.g.sessions.List() {
			if n.aborted() {
				return
			}
			mode := n.g.effectiveDiscovery(sess.Discovery)
			if codeMode && mode != DiscoveryDeferred {
				continue
			}
			fp := fingerprintTools(n.g.visibleToolsFor(sess.AccessID, sess.Group, sess.Principal != "", mode))
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
		n.deliverToolSubscribers(codeMode)
	}
	n.deliverListSubscribers(changes)
}

func (n *listChangeNotifier) deliverToolSubscribers(codeMode bool) {
	if n.g == nil || n.g.listeners == nil {
		return
	}
	for _, sub := range n.g.listeners.list() {
		if n.aborted() {
			return
		}
		if !sub.kinds.tools {
			continue
		}
		mode := n.g.effectiveDiscovery(sub.discovery)
		if codeMode && mode != DiscoveryDeferred {
			n.skipToolSubscriber(sub, "code mode")
			continue
		}
		fp := fingerprintTools(n.g.visibleToolsFor(sub.accessID, sub.group, sub.principal, mode))
		n.g.listeners.noteTools(sub.key, fp)
	}
}

// skipToolSubscriber logs a suppressed tool event when the raw scoped
// projection changed. It does not swap the composed tool fingerprint.
// The comparison uses the subscriber's stored scoped hash: toolFP is the
// composed visible list, which never equals the scoped hash under code mode.
func (n *listChangeNotifier) skipToolSubscriber(sub *listenSubscriber, reason string) {
	if n.g == nil || n.g.listeners == nil || sub == nil || !sub.kinds.tools {
		return
	}
	fp := fingerprintTools(n.g.scopedToolsFor(sub.accessID, sub.group, sub.principal))
	if n.g.listeners.noteScopedChange(sub.key, fp) {
		n.g.logger.Debug("list_changed skipped", "subscription", string(sub.id), "kind", "tools", "reason", reason)
	}
}

func (n *listChangeNotifier) deliverListSubscribers(changes map[string]notedChange) {
	if n.g == nil || n.g.listeners == nil || len(changes) == 0 {
		return
	}
	for _, sub := range n.g.listeners.list() {
		if n.aborted() {
			return
		}
		var prompts, resources bool
		for name, change := range changes {
			visible := n.changeVisible(sub, name, change)
			if change.kinds.prompts && sub.kinds.prompts {
				if visible {
					prompts = true
				} else {
					n.g.logger.Debug("list_changed skipped", "subscription", string(sub.id), "kind", "prompts", "server", name, "reason", "not visible")
				}
			}
			if change.kinds.resources && sub.kinds.resources {
				if visible {
					resources = true
				} else {
					n.g.logger.Debug("list_changed skipped", "subscription", string(sub.id), "kind", "resources", "server", name, "reason", "not visible")
				}
			}
		}
		n.g.listeners.noteLists(sub.key, prompts, resources)
	}
}

func (n *listChangeNotifier) changeVisible(sub *listenSubscriber, name string, change notedChange) bool {
	if name == "registry" {
		return true
	}
	if change.snap {
		return n.g.serverVisibleForTools(sub.accessID, sub.group, name, sub.principal, change.tools)
	}
	return n.g.serverVisibleFor(sub.accessID, sub.group, name, sub.principal)
}

// NotifyRegistryChanged tells upstream subscribers that registry prompts,
// resources, and tools may have changed. Registry entries are visible to
// every subscriber. Callers are the registry refresh paths and SetSkillPolicy.
func (g *Gateway) NotifyRegistryChanged() {
	if g == nil || g.listChanges == nil {
		return
	}
	g.listChanges.trigger()
	g.listChanges.triggerPrompts("registry")
	g.listChanges.triggerResources("registry")
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

// scopedToolsFor is the full-mode catalog for an identity: the aggregated
// list after the principal-aware client-scope filter and the group
// projection. Policies are snapshotted so the caller does not hold g.mu
// across the router read.
func (g *Gateway) scopedToolsFor(accessID, group string, principal bool) []Tool {
	g.mu.RLock()
	policy := g.clientPolicy
	groups := g.groupPolicy
	g.mu.RUnlock()

	tools := g.router.AggregatedTools()
	if policy != nil {
		tools = policy.forPrincipal(principal).Filter(accessID, tools)
	}
	if group != "" {
		tools = groups.FilterAndRewrite(group, tools)
	}
	return tools
}

// visibleToolsFor is the complete list a client sees, and the only input
// to every tool fingerprint. mode is the effective discovery mode. Code
// mode and the pinned set are read together so a reload cannot split the
// composition.
func (g *Gateway) visibleToolsFor(accessID, group string, principal bool, mode string) []Tool {
	g.mu.RLock()
	cm := g.codeMode
	pins := append([]string(nil), g.alwaysExpose...)
	g.mu.RUnlock()

	if cm == nil && mode != DiscoveryDeferred {
		return g.scopedToolsFor(accessID, group, principal)
	}
	var out []Tool
	if cm != nil {
		if listed := cm.ToolsList(); listed != nil {
			out = append(out, listed.Tools...)
		}
	}
	if mode == DiscoveryDeferred {
		out = append(out, SearchToolsTool(), CallToolTool())
		out = append(out, g.pinnedVisible(accessID, group, principal, pins)...)
	}
	return out
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
	g.downstreamTimers[name] = armTimer(delay, func(fired *time.Timer) {
		if g.dropDownstreamTimer(name, fired) {
			return
		}
		g.refreshServerToolsFromNotification(g.lifeCtx, name)
	})
}

// armTimer starts fn with the timer that fired. The send synchronizes
// that pointer with the callback. AfterFunc can run fn before it returns,
// and a direct read of the assignment would race.
func armTimer(delay time.Duration, fn func(*time.Timer)) *time.Timer {
	ready := make(chan *time.Timer, 1)
	timer := time.AfterFunc(delay, func() {
		fn(<-ready)
	})
	ready <- timer
	return timer
}

// dropDownstreamTimer removes fired when it is still the armed timer.
// It reports whether the callback must not refresh. Close and a replaced
// timer both skip the refresh and leave any newer timer in place.
func (g *Gateway) dropDownstreamTimer(name string, fired *time.Timer) bool {
	g.downstreamMu.Lock()
	defer g.downstreamMu.Unlock()
	if current, ok := g.downstreamTimers[name]; ok && current == fired {
		delete(g.downstreamTimers, name)
		return g.downstreamClosed
	}
	return true
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
	g.lintAlwaysExpose()
}
