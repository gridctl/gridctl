package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type attemptContextKey struct{}

type attemptRef struct {
	ID       string
	RootID   string
	ParentID string
}

type warnEvent struct {
	reason string
	msg    string
}

// Recorder is one stack-owned, single-writer run recorder.
type Recorder struct {
	cfg Config

	instanceID string
	logger     *slog.Logger

	gen        atomic.Uint64
	seq        atomic.Uint64
	wipeEpoch  atomic.Uint64
	enabled    atomic.Bool
	closed     atomic.Bool
	openFailed atomic.Bool

	queue  chan queuedEvent
	warnCh chan warnEvent

	drops struct {
		queueFull     atomic.Uint64
		tooLarge      atomic.Uint64
		closed        atomic.Uint64
		generation    atomic.Uint64
		capExhausted  atomic.Uint64
		writeError    atomic.Uint64
		syncError     atomic.Uint64
		shutdownLimit atomic.Uint64
		discarded     atomic.Uint64
		openError     atomic.Uint64
	}

	lastAppend atomic.Value // time.Time
	processAt  time.Time

	warnMu   sync.Mutex
	lastWarn map[string]time.Time

	mu     sync.Mutex
	writer *Writer
	omit   bool

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

type queuedEvent struct {
	gen     uint64
	epoch   uint64
	payload []byte
}

// NewRecorder starts a single writer goroutine. Enablement is requested
// independently of destination health: an unopenable destination yields a
// degraded recorder rather than a nil one.
func NewRecorder(cfg Config, logger *slog.Logger) (*Recorder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = DefaultQueueSize
	}
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = DefaultSyncInterval
	}
	if cfg.ShutdownDrain <= 0 {
		cfg.ShutdownDrain = DefaultShutdownDrain
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = DefaultMaxAge
	}
	if cfg.Dir == "" && cfg.StackName != "" {
		dir, err := Dir(cfg.StackName)
		if err == nil {
			cfg.Dir = dir
		}
	}
	r := &Recorder{
		cfg:        cfg,
		instanceID: newID(),
		logger:     logger,
		queue:      make(chan queuedEvent, cfg.QueueSize),
		warnCh:     make(chan warnEvent, 8),
		lastWarn:   make(map[string]time.Time),
		omit:       cfg.OmitLabels,
		processAt:  time.Now().UTC(),
	}
	r.gen.Store(1)
	r.enabled.Store(cfg.Enabled)
	if cfg.Enabled {
		if cfg.Dir == "" {
			r.openFailed.Store(true)
			r.drops.openError.Add(1)
		} else {
			w, err := newWriter(WriterConfig{
				Dir:         cfg.Dir,
				MaxBytes:    cfg.MaxBytes,
				MaxAge:      cfg.MaxAge,
				SegmentSize: cfg.SegmentSize,
			})
			if err != nil {
				r.openFailed.Store(true)
				r.drops.openError.Add(1)
			} else {
				r.writer = w
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go r.loop(ctx)
	if r.openFailed.Load() {
		r.warn(DropOpenError, "run recorder failed to open destination")
	}
	return r, nil
}

func (r *Recorder) loop(ctx context.Context) {
	defer r.wg.Done()
	syncTick := time.NewTicker(r.cfg.SyncInterval)
	defer syncTick.Stop()
	pruneTick := time.NewTicker(pruneInterval)
	defer pruneTick.Stop()
	for {
		select {
		case <-ctx.Done():
			r.drain(r.cfg.ShutdownDrain)
			r.mu.Lock()
			if r.writer != nil {
				_ = r.writer.Close()
			}
			r.mu.Unlock()
			return
		case ev := <-r.queue:
			r.writeOne(ctx, ev)
		case ev := <-r.warnCh:
			r.emitWarn(ev)
		case <-syncTick.C:
			r.sync(ctx)
		case <-pruneTick.C:
			r.prune()
		}
	}
}

func (r *Recorder) writeOne(ctx context.Context, ev queuedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ev.gen != r.gen.Load() || ev.epoch != r.wipeEpoch.Load() {
		r.drops.generation.Add(1)
		return
	}
	w := r.writer
	if w == nil {
		r.drops.closed.Add(1)
		return
	}
	if err := w.Append(ctx, ev.payload); err != nil {
		if err == errCapExhausted {
			r.drops.capExhausted.Add(1)
			r.warnLocked(DropCapExhausted, "run recorder stopped appending; capacity exhausted")
			return
		}
		if err == context.Canceled || err == context.DeadlineExceeded {
			r.drops.shutdownLimit.Add(1)
			return
		}
		r.drops.writeError.Add(1)
		r.warnLocked(DropWriteError, "run recorder append failed")
		return
	}
	now := time.Now().UTC()
	r.lastAppend.Store(now)
}

func (r *Recorder) sync(ctx context.Context) {
	r.mu.Lock()
	w := r.writer
	r.mu.Unlock()
	if w == nil {
		return
	}
	if err := w.Sync(ctx); err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded {
			return
		}
		r.drops.syncError.Add(1)
		r.warn(DropSyncError, "run recorder sync failed")
	}
}

func (r *Recorder) prune() {
	r.mu.Lock()
	w := r.writer
	r.mu.Unlock()
	if w == nil {
		return
	}
	if err := w.Prune(time.Now()); err != nil {
		r.drops.writeError.Add(1)
		r.warn(DropWriteError, "run recorder prune failed")
	}
}

func (r *Recorder) drain(limit time.Duration) {
	deadline := time.Now().Add(limit)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	for {
		if time.Now().After(deadline) {
			r.abandonQueue()
			return
		}
		select {
		case ev := <-r.queue:
			if time.Now().After(deadline) {
				r.drops.shutdownLimit.Add(1)
				r.abandonQueue()
				return
			}
			r.writeOne(ctx, ev)
		default:
			r.sync(ctx)
			return
		}
	}
}

func (r *Recorder) abandonQueue() {
	var n uint64
	for {
		select {
		case <-r.queue:
			n++
		default:
			if n > 0 {
				r.drops.shutdownLimit.Add(n)
			}
			return
		}
	}
}

func (r *Recorder) warn(reason, msg string) {
	if r.warnCh == nil {
		return
	}
	select {
	case r.warnCh <- warnEvent{reason: reason, msg: msg}:
	default:
	}
}

func (r *Recorder) warnLocked(reason, msg string) {
	r.warn(reason, msg)
}

func (r *Recorder) emitWarn(ev warnEvent) {
	r.warnMu.Lock()
	now := time.Now()
	if last, ok := r.lastWarn[ev.reason]; ok && now.Sub(last) < 10*time.Second {
		r.warnMu.Unlock()
		return
	}
	r.lastWarn[ev.reason] = now
	r.warnMu.Unlock()
	r.logger.Warn(ev.msg, "reason", ev.reason)
}

// Begin starts one attempt bound to the recorder generation active at entry.
func (r *Recorder) Begin(ctx context.Context, requestedName string) *Attempt {
	a := &Attempt{
		ctx:       ctx,
		rec:       r,
		started:   time.Now(),
		startedAt: time.Now().UTC(),
	}
	if r == nil || !r.enabled.Load() || r.closed.Load() {
		return a
	}
	r.mu.Lock()
	a.omitLabels = r.omit
	r.mu.Unlock()
	a.gen = r.gen.Load()
	a.epoch = r.wipeEpoch.Load()
	a.id = newID()
	a.active = true
	if parent, ok := ctx.Value(attemptContextKey{}).(attemptRef); ok && parent.ID != "" {
		a.parentID = parent.ID
		if parent.RootID != "" {
			a.rootID = parent.RootID
		} else {
			a.rootID = parent.ID
		}
	} else {
		a.rootID = a.id
	}
	a.requested, a.requestedOmit = BoundIdentifier(requestedName)
	a.ctx = context.WithValue(ctx, attemptContextKey{}, attemptRef{
		ID:       a.id,
		RootID:   a.rootID,
		ParentID: a.parentID,
	})
	return a
}

// Attempt is the per-invocation builder. Metadata is immutable once Finish
// materializes the record.
type Attempt struct {
	ctx        context.Context
	rec        *Recorder
	active     bool
	finished   atomic.Bool
	gen        uint64
	epoch      uint64
	id         string
	parentID   string
	rootID     string
	prevID     string
	started    time.Time
	startedAt  time.Time
	omitLabels bool

	requested     string
	requestedOmit string
	server        string
	serverOmit    string
	tool          string
	toolOmit      string
	replica       *int
	traceID       string
	clientLabel   string
	accessLabel   string
	downstream    time.Duration
	disposition   string
	stage         string
	reason        string
}

// Context returns the attempt context, including parent/root IDs for nested calls.
func (a *Attempt) Context() context.Context {
	if a == nil || a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// SetOutcome records the disposition at a real decision point.
func (a *Attempt) SetOutcome(disposition, stage, reason string) {
	if a == nil {
		return
	}
	a.disposition = disposition
	a.stage = stage
	a.reason = reason
}

// SetResolved stores bounded resolved identifiers. Unresolved targets stay absent.
func (a *Attempt) SetResolved(server, tool string, replicaID int) {
	if a == nil {
		return
	}
	a.server, a.serverOmit = BoundIdentifier(server)
	a.tool, a.toolOmit = BoundIdentifier(tool)
	id := replicaID
	a.replica = &id
}

// SetDownstreamDuration labels the downstream timer separately from total duration.
func (a *Attempt) SetDownstreamDuration(d time.Duration) {
	if a == nil {
		return
	}
	a.downstream = d
}

// SetTraceID stores a genuine trace identifier. Empty values are ignored.
func (a *Attempt) SetTraceID(id string) {
	if a == nil || id == "" {
		return
	}
	stored, omitted := BoundIdentifier(id)
	if omitted == "" {
		a.traceID = stored
	}
}

// SetLabels stores optional caller-declared labels. They are not authenticated principals.
func (a *Attempt) SetLabels(client, access string) {
	if a == nil {
		return
	}
	a.clientLabel, _ = BoundIdentifier(client)
	a.accessLabel, _ = BoundIdentifier(access)
}

// SetPreviousAttemptID links a resumed round when trusted internal correlation exists.
func (a *Attempt) SetPreviousAttemptID(id string) {
	if a == nil || !ValidGeneratedID(id) {
		return
	}
	a.prevID = id
}

// Finish materializes one immutable record and enqueues it. Safe to call once.
func (a *Attempt) Finish() {
	if a == nil || !a.active || a.rec == nil {
		return
	}
	if !a.finished.CompareAndSwap(false, true) {
		return
	}
	if a.disposition == "" {
		return
	}
	r := a.rec
	if r.closed.Load() {
		r.drops.closed.Add(1)
		return
	}
	if !r.enabled.Load() {
		r.drops.discarded.Add(1)
		return
	}
	if a.gen != r.gen.Load() || a.epoch != r.wipeEpoch.Load() {
		r.drops.generation.Add(1)
		return
	}
	rec := a.materialize()
	payload, err := json.Marshal(rec)
	if err != nil || len(payload) > DefaultMaxRecordBytes {
		r.drops.tooLarge.Add(1)
		return
	}
	ev := queuedEvent{gen: a.gen, epoch: a.epoch, payload: payload}
	select {
	case r.queue <- ev:
	default:
		r.drops.queueFull.Add(1)
		r.warn(DropQueueFull, "run recorder dropped event; queue full")
	}
}

func (a *Attempt) materialize() Record {
	seq := a.rec.seq.Add(1)
	rec := Record{
		SchemaVersion:      SchemaVersion,
		RecorderInstanceID: a.rec.instanceID,
		Sequence:           seq,
		AttemptID:          a.id,
		ParentAttemptID:    a.parentID,
		RootAttemptID:      a.rootID,
		PreviousAttemptID:  a.prevID,
		StartedAt:          a.startedAt,
		ReturnedAt:         time.Now().UTC(),
		DurationMS:         time.Since(a.started).Milliseconds(),
		RequestedName:      a.requested,
		RequestedOmitted:   a.requestedOmit,
		ResolvedServer:     a.server,
		ResolvedServerOmit: a.serverOmit,
		ResolvedTool:       a.tool,
		ResolvedToolOmit:   a.toolOmit,
		Disposition:        a.disposition,
		Stage:              a.stage,
		Reason:             a.reason,
		ReplicaID:          a.replica,
		TraceID:            a.traceID,
	}
	if a.downstream > 0 {
		rec.DownstreamMS = a.downstream.Milliseconds()
	}
	if !a.omitLabels {
		rec.ClientLabel = a.clientLabel
		rec.AccessLabel = a.accessLabel
	}
	if rec.RootAttemptID == rec.AttemptID {
		rec.RootAttemptID = ""
	}
	return rec
}

// Status returns independent recorder health.
func (r *Recorder) Status() Status {
	if r == nil {
		return Status{Enabled: false, Effective: false, WriterHealth: HealthUnknown, HistoricalLoss: LossUnknown, Known: false, Drops: map[string]uint64{}, Failures: map[string]uint64{}}
	}
	r.mu.Lock()
	omit := r.omit
	maxBytes := r.cfg.MaxBytes
	maxAge := r.cfg.MaxAge
	w := r.writer
	r.mu.Unlock()
	st := Status{
		Enabled:             r.enabled.Load(),
		Effective:           r.enabled.Load() && !r.closed.Load() && w != nil && !r.openFailed.Load(),
		QueueDepth:          len(r.queue),
		QueueCapacity:       cap(r.queue),
		Drops:               map[string]uint64{},
		Failures:            map[string]uint64{},
		ProcessStartedAt:    r.processAt,
		HistoricalLoss:      LossUnknown,
		WipeEpoch:           r.wipeEpoch.Load(),
		Generation:          r.gen.Load(),
		RecorderInstanceID:  r.instanceID,
		Known:               true,
		OmitLabels:          omit,
		RetentionMaxBytes:   maxBytes,
		RetentionMaxAgeDays: int(maxAge / (24 * time.Hour)),
	}
	if n := r.drops.queueFull.Load(); n > 0 {
		st.Drops[DropQueueFull] = n
	}
	if n := r.drops.tooLarge.Load(); n > 0 {
		st.Drops[DropTooLarge] = n
	}
	if n := r.drops.closed.Load(); n > 0 {
		st.Drops[DropClosed] = n
	}
	if n := r.drops.generation.Load(); n > 0 {
		st.Drops[DropGeneration] = n
	}
	if n := r.drops.capExhausted.Load(); n > 0 {
		st.Drops[DropCapExhausted] = n
	}
	if n := r.drops.shutdownLimit.Load(); n > 0 {
		st.Drops[DropShutdownLimit] = n
	}
	if n := r.drops.discarded.Load(); n > 0 {
		st.Drops[DropDiscarded] = n
	}
	if n := r.drops.openError.Load(); n > 0 {
		st.Failures[DropOpenError] = n
		st.Drops[DropOpenError] = n
	}
	if n := r.drops.writeError.Load(); n > 0 {
		st.Failures[DropWriteError] = n
		st.Drops[DropWriteError] = n
	}
	if n := r.drops.syncError.Load(); n > 0 {
		st.Failures[DropSyncError] = n
		st.Drops[DropSyncError] = n
	}
	if v := r.lastAppend.Load(); v != nil {
		t := v.(time.Time)
		st.LastSuccessfulAppend = &t
	}
	if w == nil {
		if r.openFailed.Load() || r.enabled.Load() {
			st.WriterHealth = HealthDegraded
		} else {
			st.WriterHealth = HealthStopped
		}
		st.Effective = false
		return st
	}
	snap := w.snapshot()
	st.WriterHealth = snap.Health
	st.LogicalBytes = snap.TotalBytes
	st.Synced = snap.SyncedSeq > 0 && snap.SyncedSeq == snap.WrittenSeq && !snap.LastSync.IsZero()
	if !snap.LastSync.IsZero() {
		t := snap.LastSync
		st.LastSuccessfulSync = &t
	}
	if snap.WriteErrs > 0 {
		st.Failures[DropWriteError] = snap.WriteErrs
	}
	if snap.SyncErrs > 0 {
		st.Failures[DropSyncError] = snap.SyncErrs
	}
	if snap.TailRecovered {
		st.Failures["partial_tail"] = 1
	}
	if snap.PruneFailures > 0 {
		st.Failures[DropWriteError] = snap.PruneFailures
	}
	if r.openFailed.Load() {
		st.WriterHealth = HealthDegraded
		st.Effective = false
	}
	if !st.Enabled {
		st.Effective = false
	}
	return st
}

// Apply updates enablement, labels, and retention without racing in-flight
// attempts. In-flight attempts stay bound to the generation they began with.
func (r *Recorder) Apply(cfg Config) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := cfg.Dir
	if dir == "" && cfg.StackName != "" {
		d, err := Dir(cfg.StackName)
		if err == nil {
			dir = d
		}
	}
	oldDir := r.cfg.Dir
	if dir != "" {
		r.cfg.Dir = dir
	}
	if cfg.StackName != "" {
		r.cfg.StackName = cfg.StackName
	}
	r.omit = cfg.OmitLabels
	r.cfg.OmitLabels = cfg.OmitLabels
	if cfg.MaxBytes > 0 {
		r.cfg.MaxBytes = cfg.MaxBytes
	}
	if cfg.MaxAge > 0 {
		r.cfg.MaxAge = cfg.MaxAge
	}
	enabled := cfg.Enabled
	wasEnabled := r.enabled.Load()
	r.enabled.Store(enabled)
	if !enabled {
		if wasEnabled {
			r.gen.Add(1)
		}
		if r.writer != nil {
			_ = r.writer.Close()
			r.writer = nil
		}
		r.openFailed.Store(false)
		return nil
	}
	if r.writer != nil && oldDir == dir && dir != "" {
		r.writer.setRetention(r.cfg.MaxBytes, r.cfg.MaxAge)
		r.openFailed.Store(false)
		return nil
	}
	if r.writer != nil {
		_ = r.writer.Close()
		r.writer = nil
	}
	r.gen.Add(1)
	if dir == "" {
		r.openFailed.Store(true)
		r.drops.openError.Add(1)
		r.warnLocked(DropOpenError, "run recorder failed to open destination")
		return fmt.Errorf("runs recorder: directory is required")
	}
	w, err := newWriter(WriterConfig{
		Dir:         dir,
		MaxBytes:    r.cfg.MaxBytes,
		MaxAge:      r.cfg.MaxAge,
		SegmentSize: cfg.SegmentSize,
	})
	if err != nil {
		r.openFailed.Store(true)
		r.drops.openError.Add(1)
		r.warnLocked(DropOpenError, "run recorder failed to open destination")
		return err
	}
	r.openFailed.Store(false)
	r.writer = w
	return nil
}

// Disable recording without deleting retained history.
func (r *Recorder) Disable() {
	if r == nil {
		return
	}
	r.enabled.Store(false)
}

// Close drains the queue up to the shutdown bound and closes the writer.
func (r *Recorder) Close() {
	if r == nil || !r.closed.CompareAndSwap(false, true) {
		return
	}
	r.enabled.Store(false)
	r.cancel()
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	grace := r.cfg.ShutdownDrain + 250*time.Millisecond
	select {
	case <-done:
	case <-time.After(grace):
		r.drops.shutdownLimit.Add(1)
	}
}

// Wipe is a writer-coordinated barrier. Pre-wipe queued events are dropped
// by epoch mismatch. Partial deletion is returned as an error.
func (r *Recorder) Wipe(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wipeEpoch.Add(1)
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.writer == nil {
		if r.cfg.Dir == "" {
			return nil
		}
		return deleteOwned(r.cfg.Dir)
	}
	return r.writer.wipeAndReopen()
}

// DirPath returns the configured storage directory.
func (r *Recorder) DirPath() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Dir
}

// WipeEpoch returns the current wipe generation.
func (r *Recorder) WipeEpoch() uint64 {
	if r == nil {
		return 0
	}
	return r.wipeEpoch.Load()
}

func ParentAttemptID(ctx context.Context) string {
	if ref, ok := ctx.Value(attemptContextKey{}).(attemptRef); ok {
		return ref.ID
	}
	return ""
}
