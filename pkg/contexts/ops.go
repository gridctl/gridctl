package contexts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/project"

	"github.com/pmezard/go-difflib/difflib"
)

// Client sync states. "stale" means the target still matches what gridctl
// wrote but the canonical file has changed since (a sync is pending).
// The shared vocabulary comes from the engine; "unsupported" and
// "never-synced" are context-kind extensions.
const (
	StateUnsupported   = "unsupported"
	StateNeverSynced   = "never-synced"
	StateInSync        = project.StateInSync
	StateStale         = project.StateStale
	StateDrifted       = project.StateDrifted
	StateTargetMissing = project.StateTargetMissing
)

// Sync result actions. Shared ones come from the engine; "created" and
// "would-create" are context-kind extensions.
const (
	ActionCreated            = "created"
	ActionUpdated            = project.ActionUpdated
	ActionUnchanged          = project.ActionUnchanged
	ActionSkippedDrift       = project.ActionSkippedDrift
	ActionSkippedUnavailable = project.ActionSkippedUnavailable
	ActionWouldCreate        = "would-create"
	ActionWouldUpdate        = project.ActionWouldUpdate
	ActionError              = project.ActionError
)

// ClientStatus is one client's row in `ctx status` and GET /api/context.
type ClientStatus struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Supported    bool   `json:"supported"`
	Available    bool   `json:"available"`
	Experimental bool   `json:"experimental,omitempty"`
	Strategy     string `json:"strategy,omitempty"`
	// Mode is how this client receives the context: single-file (the
	// pre-fragments default), compiled, or multi-file. Omitted while
	// fragments mode is off so pre-fragments consumers see no new field.
	Mode       string     `json:"mode,omitempty"`
	TargetPath string     `json:"target_path,omitempty"`
	State      string     `json:"state"`
	Detail     string     `json:"detail,omitempty"`
	SyncedAt   *time.Time `json:"synced_at,omitempty"`
	// Fragments lists every non-synced fragment with its own state, for
	// multi-file targets only. The aggregate State/Detail keep their
	// worst-state-wins prose (a drifted fragment would otherwise hide a
	// stale one from any structured consumer).
	Fragments []FragmentStatus `json:"fragments,omitempty"`
}

// FragmentStatus is one fragment's projection state on one multi-file
// client.
type FragmentStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`
	// Pack names the pack that applied this fragment projection; empty
	// for projections made outside a pack. Additive provenance for UI
	// chips. Whole-document (single-file and compiled) context entries
	// record no pack tag, so provenance is per-fragment only.
	Pack string `json:"pack,omitempty"`
}

// SyncOptions configure a sync pass.
type SyncOptions struct {
	// Force overwrites drifted targets and repairs corrupt blocks.
	Force bool
	// DryRun renders and diffs without writing anything.
	DryRun bool
	// Pack tags recorded fragment projections with the applying pack.
	// Empty keeps any existing tag.
	Pack string
	// PackRules limits the Pack tag to these fragment names. A pack apply
	// projects the whole fragment set (composition is global), but must
	// only claim ownership of the fragments it shipped; tagging a user
	// fragment would let pack remove retract it.
	PackRules []string
}

// packTagFor returns the pack tag to record for one fragment: the applying
// pack only when that pack listed the fragment.
func (o SyncOptions) packTagFor(fragment string) string {
	if o.Pack == "" {
		return ""
	}
	for _, n := range o.PackRules {
		if n == fragment {
			return o.Pack
		}
	}
	return ""
}

// SyncResult describes what happened (or would happen) for one client.
// In multi-file mode there is one result per (client, fragment).
type SyncResult struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Strategy   string `json:"strategy"`
	Mode       string `json:"mode,omitempty"`
	Fragment   string `json:"fragment,omitempty"`
	TargetPath string `json:"target_path"`
	Action     string `json:"action"`
	// Detail carries honest render loss: frontmatter a client's dialect
	// cannot express, named rather than silently dropped.
	Detail     string `json:"detail,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
	Diff       string `json:"diff,omitempty"`
	Error      string `json:"error,omitempty"`
}

// UnsyncResult describes the removal of one client's managed artifact.
type UnsyncResult struct {
	Slug       string `json:"slug"`
	TargetPath string `json:"target_path"`
	Fragment   string `json:"fragment,omitempty"`
	// Action is "removed-file", "removed-region", or "already-gone".
	Action string `json:"action"`
}

// Statuses computes the per-client sync state for every known client,
// supported and unsupported, in display order.
func (m *Manager) Statuses(ctx context.Context) ([]ClientStatus, error) {
	lf, err := m.loadView(ctx)
	if err != nil {
		return nil, err
	}

	// Fragments mode composes the effective canonical document; single-file
	// mode reads it directly. Neither path creates anything: status is
	// read-only, and an absent fragments directory means the mode is off.
	fragmentsActive := m.FragmentsActive()
	var fragments []*Fragment
	var flf *FragmentLockFile
	var inputHashes map[string]string
	canonicalHash := ""
	if fragmentsActive {
		if fragments, err = m.ListFragments(); err != nil {
			return nil, err
		}
		l, lerr := m.store.Load(ctx)
		if lerr != nil {
			return nil, lerr
		}
		flf = fragmentViewFromLock(l)
		composed := composeFragments(fragments)
		canonicalHash = canonicalContentHash(composed.document)
		inputHashes = composed.inputHashes
	} else if content, cerr := m.CanonicalContent(); cerr == nil {
		canonicalHash = canonicalContentHash(content)
	}

	targets := Targets()
	statuses := make([]ClientStatus, 0, len(targets)+len(Unsupported()))
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if fragmentsActive && m.usesMultiFile(t) {
			statuses = append(statuses, m.fragmentStatusFor(t, flf, fragments))
			continue
		}
		cs := m.statusFor(t, lf.Clients[t.Slug], canonicalHash, inputHashes)
		cs.Mode = m.modeFor(t, fragmentsActive)
		statuses = append(statuses, cs)
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

// statusFor computes one supported client's status row. currentInputs is
// the live fragment-name→hash map in fragments mode (nil otherwise); when
// the entry recorded InputHashes, a composite change is attributed to the
// fragments that moved.
func (m *Manager) statusFor(t Target, entry *ClientEntry, canonicalHash string, currentInputs map[string]string) ClientStatus {
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
	// Fragments mode materializes import-shim targets as managed blocks
	// (no AGENTS.md to @import); hash with the strategy that was written.
	hashTarget := t
	if len(entry.InputHashes) > 0 && t.Strategy == StrategyImportShim {
		hashTarget.Strategy = StrategyBlock
	}
	currentHash, found, err := managedRegionHash(hashTarget, string(data), m.CanonicalPath())
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
	// Import shims in single-file mode reference the canonical file
	// directly, so canonical edits flow through without a re-sync. In
	// fragments mode shims materialize the compiled document and go
	// stale like every other strategy — including a shim recorded before
	// the migration, whose @import now points at the removed AGENTS.md.
	if t.Strategy == StrategyImportShim && len(entry.InputHashes) == 0 {
		if currentInputs != nil {
			cs.State = StateStale
			cs.Detail = "canonical store migrated to fragments; run 'gridctl ctx sync' to materialize"
			return cs
		}
		cs.State = StateInSync
		return cs
	}
	if canonicalHash != entry.CanonicalHash {
		cs.State = StateStale
		if names := staleFragmentNames(entry.InputHashes, currentInputs); len(names) > 0 {
			cs.Detail = "fragment changed since sync: " + strings.Join(names, ", ")
		}
		return cs
	}
	cs.State = StateInSync
	return cs
}

// staleFragmentNames lists fragments whose hash moved between the recorded
// and current input sets, plus fragments added or removed.
func staleFragmentNames(recorded, current map[string]string) []string {
	if len(recorded) == 0 {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for name, h := range recorded {
		if current[name] != h {
			names = append(names, name)
			seen[name] = true
		}
	}
	for name := range current {
		if !seen[name] {
			if _, ok := recorded[name]; !ok {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
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

// syncSource holds the content a sync pass projects: either the single
// canonical file or a composed fragments document.
type syncSource struct {
	// canonical is the document body compiled targets receive.
	canonical string
	// fragments is non-nil only when fragments mode is active.
	fragments []*Fragment
	// compose carries input hashes and dropped-paths honesty for compiled
	// targets. Zero when fragments mode is off.
	compose composeResult
	// active is true when fragments mode is on.
	active bool
}

// loadSyncSource resolves what a sync pass projects. Read-only: never
// creates the fragments directory.
func (m *Manager) loadSyncSource() (syncSource, error) {
	if !m.FragmentsActive() {
		canonical, err := m.CanonicalContent()
		if err != nil {
			return syncSource{}, err
		}
		return syncSource{canonical: canonical}, nil
	}
	fragments, err := m.ListFragments()
	if err != nil {
		return syncSource{}, err
	}
	if len(fragments) == 0 {
		return syncSource{}, fmt.Errorf("%w: fragments mode is active but no fragments exist; add one with 'gridctl ctx add <name>'", ErrNoCanonical)
	}
	composed := composeFragments(fragments)
	return syncSource{
		canonical: composed.document,
		fragments: fragments,
		compose:   composed,
		active:    true,
	}, nil
}

// SyncAll projects the canonical context to every supported, available
// client. Unavailable clients are reported as skipped, never errors.
func (m *Manager) SyncAll(ctx context.Context, opts SyncOptions) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	src, err := m.loadSyncSource()
	if err != nil {
		return nil, err
	}
	var results []SyncResult
	err = m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		flf := fragmentViewFromLock(pl)
		results = make([]SyncResult, 0, len(Targets()))
		lockDirty, fragDirty := false, false
		// writtenPaths coalesces targets that share an absolute path
		// (historically Antigravity + Gemini on GEMINI.md): one write, one
		// lock record shape copied to the second client.
		writtenPaths := map[string]string{}
		for _, t := range Targets() {
			if err := ctx.Err(); err != nil {
				results = nil
				return err
			}
			if !t.available(m.home) {
				results = append(results, SyncResult{
					Slug: t.Slug, Name: t.Name, Strategy: string(t.Strategy),
					TargetPath: t.targetPath(m.home), Action: ActionSkippedUnavailable,
					Mode: m.modeFor(t, src.active),
				})
				continue
			}
			if src.active && m.usesMultiFile(t) {
				// Leaving single-file mode: drop the old dedicated/block
				// artifact so multi-file ownership is the only record.
				if entry := lf.Clients[t.Slug]; entry != nil {
					if !opts.DryRun {
						if _, rerr := m.removeArtifact(t, entry); rerr != nil {
							results = append(results, SyncResult{
								Slug: t.Slug, Name: t.Name, Strategy: string(t.Strategy),
								TargetPath: entry.Target, Action: ActionError, Error: rerr.Error(),
								Mode: ModeMultiFile,
							})
							continue
						}
					}
					delete(lf.Clients, t.Slug)
					lockDirty = true
				}
				fr := m.syncFragmentsForTarget(t, flf, src.fragments, opts)
				for _, r := range fr {
					if r.recorded() || r.Action == ActionRemoved {
						fragDirty = true
					}
				}
				results = append(results, fr...)
				continue
			}
			if t.targetPath(m.home) == "" {
				results = append(results, SyncResult{
					Slug: t.Slug, Name: t.Name, Strategy: string(t.Strategy),
					TargetPath: "", Action: ActionSkippedUnavailable,
					Mode: m.modeFor(t, src.active),
				})
				continue
			}
			path := t.targetPath(m.home)
			if prev, ok := writtenPaths[path]; ok {
				// Same absolute path already written this pass: share the
				// lock record so status does not oscillate between clients.
				if first := lf.Clients[prev]; first != nil {
					shared := *first
					shared.Strategy = string(t.Strategy)
					lf.Clients[t.Slug] = &shared
					lockDirty = true
				}
				results = append(results, SyncResult{
					Slug: t.Slug, Name: t.Name, Strategy: string(t.Strategy),
					TargetPath: path, Action: ActionUnchanged,
					Mode:   m.modeFor(t, src.active),
					Detail: "coalesced with " + prev + " (shared path)",
				})
				continue
			}
			res := m.syncOne(t, lf, src, opts)
			if res.recorded() {
				lockDirty = true
				if !opts.DryRun {
					writtenPaths[path] = t.Slug
				}
			}
			results = append(results, res)
		}
		if opts.DryRun {
			return nil
		}
		if lockDirty {
			if err := applyView(pl, lf); err != nil {
				return err
			}
		}
		if fragDirty {
			if err := applyFragmentView(pl, flf); err != nil {
				return err
			}
		}
		if lockDirty || fragDirty {
			return pl.Save()
		}
		return nil
	})
	return results, err
}

// SyncClient projects the canonical context to one explicitly named
// client. Unlike SyncAll, an unavailable client is an error here: the
// user asked for it by name and should hear why nothing happened.
// Multi-file targets may write several files; the returned result is a
// per-client summary (SyncAll retains the per-fragment rows).
func (m *Manager) SyncClient(ctx context.Context, slug string, opts SyncOptions) (SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	results, err := m.syncClientLocked(ctx, slug, opts)
	if err != nil {
		return SyncResult{}, err
	}
	return summarizeSyncResults(results), nil
}

// SyncClientDetailed is SyncClient with per-fragment rows for multi-file
// targets (pack apply and CLI named-client sync with full honesty).
func (m *Manager) SyncClientDetailed(ctx context.Context, slug string, opts SyncOptions) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.syncClientLocked(ctx, slug, opts)
}

// summarizeSyncResults collapses multi-file rows into one client-level
// result for the single-result API surface.
func summarizeSyncResults(results []SyncResult) SyncResult {
	if len(results) == 0 {
		return SyncResult{}
	}
	if len(results) == 1 {
		return results[0]
	}
	out := results[0]
	out.Fragment = ""
	// Worst action wins so attention-needed rows are never masked.
	priority := map[string]int{
		ActionError: 5, ActionSkippedDrift: 4, ActionCreated: 3,
		ActionUpdated: 3, ActionWouldCreate: 3, ActionWouldUpdate: 3,
		ActionRemoved: 2, ActionWouldRemove: 2, ActionUnchanged: 1,
		ActionSkippedUnavailable: 0,
	}
	for _, r := range results[1:] {
		if priority[r.Action] > priority[out.Action] {
			out.Action = r.Action
			out.Error = r.Error
			out.TargetPath = r.TargetPath
		}
		if r.Detail != "" && out.Detail == "" {
			out.Detail = r.Detail
		}
	}
	if out.Detail != "" {
		out.Detail = fmt.Sprintf("%d fragments; %s", len(results), out.Detail)
	} else {
		out.Detail = fmt.Sprintf("%d fragments", len(results))
	}
	return out
}

// syncClientLocked is the multi-result sync for one client; callers hold mu.
func (m *Manager) syncClientLocked(ctx context.Context, slug string, opts SyncOptions) ([]SyncResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return nil, err
	}
	if !t.available(m.home) {
		return nil, fmt.Errorf("%w: %s (expected one of: %s)", ErrNotAvailable, t.Name, strings.Join(t.DetectDirs, ", "))
	}
	src, err := m.loadSyncSource()
	if err != nil {
		return nil, err
	}
	var results []SyncResult
	err = m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		flf := fragmentViewFromLock(pl)
		if src.active && m.usesMultiFile(t) {
			if entry := lf.Clients[t.Slug]; entry != nil && !opts.DryRun {
				if _, rerr := m.removeArtifact(t, entry); rerr != nil {
					return rerr
				}
				delete(lf.Clients, t.Slug)
				if err := applyView(pl, lf); err != nil {
					return err
				}
			}
			results = m.syncFragmentsForTarget(t, flf, src.fragments, opts)
			if opts.DryRun {
				return nil
			}
			dirty := false
			for _, r := range results {
				if r.recorded() || r.Action == ActionRemoved {
					dirty = true
					break
				}
			}
			if dirty {
				return saveFragmentView(pl, flf)
			}
			return nil
		}
		if t.targetPath(m.home) == "" {
			return fmt.Errorf("%w: %s has no global context path on this platform", ErrUnsupported, t.Name)
		}
		res := m.syncOne(t, lf, src, opts)
		results = []SyncResult{res}
		if !opts.DryRun && res.recorded() {
			return saveView(pl, lf)
		}
		return nil
	})
	return results, err
}

// syncOne renders and writes one client's single-file/compiled target,
// updating its lock entry in place. Availability has already been checked.
func (m *Manager) syncOne(t Target, lf *LockFile, src syncSource, opts SyncOptions) SyncResult {
	res := SyncResult{
		Slug: t.Slug, Name: t.Name,
		Strategy:   string(t.Strategy),
		TargetPath: t.targetPath(m.home),
		Mode:       m.modeFor(t, src.active),
	}
	if src.active && len(src.compose.droppedPaths) > 0 {
		res.Detail = compiledDropDetail(src.compose.droppedPaths)
	}
	canonical := src.canonical

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
		hashTarget := t
		if len(entry.InputHashes) > 0 && t.Strategy == StrategyImportShim {
			hashTarget.Strategy = StrategyBlock
		}
		currentHash, found, herr := managedRegionHash(hashTarget, existing, m.CanonicalPath())
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

	newContent, err := m.renderTarget(t, existing, canonical, opts.Force, src.active)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if rendered := utf8.RuneCountInString(newContent); t.MaxChars > 0 && rendered > t.MaxChars {
		res.Action = ActionError
		res.Error = fmt.Sprintf("%v: %s rendered %d characters, limit is %d", ErrOverCap, t.Name, rendered, t.MaxChars)
		return res
	}
	// Shim and block insertions splice into a user-owned file: keep its
	// CRLF line endings instead of rewriting the whole file to LF.
	// Fragments-mode shims materialize as blocks and use the same path.
	materializesRegion := t.Strategy != StrategyDedicatedFile
	if materializesRegion && exists {
		newContent = restoreCRLF(existing, newContent)
	}

	if exists && normalizeNewlines(existing) == normalizeNewlines(newContent) {
		res.Action = ActionUnchanged
		m.recordSync(lf, t, entry, newContent, exists, src)
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
	m.recordSync(lf, t, entry, newContent, exists, src)
	return res
}

// compiledDropDetail phrases that path-scoped fragments lose their globs
// on a single-file target.
func compiledDropDetail(droppedPaths []string) string {
	n := len(droppedPaths)
	return fmt.Sprintf("%d fragment%s carry paths: globs this target does not support; written unscoped: %s",
		n, plural(n), strings.Join(droppedPaths, ", "))
}

// renderTarget produces the full new target content per strategy.
// fragmentsActive makes import-shim targets materialize the compiled
// document (there is no AGENTS.md for @import after migration).
func (m *Manager) renderTarget(t Target, existing, canonical string, force, fragmentsActive bool) (string, error) {
	switch t.Strategy {
	case StrategyDedicatedFile:
		return renderDedicated(t, canonical), nil
	case StrategyImportShim:
		if fragmentsActive {
			return upsertBlock(existing, canonical, force)
		}
		return upsertShim(existing, m.CanonicalPath()), nil
	case StrategyBlock:
		return upsertBlock(existing, canonical, force)
	}
	return "", fmt.Errorf("unknown strategy %q", t.Strategy)
}

// recordSync updates (or creates) the client's lock entry after a write
// decision. preExisted tracks CreatedFile across repeated syncs.
func (m *Manager) recordSync(lf *LockFile, t Target, prev *ClientEntry, newContent string, preExisted bool, src syncSource) {
	// Safe to discard found/err: newContent was just rendered by
	// renderTarget, so the managed region is present and well-formed by
	// construction. Fragments-mode shims materialize as blocks, so hash
	// them as blocks.
	hashStrategy := t
	if src.active && t.Strategy == StrategyImportShim {
		hashStrategy.Strategy = StrategyBlock
	}
	installedHash, _, _ := managedRegionHash(hashStrategy, newContent, m.CanonicalPath())
	created := !preExisted
	if prev != nil {
		created = prev.CreatedFile
	}
	entry := &ClientEntry{
		Strategy:      string(t.Strategy),
		Target:        t.targetPath(m.home),
		InstalledHash: installedHash,
		CanonicalHash: canonicalContentHash(src.canonical),
		CreatedFile:   created,
		SyncedAt:      time.Now().UTC(),
	}
	if src.active && len(src.compose.inputHashes) > 0 {
		entry.InputHashes = src.compose.inputHashes
	}
	lf.Clients[t.Slug] = entry
}

// Adopt pulls a target's managed content back into the canonical file
// (chezmoi re-add semantics), then re-syncs that client so its hashes
// return to in-sync. Other clients become stale, which is correct: the
// canon changed.
//
// In fragments mode: multi-file identity targets require a fragment name
// (use AdoptFragment); compiled targets refuse (use AdoptInto).
func (m *Manager) Adopt(ctx context.Context, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.FragmentsActive() {
		t, err := resolveTarget(slug)
		if err != nil {
			return err
		}
		if m.usesMultiFile(t) {
			return &adoptRefusal{
				reason: ErrAdoptRequiresFragment,
				msg:    fmt.Sprintf("%s projects fragments as individual files; adopt one with 'gridctl ctx adopt %s <fragment>'", t.Name, slug),
			}
		}
		// Compiled: refuse wholesale collapse into a single canon
		// (AdoptInto is the escape hatch).
		return m.adoptCompiledRefusal(t)
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return err
	}
	if t.Strategy == StrategyImportShim {
		return adoptImportShimRefusal(t)
	}
	lf, err := m.loadView(ctx)
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

// adoptCompiledRefusal is the fragments-mode refuse for compiled targets.
func (m *Manager) adoptCompiledRefusal(t Target) error {
	fragments, err := m.ListFragments()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(fragments))
	for _, f := range fragments {
		names = append(names, f.Name)
	}
	return &adoptRefusal{
		reason: ErrAdoptRefusesCompiled,
		msg: fmt.Sprintf("%s receives a compiled document assembled from %d fragments (%s); adopting it wholesale would collapse them into one. Edit the fragment directly with 'gridctl ctx edit <fragment>', or capture the whole file deliberately with 'gridctl ctx adopt %s --into <fragment>'",
			t.Name, len(names), strings.Join(names, ", "), t.Slug),
	}
}

// Unsync removes one client's managed artifact and clears its lock entry.
// Files gridctl created are deleted outright; files the user owned lose
// only the managed region or shim line. Multi-file fragment projections
// are removed individually; unrecorded sibling files are never touched.
func (m *Manager) Unsync(ctx context.Context, slug string) ([]UnsyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return nil, err
	}
	var results []UnsyncResult
	err = m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		flf := fragmentViewFromLock(pl)
		fragResults, ferr := m.unsyncFragments(t, flf)
		if ferr != nil {
			return ferr
		}
		results = append(results, fragResults...)
		fragDirty := len(fragResults) > 0

		entry := lf.Clients[t.Slug]
		if entry != nil {
			r, rerr := m.removeArtifact(t, entry)
			if rerr != nil {
				return rerr
			}
			delete(lf.Clients, t.Slug)
			results = append(results, r)
			if err := applyView(pl, lf); err != nil {
				return err
			}
		} else if !fragDirty {
			return fmt.Errorf("%w: %s", ErrNotSynced, t.Name)
		}
		if fragDirty {
			if err := applyFragmentView(pl, flf); err != nil {
				return err
			}
		}
		return pl.Save()
	})
	return results, err
}

// UnsyncAll removes every synced client's managed artifact. The loop
// keeps its historical fail-fast semantics: the first removal error
// aborts the pass without persisting the deletes made so far.
func (m *Manager) UnsyncAll(ctx context.Context) ([]UnsyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []UnsyncResult
	err := m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		flf := fragmentViewFromLock(pl)
		results = make([]UnsyncResult, 0, len(lf.Clients))
		for _, t := range Targets() {
			if err := ctx.Err(); err != nil {
				return err
			}
			fragResults, ferr := m.unsyncFragments(t, flf)
			if ferr != nil {
				return ferr
			}
			results = append(results, fragResults...)
			if lf.Clients[t.Slug] == nil {
				continue
			}
			res, rerr := m.removeArtifact(t, lf.Clients[t.Slug])
			if rerr != nil {
				return rerr
			}
			delete(lf.Clients, t.Slug)
			results = append(results, res)
		}
		if err := applyView(pl, lf); err != nil {
			return err
		}
		if err := applyFragmentView(pl, flf); err != nil {
			return err
		}
		return pl.Save()
	})
	return results, err
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

	// Fragments-mode shims were written as blocks; strip markers, not a
	// missing @import line.
	strategy := t.Strategy
	if len(entry.InputHashes) > 0 && strategy == StrategyImportShim {
		strategy = StrategyBlock
	}

	var remaining string
	switch strategy {
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
// client's current managed content. fragmentName scopes a multi-file
// client to one fragment; bare multi-file diff returns a per-fragment
// summary.
func (m *Manager) Diff(ctx context.Context, slug string, fragmentName ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return "", err
	}
	fragName := ""
	if len(fragmentName) > 0 {
		fragName = fragmentName[0]
	}

	if m.FragmentsActive() && m.usesMultiFile(t) {
		return m.diffMultiFile(t, fragName)
	}

	src, err := m.loadSyncSource()
	if err != nil {
		return "", err
	}
	canonical := src.canonical
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
		if src.active {
			inner, found, berr := extractBlockInner(content)
			if berr != nil {
				return "", berr
			}
			if found {
				current = inner
			} else {
				current = content
			}
			break
		}
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

// diffMultiFile diffs one fragment projection, or summarizes all of them.
func (m *Manager) diffMultiFile(t Target, fragmentName string) (string, error) {
	fragments, err := m.ListFragments()
	if err != nil {
		return "", err
	}
	if fragmentName != "" {
		var f *Fragment
		for _, cand := range fragments {
			if cand.Name == fragmentName {
				f = cand
				break
			}
		}
		if f == nil {
			return "", fmt.Errorf("%w: %s", ErrNoFragment, fragmentName)
		}
		target := filepath.Join(fragmentTargetDir(t, m.home), fragmentFileName(t, f.Name))
		existing, exists, rerr := readIfExists(target)
		if rerr != nil {
			return "", rerr
		}
		if !exists {
			return "", fmt.Errorf("%s does not exist; run 'gridctl ctx sync %s' first", target, t.Slug)
		}
		want := string(renderFragmentFor(t, f).data)
		return unifiedDiff(
			strings.TrimSpace(normalizeNewlines(want))+"\n",
			strings.TrimSpace(normalizeNewlines(existing))+"\n",
			"fragment/"+f.Name,
			target,
		), nil
	}
	var b strings.Builder
	for _, f := range fragments {
		target := filepath.Join(fragmentTargetDir(t, m.home), fragmentFileName(t, f.Name))
		existing, exists, rerr := readIfExists(target)
		if rerr != nil {
			return "", rerr
		}
		want := string(renderFragmentFor(t, f).data)
		switch {
		case !exists:
			fmt.Fprintf(&b, "%s: missing\n", f.Name)
		case normalizeNewlines(existing) == normalizeNewlines(want):
			fmt.Fprintf(&b, "%s: in-sync\n", f.Name)
		default:
			fmt.Fprintf(&b, "%s: differs\n", f.Name)
		}
	}
	return b.String(), nil
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
