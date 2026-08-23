package modelsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/gridctl/gridctl/pkg/project"
)

// Projection states: the engine vocabulary plus the context-kind
// extension for supported-but-never-synced targets.
const (
	StateInSync        = project.StateInSync
	StateStale         = project.StateStale
	StateDrifted       = project.StateDrifted
	StateTargetMissing = project.StateTargetMissing
	StateNeverSynced   = "never-synced"
)

// Actions: the engine vocabulary plus kind-specific verbs.
const (
	ActionUpdated        = project.ActionUpdated
	ActionUnchanged      = project.ActionUnchanged
	ActionError          = project.ActionError
	ActionSkippedDrift   = project.ActionSkippedDrift
	ActionWouldUpdate    = project.ActionWouldUpdate
	ActionSkippedForeign = "skipped-foreign"
	ActionRemoved        = "removed"
	ActionWouldRemove    = "would-remove"
	ActionKeptDrift      = "kept-drift"
	ActionAlreadyGone    = "already-gone"
	ActionAdopted        = "adopted"
)

// SyncOptions configure a sync pass.
type SyncOptions struct {
	// DryRun previews without writing files or lockfile entries.
	DryRun bool
	// Diff attaches unified diffs to would-update results.
	Diff bool
	// Force overwrites drifted and foreign targets. Recorded drift is
	// otherwise skipped with guidance; a foreign file at the fragment
	// path is otherwise never touched.
	Force bool
}

// SyncResult is one (target) outcome of a sync pass.
type SyncResult struct {
	Target     string `json:"target"`
	Client     string `json:"client"`
	Path       string `json:"path"`
	Action     string `json:"action"`
	Detail     string `json:"detail,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
	Diff       string `json:"diff,omitempty"`
	Error      string `json:"error,omitempty"`
}

// UnsyncOptions configure removal.
type UnsyncOptions struct {
	// Force removes drifted targets too. Foreign content is never
	// removed, force or not.
	Force bool
}

// UnsyncResult is one (target) removal outcome.
type UnsyncResult struct {
	Target     string `json:"target"`
	Client     string `json:"client"`
	Path       string `json:"path"`
	Action     string `json:"action"`
	Detail     string `json:"detail,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Status is one target's projection state.
type Status struct {
	Target string `json:"target"`
	Client string `json:"client"`
	State  string `json:"state"`
	// RestartPending annotates the LiteLLM fragment: the running proxy
	// has not been acknowledged as restarted since the last write. An
	// annotation, never a drift state: it does not affect exit codes.
	RestartPending bool       `json:"restart_pending,omitempty"`
	Path           string     `json:"path,omitempty"`
	Detail         string     `json:"detail,omitempty"`
	SyncedAt       *time.Time `json:"synced_at,omitempty"`
}

// NeedsAttention reports whether any status row needs a sync or a
// decision. Restart-pending alone is not attention.
func NeedsAttention(statuses []Status) bool {
	for _, s := range statuses {
		if s.State != StateInSync {
			return true
		}
	}
	return false
}

// HasFailures reports whether any sync result failed or was skipped.
func HasFailures(results []SyncResult) bool {
	for _, r := range results {
		switch r.Action {
		case ActionError, ActionSkippedDrift, ActionSkippedForeign:
			return true
		}
	}
	return false
}

// view is the models-kind slice of the unified lockfile, keyed by
// Entry.Source. Entries are engine entries directly: a new kind needs
// no legacy-shaped translation layer.
type view struct {
	entries map[string]*project.Entry
}

func viewFromLock(pl *project.Lock) *view {
	v := &view{entries: map[string]*project.Entry{}}
	for _, e := range pl.Entries(project.KindModels) {
		v.entries[e.Source] = e
	}
	return v
}

// saveView flushes the view and persists the lockfile.
func saveView(pl *project.Lock, v *view) error {
	entries := make([]*project.Entry, 0, len(v.entries))
	for _, e := range v.entries {
		entries = append(entries, e)
	}
	if err := pl.ReplaceKind(project.KindModels, entries); err != nil {
		return err
	}
	return pl.Save()
}

// relIncludeRef computes the include string written into the parent:
// relative to the parent's directory when the fragment sits at or
// below it (LiteLLM resolves includes relative to the parent), else
// the absolute path with a portability caveat carried in the result
// detail.
func relIncludeRef(parent, fragment string) (ref string, relative bool) {
	rel, err := filepath.Rel(filepath.Dir(parent), fragment)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fragment, false
	}
	return rel, true
}

// Sync projects the policy into every declared target. Per-target
// results never abort the pass; infrastructure errors (no policy,
// invalid policy, lockfile) do.
func (m *Manager) Sync(ctx context.Context, opts SyncOptions) ([]SyncResult, error) {
	p, err := m.LoadPolicy()
	if err != nil {
		return nil, err
	}
	if issues := m.Validate(p); HasErrors(issues) {
		return nil, fmt.Errorf("policy is invalid; run 'gridctl models validate' (%d error(s), first: %s)",
			countErrors(issues), firstError(issues))
	}
	policyHash := p.Hash()

	m.mu.Lock()
	defer m.mu.Unlock()

	var results []SyncResult
	err = m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		v := viewFromLock(pl)

		fragPath, ferr := m.FragmentPath(p)
		if ferr != nil {
			return ferr
		}
		res, changed := m.syncFragment(p, v, fragPath, policyHash, opts)
		results = append(results, res)
		if changed && !opts.DryRun {
			if err := saveView(pl, v); err != nil {
				return err
			}
		}

		if p.Targets.LiteLLM != nil && p.Targets.LiteLLM.ConfigPath != "" {
			res, changed := m.syncInclude(p, v, fragPath, opts)
			results = append(results, res)
			if changed && !opts.DryRun {
				if err := saveView(pl, v); err != nil {
					return err
				}
			}
		}

		if p.Clients.OpenCode != nil {
			res, changed := m.syncOpenCode(p, v, opts)
			results = append(results, res)
			if changed && !opts.DryRun {
				if err := saveView(pl, v); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func countErrors(issues []Issue) int {
	n := 0
	for _, i := range issues {
		if i.Severity == SeverityError {
			n++
		}
	}
	return n
}

func firstError(issues []Issue) string {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return i.Field + ": " + i.Message
		}
	}
	return ""
}

// syncFragment materializes the rendered LiteLLM fragment. The second
// return reports whether the lockfile view changed.
func (m *Manager) syncFragment(p *Policy, v *view, fragPath, policyHash string, opts SyncOptions) (SyncResult, bool) {
	res := SyncResult{Target: srcFragment, Client: clientLiteLLM, Path: fragPath}
	rendered, err := RenderLiteLLM(p, policyHash)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	renderedHash := contentHash(rendered)

	e := v.entries[srcFragment]
	disk, diskErr := os.ReadFile(fragPath)
	exists := diskErr == nil

	switch {
	case e == nil && exists && !opts.Force:
		res.Action = ActionSkippedForeign
		res.Detail = "a file already exists at the fragment path but gridctl did not create it; re-run with --force to overwrite"
		return res, false
	case e != nil && exists && contentHash(disk) != e.InstalledHash && !opts.Force:
		res.Action = ActionSkippedDrift
		res.Detail = "the fragment was edited since gridctl wrote it; adopt the edit or re-run with --force to overwrite"
		return res, false
	case exists && contentHash(disk) == renderedHash && e != nil:
		res.Action = ActionUnchanged
		return res, false
	}

	if opts.DryRun {
		res.Action = ActionWouldUpdate
		if opts.Diff {
			res.Diff = unifiedDiff(string(disk), string(rendered),
				fragPath+" (current)", fragPath+" (after sync)")
		}
		return res, false
	}

	if err := os.MkdirAll(filepath.Dir(fragPath), 0755); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	backup, err := createBackup(fragPath)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	if err := project.AtomicWriteFile(fragPath, rendered); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	entry := &project.Entry{
		Kind:          project.KindModels,
		Client:        clientLiteLLM,
		Source:        srcFragment,
		Path:          fragPath,
		InstalledHash: renderedHash,
		CanonicalHash: policyHash,
		CreatedFile:   !exists || (e != nil && e.CreatedFile),
		SyncedAt:      time.Now(),
	}
	if e != nil {
		entry.AckedHash = e.AckedHash
		entry.Pack = e.Pack
	}
	v.entries[srcFragment] = entry
	res.Action, res.BackupPath = ActionUpdated, backup
	res.Detail = "LiteLLM reads config only at startup; restart it, then run 'gridctl models ack-restart'"
	return res, true
}

// syncInclude keeps the parent config's include: entry pointing at the
// fragment.
func (m *Manager) syncInclude(p *Policy, v *view, fragPath string, opts SyncOptions) (SyncResult, bool) {
	res := SyncResult{Target: srcInclude, Client: clientLiteLLM}
	parent, err := m.expandPath(p.Targets.LiteLLM.ConfigPath)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	res.Path = parent

	raw, err := os.ReadFile(parent)
	if err != nil {
		res.Action = ActionError
		res.Error = fmt.Sprintf("parent LiteLLM config not found: %s", parent)
		return res, false
	}
	content := string(raw)
	ref, relative := relIncludeRef(parent, fragPath)
	e := v.entries[srcInclude]

	// A fragment path change leaves a stale include entry behind; drop
	// the old line first so the parent never includes a missing file.
	if e != nil && e.IncludeRef != "" && e.IncludeRef != ref && hasIncludeLine(content, e.IncludeRef) {
		if opts.DryRun {
			res.Action = ActionWouldUpdate
			res.Detail = fmt.Sprintf("would replace include entry %q with %q", e.IncludeRef, ref)
			return res, false
		}
		updated, rerr := removeIncludeLine(content, e.IncludeRef, e.IncludeMode, e.IncludeOriginal)
		if rerr != nil {
			res.Action, res.Error = ActionError, rerr.Error()
			return res, false
		}
		content = updated
	}

	if hasIncludeLine(content, ref) {
		if e == nil {
			// Line already present but unrecorded: adopt it so unsync can
			// remove exactly this line later.
			v.entries[srcInclude] = includeEntry(parent, ref, includeAppended, "", "")
			res.Action = ActionUnchanged
			res.Detail = "include line already present; recorded as gridctl-owned"
			return res, !opts.DryRun
		}
		res.Action = ActionUnchanged
		return res, false
	}

	if e != nil && !opts.Force {
		res.Action = ActionSkippedDrift
		res.Detail = "the include line was removed from the parent config; re-run with --force to re-add it, or 'gridctl models unsync' to drop the record"
		return res, false
	}

	if opts.DryRun {
		res.Action = ActionWouldUpdate
		if opts.Diff {
			edit, derr := upsertIncludeLine(content, ref)
			if derr == nil {
				res.Diff = unifiedDiff(normalizeNewlines(content), edit.Content,
					parent+" (current)", parent+" (after sync)")
			}
		}
		return res, false
	}

	edit, err := upsertIncludeLine(content, ref)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	backup, err := createBackup(parent)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	if err := project.AtomicWriteFile(parent, []byte(restoreCRLF(content, edit.Content))); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	mode, original := edit.Mode, edit.Original
	if e != nil { // forced re-add keeps the original mutation record
		mode, original = e.IncludeMode, e.IncludeOriginal
	}
	v.entries[srcInclude] = includeEntry(parent, ref, mode, original, "")
	res.Action, res.BackupPath = ActionUpdated, backup
	if !relative {
		res.Detail = "fragment is outside the parent config's directory, so the include uses an absolute path; keep both files together for a portable config"
	}
	return res, true
}

func includeEntry(parent, ref, mode, original, pack string) *project.Entry {
	return &project.Entry{
		Kind:            project.KindModels,
		Client:          clientLiteLLM,
		Source:          srcInclude,
		Path:            parent + "#include:" + ref,
		ConfigPath:      parent,
		IncludeRef:      ref,
		IncludeMode:     mode,
		IncludeOriginal: original,
		Hashes:          []string{contentHash([]byte(ref))},
		Pack:            pack,
		SyncedAt:        time.Now(),
	}
}

// syncOpenCode writes the owned provider subtree into opencode.json.
func (m *Manager) syncOpenCode(p *Policy, v *view, opts SyncOptions) (SyncResult, bool) {
	res := SyncResult{Target: srcOpenCode, Client: clientOpenCode}
	oc := p.Clients.OpenCode
	cfgPath, err := m.opencodeConfigPath(oc)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	res.Path = cfgPath

	schema := ResolveOpenCodeSchema(oc.Schema, cfgPath)
	render, err := RenderOpenCode(p, schema)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	plannedHash, err := valueHash(render.Value)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}

	e := v.entries[srcOpenCode]
	cur, exists, err := readProviderValue(cfgPath, render.Container, oc.ProviderID)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	if exists {
		curHash, herr := valueHash(cur)
		if herr != nil {
			res.Action, res.Error = ActionError, herr.Error()
			return res, false
		}
		switch {
		case e == nil && curHash != plannedHash && !opts.Force:
			res.Action = ActionSkippedForeign
			res.Detail = fmt.Sprintf("a %s.%s entry exists but gridctl did not write it; re-run with --force to overwrite", render.Container, oc.ProviderID)
			return res, false
		case e != nil && !hashRecorded(e.Hashes, curHash) && !opts.Force:
			res.Action = ActionSkippedDrift
			res.Detail = "the provider entry was edited since gridctl wrote it; 'gridctl models adopt' keeps the edit, --force overwrites it"
			return res, false
		case curHash == plannedHash && e != nil:
			res.Action = ActionUnchanged
			return res, false
		}
	}

	if opts.DryRun {
		res.Action = ActionWouldUpdate
		res.Detail = fmt.Sprintf("would write %s.%s (%s schema)", render.Container, oc.ProviderID, schema)
		return res, false
	}

	backup, err := upsertProviderValue(cfgPath, render.Container, oc.ProviderID, render.Value)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res, false
	}
	var prior []string
	pack := ""
	if e != nil {
		prior, pack = e.Hashes, e.Pack
	}
	v.entries[srcOpenCode] = &project.Entry{
		Kind:       project.KindModels,
		Client:     clientOpenCode,
		Source:     srcOpenCode,
		Path:       cfgPath + "#" + render.Container + "." + oc.ProviderID,
		ConfigPath: cfgPath,
		Strategy:   schema,
		Hashes:     appendHash(prior, plannedHash),
		Pack:       pack,
		SyncedAt:   time.Now(),
	}
	res.Action, res.BackupPath = ActionUpdated, backup
	res.Detail = fmt.Sprintf("select the %q model in OpenCode to route through the policy", oc.ProviderID+"/"+p.Router.EntryModel)
	return res, true
}

// Statuses reports every target's projection state. Read-only.
func (m *Manager) Statuses(ctx context.Context) ([]Status, error) {
	pl, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	v := viewFromLock(pl)

	p, perr := m.LoadPolicy()
	if perr != nil && !errors.Is(perr, ErrNoPolicy) {
		return nil, perr
	}

	var statuses []Status
	statuses = append(statuses, m.fragmentStatus(p, v))
	if row, ok := m.includeStatus(p, v); ok {
		statuses = append(statuses, row)
	}
	if row, ok := m.opencodeStatus(p, v); ok {
		statuses = append(statuses, row)
	}
	return statuses, nil
}

func (m *Manager) fragmentStatus(p *Policy, v *view) Status {
	row := Status{Target: srcFragment, Client: clientLiteLLM}
	e := v.entries[srcFragment]
	if e == nil {
		row.State = StateNeverSynced
		if p == nil {
			row.Detail = "no policy; run 'gridctl models init'"
		}
		return row
	}
	row.Path = e.Path
	t := e.SyncedAt
	row.SyncedAt = &t
	row.RestartPending = e.InstalledHash != e.AckedHash

	disk, err := os.ReadFile(e.Path)
	switch {
	case err != nil:
		row.State = StateTargetMissing
		row.Detail = "the rendered fragment is gone; re-run 'gridctl models sync'"
	case contentHash(disk) != e.InstalledHash:
		row.State = StateDrifted
		row.Detail = "the fragment was edited since gridctl wrote it"
	case p != nil && p.Hash() != e.CanonicalHash:
		row.State = StateStale
		row.Detail = "the policy changed since the last sync"
	default:
		row.State = StateInSync
	}
	return row
}

func (m *Manager) includeStatus(p *Policy, v *view) (Status, bool) {
	e := v.entries[srcInclude]
	declared := p != nil && p.Targets.LiteLLM != nil && p.Targets.LiteLLM.ConfigPath != ""
	if e == nil && !declared {
		return Status{}, false
	}
	row := Status{Target: srcInclude, Client: clientLiteLLM}
	if e == nil {
		row.State = StateNeverSynced
		return row, true
	}
	row.Path = e.ConfigPath
	t := e.SyncedAt
	row.SyncedAt = &t

	raw, err := os.ReadFile(e.ConfigPath)
	switch {
	case err != nil:
		row.State = StateTargetMissing
		row.Detail = "the parent LiteLLM config is gone"
	case !hasIncludeLine(string(raw), e.IncludeRef):
		row.State = StateDrifted
		row.Detail = "the include line was removed from the parent config"
	default:
		row.State = StateInSync
		if declared {
			if frag, ferr := m.FragmentPath(p); ferr == nil {
				if ref, _ := relIncludeRef(mustExpand(m, p.Targets.LiteLLM.ConfigPath, e.ConfigPath), frag); ref != e.IncludeRef {
					row.State = StateStale
					row.Detail = "the fragment path changed since the last sync"
				}
			}
		}
	}
	return row, true
}

// mustExpand resolves a policy path, falling back to the recorded one.
func mustExpand(m *Manager, declared, recorded string) string {
	if p, err := m.expandPath(declared); err == nil {
		return p
	}
	return recorded
}

func (m *Manager) opencodeStatus(p *Policy, v *view) (Status, bool) {
	e := v.entries[srcOpenCode]
	declared := p != nil && p.Clients.OpenCode != nil
	if e == nil && !declared {
		return Status{}, false
	}
	row := Status{Target: srcOpenCode, Client: clientOpenCode}
	if e == nil {
		row.State = StateNeverSynced
		return row, true
	}
	row.Path = e.ConfigPath
	t := e.SyncedAt
	row.SyncedAt = &t

	container := "provider"
	if e.Strategy == SchemaV2 {
		container = "providers"
	}
	providerID := providerIDFromPath(e.Path, container)
	cur, exists, err := readProviderValue(e.ConfigPath, container, providerID)
	switch {
	case err != nil:
		row.State = StateTargetMissing
		row.Detail = err.Error()
	case !exists:
		row.State = StateTargetMissing
		if _, serr := os.Stat(e.ConfigPath); serr != nil {
			row.Detail = "the OpenCode config is gone"
		} else {
			row.Detail = "the provider entry was removed from the OpenCode config"
		}
	default:
		curHash, herr := valueHash(cur)
		switch {
		case herr != nil:
			row.State = StateDrifted
			row.Detail = herr.Error()
		case !hashRecorded(e.Hashes, curHash):
			row.State = StateDrifted
			row.Detail = "the provider entry was edited since gridctl wrote it"
		default:
			row.State = StateInSync
			if declared {
				if planned, ok := m.plannedOpenCodeHash(p, e.Strategy); ok && planned != curHash {
					row.State = StateStale
					row.Detail = "the policy changed since the last sync"
				}
			}
		}
	}
	if declared && row.State == StateInSync {
		expected := p.Clients.OpenCode.ProviderID + "/" + p.Router.EntryModel
		if current := readTopLevelString(e.ConfigPath, "model"); current != "" && current != expected {
			row.Detail = fmt.Sprintf("OpenCode's default model is %q, not the policy router %q; gridctl never owns that key", current, expected)
		}
	}
	return row, true
}

func (m *Manager) plannedOpenCodeHash(p *Policy, schema string) (string, bool) {
	render, err := RenderOpenCode(p, schema)
	if err != nil {
		return "", false
	}
	h, err := valueHash(render.Value)
	if err != nil {
		return "", false
	}
	return h, true
}

// providerIDFromPath recovers the provider id from the composite
// lockfile path (<config>#<container>.<id>). The composite exists only
// for the engine's one-owner invariant; this is the single reader, and
// only because the id predates any policy reload.
func providerIDFromPath(composite, container string) string {
	marker := "#" + container + "."
	if i := strings.LastIndex(composite, marker); i >= 0 {
		return composite[i+len(marker):]
	}
	return ""
}

// Unsync removes every projected target in reverse dependency order:
// the provider stanza, the include line, then the fragment file.
func (m *Manager) Unsync(ctx context.Context, opts UnsyncOptions) ([]UnsyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []UnsyncResult
	err := m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		v := viewFromLock(pl)

		if e := v.entries[srcOpenCode]; e != nil {
			results = append(results, m.unsyncOpenCode(v, e, opts))
			if err := saveView(pl, v); err != nil {
				return err
			}
		}
		if e := v.entries[srcInclude]; e != nil {
			results = append(results, m.unsyncInclude(v, e))
			if err := saveView(pl, v); err != nil {
				return err
			}
		}
		if e := v.entries[srcFragment]; e != nil {
			results = append(results, m.unsyncFragment(v, e, opts))
			if err := saveView(pl, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (m *Manager) unsyncOpenCode(v *view, e *project.Entry, opts UnsyncOptions) UnsyncResult {
	res := UnsyncResult{Target: srcOpenCode, Client: clientOpenCode, Path: e.ConfigPath}
	container := "provider"
	if e.Strategy == SchemaV2 {
		container = "providers"
	}
	providerID := providerIDFromPath(e.Path, container)

	cur, exists, err := readProviderValue(e.ConfigPath, container, providerID)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if !exists {
		res.Action = ActionAlreadyGone
		delete(v.entries, srcOpenCode)
		return res
	}
	if curHash, herr := valueHash(cur); herr == nil && !hashRecorded(e.Hashes, curHash) && !opts.Force {
		res.Action = ActionKeptDrift
		res.Detail = "the provider entry was hand-edited; kept (re-run with --force to remove)"
		return res
	}
	backup, _, err := removeProviderValue(e.ConfigPath, container, providerID)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	delete(v.entries, srcOpenCode)
	res.Action, res.BackupPath = ActionRemoved, backup
	return res
}

func (m *Manager) unsyncInclude(v *view, e *project.Entry) UnsyncResult {
	res := UnsyncResult{Target: srcInclude, Client: clientLiteLLM, Path: e.ConfigPath}
	raw, err := os.ReadFile(e.ConfigPath)
	if err != nil {
		res.Action = ActionAlreadyGone
		delete(v.entries, srcInclude)
		return res
	}
	content := string(raw)
	if !hasIncludeLine(content, e.IncludeRef) {
		res.Action = ActionAlreadyGone
		delete(v.entries, srcInclude)
		return res
	}
	updated, err := removeIncludeLine(content, e.IncludeRef, e.IncludeMode, e.IncludeOriginal)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	backup, err := createBackup(e.ConfigPath)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if err := project.AtomicWriteFile(e.ConfigPath, []byte(restoreCRLF(content, updated))); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	delete(v.entries, srcInclude)
	res.Action, res.BackupPath = ActionRemoved, backup
	return res
}

func (m *Manager) unsyncFragment(v *view, e *project.Entry, opts UnsyncOptions) UnsyncResult {
	res := UnsyncResult{Target: srcFragment, Client: clientLiteLLM, Path: e.Path}
	disk, err := os.ReadFile(e.Path)
	if err != nil {
		res.Action = ActionAlreadyGone
		delete(v.entries, srcFragment)
		return res
	}
	if contentHash(disk) != e.InstalledHash && !opts.Force {
		res.Action = ActionKeptDrift
		res.Detail = "the fragment was hand-edited; kept (re-run with --force to remove)"
		return res
	}
	if err := os.Remove(e.Path); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	delete(v.entries, srcFragment)
	res.Action = ActionRemoved
	return res
}

// AckRestart marks the running LiteLLM as restarted since the last
// fragment write: the only way the restart-pending latch clears.
func (m *Manager) AckRestart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		v := viewFromLock(pl)
		e := v.entries[srcFragment]
		if e == nil {
			return fmt.Errorf("%w: run 'gridctl models sync' first", ErrNotSynced)
		}
		e.AckedHash = e.InstalledHash
		return saveView(pl, v)
	})
}

// AdoptResult is one target's adopt outcome.
type AdoptResult struct {
	Target string `json:"target"`
	Client string `json:"client"`
	Path   string `json:"path"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

// Adopt records the current on-disk state of every recorded target as
// gridctl-owned, clearing drift without touching any file.
func (m *Manager) Adopt(ctx context.Context) ([]AdoptResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []AdoptResult
	err := m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		v := viewFromLock(pl)

		if e := v.entries[srcFragment]; e != nil {
			res := AdoptResult{Target: srcFragment, Client: clientLiteLLM, Path: e.Path}
			if disk, derr := os.ReadFile(e.Path); derr == nil {
				e.InstalledHash = contentHash(disk)
				res.Action = ActionAdopted
			} else {
				res.Action = ActionAlreadyGone
				res.Detail = "nothing on disk to adopt"
			}
			results = append(results, res)
		}
		if e := v.entries[srcOpenCode]; e != nil {
			res := AdoptResult{Target: srcOpenCode, Client: clientOpenCode, Path: e.ConfigPath}
			container := "provider"
			if e.Strategy == SchemaV2 {
				container = "providers"
			}
			id := providerIDFromPath(e.Path, container)
			if cur, exists, rerr := readProviderValue(e.ConfigPath, container, id); rerr == nil && exists {
				if h, herr := valueHash(cur); herr == nil {
					e.Hashes = appendHash(e.Hashes, h)
					res.Action = ActionAdopted
				} else {
					res.Action, res.Detail = ActionError, herr.Error()
				}
			} else {
				res.Action = ActionAlreadyGone
				res.Detail = "nothing in the config to adopt"
			}
			results = append(results, res)
		}
		if len(results) == 0 {
			return fmt.Errorf("%w: run 'gridctl models sync' first", ErrNotSynced)
		}
		return saveView(pl, v)
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// unifiedDiff renders a unified diff between two labeled texts. The
// error is safe to swallow: writes go to an in-memory buffer.
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
