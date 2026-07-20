package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ledgerVersion is the on-disk schema version. A newer version than we know
// is treated like corruption: WARN and start fresh (the ledger is a cache of
// spend, not a source of configuration).
const ledgerVersion = 1

// ledgerFile is the durable spend record: one row per budget entry, keyed by
// scope|key|period. It exists so a daemon restart mid-window never refills a
// spent budget — independent of the opt-in telemetry persistence.
type ledgerFile struct {
	Version int                    `json:"version"`
	Entries map[string]ledgerEntry `json:"entries"`
}

type ledgerEntry struct {
	WindowStart   time.Time `json:"window_start"`
	SpentMicroUSD int64     `json:"spent_micro_usd"`
	Warned        bool      `json:"warned,omitempty"`
	Exceeded      bool      `json:"exceeded,omitempty"`
}

// orphanMaxAge bounds how long ledger rows for removed config entries are
// preserved: long enough to outlive any window (a month plus slack), short
// enough that the file never accretes stale rows forever.
const orphanMaxAge = 35 * 24 * time.Hour

// loadLedger seeds budget entries from the ledger file. Only rows whose
// stored window matches the entry's current window are adopted; stale rows
// stay at zero (the window rolled while the daemon was down). Window
// comparison is by instant, so a machine timezone change between runs makes
// the stored window a different instant and resets spend — consistent with
// the local-calendar-window design. Rows matching no compiled entry are
// retained (bounded by orphanMaxAge) so removing and re-adding a budget
// within its window never refills spent budget. Missing files are silent;
// unreadable or corrupt files WARN and start fresh.
func (p *Policy) loadLedger(now time.Time) {
	data, err := os.ReadFile(p.ledgerPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			p.logger.Warn("limits: could not read spend ledger; starting fresh",
				"path", p.ledgerPath, "error", err)
		}
		return
	}
	var lf ledgerFile
	if err := json.Unmarshal(data, &lf); err != nil || lf.Version != ledgerVersion {
		p.logger.Warn("limits: spend ledger corrupt or from a different version; starting fresh",
			"path", p.ledgerPath, "version", lf.Version, "error", err)
		return
	}
	compiled := make(map[string]bool, len(p.budgets))
	for _, e := range p.budgets {
		compiled[e.ledgerKey()] = true
		row, ok := lf.Entries[e.ledgerKey()]
		if !ok {
			continue
		}
		if !row.WindowStart.Equal(windowStart(e.period, now)) {
			continue // stale window; stays zero
		}
		e.mu.Lock()
		e.spentMicro = row.SpentMicroUSD
		e.warned = row.Warned
		e.overLogged = row.Exceeded
		e.mu.Unlock()
	}
	for key, row := range lf.Entries {
		if compiled[key] || now.Sub(row.WindowStart) > orphanMaxAge {
			continue
		}
		if p.orphanRows == nil {
			p.orphanRows = make(map[string]ledgerEntry)
		}
		p.orphanRows[key] = row
	}
}

// snapshotLedger captures the current budget state as a ledger file,
// carrying preserved orphan rows along so they survive rewrites.
func (p *Policy) snapshotLedger() ledgerFile {
	lf := ledgerFile{Version: ledgerVersion, Entries: make(map[string]ledgerEntry, len(p.budgets)+len(p.orphanRows))}
	for key, row := range p.orphanRows {
		lf.Entries[key] = row
	}
	for _, e := range p.budgets {
		e.mu.Lock()
		lf.Entries[e.ledgerKey()] = ledgerEntry{
			WindowStart:   e.windowStart,
			SpentMicroUSD: e.spentMicro,
			Warned:        e.warned,
			Exceeded:      e.overLogged,
		}
		e.mu.Unlock()
	}
	return lf
}

// Flush writes the ledger atomically (temp file + rename). Failures WARN and
// are retried on the next dirty signal; spend is never worth crashing over.
func (p *Policy) Flush(_ context.Context) {
	p.flushNow()
}

// flushNow is the context-free core of Flush. The shutdown paths use it
// directly: the final flush must complete even when the run context is
// already canceled, and the write is a single small local file.
func (p *Policy) flushNow() {
	if p == nil || len(p.budgets) == 0 || p.ledgerPath == "" {
		return
	}
	lf := p.snapshotLedger()
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		p.logger.Warn("limits: could not encode spend ledger", "error", err)
		return
	}
	if err := atomicWrite(p.ledgerPath, data); err != nil {
		p.logger.Warn("limits: could not write spend ledger", "path", p.ledgerPath, "error", err)
	}
}

// markDirty nudges the flusher without blocking the dispatch path.
func (p *Policy) markDirty() {
	if p == nil || p.ledgerPath == "" {
		return
	}
	select {
	case p.dirty <- struct{}{}:
	default:
	}
}

// Start launches the debounced ledger flusher. It is a no-op for a nil
// policy, a policy with no budgets, or an empty ledger path, so a stack
// without budgets gains zero goroutines. Idempotent: only the first call
// starts the goroutine. ctx cancellation and Stop both terminate the
// flusher after a final flush.
func (p *Policy) Start(ctx context.Context) {
	if p == nil || len(p.budgets) == 0 || p.ledgerPath == "" {
		return
	}
	alreadyStarted := true
	p.startOnce.Do(func() {
		p.started.Store(true)
		alreadyStarted = false
	})
	if alreadyStarted {
		return
	}
	go func() {
		defer close(p.done)
		timer := time.NewTimer(flushDebounce)
		if !timer.Stop() {
			<-timer.C
		}
		pending := false
		for {
			select {
			case <-p.dirty:
				if !pending {
					timer.Reset(flushDebounce)
					pending = true
				}
			case <-timer.C:
				pending = false
				p.flushNow()
			case <-ctx.Done():
				p.flushNow()
				return
			case <-p.stop:
				p.flushNow()
				return
			}
		}
	}()
}

// Stop terminates the flusher after a final flush. Safe to call more than
// once and on a policy that never started: without a running flusher it
// performs the final flush inline instead of waiting on the done channel.
func (p *Policy) Stop() {
	if p == nil || len(p.budgets) == 0 || p.ledgerPath == "" {
		return
	}
	p.stopOnce.Do(func() {
		if !p.started.Load() {
			p.flushNow()
			return
		}
		close(p.stop)
		<-p.done
	})
}

// atomicWrite writes data to path via a temp file and rename, creating the
// parent directory when needed. Mirrors the stackedit.AtomicWrite idiom.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ledger dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
