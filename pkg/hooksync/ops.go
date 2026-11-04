package hooksync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

const (
	ActionCopied             = "copied"
	ActionUpdated            = project.ActionUpdated
	ActionUnchanged          = project.ActionUnchanged
	ActionRemoved            = "removed"
	ActionSkippedDrift       = project.ActionSkippedDrift
	ActionSkippedForeign     = "skipped-foreign"
	ActionSkippedPinDrift    = "skipped-pin-drift"
	ActionSkippedUnmanaged   = "skipped-unmanaged"
	ActionSkippedUnavailable = project.ActionSkippedUnavailable
	ActionSkippedUnsupported = "skipped-unsupported"
	ActionWouldCopy          = "would-copy"
	ActionWouldUpdate        = project.ActionWouldUpdate
	ActionWouldRemove        = "would-remove"
	ActionAlreadyGone        = "already-gone"
	ActionError              = project.ActionError

	StateInSync        = project.StateInSync
	StateStale         = project.StateStale
	StateDrifted       = project.StateDrifted
	StateTargetMissing = project.StateTargetMissing

	foreignDetail       = "settings groups already point at this hook's script directory but gridctl has no record of writing them"
	removedDetail       = "the recorded groups were removed from the settings file"
	scriptRemovedDetail = "the script copy was removed"
)

// SyncOptions configure a sync pass.
type SyncOptions struct {
	Clients []string
	Force   bool
	DryRun  bool
	// Pack tags recorded projections. Empty keeps any existing tag.
	Pack string
}

// SyncResult is one (hook, client) outcome.
type SyncResult struct {
	Hook        string `json:"hook,omitempty"`
	Client      string `json:"client"`
	Channel     string `json:"channel,omitempty"`
	Target      string `json:"target,omitempty"`
	Action      string `json:"action"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	BackupPath  string `json:"backup_path,omitempty"`
	Error       string `json:"error,omitempty"`
	ScriptDir   string `json:"script_dir,omitempty"`
}

// UnsyncOptions configure an unsync pass.
type UnsyncOptions struct {
	All     bool
	Clients []string
	Force   bool
	DryRun  bool
}

// UnsyncResult describes one removal.
type UnsyncResult struct {
	Hook       string `json:"hook"`
	Client     string `json:"client"`
	Target     string `json:"target"`
	Action     string `json:"action"`
	Detail     string `json:"detail,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ProjectionStatus is one recorded (hook, client) row.
type ProjectionStatus struct {
	Hook        string     `json:"hook"`
	Client      string     `json:"client"`
	Channel     string     `json:"channel"`
	Render      string     `json:"render"`
	State       string     `json:"state"`
	Target      string     `json:"target"`
	ScriptDir   string     `json:"script_dir,omitempty"`
	Pack        string     `json:"pack,omitempty"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
	Detail      string     `json:"detail,omitempty"`
	Remediation string     `json:"remediation,omitempty"`
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

// HasFailures reports whether any sync result needs the caller's attention.
// Every skipped or failed row counts. Dry-run would-* rows do not.
func HasFailures(results []SyncResult) bool {
	for _, r := range results {
		switch r.Action {
		case ActionError, ActionSkippedDrift, ActionSkippedForeign, ActionSkippedPinDrift,
			ActionSkippedUnmanaged, ActionSkippedUnavailable, ActionSkippedUnsupported:
			return true
		}
	}
	return false
}

// Sync projects hooks into every available target, or into --clients.
// The settings file is read and written once per client, not once per hook.
func (m *Manager) Sync(ctx context.Context, names []string, opts SyncOptions) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hooks, err := skills.ListHooks(m.registryDir)
	if err != nil {
		return nil, err
	}
	byName := map[string]skills.InstalledHook{}
	for _, h := range hooks {
		byName[h.Name] = h
	}
	selected, err := selectHooks(byName, names)
	if err != nil {
		return nil, err
	}
	targets, unsupported, skipped, err := m.resolveClients(opts.Clients)
	if err != nil {
		return nil, err
	}

	var results []SyncResult
	results = append(results, unavailableRows(m.home, selected, skipped)...)
	results = append(results, unsupportedRows(selected, unsupported)...)
	if len(targets) == 0 || len(selected) == 0 {
		return results, nil
	}

	err = m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		for _, t := range targets {
			if err := ctx.Err(); err != nil {
				return err
			}
			clientResults, cerr := m.syncClient(ctx, lf, t, selected, opts)
			results = append(results, clientResults...)
			if cerr != nil {
				return cerr
			}
			if opts.DryRun || !syncRecorded(clientResults) {
				continue
			}
			if err := saveView(pl, lf); err != nil {
				return err
			}
		}
		return nil
	})
	return results, err
}

func selectHooks(byName map[string]skills.InstalledHook, names []string) ([]skills.InstalledHook, error) {
	if len(names) == 0 {
		var out []skills.InstalledHook
		for _, h := range byName {
			out = append(out, h)
		}
		sortHooks(out)
		return out, nil
	}
	var out []skills.InstalledHook
	var bad []string
	for _, name := range names {
		h, ok := byName[name]
		if !ok {
			bad = append(bad, name)
			continue
		}
		out = append(out, h)
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("%w: %s (see 'gridctl skill list --kind hook')", ErrUnknownHook, strings.Join(bad, ", "))
	}
	return out, nil
}

func sortHooks(hooks []skills.InstalledHook) {
	for i := 1; i < len(hooks); i++ {
		for j := i; j > 0 && hooks[j].Name < hooks[j-1].Name; j-- {
			hooks[j], hooks[j-1] = hooks[j-1], hooks[j]
		}
	}
}

func (m *Manager) resolveClients(slugs []string) (targets []Target, unsupported []UnsupportedClient, skipped []Target, err error) {
	if len(slugs) == 0 {
		for _, t := range Targets() {
			if t.available(m.home) {
				targets = append(targets, t)
			} else {
				skipped = append(skipped, t)
			}
		}
		return targets, nil, skipped, nil
	}
	for _, slug := range slugs {
		if t, ok := FindTarget(slug); ok {
			if !t.available(m.home) {
				skipped = append(skipped, t)
				continue
			}
			targets = append(targets, t)
			continue
		}
		if u, ok := FindUnsupported(slug); ok {
			unsupported = append(unsupported, u)
			continue
		}
		return nil, nil, nil, fmt.Errorf("%w: %q (known clients: %s)", ErrUnknownClient, slug, strings.Join(KnownSlugs(), ", "))
	}
	return targets, unsupported, skipped, nil
}

func unavailableRows(home string, hooks []skills.InstalledHook, skipped []Target) []SyncResult {
	var out []SyncResult
	for _, t := range skipped {
		target := t.settingsPath(home)
		if len(hooks) == 0 {
			out = append(out, SyncResult{
				Client: t.Slug, Target: target, Action: ActionSkippedUnavailable,
				Detail: "client not initialized on this machine",
			})
			continue
		}
		for _, h := range hooks {
			out = append(out, SyncResult{
				Hook: h.Name, Client: t.Slug, Channel: ChannelMergeKey,
				Target: target, Action: ActionSkippedUnavailable,
				Detail: "client not initialized on this machine",
			})
		}
	}
	return out
}

func unsupportedRows(hooks []skills.InstalledHook, clients []UnsupportedClient) []SyncResult {
	var out []SyncResult
	for _, c := range clients {
		if len(hooks) == 0 {
			out = append(out, SyncResult{
				Client: c.Slug, Action: ActionSkippedUnsupported, Detail: c.Location,
			})
			continue
		}
		for _, h := range hooks {
			out = append(out, SyncResult{
				Hook: h.Name, Client: c.Slug, Channel: ChannelMergeKey,
				Action: ActionSkippedUnsupported, Detail: c.Location,
			})
		}
	}
	return out
}

type hookWork struct {
	hook        skills.InstalledHook
	res         SyncResult
	write       bool
	groups      []renderedGroup
	plannedHash string
	plannedTree string
	scriptDir   string
	located     []locatedGroup
	replace     bool
	remove      bool
	scriptsOnly bool
	createdFile bool
}

func (m *Manager) syncClient(ctx context.Context, lf *LockFile, t Target, hooks []skills.InstalledHook, opts SyncOptions) ([]SyncResult, error) {
	settingsPath := t.settingsPath(m.home)
	sf, err := readSettings(settingsPath)
	if err != nil {
		return []SyncResult{{Client: t.Slug, Target: settingsPath, Action: ActionError, Error: err.Error()}}, nil
	}
	var work []hookWork
	for _, h := range hooks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		work = append(work, m.planHook(h, t, sf, lf, opts))
	}
	if sf.ParseErr != nil {
		for i := range work {
			if work[i].res.Action == "" {
				work[i].res.Action = ActionSkippedUnmanaged
				work[i].res.Detail = "settings file is not valid JSON or JSONC: " + sf.ParseErr.Error()
				work[i].write = false
			}
		}
	}

	writing := false
	for _, w := range work {
		if w.write {
			writing = true
			break
		}
	}
	if writing && !opts.DryRun {
		if err := m.applyClient(sf, work, opts); err != nil {
			return nil, err
		}
	}
	var results []SyncResult
	for _, w := range work {
		if w.write && opts.DryRun {
			if w.res.Action == ActionCopied {
				w.res.Action = ActionWouldCopy
			} else {
				w.res.Action = ActionWouldUpdate
			}
		}
		if w.write && !opts.DryRun {
			m.record(lf, w, opts.Pack)
		}
		if w.res.Action == ActionUnchanged && !opts.DryRun {
			m.record(lf, w, opts.Pack)
		}
		results = append(results, w.res)
	}
	return results, nil
}

func (m *Manager) planHook(h skills.InstalledHook, t Target, sf *settingsFile, lf *LockFile, opts SyncOptions) hookWork {
	settingsPath := t.settingsPath(m.home)
	scriptDir := scriptDirFor(m.home, h.Name)
	w := hookWork{
		hook:      h,
		scriptDir: scriptDir,
		res: SyncResult{
			Hook: h.Name, Client: t.Slug, Channel: ChannelMergeKey,
			Target: settingsPath, ScriptDir: scriptDir,
		},
	}
	if h.Err != "" || h.Definition == nil {
		w.res.Action = ActionError
		w.res.Error = h.Err
		if w.res.Error == "" {
			w.res.Error = "hook definition is not valid"
		}
		return w
	}
	ok, composite, detail, err := m.pinOK(h.Name)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	if !ok {
		w.res.Action = ActionSkippedPinDrift
		w.res.Detail = detail
		if composite != "" {
			w.res.Remediation = fmt.Sprintf("gridctl skill pins approve %s --kind hook --expect %s", h.Name, composite)
		}
		return w
	}
	groups, err := renderDefinition(h.Definition.Raw, h.Name, scriptDir)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	plannedHash, err := groupsHash(groups)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	plannedTree, err := plannedTreeHash(h.Dir)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	w.groups = groups
	w.plannedHash = plannedHash
	w.plannedTree = plannedTree
	if sf.ParseErr != nil {
		return w
	}
	located := locateGroups(sf, scriptDir)
	w.located = located
	partial := false
	for _, g := range located {
		if g.Partial {
			partial = true
		}
	}
	var currentHash string
	if !partial && len(located) > 0 {
		currentHash, err = groupsHash(locatedAsRendered(located))
		if err != nil {
			w.res.Action = ActionError
			w.res.Error = err.Error()
			return w
		}
	}
	currentTree, terr := treeHash(scriptDir)
	if terr != nil {
		w.res.Action = ActionError
		w.res.Error = terr.Error()
		return w
	}
	rec := lf.entry(h.Name, t.Slug)
	w.createdFile = sf.Missing
	if rec != nil {
		w.createdFile = rec.CreatedFile
	}

	switch {
	case rec == nil && len(located) == 0:
		w.write = true
		w.res.Action = ActionCopied
	case rec == nil:
		if !opts.Force {
			w.res.Action = ActionSkippedForeign
			w.res.Detail = foreignDetail
			return w
		}
		w.write = true
		w.remove = true
		w.res.Action = ActionUpdated
	case len(located) == 0:
		w.write = true
		w.res.Action = ActionUpdated
		w.res.Detail = removedDetail
	case partial || !hashRecorded(rec.Hashes, currentHash):
		if !opts.Force {
			w.res.Action = ActionSkippedDrift
			w.res.Detail = "settings groups were edited since gridctl wrote them"
			return w
		}
		w.write = true
		w.remove = true
		w.res.Action = ActionUpdated
	default:
		// "" is gone (missing or empty), not a hand edit. The unchanged
		// branch must not treat it as a match: that would skip the restore.
		scriptsGone := currentTree == ""
		scriptsDrifted := !scriptsGone && currentTree != rec.TreeHash && currentTree != plannedTree
		if scriptsDrifted && !opts.Force {
			w.res.Action = ActionSkippedDrift
			w.res.Detail = "script copy was edited since gridctl wrote it"
			return w
		}
		settingsMatch := currentHash == plannedHash
		if settingsMatch && currentTree == plannedTree {
			w.res.Action = ActionUnchanged
			w.plannedHash = plannedHash
			return w
		}
		if settingsMatch && sameShape(located, groups) {
			// Groups are what gridctl wrote. Refresh scripts only, so a
			// removed copy or a forced overwrite does not append a second
			// group into the settings file.
			w.write = true
			w.scriptsOnly = true
			w.res.Action = ActionUpdated
			if scriptsGone {
				w.res.Detail = scriptRemovedDetail
			}
			return w
		}
		w.write = true
		w.res.Action = ActionUpdated
		if scriptsGone {
			w.res.Detail = scriptRemovedDetail
		}
		if sameShape(located, groups) && !scriptsDrifted {
			w.replace = true
			w.remove = false
		} else if len(located) > 0 && !sameShape(located, groups) {
			w.remove = true
		}
	}
	return w
}

func sameShape(located []locatedGroup, groups []renderedGroup) bool {
	if len(located) != len(groups) {
		return false
	}
	got := map[string]int{}
	want := map[string]int{}
	for _, g := range located {
		if g.Partial {
			return false
		}
		got[g.Event]++
	}
	for _, g := range groups {
		want[g.Event]++
	}
	if len(got) != len(want) {
		return false
	}
	for event, n := range got {
		if want[event] != n {
			return false
		}
	}
	return true
}

func (m *Manager) pinOK(name string) (bool, string, string, error) {
	if err := m.pins.Load(); err != nil && !errors.Is(err, hookpins.ErrCorrupt) {
		return false, "", "", err
	}
	vr, err := m.pins.Verify(name, m.registryDir)
	if err != nil {
		if errors.Is(err, hookpins.ErrNotPinned) {
			return false, "", "hook is not pinned", nil
		}
		return false, "", "", err
	}
	if vr.Status == hookpins.StatusDrift {
		return false, vr.CompositeHash, "registry bytes differ from the approved pin", nil
	}
	return true, vr.CompositeHash, "", nil
}

func (m *Manager) applyClient(sf *settingsFile, work []hookWork, opts SyncOptions) error {
	var writing []hookWork
	for _, w := range work {
		if w.write {
			writing = append(writing, w)
		}
	}
	if len(writing) == 0 {
		return nil
	}
	for _, w := range writing {
		if err := m.writeScripts(w); err != nil {
			return err
		}
	}
	var patching []hookWork
	for _, w := range writing {
		if !w.scriptsOnly {
			patching = append(patching, w)
		}
	}
	if len(patching) == 0 {
		return nil
	}
	if sf.Missing {
		var groups []renderedGroup
		for _, w := range patching {
			groups = append(groups, w.groups...)
		}
		data, err := newSettings(groups, "  ")
		if err != nil {
			return err
		}
		return writeSettings(sf.Path, data)
	}
	indent := sf.indentOrDefault()
	var replacing []patchOp
	var removing []locatedGroup
	var appending []renderedGroup
	for i := range patching {
		w := &patching[i]
		if w.replace {
			rep, rerr := replaceOps(w.located, w.groups, indent)
			if rerr != nil {
				w.replace = false
				w.remove = len(w.located) > 0
			} else {
				replacing = append(replacing, rep...)
				continue
			}
		}
		if w.remove {
			removing = append(removing, w.located...)
		}
		appending = append(appending, w.groups...)
	}
	// Replaces do not change array length, so indexes computed against
	// the current tree stay valid. Removes then run in descending index
	// order. A hook either replaces its own groups or removes them,
	// never both, and two hooks do not share a group, so a replace and
	// a remove never name the same index. Appends use /- and do not
	// depend on those indexes.
	ops := append(replacing, removeOps(removing)...)
	add, err := appendOps(sf, appending)
	if err != nil {
		return err
	}
	ops = append(ops, add...)
	if len(ops) == 0 {
		return nil
	}
	if _, err := createBackup(sf.Path); err != nil {
		return err
	}
	packed, err := applyPatch(sf.Tree, ops)
	if err != nil {
		return err
	}
	packed = separateNewKeys(sf.Raw, packed, sf.indentOrDefault())
	return writeSettings(sf.Path, packed)
}

func (m *Manager) writeScripts(w hookWork) error {
	current, err := treeHash(w.scriptDir)
	if err != nil {
		return err
	}
	if current == w.plannedTree && current != "" {
		return nil
	}
	if current != "" {
		if _, err := backupTree(m.home, w.hook.Name, w.scriptDir); err != nil {
			return err
		}
	}
	return copyScripts(w.hook.Dir, w.scriptDir)
}

func (m *Manager) record(lf *LockFile, w hookWork, packTag string) {
	if packTag == "" {
		if prev := lf.entry(w.hook.Name, w.res.Client); prev != nil {
			packTag = prev.Pack
		}
	}
	history := []string{w.plannedHash}
	created := w.createdFile
	if prev := lf.entry(w.hook.Name, w.res.Client); prev != nil {
		history = appendHash(prev.Hashes, w.plannedHash)
		if !w.res.ActionWasCopy() {
			created = prev.CreatedFile || w.createdFile
		}
	}
	if w.res.Action == ActionCopied {
		created = w.createdFile
	}
	lf.set(w.hook.Name, w.res.Client, &Entry{
		ConfigPath:  w.res.Target,
		ScriptDir:   w.scriptDir,
		Hashes:      history,
		TreeHash:    w.plannedTree,
		CreatedFile: created,
		Pack:        packTag,
		SyncedAt:    time.Now().UTC(),
	})
}

func (r SyncResult) ActionWasCopy() bool {
	return r.Action == ActionCopied || r.Action == ActionWouldCopy
}

func syncRecorded(results []SyncResult) bool {
	for _, r := range results {
		switch r.Action {
		case ActionCopied, ActionUpdated, ActionUnchanged:
			return true
		}
	}
	return false
}

// Unsync removes recorded groups whose hash is still one gridctl wrote,
// or any located groups when Force is set. Groups gridctl did not record
// are never deleted. The settings file is read and written once per client.
func (m *Manager) Unsync(ctx context.Context, names []string, opts UnsyncOptions) ([]UnsyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var results []UnsyncResult
	err := m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		selected, err := unsyncKeys(lf, names, opts)
		if err != nil {
			return err
		}
		byClient := map[string][]recordKey{}
		var clients []string
		for _, key := range selected {
			if _, ok := byClient[key.client]; !ok {
				clients = append(clients, key.client)
			}
			byClient[key.client] = append(byClient[key.client], key)
		}
		sortStrings(clients)
		for _, client := range clients {
			if err := ctx.Err(); err != nil {
				return err
			}
			clientResults, uerr := m.unsyncClient(lf, client, byClient[client], opts)
			results = append(results, clientResults...)
			if uerr != nil {
				return uerr
			}
		}
		if opts.DryRun || !unsyncRecorded(results) {
			return nil
		}
		return saveView(pl, lf)
	})
	return results, err
}

func unsyncKeys(lf *LockFile, names []string, opts UnsyncOptions) ([]recordKey, error) {
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	clients := map[string]bool{}
	for _, c := range opts.Clients {
		clients[c] = true
	}
	var keys []recordKey
	for _, key := range sortedRecordKeys(lf) {
		if len(names) > 0 && !want[key.name] {
			continue
		}
		if len(names) == 0 && !opts.All {
			continue
		}
		if len(opts.Clients) > 0 && !clients[key.client] {
			continue
		}
		keys = append(keys, key)
	}
	if len(names) > 0 && !opts.All {
		found := map[string]bool{}
		for _, key := range keys {
			found[key.name] = true
		}
		var missing []string
		for _, name := range names {
			if !found[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrNotProjected, strings.Join(missing, ", "))
		}
	}
	return keys, nil
}

func (m *Manager) unsyncClient(lf *LockFile, client string, keys []recordKey, opts UnsyncOptions) ([]UnsyncResult, error) {
	t, ok := FindTarget(client)
	if !ok {
		var out []UnsyncResult
		for _, key := range keys {
			entry := lf.entry(key.name, key.client)
			out = append(out, UnsyncResult{Hook: key.name, Client: client, Target: entry.ConfigPath, Action: ActionError, Error: "client is no longer a projection target"})
		}
		return out, nil
	}
	settingsPath := t.settingsPath(m.home)
	sf, err := readSettings(settingsPath)
	if err != nil {
		return nil, err
	}
	type item struct {
		key     recordKey
		entry   *Entry
		located []locatedGroup
		remove  bool
		purge   bool
		res     UnsyncResult
	}
	var items []item
	for _, key := range keys {
		entry := lf.entry(key.name, key.client)
		it := item{key: key, entry: entry, res: UnsyncResult{Hook: key.name, Client: client, Target: settingsPath}}
		if sf.Missing || sf.ParseErr != nil {
			it.purge = true
			it.res.Action = ActionAlreadyGone
			if sf.ParseErr != nil {
				it.res.Detail = "settings file is not valid JSON or JSONC: " + sf.ParseErr.Error()
				it.purge = false
				it.res.Action = ActionSkippedUnmanaged
			}
			items = append(items, it)
			continue
		}
		located := locateGroups(sf, entry.ScriptDir)
		it.located = located
		if len(located) == 0 {
			it.purge = true
			it.res.Action = ActionAlreadyGone
			items = append(items, it)
			continue
		}
		partial := false
		for _, g := range located {
			if g.Partial {
				partial = true
			}
		}
		currentHash := ""
		if !partial {
			currentHash, err = groupsHash(locatedAsRendered(located))
			if err != nil {
				it.res.Action = ActionError
				it.res.Error = err.Error()
				items = append(items, it)
				continue
			}
		}
		if partial || !hashRecorded(entry.Hashes, currentHash) {
			if !opts.Force {
				it.res.Action = ActionSkippedDrift
				it.res.Detail = "settings groups were edited since gridctl wrote them"
				items = append(items, it)
				continue
			}
		}
		it.remove = true
		it.purge = true
		it.res.Action = ActionRemoved
		items = append(items, it)
	}
	if !opts.DryRun {
		var removing []locatedGroup
		for _, it := range items {
			if it.remove {
				removing = append(removing, it.located...)
			}
		}
		if err := applyUnsync(sf, removing); err != nil {
			return nil, err
		}
		for i := range items {
			if !items[i].purge {
				continue
			}
			if _, err := backupTree(m.home, items[i].key.name, items[i].entry.ScriptDir); err != nil {
				return nil, err
			}
			if err := os.RemoveAll(items[i].entry.ScriptDir); err != nil {
				return nil, err
			}
			lf.remove(items[i].key.name, items[i].key.client)
		}
	} else {
		for i := range items {
			if items[i].res.Action == ActionRemoved {
				items[i].res.Action = ActionWouldRemove
			}
		}
	}
	var out []UnsyncResult
	for _, it := range items {
		out = append(out, it.res)
	}
	return out, nil
}

func applyUnsync(sf *settingsFile, removing []locatedGroup) error {
	if sf == nil || !sf.Parsed || len(removing) == 0 {
		return nil
	}
	ops := removeOps(removing)
	ops = append(ops, dropEmptyEvents(eventLengths(sf), removing)...)
	if _, err := createBackup(sf.Path); err != nil {
		return err
	}
	packed, err := applyPatch(sf.Tree, ops)
	if err != nil {
		return err
	}
	return writeSettings(sf.Path, packed)
}

func unsyncRecorded(results []UnsyncResult) bool {
	for _, r := range results {
		switch r.Action {
		case ActionRemoved, ActionAlreadyGone:
			return true
		}
	}
	return false
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// Statuses returns one row per recorded hook projection.
func (m *Manager) Statuses(ctx context.Context) ([]ProjectionStatus, error) {
	lf, err := m.loadView(ctx)
	if err != nil {
		return nil, err
	}
	var out []ProjectionStatus
	for _, key := range sortedRecordKeys(lf) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, m.statusFor(key.name, key.client, lf.entry(key.name, key.client)))
	}
	return out, nil
}

func (m *Manager) statusFor(name, client string, entry *Entry) ProjectionStatus {
	ps := ProjectionStatus{
		Hook:      name,
		Client:    client,
		Channel:   ChannelMergeKey,
		Render:    RenderIdentity,
		Target:    entry.ConfigPath,
		ScriptDir: entry.ScriptDir,
		Pack:      entry.Pack,
		State:     StateInSync,
	}
	synced := entry.SyncedAt
	ps.SyncedAt = &synced
	if t, ok := FindTarget(client); ok && !t.available(m.home) {
		ps.State = StateTargetMissing
		ps.Detail = "client not initialized on this machine; script directory " + entry.ScriptDir
		return ps
	}
	sf, err := readSettings(entry.ConfigPath)
	if err != nil {
		ps.State = StateTargetMissing
		ps.Detail = err.Error()
		return ps
	}
	if sf.Missing {
		ps.State = StateTargetMissing
		ps.Detail = "settings file is gone; script directory " + entry.ScriptDir
		return ps
	}
	if sf.ParseErr != nil {
		ps.State = StateDrifted
		ps.Detail = "settings file is not valid JSON or JSONC: " + sf.ParseErr.Error()
		return ps
	}
	if _, err := os.Stat(entry.ScriptDir); err != nil {
		ps.State = StateTargetMissing
		ps.Detail = "script directory is gone (" + entry.ScriptDir + ")"
		return ps
	}
	located := locateGroups(sf, entry.ScriptDir)
	if len(located) == 0 {
		ps.State = StateTargetMissing
		ps.Detail = removedDetail + "; script directory " + entry.ScriptDir
		return ps
	}
	partial := false
	for _, g := range located {
		if g.Partial {
			partial = true
		}
	}
	currentTree, terr := treeHash(entry.ScriptDir)
	if terr != nil {
		ps.State = StateTargetMissing
		ps.Detail = terr.Error()
		return ps
	}
	scriptGone := currentTree == ""
	scriptDrift := !scriptGone && currentTree != entry.TreeHash
	var currentHash string
	if !partial {
		currentHash, _ = groupsHash(locatedAsRendered(located))
	}
	settingsDrift := partial || !hashRecorded(entry.Hashes, currentHash)
	if scriptGone && !settingsDrift {
		ps.State = StateTargetMissing
		ps.Detail = "script copy is empty (" + entry.ScriptDir + ")"
		return ps
	}
	if settingsDrift || scriptDrift {
		ps.State = StateDrifted
		switch {
		case settingsDrift && scriptDrift:
			ps.Detail = "settings groups and script copy were edited since gridctl wrote them; script directory " + entry.ScriptDir
		case scriptDrift:
			ps.Detail = "script copy was edited since gridctl wrote it; script directory " + entry.ScriptDir
		default:
			ps.Detail = "settings groups were edited since gridctl wrote them; script directory " + entry.ScriptDir
		}
		return ps
	}
	var stale []string
	hook, herr := skills.GetHook(m.registryDir, name)
	if herr != nil {
		stale = append(stale, "hook is no longer in the registry")
	} else {
		ok, _, detail, perr := m.pinOK(name)
		if perr != nil {
			stale = append(stale, perr.Error())
		} else if !ok {
			if strings.Contains(detail, "not pinned") {
				stale = append(stale, "pin drift: hook is not pinned")
			} else {
				stale = append(stale, "pin drift")
			}
		}
		if hook != nil && hook.Definition != nil {
			groups, rerr := renderDefinition(hook.Definition.Raw, name, entry.ScriptDir)
			if rerr != nil {
				stale = append(stale, rerr.Error())
			} else if planned, herr := groupsHash(groups); herr != nil {
				stale = append(stale, herr.Error())
			} else if planned != entry.latestHash() {
				stale = append(stale, "planned render differs from the synced groups")
			}
			if plannedTree, terr := plannedTreeHash(hook.Dir); terr == nil && plannedTree != entry.TreeHash {
				stale = append(stale, "registry scripts differ from the synced copy")
			}
		}
	}
	if len(stale) > 0 {
		ps.State = StateStale
		ps.Detail = strings.Join(stale, "; ")
	}
	return ps
}
