package contexts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
)

// Client sync states. "stale" means the target still matches what gridctl
// wrote but the canonical file has changed since (a sync is pending).
const (
	StateUnsupported   = "unsupported"
	StateNeverSynced   = "never-synced"
	StateInSync        = "in-sync"
	StateStale         = "stale"
	StateDrifted       = "drifted"
	StateTargetMissing = "target-missing"
)

// Sync result actions.
const (
	ActionCreated            = "created"
	ActionUpdated            = "updated"
	ActionUnchanged          = "unchanged"
	ActionSkippedDrift       = "skipped-drift"
	ActionSkippedUnavailable = "skipped-unavailable"
	ActionWouldCreate        = "would-create"
	ActionWouldUpdate        = "would-update"
	ActionError              = "error"
)

// ClientStatus is one client's row in `ctx status` and GET /api/context.
type ClientStatus struct {
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	Supported    bool       `json:"supported"`
	Available    bool       `json:"available"`
	Experimental bool       `json:"experimental,omitempty"`
	Strategy     string     `json:"strategy,omitempty"`
	TargetPath   string     `json:"target_path,omitempty"`
	State        string     `json:"state"`
	Detail       string     `json:"detail,omitempty"`
	SyncedAt     *time.Time `json:"synced_at,omitempty"`
}

// SyncOptions configure a sync pass.
type SyncOptions struct {
	// Force overwrites drifted targets and repairs corrupt blocks.
	Force bool
	// DryRun renders and diffs without writing anything.
	DryRun bool
}

// SyncResult describes what happened (or would happen) for one client.
type SyncResult struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Strategy   string `json:"strategy"`
	TargetPath string `json:"target_path"`
	Action     string `json:"action"`
	BackupPath string `json:"backup_path,omitempty"`
	Diff       string `json:"diff,omitempty"`
	Error      string `json:"error,omitempty"`
}

// UnsyncResult describes the removal of one client's managed artifact.
type UnsyncResult struct {
	Slug       string `json:"slug"`
	TargetPath string `json:"target_path"`
	// Action is "removed-file", "removed-region", or "already-gone".
	Action string `json:"action"`
}

// Statuses computes the per-client sync state for every known client,
// supported and unsupported, in display order.
func (m *Manager) Statuses(ctx context.Context) ([]ClientStatus, error) {
	lf, err := readLockFile(m.lockPath())
	if err != nil {
		return nil, err
	}
	canonicalHash := ""
	if content, cerr := m.CanonicalContent(); cerr == nil {
		canonicalHash = canonicalContentHash(content)
	}

	targets := Targets()
	statuses := make([]ClientStatus, 0, len(targets)+len(Unsupported()))
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		statuses = append(statuses, m.statusFor(t, lf.Clients[t.Slug], canonicalHash))
	}
	for _, u := range Unsupported() {
		statuses = append(statuses, ClientStatus{
			Slug:   u.Slug,
			Name:   u.Name,
			State:  StateUnsupported,
			Detail: u.Reason,
		})
	}
	return statuses, nil
}

// statusFor computes one supported client's status row.
func (m *Manager) statusFor(t Target, entry *ClientEntry, canonicalHash string) ClientStatus {
	cs := ClientStatus{
		Slug:         t.Slug,
		Name:         t.Name,
		Supported:    true,
		Available:    t.available(m.home),
		Experimental: t.Experimental,
		Strategy:     string(t.Strategy),
		TargetPath:   t.targetPath(m.home),
	}
	if cs.TargetPath == "" {
		cs.Supported = false
		cs.State = StateUnsupported
		cs.Detail = "no global context path on this platform"
		return cs
	}
	if entry == nil {
		cs.State = StateNeverSynced
		return cs
	}
	cs.SyncedAt = &entry.SyncedAt

	// A recorded sync whose canonical file has since disappeared needs
	// attention: the next sync would fail with ErrNoCanonical.
	if canonicalHash == "" {
		cs.State = StateStale
		cs.Detail = "canonical context file is missing; run 'gridctl ctx init'"
		return cs
	}

	data, err := os.ReadFile(cs.TargetPath)
	if err != nil {
		cs.State = StateTargetMissing
		if !os.IsNotExist(err) {
			// Unreadable is not the same as gone; surface the reason.
			cs.Detail = err.Error()
		}
		return cs
	}
	currentHash, found, err := managedRegionHash(t, string(data), m.CanonicalPath())
	if err != nil {
		cs.State = StateDrifted
		cs.Detail = err.Error()
		return cs
	}
	if !found {
		cs.State = StateDrifted
		cs.Detail = "managed content was removed from the target"
		return cs
	}
	if currentHash != entry.InstalledHash {
		cs.State = StateDrifted
		return cs
	}
	// Import shims reference the canonical file directly, so canonical
	// edits flow through without a re-sync; they are never stale.
	if t.Strategy != StrategyImportShim && canonicalHash != entry.CanonicalHash {
		cs.State = StateStale
		return cs
	}
	cs.State = StateInSync
	return cs
}

// NeedsSync reports whether any client requires attention: drifted,
// stale, or a recorded sync whose target file has gone missing. Backs
// `ctx sync --check` and the status exit code.
func NeedsSync(statuses []ClientStatus) bool {
	for _, cs := range statuses {
		switch cs.State {
		case StateDrifted, StateStale, StateTargetMissing:
			return true
		}
	}
	return false
}

// recorded reports whether this result updated the client's lock entry
// (unchanged still refreshes the canonical hash and timestamp).
func (r SyncResult) recorded() bool {
	return r.Action == ActionCreated || r.Action == ActionUpdated || r.Action == ActionUnchanged
}

// HasFailures reports whether any result needs the caller's attention:
// a write error or a drifted target that was skipped.
func HasFailures(results []SyncResult) bool {
	for _, r := range results {
		if r.Action == ActionError || r.Action == ActionSkippedDrift {
			return true
		}
	}
	return false
}

// resolveTarget maps a slug to its supported target, distinguishing
// deliberately-unsupported clients (with their reason) from typos.
func resolveTarget(slug string) (Target, error) {
	if t, ok := FindTarget(slug); ok {
		return t, nil
	}
	if u, ok := findUnsupported(slug); ok {
		return Target{}, fmt.Errorf("%w: %s (%s)", ErrUnsupported, u.Name, u.Reason)
	}
	return Target{}, fmt.Errorf("%w: %q (known clients: %s)", ErrUnknownClient, slug, strings.Join(SupportedSlugs(), ", "))
}

// SyncAll projects the canonical context to every supported, available
// client. Unavailable clients are reported as skipped, never errors.
func (m *Manager) SyncAll(ctx context.Context, opts SyncOptions) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	canonical, err := m.CanonicalContent()
	if err != nil {
		return nil, err
	}
	lf, err := readLockFile(m.lockPath())
	if err != nil {
		return nil, err
	}
	results := make([]SyncResult, 0, len(Targets()))
	lockDirty := false
	for _, t := range Targets() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if t.targetPath(m.home) == "" || !t.available(m.home) {
			results = append(results, SyncResult{
				Slug: t.Slug, Name: t.Name, Strategy: string(t.Strategy),
				TargetPath: t.targetPath(m.home), Action: ActionSkippedUnavailable,
			})
			continue
		}
		res := m.syncOne(t, lf, canonical, opts)
		if res.recorded() {
			lockDirty = true
		}
		results = append(results, res)
	}
	if lockDirty && !opts.DryRun {
		if err := writeLockFile(m.lockPath(), lf); err != nil {
			return results, err
		}
	}
	return results, nil
}

// SyncClient projects the canonical context to one explicitly named
// client. Unlike SyncAll, an unavailable client is an error here: the
// user asked for it by name and should hear why nothing happened.
func (m *Manager) SyncClient(ctx context.Context, slug string, opts SyncOptions) (SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.syncClientLocked(ctx, slug, opts)
}

// syncClientLocked is SyncClient for callers already holding mu (Adopt).
func (m *Manager) syncClientLocked(ctx context.Context, slug string, opts SyncOptions) (SyncResult, error) {
	if err := ctx.Err(); err != nil {
		return SyncResult{}, err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return SyncResult{}, err
	}
	if t.targetPath(m.home) == "" {
		return SyncResult{}, fmt.Errorf("%w: %s has no global context path on this platform", ErrUnsupported, t.Name)
	}
	if !t.available(m.home) {
		return SyncResult{}, fmt.Errorf("%w: %s (expected one of: %s)", ErrNotAvailable, t.Name, strings.Join(t.DetectDirs, ", "))
	}
	canonical, err := m.CanonicalContent()
	if err != nil {
		return SyncResult{}, err
	}
	lf, err := readLockFile(m.lockPath())
	if err != nil {
		return SyncResult{}, err
	}
	res := m.syncOne(t, lf, canonical, opts)
	if !opts.DryRun && res.recorded() {
		if err := writeLockFile(m.lockPath(), lf); err != nil {
			return res, err
		}
	}
	return res, nil
}

// syncOne renders and writes one client's target, updating its lock entry
// in place. Availability has already been checked.
func (m *Manager) syncOne(t Target, lf *LockFile, canonical string, opts SyncOptions) SyncResult {
	res := SyncResult{
		Slug: t.Slug, Name: t.Name,
		Strategy:   string(t.Strategy),
		TargetPath: t.targetPath(m.home),
	}

	existing, exists, err := readIfExists(res.TargetPath)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}

	// Clients self-mutate their config trees, so the drift check re-reads
	// and re-hashes immediately before the write decision.
	entry := lf.Clients[t.Slug]

	// A dedicated-file target that exists without a lock entry was not
	// written by this store (lost lock, or a user file with our name);
	// replacing it wholesale needs an explicit --force.
	if entry == nil && exists && t.Strategy == StrategyDedicatedFile && !opts.Force {
		res.Action = ActionSkippedDrift
		res.Error = res.TargetPath + " exists but is not tracked by gridctl; re-run with --force to overwrite it"
		return res
	}

	if entry != nil && exists && !opts.Force {
		currentHash, found, herr := managedRegionHash(t, existing, m.CanonicalPath())
		if herr != nil {
			res.Action = ActionSkippedDrift
			res.Error = herr.Error() + "; re-run with --force to repair after reviewing the file"
			return res
		}
		if found && currentHash != entry.InstalledHash {
			res.Action = ActionSkippedDrift
			return res
		}
		if !found {
			res.Action = ActionSkippedDrift
			res.Error = "managed content was removed from the target; re-run with --force to restore it"
			return res
		}
	}

	newContent, err := m.renderTarget(t, existing, canonical, opts.Force)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if rendered := utf8.RuneCountInString(newContent); t.MaxChars > 0 && rendered > t.MaxChars {
		res.Action = ActionError
		res.Error = fmt.Sprintf("%v: %d characters rendered, limit is %d", ErrOverCap, rendered, t.MaxChars)
		return res
	}
	// Shim and block insertions splice into a user-owned file: keep its
	// CRLF line endings instead of rewriting the whole file to LF.
	if exists && t.Strategy != StrategyDedicatedFile {
		newContent = restoreCRLF(existing, newContent)
	}

	if exists && normalizeNewlines(existing) == normalizeNewlines(newContent) {
		res.Action = ActionUnchanged
		m.recordSync(lf, t, entry, newContent, exists, canonical)
		return res
	}

	if opts.DryRun {
		if exists {
			res.Action = ActionWouldUpdate
		} else {
			res.Action = ActionWouldCreate
		}
		res.Diff = unifiedDiff(existing, newContent, res.TargetPath+" (current)", res.TargetPath+" (after sync)")
		return res
	}

	if err := os.MkdirAll(filepath.Dir(res.TargetPath), 0755); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	backup, err := createBackup(res.TargetPath)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	res.BackupPath = backup
	if err := atomicWriteFile(res.TargetPath, []byte(newContent)); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if exists {
		res.Action = ActionUpdated
	} else {
		res.Action = ActionCreated
	}
	m.recordSync(lf, t, entry, newContent, exists, canonical)
	return res
}

// renderTarget produces the full new target content per strategy.
func (m *Manager) renderTarget(t Target, existing, canonical string, force bool) (string, error) {
	switch t.Strategy {
	case StrategyDedicatedFile:
		return renderDedicated(t, canonical), nil
	case StrategyImportShim:
		return upsertShim(existing, m.CanonicalPath()), nil
	case StrategyBlock:
		return upsertBlock(existing, canonical, force)
	}
	return "", fmt.Errorf("unknown strategy %q", t.Strategy)
}

// recordSync updates (or creates) the client's lock entry after a write
// decision. preExisted tracks CreatedFile across repeated syncs.
func (m *Manager) recordSync(lf *LockFile, t Target, prev *ClientEntry, newContent string, preExisted bool, canonical string) {
	// Safe to discard found/err: newContent was just rendered by
	// renderTarget, so the managed region is present and well-formed by
	// construction.
	installedHash, _, _ := managedRegionHash(t, newContent, m.CanonicalPath())
	created := !preExisted
	if prev != nil {
		created = prev.CreatedFile
	}
	lf.Clients[t.Slug] = &ClientEntry{
		Strategy:      string(t.Strategy),
		Target:        t.targetPath(m.home),
		InstalledHash: installedHash,
		CanonicalHash: canonicalContentHash(canonical),
		CreatedFile:   created,
		SyncedAt:      time.Now().UTC(),
	}
}

// Adopt pulls a target's managed content back into the canonical file
// (chezmoi re-add semantics), then re-syncs that client so its hashes
// return to in-sync. Other clients become stale, which is correct: the
// canon changed.
func (m *Manager) Adopt(ctx context.Context, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return err
	}
	if t.Strategy == StrategyImportShim {
		return fmt.Errorf("%s uses an import shim that references the canonical file directly; there is no copied content to adopt", t.Name)
	}
	lf, err := readLockFile(m.lockPath())
	if err != nil {
		return err
	}
	if lf.Clients[t.Slug] == nil {
		return fmt.Errorf("%w: %s", ErrNotSynced, t.Name)
	}
	data, err := os.ReadFile(t.targetPath(m.home))
	if err != nil {
		return fmt.Errorf("reading %s: %w", t.targetPath(m.home), err)
	}

	var body string
	switch t.Strategy {
	case StrategyDedicatedFile:
		body = stripManagedChrome(t, string(data))
	case StrategyBlock:
		inner, found, berr := extractBlockInner(string(data))
		if berr != nil {
			return berr
		}
		if !found {
			return fmt.Errorf("no managed block found in %s; nothing to adopt", t.targetPath(m.home))
		}
		body = inner
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("managed content in %s is empty; refusing to adopt an empty canonical file", t.targetPath(m.home))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.saveCanonical(body); err != nil {
		return err
	}
	_, err = m.syncClientLocked(ctx, slug, SyncOptions{Force: true})
	return err
}

// Unsync removes one client's managed artifact and clears its lock entry.
// Files gridctl created are deleted outright; files the user owned lose
// only the managed region or shim line.
func (m *Manager) Unsync(ctx context.Context, slug string) (UnsyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return UnsyncResult{}, err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return UnsyncResult{}, err
	}
	lf, err := readLockFile(m.lockPath())
	if err != nil {
		return UnsyncResult{}, err
	}
	entry := lf.Clients[t.Slug]
	if entry == nil {
		return UnsyncResult{}, fmt.Errorf("%w: %s", ErrNotSynced, t.Name)
	}
	res, err := m.removeArtifact(t, entry)
	if err != nil {
		return res, err
	}
	delete(lf.Clients, t.Slug)
	if err := writeLockFile(m.lockPath(), lf); err != nil {
		return res, err
	}
	return res, nil
}

// UnsyncAll removes every synced client's managed artifact.
func (m *Manager) UnsyncAll(ctx context.Context) ([]UnsyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lf, err := readLockFile(m.lockPath())
	if err != nil {
		return nil, err
	}
	results := make([]UnsyncResult, 0, len(lf.Clients))
	for _, t := range Targets() {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if lf.Clients[t.Slug] == nil {
			continue
		}
		res, rerr := m.removeArtifact(t, lf.Clients[t.Slug])
		if rerr != nil {
			return results, rerr
		}
		delete(lf.Clients, t.Slug)
		results = append(results, res)
	}
	if err := writeLockFile(m.lockPath(), lf); err != nil {
		return results, err
	}
	return results, nil
}

// removeArtifact deletes the managed artifact per strategy.
func (m *Manager) removeArtifact(t Target, entry *ClientEntry) (UnsyncResult, error) {
	res := UnsyncResult{Slug: t.Slug, TargetPath: entry.Target}
	content, exists, err := readIfExists(entry.Target)
	if err != nil {
		return res, err
	}
	if !exists {
		res.Action = "already-gone"
		return res, nil
	}

	if t.Strategy == StrategyDedicatedFile {
		if _, err := createBackup(entry.Target); err != nil {
			return res, err
		}
		if err := os.Remove(entry.Target); err != nil {
			return res, fmt.Errorf("removing %s: %w", entry.Target, err)
		}
		res.Action = "removed-file"
		return res, nil
	}

	var remaining string
	switch t.Strategy {
	case StrategyImportShim:
		remaining = removeShim(content, m.CanonicalPath())
	case StrategyBlock:
		remaining, err = removeBlock(content)
		if err != nil {
			return res, err
		}
	}

	if entry.CreatedFile && strings.TrimSpace(remaining) == "" {
		if _, err := createBackup(entry.Target); err != nil {
			return res, err
		}
		if err := os.Remove(entry.Target); err != nil {
			return res, fmt.Errorf("removing %s: %w", entry.Target, err)
		}
		res.Action = "removed-file"
		return res, nil
	}

	if _, err := createBackup(entry.Target); err != nil {
		return res, err
	}
	if err := atomicWriteFile(entry.Target, []byte(remaining)); err != nil {
		return res, err
	}
	res.Action = "removed-region"
	return res, nil
}

// Diff renders a unified diff between the canonical content and one
// client's current managed content.
func (m *Manager) Diff(ctx context.Context, slug string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return "", err
	}
	canonical, err := m.CanonicalContent()
	if err != nil {
		return "", err
	}
	targetPath := t.targetPath(m.home)
	content, exists, err := readIfExists(targetPath)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("%s does not exist; run 'gridctl ctx sync %s' first", targetPath, slug)
	}

	var current string
	switch t.Strategy {
	case StrategyImportShim:
		if hasShim(content, m.CanonicalPath()) {
			return "", nil
		}
		return fmt.Sprintf("import line %q is missing from %s\n", shimLine(m.CanonicalPath()), targetPath), nil
	case StrategyDedicatedFile:
		current = stripManagedChrome(t, content)
	case StrategyBlock:
		inner, found, berr := extractBlockInner(content)
		if berr != nil {
			return "", berr
		}
		if !found {
			return fmt.Sprintf("no managed block found in %s\n", targetPath), nil
		}
		current = inner
	}
	return unifiedDiff(
		strings.TrimSpace(normalizeNewlines(canonical))+"\n",
		strings.TrimSpace(current)+"\n",
		"canonical",
		targetPath,
	), nil
}

// SupportedSlugs lists the supported client slugs, derived from the
// strategy table so error messages and help text never go stale.
func SupportedSlugs() []string {
	targets := Targets()
	slugs := make([]string, len(targets))
	for i, t := range targets {
		slugs[i] = t.Slug
	}
	return slugs
}

// readIfExists reads path, distinguishing absence from read errors.
func readIfExists(path string) (content string, exists bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), true, nil
}

// unifiedDiff renders a unified diff between two labeled texts. The
// error is safe to swallow: GetUnifiedDiffString writes into an
// in-memory buffer, whose writes cannot fail.
func unifiedDiff(a, b, fromLabel, toLabel string) string {
	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(normalizeNewlines(a)),
		B:        difflib.SplitLines(normalizeNewlines(b)),
		FromFile: fromLabel,
		ToFile:   toLabel,
		Context:  3,
	})
	if err != nil {
		return ""
	}
	return text
}
