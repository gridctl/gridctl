package contexts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Projection modes reported per client. single-file is the pre-fragments
// behavior and stays the default; compiled and multi-file only appear once
// fragments mode is active.
const (
	ModeSingleFile = "single-file"
	ModeCompiled   = "compiled"
	ModeMultiFile  = "multi-file"
)

// Fragment-specific sync actions, extending the shared vocabulary.
const (
	ActionRemoved     = "removed"
	ActionWouldRemove = "would-remove"
)

// modeFor reports how a target receives fragments. Callers pass whether
// fragments mode is active; false yields "" so pre-fragments JSON stays
// byte-identical (the mode field is omitempty). Uses the machine-aware
// gate so a capable client whose rules tree is absent labels honestly as
// compiled.
func (m *Manager) modeFor(t Target, fragmentsActive bool) string {
	if !fragmentsActive {
		return ""
	}
	if m.usesMultiFile(t) {
		return ModeMultiFile
	}
	return ModeCompiled
}

// syncFragmentsForTarget projects every fragment as its own file into one
// client's rules directory, and removes files for fragments that are gone.
// Each projected file is individually owned: a file gridctl did not record
// is foreign and is never overwritten without --force, and never deleted.
func (m *Manager) syncFragmentsForTarget(t Target, flf *FragmentLockFile, fragments []*Fragment, opts SyncOptions) []SyncResult {
	dir := fragmentTargetDir(t, m.home)
	results := make([]SyncResult, 0, len(fragments))
	live := make(map[string]bool, len(fragments))

	for _, f := range fragments {
		live[f.Name] = true
		results = append(results, m.syncOneFragment(t, dir, flf, f, opts))
	}

	// Fragments that left the store lose their projections. Only recorded
	// files are removed; nothing on disk is enumerated.
	for _, name := range sortedFragmentNames(flf) {
		if live[name] {
			continue
		}
		entry := flf.entry(name, t.Slug)
		if entry == nil {
			continue
		}
		results = append(results, m.removeFragmentProjection(t, flf, name, entry, opts))
	}
	return results
}

// syncOneFragment renders and writes one fragment for one client.
func (m *Manager) syncOneFragment(t Target, dir string, flf *FragmentLockFile, f *Fragment, opts SyncOptions) SyncResult {
	target := filepath.Join(dir, fragmentFileName(t, f.Name))
	res := SyncResult{
		Slug: t.Slug, Name: t.Name,
		Strategy:   string(t.Strategy),
		TargetPath: target,
		Mode:       ModeMultiFile,
		Fragment:   f.Name,
	}

	rendered := renderFragmentFor(t, f)
	res.Detail = fragmentDropDetail(t, rendered.dropped)

	existing, exists, err := readIfExists(target)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	entry := flf.entry(f.Name, t.Slug)

	// A file we never recorded belongs to the user (or a lost lock);
	// claiming it needs an explicit --force, and the backup goes out of
	// tree so it can never surface as a phantom rule.
	if entry == nil && exists && !opts.Force {
		res.Action = ActionSkippedDrift
		res.Error = target + " exists but is not tracked by gridctl; re-run with --force to back it up and replace it"
		return res
	}
	// Content equality beats the drift guard: when the file already holds
	// exactly what this sync would write, the drift is definitionally
	// resolved (adopt round-trips converge here), so record it rather
	// than skipping forever on a stale hash.
	newContent := string(rendered.data)
	if exists && normalizeNewlines(existing) == normalizeNewlines(newContent) {
		res.Action = ActionUnchanged
		recordFragmentSync(flf, t, f, target, newContent, entry, opts.packTagFor(f.Name))
		return res
	}
	if entry != nil && exists && !opts.Force && contentHash(existing) != entry.InstalledHash {
		res.Action = ActionSkippedDrift
		return res
	}

	if opts.DryRun {
		if exists {
			res.Action = ActionWouldUpdate
		} else {
			res.Action = ActionWouldCreate
		}
		res.Diff = unifiedDiff(existing, newContent, target+" (current)", target+" (after sync)")
		return res
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if exists {
		backup, berr := backupFragmentFile(m.home, t.Slug, f.Name, target)
		if berr != nil {
			res.Action, res.Error = ActionError, berr.Error()
			return res
		}
		res.BackupPath = backup
	}
	if err := atomicWriteFile(target, []byte(newContent)); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if exists {
		res.Action = ActionUpdated
	} else {
		res.Action = ActionCreated
	}
	recordFragmentSync(flf, t, f, target, newContent, entry, opts.packTagFor(f.Name))
	return res
}

// removeFragmentProjection deletes one recorded projection whose fragment
// left the store.
func (m *Manager) removeFragmentProjection(t Target, flf *FragmentLockFile, name string, entry *FragmentEntry, opts SyncOptions) SyncResult {
	res := SyncResult{
		Slug: t.Slug, Name: t.Name,
		Strategy:   string(t.Strategy),
		TargetPath: entry.Target,
		Mode:       ModeMultiFile,
		Fragment:   name,
	}
	if opts.DryRun {
		res.Action = ActionWouldRemove
		return res
	}
	if _, err := os.Stat(entry.Target); err == nil {
		if _, berr := backupFragmentFile(m.home, t.Slug, name, entry.Target); berr != nil {
			res.Action, res.Error = ActionError, berr.Error()
			return res
		}
		if err := os.Remove(entry.Target); err != nil {
			res.Action, res.Error = ActionError, err.Error()
			return res
		}
	}
	flf.remove(name, t.Slug)
	res.Action = ActionRemoved
	return res
}

// recordFragmentSync updates one (fragment, client) ownership record.
// An existing pack tag carries forward unless this pass sets one, matching
// the skill/agent kinds: a plain re-sync never strips pack ownership.
func recordFragmentSync(flf *FragmentLockFile, t Target, f *Fragment, target, written string, prev *FragmentEntry, pack string) {
	if pack == "" && prev != nil {
		pack = prev.Pack
	}
	flf.set(f.Name, t.Slug, &FragmentEntry{
		Target:        target,
		InstalledHash: contentHash(written),
		CanonicalHash: canonicalContentHash(string(f.Raw)),
		Pack:          pack,
		SyncedAt:      time.Now().UTC(),
	})
}

// fragmentStatusFor aggregates a multi-file client's per-fragment records
// into one status row: the worst state wins, and the detail names the
// fragments responsible so the row is actionable rather than a bare verdict.
func (m *Manager) fragmentStatusFor(t Target, flf *FragmentLockFile, fragments []*Fragment) ClientStatus {
	cs := ClientStatus{
		Slug:         t.Slug,
		Name:         t.Name,
		Supported:    true,
		Available:    t.available(m.home),
		Experimental: t.Experimental,
		Strategy:     string(t.Strategy),
		TargetPath:   fragmentTargetDir(t, m.home),
		Mode:         ModeMultiFile,
	}
	if cs.TargetPath == "" {
		cs.Supported = false
		cs.State = StateUnsupported
		cs.Detail = "no global context path on this platform"
		return cs
	}

	var drifted, stale, missing, synced []string
	var latest *time.Time
	for _, f := range fragments {
		entry := flf.entry(f.Name, t.Slug)
		if entry == nil {
			continue
		}
		if latest == nil || entry.SyncedAt.After(*latest) {
			at := entry.SyncedAt
			latest = &at
		}
		data, err := os.ReadFile(entry.Target)
		if err != nil {
			missing = append(missing, f.Name)
			cs.Fragments = append(cs.Fragments, FragmentStatus{Name: f.Name, State: StateTargetMissing, Pack: entry.Pack})
			continue
		}
		if contentHash(string(data)) != entry.InstalledHash {
			drifted = append(drifted, f.Name)
			cs.Fragments = append(cs.Fragments, FragmentStatus{Name: f.Name, State: StateDrifted, Pack: entry.Pack})
			continue
		}
		if canonicalContentHash(string(f.Raw)) != entry.CanonicalHash {
			stale = append(stale, f.Name)
			cs.Fragments = append(cs.Fragments, FragmentStatus{Name: f.Name, State: StateStale, Pack: entry.Pack})
			continue
		}
		synced = append(synced, f.Name)
	}
	cs.SyncedAt = latest

	total := len(drifted) + len(stale) + len(missing) + len(synced)
	switch {
	case total == 0:
		cs.State = StateNeverSynced
	case len(drifted) > 0:
		cs.State = StateDrifted
		cs.Detail = "edited since sync: " + strings.Join(drifted, ", ")
	case len(missing) > 0:
		cs.State = StateTargetMissing
		cs.Detail = "projected file gone: " + strings.Join(missing, ", ")
	case len(stale) > 0:
		cs.State = StateStale
		cs.Detail = "fragment changed since sync: " + strings.Join(stale, ", ")
	default:
		cs.State = StateInSync
		cs.Detail = fmt.Sprintf("%d fragment%s projected", len(synced), plural(len(synced)))
	}
	return cs
}

// AdoptFragment pulls one hand-edited projected fragment file back into
// the canonical fragment, then re-syncs that client so its hashes return
// to in-sync (the whole-client Adopt contract). Only the identity render
// round-trips: a lossy dialect cannot reconstruct what it dropped, so
// those targets refuse.
func (m *Manager) AdoptFragment(ctx context.Context, slug, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateFragmentName(name); err != nil {
		return err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return err
	}
	if !m.FragmentsActive() {
		return ErrFragmentsInactive
	}
	if !m.usesMultiFile(t) {
		return m.adoptCompiledRefusal(t)
	}
	if !fragmentRenderIdentity(t) {
		return &adoptRefusal{
			reason: ErrAdoptLossyRender,
			msg:    fmt.Sprintf("%s's fragment files are a lossy render (frontmatter this dialect cannot express is dropped at write time), so they cannot flow back into the canonical store; adopt from an identity target instead, or hand-maintain the file and detach it with 'gridctl ctx unsync %s'", t.Name, slug),
		}
	}
	// The fragment must exist in the canonical store: adopt updates a
	// fragment, it never conjures one from a stray projected file.
	if _, err := m.ReadFragment(name); err != nil {
		return err
	}

	target := filepath.Join(fragmentTargetDir(t, m.home), fragmentFileName(t, name))
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s has no projected copy of fragment %s", ErrNotSynced, t.Name, name)
		}
		return fmt.Errorf("reading %s: %w", target, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return fmt.Errorf("%s is empty; refusing to adopt an empty fragment", target)
	}
	f, err := ParseFragment(name, data)
	if err != nil {
		return err
	}
	if err := m.SaveFragment(f); err != nil {
		return err
	}
	// Non-force: the adopted fragment now byte-matches its render and
	// converges, while other hand-edited fragments stay drift-protected.
	_, err = m.syncClientLocked(ctx, slug, SyncOptions{})
	return err
}

// AdoptInto captures a compiled target's whole managed body into one
// designated fragment (the deliberate escape hatch for the refusal
// above), then force re-syncs that client so the freshly assembled
// document replaces the hand edit it just captured.
func (m *Manager) AdoptInto(ctx context.Context, slug, fragmentName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateFragmentName(fragmentName); err != nil {
		return err
	}
	t, err := resolveTarget(slug)
	if err != nil {
		return err
	}
	if !m.FragmentsActive() {
		return ErrFragmentsInactive
	}
	if t.Strategy == StrategyImportShim {
		return adoptImportShimRefusal(t)
	}
	targetPath := t.targetPath(m.home)
	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrNotSynced, t.Name)
		}
		return fmt.Errorf("reading %s: %w", targetPath, err)
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
			return fmt.Errorf("no managed block found in %s; nothing to adopt", targetPath)
		}
		body = inner
	}
	body = stripSourceMarkers(body)
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("managed content in %s is empty; refusing to adopt an empty fragment", targetPath)
	}
	if err := m.SaveFragment(&Fragment{Name: fragmentName, FileName: fragmentName + ".md", Body: body}); err != nil {
		return err
	}
	// Force: the capture preserved the hand edit in the canon, so the
	// re-assembled document (which now includes it) replaces the target.
	_, err = m.syncClientLocked(ctx, slug, SyncOptions{Force: true})
	return err
}

// stripSourceMarkers removes the provenance comments compose adds, so a
// captured body does not accumulate them on the next compile.
func stripSourceMarkers(body string) string {
	lines := strings.Split(normalizeNewlines(body), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "<!-- Source: ") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// unsyncFragments removes every recorded fragment projection for a client.
func (m *Manager) unsyncFragments(t Target, flf *FragmentLockFile) ([]UnsyncResult, error) {
	var results []UnsyncResult
	for _, name := range sortedFragmentNames(flf) {
		entry := flf.entry(name, t.Slug)
		if entry == nil {
			continue
		}
		res := UnsyncResult{Slug: t.Slug, TargetPath: entry.Target, Fragment: name}
		if _, err := os.Stat(entry.Target); err != nil {
			res.Action = "already-gone"
		} else {
			if _, berr := backupFragmentFile(m.home, t.Slug, name, entry.Target); berr != nil {
				return results, berr
			}
			if err := os.Remove(entry.Target); err != nil {
				return results, fmt.Errorf("removing %s: %w", entry.Target, err)
			}
			res.Action = "removed-file"
		}
		flf.remove(name, t.Slug)
		results = append(results, res)
	}
	return results, nil
}

// sortedFragmentNames lists recorded fragment names deterministically.
func sortedFragmentNames(flf *FragmentLockFile) []string {
	names := make([]string, 0, len(flf.Projections))
	for name := range flf.Projections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
