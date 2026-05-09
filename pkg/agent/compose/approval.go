// Package compose hosts the agent runtime's graph-level primitives that
// sit just above the eino adapter — primitives the rest of pkg/agent
// composes against without ever importing eino types directly. Phase E
// adds the approval gate: a node-shaped primitive that suspends a run
// to wait on a human decision delivered by CLI, web UI, or MCP.
//
// The approval gate is structurally simple: a Gate ties an Approver
// (the sandbox-side interface) to a persist.Recorder (the run's JSONL
// ledger) and a Notifier (the channel that surfaces the request). Each
// approval gets a fresh, namespaced ID; pending gates live in a
// process-wide Registry so an out-of-band consumer (CLI subcommand,
// HTTP handler, MCP notification) can resolve them by ID without
// holding a reference to the running graph.
package compose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
)

// DefaultApprovalTimeout is the wall-clock window the runtime waits
// for an approval response before auto-rejecting. 24h is the design's
// quoted default; operators can override per-call via Gate.WithTimeout.
const DefaultApprovalTimeout = 24 * time.Hour

// DefaultApprovalWarnFraction is the fraction of DefaultApprovalTimeout
// at which a "still pending" warning is emitted. 0.8 means the warning
// fires after 19h12m for the default 24h window.
const DefaultApprovalWarnFraction = 0.8

// approvalIDBytes is the entropy length for a freshly minted approval
// ID. 8 bytes (16 hex chars) gives ~10^19 possibilities — sufficient
// against accidental collision in a single-operator deployment without
// inflating the on-disk ledger size.
const approvalIDBytes = 8

// ApprovalRequest carries the data a Notifier renders when an approval
// gate fires. The shape mirrors persist.ApprovalRequestPayload but
// carries the run ID and skill explicitly so a notifier can render a
// CLI banner or web push without a second JSONL read.
type ApprovalRequest struct {
	// RunID is the run that suspended on the gate.
	RunID string

	// ApprovalID is the handle a consumer responds against.
	ApprovalID string

	// Prompt is the human-readable description of what's being
	// approved.
	Prompt string

	// Skill is the unprefixed skill name the run is executing under,
	// when known. Empty for ad-hoc graph compositions.
	Skill string

	// Timeout is the wall-clock window before auto-rejection.
	Timeout time.Duration

	// CreatedAt is the wall-clock time the request was raised.
	CreatedAt time.Time
}

// Notifier surfaces an ApprovalRequest to consumers (CLI banners, web
// UI banners, MCP notifications). Implementations MUST be non-blocking
// and idempotent — the gate fans out a single Notify call per request,
// then returns control to the run loop while it blocks on the
// decision channel.
type Notifier interface {
	NotifyApproval(ctx context.Context, req ApprovalRequest) error
}

// SlogNotifier is the default Notifier: it emits a structured warning
// log line each time an approval gate fires. The CLI banner and web
// UI banner are surfaced separately by the API/CLI layers; the slog
// line is the operator-side audit trail.
type SlogNotifier struct {
	// Logger receives the warning line. nil falls back to
	// slog.Default at call time.
	Logger *slog.Logger
}

// NotifyApproval implements Notifier.
func (n *SlogNotifier) NotifyApproval(_ context.Context, req ApprovalRequest) error {
	logger := n.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("agent.approval.pending",
		"run_id", req.RunID,
		"approval_id", req.ApprovalID,
		"skill", req.Skill,
		"timeout_seconds", int64(req.Timeout/time.Second),
	)
	return nil
}

// MultiNotifier fans an ApprovalRequest out to several notifiers in
// order. A non-nil error from any notifier is returned and stops the
// fan-out; in practice operators wire SlogNotifier first so the audit
// line lands even if a downstream notifier (MCP, web push) fails.
type MultiNotifier struct {
	Notifiers []Notifier
}

// NotifyApproval implements Notifier.
func (m *MultiNotifier) NotifyApproval(ctx context.Context, req ApprovalRequest) error {
	for _, n := range m.Notifiers {
		if n == nil {
			continue
		}
		if err := n.NotifyApproval(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// Gate is the runtime-side approval primitive. One Gate is created
// per run — its RunID anchors every event and notification it emits.
// Gate satisfies sandbox.Approver, so it drops in wherever the TS
// sandbox expects an Approver and gives the run author a single
// `approval(prompt)` call site that surfaces simultaneously to the
// CLI, the web UI, and any MCP consumer.
type Gate struct {
	runID    string
	skill    string
	recorder *persist.Recorder
	registry *Registry
	notifier Notifier
	logger   *slog.Logger

	timeout       time.Duration
	warnFraction  float64
	timeoutPolicy TimeoutPolicy
}

// TimeoutPolicy describes what happens when an approval window
// elapses without a response. The runtime always emits a "warn at
// 80%" log line first; the policy controls only the terminal action.
type TimeoutPolicy int

const (
	// TimeoutReject auto-rejects the request when the window
	// elapses. Default — matches the prompt's "approval gate timeout
	// transitions run to error" guidance.
	TimeoutReject TimeoutPolicy = iota

	// TimeoutBlock keeps the gate open indefinitely. Useful for
	// long-running interactive runs where the human is the
	// scheduling constraint.
	TimeoutBlock
)

// GateOption configures a Gate at construction time.
type GateOption func(*Gate)

// WithTimeout overrides DefaultApprovalTimeout. A non-positive value
// is silently dropped so callers can pass through a config field that
// may be unset.
func WithTimeout(d time.Duration) GateOption {
	return func(g *Gate) {
		if d > 0 {
			g.timeout = d
		}
	}
}

// WithWarnFraction overrides DefaultApprovalWarnFraction. Values
// outside (0, 1) are clamped to the default to avoid pathological
// no-warning or warn-immediately behaviour.
func WithWarnFraction(f float64) GateOption {
	return func(g *Gate) {
		if f > 0 && f < 1 {
			g.warnFraction = f
		}
	}
}

// WithTimeoutPolicy overrides the default TimeoutReject behaviour.
func WithTimeoutPolicy(p TimeoutPolicy) GateOption {
	return func(g *Gate) {
		g.timeoutPolicy = p
	}
}

// WithLogger sets the slog.Logger the gate uses for its warn-at-80%
// line. Nil falls back to slog.Default at warn time.
func WithLogger(logger *slog.Logger) GateOption {
	return func(g *Gate) {
		g.logger = logger
	}
}

// WithSkill records the skill name on the request so notifiers can
// render it without a second JSONL read.
func WithSkill(name string) GateOption {
	return func(g *Gate) {
		g.skill = name
	}
}

// NewGate constructs a Gate for a run. The recorder MUST be the
// run's JSONL writer; nil is rejected because the gate is not safe
// to use without persistence (a crashed approval would leave the
// CLI/web/MCP consumers unable to discover it on restart). Pass a
// SlogNotifier via WithNotifier to keep the operator-side audit line
// when no MCP/web notifier is wired.
func NewGate(runID string, recorder *persist.Recorder, registry *Registry, notifier Notifier, opts ...GateOption) (*Gate, error) {
	if runID == "" {
		return nil, errors.New("compose: run id is required")
	}
	if recorder == nil {
		return nil, errors.New("compose: recorder is required")
	}
	if registry == nil {
		return nil, errors.New("compose: registry is required")
	}
	if notifier == nil {
		notifier = &SlogNotifier{}
	}
	g := &Gate{
		runID:         runID,
		recorder:      recorder,
		registry:      registry,
		notifier:      notifier,
		timeout:       DefaultApprovalTimeout,
		warnFraction:  DefaultApprovalWarnFraction,
		timeoutPolicy: TimeoutReject,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g, nil
}

// Approve implements sandbox.Approver. The TS sandbox calls this when
// a skill invokes the approval(prompt) binding; a Go skill that needs
// the gate calls it directly. The method blocks until a decision is
// delivered through the Registry or the configured timeout elapses.
func (g *Gate) Approve(ctx context.Context, prompt string) (sandbox.ApprovalDecision, error) {
	id, err := newApprovalID()
	if err != nil {
		return sandbox.ApprovalDecision{}, fmt.Errorf("compose: minting approval id: %w", err)
	}
	now := time.Now().UTC()
	timeoutSeconds := int64(g.timeout / time.Second)
	warnAt := time.Duration(float64(g.timeout) * g.warnFraction)
	warnSeconds := int64(warnAt / time.Second)
	if _, err := g.recorder.Record(persist.EventApprovalRequest, persist.ApprovalRequestPayload{
		ApprovalID:     id,
		Prompt:         prompt,
		TimeoutSeconds: timeoutSeconds,
		WarnAtSeconds:  warnSeconds,
	}); err != nil {
		return sandbox.ApprovalDecision{}, fmt.Errorf("compose: recording approval request: %w", err)
	}

	pending := &Pending{
		ID:        id,
		RunID:     g.runID,
		Skill:     g.skill,
		Prompt:    prompt,
		CreatedAt: now,
		Timeout:   g.timeout,
		responses: make(chan resolution, 1),
	}
	if err := g.registry.register(pending); err != nil {
		return sandbox.ApprovalDecision{}, err
	}
	defer g.registry.remove(id)

	if err := g.notifier.NotifyApproval(ctx, ApprovalRequest{
		RunID:      g.runID,
		ApprovalID: id,
		Prompt:     prompt,
		Skill:      g.skill,
		Timeout:    g.timeout,
		CreatedAt:  now,
	}); err != nil {
		// Notification failure is logged but does not abort the
		// gate: the JSONL ledger and the registry are the load-
		// bearing surfaces; an out-of-band notification is best-
		// effort.
		g.warnf("agent.approval.notify_failed",
			"run_id", g.runID,
			"approval_id", id,
			"err", err.Error(),
		)
	}

	decision, source, err := g.wait(ctx, pending, warnAt)
	if err != nil {
		return sandbox.ApprovalDecision{}, err
	}
	if _, recErr := g.recorder.Record(persist.EventApprovalResponse, persist.ApprovalResponsePayload{
		ApprovalID: id,
		Approved:   decision.Approved,
		Reason:     decision.Reason,
		Source:     source,
	}); recErr != nil {
		return sandbox.ApprovalDecision{}, fmt.Errorf("compose: recording approval response: %w", recErr)
	}
	return decision, nil
}

// wait blocks until the gate is resolved by the registry, the run
// context is cancelled, or the timeout fires. The 80%-warn line is
// emitted exactly once; for short timeouts (where 80% < 1s) the
// warning fires immediately on entry.
func (g *Gate) wait(ctx context.Context, pending *Pending, warnAt time.Duration) (sandbox.ApprovalDecision, string, error) {
	// Use NewTimer so we can drain the channel deterministically on
	// fast exits without a Go-runtime "garbage collected unfired
	// timer" warning under -race.
	warnTimer := time.NewTimer(warnAt)
	defer warnTimer.Stop()
	var timeoutCh <-chan time.Time
	var timeoutTimer *time.Timer
	if g.timeoutPolicy == TimeoutReject {
		timeoutTimer = time.NewTimer(g.timeout)
		timeoutCh = timeoutTimer.C
		defer timeoutTimer.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			return sandbox.ApprovalDecision{}, "ctx", ctx.Err()
		case <-warnTimer.C:
			g.warnf("agent.approval.still_pending",
				"run_id", g.runID,
				"approval_id", pending.ID,
				"skill", g.skill,
			)
		case <-timeoutCh:
			return sandbox.ApprovalDecision{Approved: false, Reason: "approval window elapsed"}, "timeout", nil
		case res := <-pending.responses:
			return sandbox.ApprovalDecision{Approved: res.approved, Reason: res.reason}, res.source, nil
		}
	}
}

// warnf emits a slog warning, defaulting to slog.Default when no
// gate-level logger is configured.
func (g *Gate) warnf(msg string, args ...any) {
	logger := g.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn(msg, args...)
}

// resolution is the internal channel value Pending uses to carry a
// decision. The gate field of the registry exposes Resolve(...) for
// out-of-band consumers — the channel itself stays unexported so
// callers cannot deliver multiple decisions for one ID.
type resolution struct {
	approved bool
	reason   string
	source   string
}

// Pending is the registry-side view of an in-flight approval. List
// returns slices of these for `runs list --format json` and for the
// web UI's polling endpoint; the Resolve method on the registry
// drains the corresponding channel once.
type Pending struct {
	ID        string
	RunID     string
	Skill     string
	Prompt    string
	CreatedAt time.Time
	Timeout   time.Duration

	// responses delivers the resolution exactly once; the registry
	// closes the channel after a successful Resolve so a second
	// Resolve call returns ErrAlreadyResolved instead of blocking.
	responses chan resolution
	resolved  bool
}

// Registry is the process-wide pending-approvals table. Goroutines
// inside a Gate.Approve register a Pending; CLI / API / MCP consumers
// look up by ID and Resolve. Both sides are fully thread-safe.
type Registry struct {
	mu      sync.Mutex
	pending map[string]*Pending
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{pending: make(map[string]*Pending)}
}

// register adds a Pending. Internal; called only by Gate.Approve.
func (r *Registry) register(p *Pending) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.pending[p.ID]; exists {
		return fmt.Errorf("compose: approval id %q already registered", p.ID)
	}
	r.pending[p.ID] = p
	return nil
}

// remove drops a Pending without delivering a decision. Used by
// Gate.Approve as a defer to prevent leaks on ctx cancellation.
func (r *Registry) remove(id string) {
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

// ErrApprovalNotFound is returned by Resolve when the ID does not
// match a registered pending approval. Callers map this to HTTP 404 /
// CLI exit-2.
var ErrApprovalNotFound = errors.New("compose: approval not found or already resolved")

// ErrAlreadyResolved is returned by Resolve when the same ID has been
// resolved already. Distinguished from ErrApprovalNotFound so callers
// can render different UX (404 vs 409 in HTTP, "already approved" vs
// "no such approval" in CLI).
var ErrAlreadyResolved = errors.New("compose: approval already resolved")

// Resolve delivers a decision to a pending approval. Source is one of
// "cli", "web", "mcp", or "timeout"; it is recorded verbatim in the
// JSONL ledger so the inspect view can render where the response
// came from.
func (r *Registry) Resolve(id string, approved bool, reason, source string) error {
	r.mu.Lock()
	pending, ok := r.pending[id]
	if !ok {
		r.mu.Unlock()
		return ErrApprovalNotFound
	}
	if pending.resolved {
		r.mu.Unlock()
		return ErrAlreadyResolved
	}
	pending.resolved = true
	r.mu.Unlock()
	pending.responses <- resolution{approved: approved, reason: reason, source: source}
	return nil
}

// Lookup returns a snapshot of a Pending by ID. The returned struct
// is safe to share — no mutable channel reference is exposed.
func (r *Registry) Lookup(id string) (Pending, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pending[id]
	if !ok {
		return Pending{}, false
	}
	return Pending{
		ID:        p.ID,
		RunID:     p.RunID,
		Skill:     p.Skill,
		Prompt:    p.Prompt,
		CreatedAt: p.CreatedAt,
		Timeout:   p.Timeout,
	}, true
}

// List returns every currently pending approval. The slice is a
// snapshot; subsequent registrations or resolutions do not mutate the
// returned slice.
func (r *Registry) List() []Pending {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Pending, 0, len(r.pending))
	for _, p := range r.pending {
		out = append(out, Pending{
			ID:        p.ID,
			RunID:     p.RunID,
			Skill:     p.Skill,
			Prompt:    p.Prompt,
			CreatedAt: p.CreatedAt,
			Timeout:   p.Timeout,
		})
	}
	return out
}

// newApprovalID mints a fresh approval handle. The "ap_" prefix keeps
// the ID self-describing in CLI output and JSONL ledgers.
func newApprovalID() (string, error) {
	buf := make([]byte, approvalIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ap_" + hex.EncodeToString(buf), nil
}

// Static check: Gate satisfies the sandbox.Approver contract. The
// blank-identifier assignment surfaces a build-time error if the
// interface drifts.
var _ sandbox.Approver = (*Gate)(nil)
