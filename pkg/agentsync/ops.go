package agentsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

// ChannelCopy is the only agent channel: agents are always copied, the
// single-file dedicated ownership model has no symlink variant. A plain
// string (unlike skillsync's two-valued Channel type) because nothing
// ever branches on it.
const ChannelCopy = "copy"

// Projection states, from the engine's shared vocabulary.
const (
	StateInSync        = project.StateInSync
	StateStale         = project.StateStale
	StateDrifted       = project.StateDrifted
	StateTargetMissing = project.StateTargetMissing
)

// Sync result actions. Shared ones come from the engine; the rest
// mirror the skill-kind extensions so the CLI vocabulary stays one
// language.
const (
	ActionCopied             = "copied"
	ActionUpdated            = project.ActionUpdated
	ActionUnchanged          = project.ActionUnchanged
	ActionRemoved            = "removed"
	ActionSkippedDrift       = project.ActionSkippedDrift
	ActionSkippedUnmanaged   = "skipped-unmanaged"
	ActionSkippedUnavailable = project.ActionSkippedUnavailable
	ActionSkippedEmptyStore  = "skipped-empty-store"
	ActionWouldCopy          = "would-copy"
	ActionWouldUpdate        = project.ActionWouldUpdate
	ActionWouldRemove        = "would-remove"
	ActionAlreadyGone        = "already-gone"
	ActionError              = project.ActionError
)

// SyncOptions configure a sync pass.
type SyncOptions struct {
	// Clients restricts the pass to these target slugs. Empty means every
	// available target.
	Clients []string
	// Force overwrites drifted copies and unmanaged destination files
	// (after a timestamped backup).
	Force bool
	// DryRun reports the plan without writing anything.
	DryRun bool
}

// SyncResult describes what happened (or would happen) for one
// (agent, client) projection.
type SyncResult struct {
	Agent      string `json:"agent"`
	Client     string `json:"client"`
	Channel    string `json:"channel,omitempty"`
	Target     string `json:"target,omitempty"`
	Action     string `json:"action"`
	BackupPath string `json:"backup_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// UnsyncOptions configure an unsync pass.
type UnsyncOptions struct {
	// All removes every projection instead of named agents.
	All bool
	// Clients restricts removal to these target slugs.
	Clients []string
	// DryRun reports what would be removed without writing.
	DryRun bool
}

// UnsyncResult describes the removal of one projection.
type UnsyncResult struct {
	Agent      string `json:"agent"`
	Client     string `json:"client"`
	Target     string `json:"target"`
	Action     string `json:"action"`
	BackupPath string `json:"backup_path,omitempty"`
}

// ProjectionStatus is one (agent, client) row in status output.
type ProjectionStatus struct {
	Agent        string     `json:"agent"`
	Client       string     `json:"client"`
	Channel      string     `json:"channel"`
	Target       string     `json:"target"`
	State        string     `json:"state"`
	Detail       string     `json:"detail,omitempty"`
	Experimental bool       `json:"experimental,omitempty"`
	SyncedAt     *time.Time `json:"synced_at,omitempty"`
}

// NeedsAttention reports whether any projection requires action.
func NeedsAttention(statuses []ProjectionStatus) bool {
	for _, s := range statuses {
		switch s.State {
		case StateDrifted, StateStale, StateTargetMissing:
			return true
		}
	}
	return false
}

// HasFailures reports whether any result needs the caller's attention.
func HasFailures(results []SyncResult) bool {
	for _, r := range results {
		switch r.Action {
		case ActionError, ActionSkippedDrift, ActionSkippedUnmanaged:
			return true
		}
	}
	return false
}

// contentHash hashes raw bytes with the engine's scheme prefix. Agents
// are copied verbatim, so no newline normalization is applied: the
// destination is byte-identical to the canon by construction.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return project.HashScheme + hex.EncodeToString(sum[:])
}

// Sync projects agents into client agent directories. With names, the
// named imported agents are projected to the resolved targets. With no
// names, every imported agent is projected and recorded projections
// whose agent left the store are removed (agents are single small
// files, so the all-by-default divergence from skill sync's named-only
// contract does not carry its context-flooding cost).
func (m *Manager) Sync(ctx context.Context, names []string, opts SyncOptions) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	agents, err := skills.ListAgents(m.registryDir)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]skills.InstalledAgent, len(agents))
	for _, a := range agents {
		byName[a.Name] = a
	}

	selected := agents
	if len(names) > 0 {
		var bad []string
		selected = nil
		for _, name := range names {
			a, ok := byName[name]
			if !ok {
				bad = append(bad, name)
				continue
			}
			selected = append(selected, a)
		}
		if len(bad) > 0 {
			return nil, fmt.Errorf("unknown agent(s): %s (see 'gridctl skill list --kind agent')", strings.Join(bad, ", "))
		}
	}

	targets, skipped, err := m.resolveTargets(opts.Clients)
	if err != nil {
		return nil, err
	}

	var results []SyncResult
	err = m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		for _, t := range skipped {
			results = append(results, SyncResult{Client: t.Slug, Target: t.agentsDir(m.home), Action: ActionSkippedUnavailable})
		}

		// A bare sync also reconciles removals: recorded projections
		// whose agent left the store are cleaned up.
		if len(names) == 0 {
			for _, key := range sortedProjectionKeys(lf) {
				if err := ctx.Err(); err != nil {
					return err
				}
				if _, ok := byName[key.agent]; ok {
					continue
				}
				res := m.removeOne(key.agent, key.client, lf.entry(key.agent, key.client), lf, opts.DryRun)
				results = append(results, res)
				if err := m.persistIfRecorded(pl, lf, res, opts.DryRun); err != nil {
					return err
				}
			}
		}

		for _, a := range selected {
			for _, t := range targets {
				if err := ctx.Err(); err != nil {
					return err
				}
				res := m.materialize(a, t, lf, opts)
				results = append(results, res)
				if err := m.persistIfRecorded(pl, lf, res, opts.DryRun); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return results, err
}

// Reconcile re-syncs the recorded projection set. The daemon calls it
// after every registry refresh; it is a fast no-op when nothing is
// projected. An empty agent store while projections are recorded is
// refused rather than mass-removed, mirroring the skill-kind guard: a
// missing or unreadable store directory reads as empty.
func (m *Manager) Reconcile(ctx context.Context) ([]SyncResult, error) {
	has, err := m.HasProjections(ctx)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	agents, err := skills.ListAgents(m.registryDir)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return []SyncResult{{
			Action: ActionSkippedEmptyStore,
			Error: fmt.Sprintf("agent store %s reports no agents while %s records projections; if this is intentional, run 'gridctl skill project sync --kind agent' to reconcile explicitly",
				skills.AgentsRoot(m.registryDir), m.LockPath()),
		}}, nil
	}
	return m.reconcileRecorded(ctx)
}

// reconcileRecorded re-materializes only the recorded projection set,
// removing entries whose agent left the store. Unlike a bare Sync, it
// never projects agents that were not explicitly synced before, so the
// daemon cannot silently grow the projected set after an import.
func (m *Manager) reconcileRecorded(ctx context.Context) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []SyncResult
	err := m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		for _, key := range sortedProjectionKeys(lf) {
			if err := ctx.Err(); err != nil {
				return err
			}
			entry := lf.entry(key.agent, key.client)
			a, gerr := skills.GetAgent(m.registryDir, key.agent)
			if gerr != nil {
				res := m.removeOne(key.agent, key.client, entry, lf, false)
				results = append(results, res)
				if err := m.persistIfRecorded(pl, lf, res, false); err != nil {
					return err
				}
				continue
			}
			t, ok := FindTarget(key.client)
			if !ok {
				results = append(results, SyncResult{
					Agent: key.agent, Client: key.client, Target: entry.Target,
					Action: ActionError, Error: "client is no longer a projection target; run 'gridctl skill project unsync --kind agent' to clean up",
				})
				continue
			}
			res := m.materialize(*a, t, lf, SyncOptions{})
			results = append(results, res)
			if err := m.persistIfRecorded(pl, lf, res, false); err != nil {
				return err
			}
		}
		return nil
	})
	return results, err
}

// resolveTargets maps requested client slugs to targets. Explicitly
// named unavailable clients are errors; when defaulting to all targets,
// unavailable ones are reported as skipped.
func (m *Manager) resolveTargets(slugs []string) (targets, skipped []Target, err error) {
	if len(slugs) == 0 {
		for _, t := range Targets() {
			if t.available(m.home) {
				targets = append(targets, t)
			} else {
				skipped = append(skipped, t)
			}
		}
		return targets, skipped, nil
	}
	for _, slug := range slugs {
		t, ok := FindTarget(slug)
		if !ok {
			return nil, nil, fmt.Errorf("%w: %q (known clients: %s)", ErrUnknownClient, slug, strings.Join(SupportedSlugs(), ", "))
		}
		if !t.available(m.home) {
			return nil, nil, fmt.Errorf("%w: %s (expected one of: %s)", ErrNotAvailable, t.Name, strings.Join(t.DetectDirs, ", "))
		}
		targets = append(targets, t)
	}
	return targets, nil, nil
}

// resultRecorded reports whether one result changed lockfile state.
func resultRecorded(action string) bool {
	switch action {
	case ActionCopied, ActionUpdated, ActionUnchanged, ActionRemoved:
		return true
	}
	return false
}

// persistIfRecorded writes the lockfile immediately after a mutating
// result, so a crash mid-pass never leaves files on disk the lockfile
// does not own.
func (m *Manager) persistIfRecorded(pl *project.Lock, lf *LockFile, res SyncResult, dryRun bool) error {
	if dryRun || !resultRecorded(res.Action) {
		return nil
	}
	return saveView(pl, lf)
}

// materialize creates or refreshes one (agent, client) projection,
// updating its lock entry in place.
func (m *Manager) materialize(a skills.InstalledAgent, t Target, lf *LockFile, opts SyncOptions) SyncResult {
	dest := filepath.Join(t.agentsDir(m.home), a.Name+".md")
	res := SyncResult{Agent: a.Name, Client: t.Slug, Channel: ChannelCopy, Target: dest}
	entry := lf.entry(a.Name, t.Slug)

	src, err := os.ReadFile(filepath.Join(a.Dir, "AGENT.md")) // #nosec G304 -- fixed name inside the managed store
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	srcHash := contentHash(src)

	existing, exists, err := readIfExists(dest)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}

	// A destination that exists without a lock entry was not created by
	// gridctl: a hand-authored subagent definition. Never clobber it
	// silently; --force backs it up first.
	if exists && entry == nil && !opts.Force {
		res.Action = ActionSkippedUnmanaged
		res.Error = dest + " exists but is not managed by gridctl (a hand-authored subagent definition); re-run with --force to back it up and replace it"
		return res
	}

	needsBackup := entry == nil

	var destHash string
	if exists {
		destHash = contentHash(existing)
	}
	if exists && entry != nil {
		if destHash != entry.InstalledHash {
			if !opts.Force {
				res.Action = ActionSkippedDrift
				return res
			}
			needsBackup = true
		}
		if destHash == entry.InstalledHash && srcHash == entry.CanonicalHash {
			res.Action = ActionUnchanged
			m.record(lf, a.Name, t.Slug, dest, srcHash)
			return res
		}
	}
	// Only content the write would destroy deserves a backup slot: when
	// the destination already matches the canon byte for byte (adopt's
	// force-resync right after it copied the edit into the store), a
	// backup would spend one of the rotation slots on a duplicate.
	if destHash == srcHash {
		needsBackup = false
	}

	if opts.DryRun {
		if exists {
			res.Action = ActionWouldUpdate
		} else {
			res.Action = ActionWouldCopy
		}
		return res
	}

	if exists && needsBackup {
		backup, berr := m.backupProjection(t.Slug, a.Name, dest)
		if berr != nil {
			res.Action, res.Error = ActionError, berr.Error()
			return res
		}
		res.BackupPath = backup
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if err := project.AtomicWriteFile(dest, src); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	m.record(lf, a.Name, t.Slug, dest, srcHash)
	if exists {
		res.Action = ActionUpdated
	} else {
		res.Action = ActionCopied
	}
	return res
}

// record updates the lock entry for one projection. The render is
// identity, so the installed and canonical hashes coincide at write.
func (m *Manager) record(lf *LockFile, agent, client, target, hash string) {
	lf.set(agent, client, &Entry{
		Target:           target,
		InstalledHash:    hash,
		CanonicalHash:    hash,
		CreatedByGridctl: true,
		SyncedAt:         time.Now().UTC(),
	})
}

// removeOne removes a projection whose agent left the canonical store.
func (m *Manager) removeOne(agent, client string, entry *Entry, lf *LockFile, dryRun bool) SyncResult {
	res := SyncResult{Agent: agent, Client: client, Channel: ChannelCopy, Target: entry.Target}
	if dryRun {
		res.Action = ActionWouldRemove
		return res
	}
	backup, err := m.backupProjection(client, agent, entry.Target)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	res.BackupPath = backup
	if err := os.Remove(entry.Target); err != nil && !os.IsNotExist(err) {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	lf.remove(agent, client)
	res.Action = ActionRemoved
	return res
}

// Statuses computes the per-projection state for everything in the
// projection set, sorted by agent then client. Reads are lock-free: the
// lockfile is written atomically.
func (m *Manager) Statuses(ctx context.Context) ([]ProjectionStatus, error) {
	lf, err := m.loadView(ctx)
	if err != nil {
		return nil, err
	}
	var statuses []ProjectionStatus
	for _, key := range sortedProjectionKeys(lf) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		statuses = append(statuses, m.statusFor(key.agent, key.client, lf.entry(key.agent, key.client)))
	}
	return statuses, nil
}

// statusFor computes one projection's status row.
func (m *Manager) statusFor(agent, client string, entry *Entry) ProjectionStatus {
	ps := ProjectionStatus{
		Agent:   agent,
		Client:  client,
		Channel: ChannelCopy,
		Target:  entry.Target,
	}
	syncedAt := entry.SyncedAt
	ps.SyncedAt = &syncedAt
	if t, ok := FindTarget(client); ok {
		ps.Experimental = t.Experimental
	}

	a, gerr := skills.GetAgent(m.registryDir, agent)
	if gerr != nil {
		ps.State = StateStale
		ps.Detail = "agent is no longer in the store; run 'gridctl skill project sync --kind agent' to remove the projection"
		return ps
	}

	existing, exists, err := readIfExists(entry.Target)
	if err != nil {
		ps.State = StateTargetMissing
		ps.Detail = err.Error()
		return ps
	}
	if !exists {
		ps.State = StateTargetMissing
		return ps
	}
	if contentHash(existing) != entry.InstalledHash {
		ps.State = StateDrifted
		return ps
	}
	src, rerr := os.ReadFile(filepath.Join(a.Dir, "AGENT.md")) // #nosec G304 -- fixed name inside the managed store
	if rerr != nil {
		ps.State = StateStale
		ps.Detail = rerr.Error()
		return ps
	}
	if contentHash(src) != entry.CanonicalHash {
		ps.State = StateStale
		return ps
	}
	ps.State = StateInSync
	return ps
}

// Unsync removes projections: named agents, or the whole set with All.
// Only gridctl-owned files (those with a lock entry) are touched;
// unmanaged files are left alone. Removed files are backed up first.
func (m *Manager) Unsync(ctx context.Context, names []string, opts UnsyncOptions) ([]UnsyncResult, error) {
	if !opts.All && len(names) == 0 {
		return nil, fmt.Errorf("name at least one agent or pass --all")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []UnsyncResult
	err := m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		selected := map[string]bool{}
		if !opts.All {
			var missing []string
			for _, name := range names {
				if len(lf.Projections[name]) == 0 {
					missing = append(missing, name)
					continue
				}
				selected[name] = true
			}
			if len(missing) > 0 {
				return fmt.Errorf("%w: %s", ErrNotProjected, strings.Join(missing, ", "))
			}
		}
		clientFilter := map[string]bool{}
		for _, c := range opts.Clients {
			clientFilter[c] = true
		}
		for _, key := range sortedProjectionKeys(lf) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !opts.All && !selected[key.agent] {
				continue
			}
			if len(clientFilter) > 0 && !clientFilter[key.client] {
				continue
			}
			entry := lf.entry(key.agent, key.client)
			res := UnsyncResult{Agent: key.agent, Client: key.client, Target: entry.Target}
			if opts.DryRun {
				res.Action = ActionWouldRemove
				results = append(results, res)
				continue
			}
			if _, lerr := os.Lstat(entry.Target); lerr != nil && os.IsNotExist(lerr) {
				res.Action = ActionAlreadyGone
			} else {
				backup, berr := m.backupProjection(key.client, key.agent, entry.Target)
				if berr != nil {
					return berr
				}
				res.BackupPath = backup
				if err := os.Remove(entry.Target); err != nil {
					return err
				}
				res.Action = ActionRemoved
			}
			// Persist each removal immediately: a crash between file
			// removal and a deferred lockfile write would leave an entry
			// whose target is gone, and the next reconcile would resurrect
			// the agent the user just unsynced.
			lf.remove(key.agent, key.client)
			results = append(results, res)
			if err := saveView(pl, lf); err != nil {
				return err
			}
		}
		return nil
	})
	return results, err
}

// backupProjection copies a projected file out of tree before it is
// replaced or removed, under the design's shared backup root
// (~/.gridctl/project-backups/agent/<client>/<name>/), so a backup can
// never surface as a phantom agent in a client-scanned directory.
func (m *Manager) backupProjection(client, agent, path string) (string, error) {
	data, exists, err := readIfExists(path)
	if err != nil || !exists {
		return "", err
	}
	dir := filepath.Join(m.home, ".gridctl", "project-backups", "agent", client, agent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating backup directory: %w", err)
	}
	backup := filepath.Join(dir, time.Now().UTC().Format("20060102-150405.000000000")+"-"+filepath.Base(path))
	if err := project.AtomicWriteFile(backup, data); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}
	m.pruneBackups(dir)
	return backup, nil
}

// pruneBackups keeps the newest project.MaxBackups backups in dir.
// Failures are best-effort: a backup that cannot be pruned never blocks
// the sync that triggered it.
func (m *Manager) pruneBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if !e.IsDir() {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}
	for _, stale := range project.StaleBackups(backups, project.MaxBackups) {
		_ = os.Remove(stale)
	}
}

// readIfExists reads a file, distinguishing absence from failure.
func readIfExists(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- paths come from the lockfile and target table
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// projectionKey identifies one (agent, client) pair.
type projectionKey struct{ agent, client string }

// sortedProjectionKeys returns the lockfile's projection pairs in
// deterministic agent-then-client order.
func sortedProjectionKeys(lf *LockFile) []projectionKey {
	var keys []projectionKey
	for agent, clients := range lf.Projections {
		for client := range clients {
			keys = append(keys, projectionKey{agent: agent, client: client})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].agent != keys[j].agent {
			return keys[i].agent < keys[j].agent
		}
		return keys[i].client < keys[j].client
	})
	return keys
}
