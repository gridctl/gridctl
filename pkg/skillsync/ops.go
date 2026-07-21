package skillsync

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/registry"
)

// Projection states, sharing the pkg/contexts vocabulary. Symlink
// projections of active skills are never content-stale (the link
// references the registry directly), but any projection goes stale
// when its skill leaves the active set: the pending action is removal.
const (
	StateInSync        = "in-sync"
	StateStale         = "stale"
	StateDrifted       = "drifted"
	StateTargetMissing = "target-missing"
)

// Sync result actions.
const (
	ActionLinked             = "linked"
	ActionCopied             = "copied"
	ActionUpdated            = "updated"
	ActionUnchanged          = "unchanged"
	ActionRemoved            = "removed"
	ActionSkippedDrift       = "skipped-drift"
	ActionSkippedUnmanaged   = "skipped-unmanaged"
	ActionSkippedUnavailable = "skipped-unavailable"
	ActionWouldLink          = "would-link"
	ActionWouldCopy          = "would-copy"
	ActionWouldUpdate        = "would-update"
	ActionWouldRemove        = "would-remove"
	ActionAlreadyGone        = "already-gone"
	ActionError              = "error"
)

// SyncOptions configure a sync pass.
type SyncOptions struct {
	// Clients restricts the pass to these target slugs. Empty means every
	// available target.
	Clients []string
	// Copy projects copies instead of symlinks (copy-forced targets copy
	// regardless).
	Copy bool
	// Force overwrites drifted copies and unmanaged destination paths
	// (after a timestamped backup).
	Force bool
	// DryRun reports the plan without writing anything.
	DryRun bool
}

// SyncResult describes what happened (or would happen) for one
// (skill, client) projection.
type SyncResult struct {
	Skill      string `json:"skill"`
	Client     string `json:"client"`
	Channel    string `json:"channel,omitempty"`
	Target     string `json:"target,omitempty"`
	Action     string `json:"action"`
	BackupPath string `json:"backup_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// UnsyncOptions configure an unsync pass.
type UnsyncOptions struct {
	// All removes every projection instead of named skills.
	All bool
	// Clients restricts removal to these target slugs.
	Clients []string
	// DryRun reports what would be removed without writing.
	DryRun bool
}

// UnsyncResult describes the removal of one projection.
type UnsyncResult struct {
	Skill      string `json:"skill"`
	Client     string `json:"client"`
	Target     string `json:"target"`
	Action     string `json:"action"`
	BackupPath string `json:"backup_path,omitempty"`
}

// ProjectionStatus is one (skill, client) row in `skill project status`.
type ProjectionStatus struct {
	Skill        string     `json:"skill"`
	Client       string     `json:"client"`
	Channel      string     `json:"channel"`
	Target       string     `json:"target"`
	State        string     `json:"state"`
	Detail       string     `json:"detail,omitempty"`
	Experimental bool       `json:"experimental,omitempty"`
	SyncedAt     *time.Time `json:"synced_at,omitempty"`
}

// NeedsAttention reports whether any projection requires action:
// drifted, stale, or a missing target. Backs the status exit code.
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

// Sync projects skills into client skill directories. With names, the
// named active skills are added to the projection set for the resolved
// targets and materialized. With no names, the recorded projection set
// is reconciled: dangling or missing artifacts are repaired, stale
// copies refreshed, and projections whose skill was deactivated or
// deleted are removed. Nothing is ever projected without an explicit
// prior request (the deliberate divergence from ctx sync's
// all-available default: ~90 active skills would bloat client context).
func (m *Manager) Sync(ctx context.Context, names []string, opts SyncOptions) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []SyncResult
	err := m.withFileLock(ctx, func() error {
		lf, err := readLockFile(m.LockPath())
		if err != nil {
			return err
		}
		if len(names) == 0 {
			results, err = m.reconcileLocked(ctx, lf, opts)
		} else {
			results, err = m.syncNamedLocked(ctx, lf, names, opts)
		}
		return err
	})
	return results, err
}

// Reconcile re-syncs the recorded projection set. The daemon calls it
// after every registry refresh; it is a fast no-op when nothing is
// projected.
func (m *Manager) Reconcile(ctx context.Context) ([]SyncResult, error) {
	has, err := m.HasProjections()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	return m.Sync(ctx, nil, SyncOptions{})
}

// resultRecorded reports whether one result changed lockfile state
// (unchanged still refreshes the timestamp).
func resultRecorded(action string) bool {
	switch action {
	case ActionLinked, ActionCopied, ActionUpdated, ActionUnchanged, ActionRemoved:
		return true
	}
	return false
}

// persistIfRecorded writes the lockfile immediately after a mutating
// result, so a crash mid-pass never leaves artifacts on disk that the
// lockfile does not own (which the next sync would refuse to touch as
// foreign paths).
func (m *Manager) persistIfRecorded(lf *LockFile, res SyncResult, dryRun bool) error {
	if dryRun || !resultRecorded(res.Action) {
		return nil
	}
	return writeLockFile(m.LockPath(), lf)
}

// syncNamedLocked validates and materializes named skills. All names are
// validated before any disk write, so a typo never half-projects.
func (m *Manager) syncNamedLocked(ctx context.Context, lf *LockFile, names []string, opts SyncOptions) ([]SyncResult, error) {
	skillsByName := make(map[string]*registry.AgentSkill, len(names))
	var bad []string
	for _, name := range names {
		sk, err := m.store.GetSkill(name)
		if err != nil {
			bad = append(bad, name+" (not found)")
			continue
		}
		if sk.State != registry.StateActive {
			bad = append(bad, fmt.Sprintf("%s (%s)", name, sk.State))
			continue
		}
		skillsByName[name] = sk
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("only active skills can be projected: %s", strings.Join(bad, ", "))
	}

	targets, skipped, err := m.resolveTargets(opts.Clients)
	if err != nil {
		return nil, err
	}

	var results []SyncResult
	for _, t := range skipped {
		results = append(results, SyncResult{Client: t.Slug, Target: t.skillsDir(m.home), Action: ActionSkippedUnavailable})
	}
	for _, name := range names {
		sk := skillsByName[name]
		for _, t := range targets {
			if err := ctx.Err(); err != nil {
				return results, err
			}
			res := m.materialize(sk, t, t.channel(opts.Copy), lf, opts)
			results = append(results, res)
			if err := m.persistIfRecorded(lf, res, opts.DryRun); err != nil {
				return results, err
			}
		}
	}
	return results, nil
}

// resolveTargets maps requested client slugs to targets. Explicitly
// named unavailable clients are errors (the user asked by name and
// should hear why nothing happened); when defaulting to all targets,
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

// reconcileLocked re-materializes every recorded projection, removing
// those whose skill is gone or no longer active. Reconcile never forces
// over a drifted copy; that stays an explicit user decision.
func (m *Manager) reconcileLocked(ctx context.Context, lf *LockFile, opts SyncOptions) ([]SyncResult, error) {
	clientFilter := map[string]bool{}
	for _, c := range opts.Clients {
		clientFilter[c] = true
	}
	var results []SyncResult
	for _, key := range sortedProjectionKeys(lf) {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if len(clientFilter) > 0 && !clientFilter[key.client] {
			continue
		}
		entry := lf.entry(key.skill, key.client)
		sk, gerr := m.store.GetSkill(key.skill)
		if gerr != nil || sk.State != registry.StateActive {
			res := m.removeOne(key.skill, key.client, entry, lf, opts.DryRun)
			results = append(results, res)
			if err := m.persistIfRecorded(lf, res, opts.DryRun); err != nil {
				return results, err
			}
			continue
		}
		t, ok := FindTarget(key.client)
		if !ok {
			results = append(results, SyncResult{
				Skill: key.skill, Client: key.client, Target: entry.Target,
				Action: ActionError, Error: "client is no longer a projection target; run 'gridctl skill project unsync' to clean up",
			})
			continue
		}
		// Preserve the recorded channel, unless the table now forces one.
		res := m.materialize(sk, t, t.channel(entry.Channel == ChannelCopy), lf, opts)
		results = append(results, res)
		if err := m.persistIfRecorded(lf, res, opts.DryRun); err != nil {
			return results, err
		}
	}
	return results, nil
}

// removeOne removes a projection whose skill left the active set.
func (m *Manager) removeOne(skill, client string, entry *Entry, lf *LockFile, dryRun bool) SyncResult {
	res := SyncResult{Skill: skill, Client: client, Channel: string(entry.Channel), Target: entry.Target}
	if dryRun {
		res.Action = ActionWouldRemove
		return res
	}
	backup, err := m.backupProjection(client, skill, entry.Target)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	res.BackupPath = backup
	if err := removeProjection(entry.Target); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	lf.remove(skill, client)
	res.Action = ActionRemoved
	return res
}

// materialize creates or refreshes one (skill, client) projection,
// updating its lock entry in place.
func (m *Manager) materialize(sk *registry.AgentSkill, t Target, ch Channel, lf *LockFile, opts SyncOptions) SyncResult {
	src := m.skillSourceDir(sk)
	dest := filepath.Join(t.skillsDir(m.home), sk.Name)
	res := SyncResult{Skill: sk.Name, Client: t.Slug, Channel: string(ch), Target: dest}
	entry := lf.entry(sk.Name, t.Slug)

	info, lerr := os.Lstat(dest)
	exists := lerr == nil
	if lerr != nil && !os.IsNotExist(lerr) {
		res.Action, res.Error = ActionError, lerr.Error()
		return res
	}

	// A destination that exists without a lock entry was not created by
	// gridctl (npx skills, a hand copy, a lost lockfile): never clobber
	// it silently.
	if exists && entry == nil && !opts.Force {
		res.Action = ActionSkippedUnmanaged
		res.Error = dest + " exists but is not managed by gridctl; re-run with --force to back it up and replace it"
		return res
	}

	// Only content gridctl cannot reproduce deserves a backup: unmanaged
	// paths and drifted copies. A clean managed copy is registry-derived,
	// so backing it up on every refresh would spray backups for nothing.
	needsBackup := entry == nil

	// Managed destinations get a drift check before any replace, and an
	// unchanged short-circuit when nothing moved.
	if exists && entry != nil {
		switch entry.Channel {
		case ChannelCopy:
			destHash, herr := treeHash(dest)
			if herr != nil {
				res.Action, res.Error = ActionError, herr.Error()
				return res
			}
			if destHash != entry.TreeHash {
				if !opts.Force {
					res.Action = ActionSkippedDrift
					return res
				}
				needsBackup = true
			}
			if destHash == entry.TreeHash && ch == ChannelCopy {
				srcHash, herr := treeHash(src)
				if herr != nil {
					res.Action, res.Error = ActionError, herr.Error()
					return res
				}
				if srcHash == entry.TreeHash {
					res.Action = ActionUnchanged
					m.record(lf, sk.Name, t.Slug, ch, dest, entry.TreeHash)
					return res
				}
			}
		case ChannelSymlink:
			link, rerr := os.Readlink(dest)
			if rerr != nil {
				// The managed symlink was replaced by something else.
				if !opts.Force {
					res.Action = ActionSkippedDrift
					res.Error = dest + " is no longer the symlink gridctl created; re-run with --force to replace it"
					return res
				}
				needsBackup = true
			}
			if rerr == nil && ch == ChannelSymlink && link == src {
				res.Action = ActionUnchanged
				m.record(lf, sk.Name, t.Slug, ch, dest, "")
				return res
			}
			// A link re-pointed away from the registry is drift, exactly
			// like a hand-edited copy: replacing it stays an explicit
			// user decision.
			if rerr == nil && link != src && !opts.Force {
				res.Action = ActionSkippedDrift
				res.Error = dest + " points at " + link + " instead of the registry; re-run with --force to replace it"
				return res
			}
		}
	}

	if opts.DryRun {
		switch {
		case exists:
			res.Action = ActionWouldUpdate
		case ch == ChannelSymlink:
			res.Action = ActionWouldLink
		default:
			res.Action = ActionWouldCopy
		}
		return res
	}

	// Back up content gridctl cannot reproduce before it is replaced; a
	// managed symlink or a clean managed copy carries nothing to
	// preserve.
	if exists && info.Mode()&fs.ModeSymlink == 0 && needsBackup {
		backup, berr := m.backupProjection(t.Slug, sk.Name, dest)
		if berr != nil {
			res.Action, res.Error = ActionError, berr.Error()
			return res
		}
		res.BackupPath = backup
	}

	switch ch {
	case ChannelSymlink:
		// Rename cannot replace a directory, so a real dir at dest is
		// cleared first; copyDirAtomic swaps over existing dests itself,
		// keeping a skill present in the client throughout a refresh.
		if exists && info.Mode()&fs.ModeSymlink == 0 {
			if err := removeProjection(dest); err != nil {
				res.Action, res.Error = ActionError, err.Error()
				return res
			}
		}
		if err := replaceSymlink(dest, src); err != nil {
			res.Action, res.Error = ActionError, err.Error()
			return res
		}
		m.record(lf, sk.Name, t.Slug, ch, dest, "")
		res.Action = ActionLinked
	case ChannelCopy:
		// The source hash is computed before the copy; the copy is
		// content-identical by construction, so a post-copy hash cannot
		// fail and leave an unrecorded gridctl artifact behind.
		h, herr := treeHash(src)
		if herr != nil {
			res.Action, res.Error = ActionError, herr.Error()
			return res
		}
		if err := copyDirAtomic(src, dest); err != nil {
			res.Action, res.Error = ActionError, err.Error()
			return res
		}
		m.record(lf, sk.Name, t.Slug, ch, dest, h)
		res.Action = ActionCopied
	}
	if exists {
		res.Action = ActionUpdated
	}
	return res
}

// record updates the lock entry for one projection.
func (m *Manager) record(lf *LockFile, skill, client string, ch Channel, target, hash string) {
	lf.set(skill, client, &Entry{
		Channel:          ch,
		Target:           target,
		CreatedByGridctl: true,
		TreeHash:         hash,
		SyncedAt:         time.Now().UTC(),
	})
}

// Statuses computes the per-projection state for everything in the
// projection set, sorted by skill then client. Reads are lock-free: the
// lockfile is written atomically.
func (m *Manager) Statuses(ctx context.Context) ([]ProjectionStatus, error) {
	lf, err := readLockFile(m.LockPath())
	if err != nil {
		return nil, err
	}
	var statuses []ProjectionStatus
	for _, key := range sortedProjectionKeys(lf) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		statuses = append(statuses, m.statusFor(key.skill, key.client, lf.entry(key.skill, key.client)))
	}
	return statuses, nil
}

// statusFor computes one projection's status row.
func (m *Manager) statusFor(skill, client string, entry *Entry) ProjectionStatus {
	ps := ProjectionStatus{
		Skill:   skill,
		Client:  client,
		Channel: string(entry.Channel),
		Target:  entry.Target,
	}
	syncedAt := entry.SyncedAt
	ps.SyncedAt = &syncedAt
	if t, ok := FindTarget(client); ok {
		ps.Experimental = t.Experimental
	}

	sk, gerr := m.store.GetSkill(skill)
	skillActive := gerr == nil && sk.State == registry.StateActive
	if !skillActive {
		ps.State = StateStale
		ps.Detail = "skill is no longer active in the registry; run 'gridctl skill project sync' to remove the projection"
		return ps
	}

	if _, lerr := os.Lstat(entry.Target); lerr != nil {
		ps.State = StateTargetMissing
		if !os.IsNotExist(lerr) {
			ps.Detail = lerr.Error()
		}
		return ps
	}

	switch entry.Channel {
	case ChannelSymlink:
		link, rerr := os.Readlink(entry.Target)
		if rerr != nil {
			ps.State = StateDrifted
			ps.Detail = "the managed symlink was replaced by a file or directory"
			return ps
		}
		src := m.skillSourceDir(sk)
		if link != src {
			ps.State = StateDrifted
			ps.Detail = fmt.Sprintf("symlink points at %s instead of the registry", link)
			return ps
		}
		if _, serr := os.Stat(entry.Target); serr != nil {
			// The link is ours but resolves nowhere (registry dir moved).
			ps.State = StateTargetMissing
			ps.Detail = "symlink target no longer exists"
			return ps
		}
		ps.State = StateInSync
		return ps
	case ChannelCopy:
		destHash, herr := treeHash(entry.Target)
		if herr != nil {
			ps.State = StateDrifted
			ps.Detail = herr.Error()
			return ps
		}
		if destHash != entry.TreeHash {
			ps.State = StateDrifted
			return ps
		}
		srcHash, herr := treeHash(m.skillSourceDir(sk))
		if herr != nil {
			ps.State = StateStale
			ps.Detail = herr.Error()
			return ps
		}
		if srcHash != entry.TreeHash {
			ps.State = StateStale
			return ps
		}
		ps.State = StateInSync
		return ps
	}
	ps.State = StateDrifted
	ps.Detail = fmt.Sprintf("unknown channel %q in lockfile", entry.Channel)
	return ps
}

// Unsync removes projections: named skills, or the whole set with All.
// Only gridctl-created artifacts are touched; copies are backed up
// before removal.
func (m *Manager) Unsync(ctx context.Context, names []string, opts UnsyncOptions) ([]UnsyncResult, error) {
	if !opts.All && len(names) == 0 {
		return nil, fmt.Errorf("name at least one skill or pass --all")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []UnsyncResult
	err := m.withFileLock(ctx, func() error {
		lf, err := readLockFile(m.LockPath())
		if err != nil {
			return err
		}
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
			if !opts.All && !selected[key.skill] {
				continue
			}
			if len(clientFilter) > 0 && !clientFilter[key.client] {
				continue
			}
			entry := lf.entry(key.skill, key.client)
			res := UnsyncResult{Skill: key.skill, Client: key.client, Target: entry.Target}
			if opts.DryRun {
				res.Action = ActionWouldRemove
				results = append(results, res)
				continue
			}
			if _, lerr := os.Lstat(entry.Target); lerr != nil && os.IsNotExist(lerr) {
				res.Action = ActionAlreadyGone
			} else {
				backup, berr := m.backupProjection(key.client, key.skill, entry.Target)
				if berr != nil {
					return berr
				}
				res.BackupPath = backup
				if err := removeProjection(entry.Target); err != nil {
					return err
				}
				res.Action = ActionRemoved
			}
			// Persist each removal immediately: a crash between artifact
			// removal and a deferred lockfile write would leave an entry
			// whose target is gone, and the next reconcile would
			// resurrect the skill the user just unsynced.
			lf.remove(key.skill, key.client)
			results = append(results, res)
			if err := writeLockFile(m.LockPath(), lf); err != nil {
				return err
			}
		}
		return nil
	})
	return results, err
}

// projectionKey identifies one (skill, client) pair.
type projectionKey struct{ skill, client string }

// sortedProjectionKeys returns the lockfile's projection pairs in
// deterministic skill-then-client order.
func sortedProjectionKeys(lf *LockFile) []projectionKey {
	var keys []projectionKey
	for skill, clients := range lf.Projections {
		for client := range clients {
			keys = append(keys, projectionKey{skill: skill, client: client})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].skill != keys[j].skill {
			return keys[i].skill < keys[j].skill
		}
		return keys[i].client < keys[j].client
	})
	return keys
}
