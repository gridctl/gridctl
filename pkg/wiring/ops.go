package wiring

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// fileMissing reports a definitively absent config file (stat says the
// path does not exist; other stat failures read as present so a
// permission problem is not mistaken for a wipe).
func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// Wiring states. in-sync, stale, drifted, and target-missing come from
// the engine vocabulary; foreign and missing are wiring extensions.
const (
	StateInSync        = "in-sync"
	StateStale         = "stale"
	StateDrifted       = "drifted"
	StateTargetMissing = "target-missing"
	// StateForeign marks an entry at a gridctl name with no record: it
	// is never deleted and only overwritten with --force (or adopted).
	StateForeign = "foreign"
	// StateMissing marks a detected client with nothing recorded and
	// nothing present: the doctor "detected but not linked" row.
	StateMissing = "missing"
)

// Result actions.
const (
	ActionLinked             = "linked"
	ActionUpdated            = "updated"
	ActionUnchanged          = "unchanged"
	ActionAdopted            = "adopted"
	ActionRemoved            = "removed"
	ActionAlreadyGone        = "already-gone"
	ActionNotLinked          = "not-linked"
	ActionSkippedForeign     = "skipped-foreign"
	ActionSkippedDrift       = "skipped-drift"
	ActionSkippedUnavailable = "skipped-unavailable"
	ActionError              = "error"
	ActionWouldLink          = "would-link"
	ActionWouldUpdate        = "would-update"
	ActionWouldAdopt         = "would-adopt"
	ActionWouldRemove        = "would-remove"
)

// Result describes one ownership decision for a (client, entry) pair.
type Result struct {
	Client      string `json:"client"`
	Name        string `json:"name"`
	Target      string `json:"target,omitempty"`
	Action      string `json:"action"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Row is one (client, entry) line in status output.
type Row struct {
	Client      string     `json:"client"`
	Name        string     `json:"name"`
	Channel     string     `json:"channel"`
	Target      string     `json:"target,omitempty"`
	State       string     `json:"state"`
	Detail      string     `json:"detail,omitempty"`
	Remediation string     `json:"remediation,omitempty"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
}

// SyncOptions configure a sync pass (the ownership-aware link --all).
type SyncOptions struct {
	// Clients restricts the pass to these slugs. Empty means every
	// detected client.
	Clients []string
	// ServerName is the entry key to write (default "gridctl";
	// "gridctl-<group>" for group links).
	ServerName string
	// GatewayURL, Port, Group, and ClientID compose the entry exactly as
	// `gridctl link` flags do.
	GatewayURL string
	Port       int
	Group      string
	ClientID   string
	// Force overwrites foreign and drifted entries (after backup).
	Force bool
	// DryRun reports the plan without writing anything.
	DryRun bool
}

// StatusOptions configure a status pass.
type StatusOptions struct {
	// Port anchors the planned-value comparison (staleness) to the
	// current gateway port.
	Port int
	// ServerName is the default entry name scanned for foreign and
	// missing rows (default "gridctl").
	ServerName string
}

// NeedsAttention reports whether any row requires action. Missing rows
// (detected but never linked) are advisory and do not count.
func NeedsAttention(rows []Row) bool {
	for _, r := range rows {
		switch r.State {
		case StateDrifted, StateStale, StateTargetMissing, StateForeign:
			return true
		}
	}
	return false
}

// HasFailures reports whether any result needs the caller's attention.
func HasFailures(results []Result) bool {
	for _, r := range results {
		switch r.Action {
		case ActionError, ActionSkippedForeign, ActionSkippedDrift:
			return true
		}
	}
	return false
}

// linkOptions builds the per-client LinkOptions for a sync pass.
func (o SyncOptions) linkOptions() provisioner.LinkOptions {
	return provisioner.LinkOptions{
		GatewayURL: o.GatewayURL,
		Port:       o.Port,
		ServerName: o.ServerName,
		ClientID:   o.ClientID,
		Group:      o.Group,
		Force:      o.Force,
		DryRun:     o.DryRun,
	}
}

// LinkClient links one client with ownership recorded: the decision
// (write, adopt, refuse) comes from the lockfile and value hashes, and
// the provisioner is invoked with ownership pre-resolved. All three
// gridctl link surfaces (CLI, declarative apply reconcile, UI API)
// route through here so the lockfile never lies.
func (m *Manager) LinkClient(ctx context.Context, prov provisioner.ClientProvisioner, configPath string, opts provisioner.LinkOptions) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res Result
	err := m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		res = m.linkLocked(lf, prov, configPath, opts)
		if opts.DryRun || !linkRecorded(res.Action) {
			return nil
		}
		return saveView(pl, lf)
	})
	return res, err
}

// linkRecorded reports whether a link result changed lockfile state.
func linkRecorded(action string) bool {
	switch action {
	case ActionLinked, ActionUpdated, ActionUnchanged, ActionAdopted:
		return true
	}
	return false
}

// linkLocked makes the ownership decision for one client entry and
// performs the write. It mutates the view; the caller persists it.
func (m *Manager) linkLocked(lf *LockFile, prov provisioner.ClientProvisioner, configPath string, opts provisioner.LinkOptions) Result {
	slug, name := prov.Slug(), opts.ServerName
	res := Result{Client: slug, Name: name, Target: configPath}

	planned, err := plannedHash(prov, opts)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	current, exists, err := currentValue(prov, configPath, name)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	var curHash string
	if exists {
		if curHash, err = ValueHash(current); err != nil {
			res.Action, res.Error = ActionError, err.Error()
			return res
		}
	}
	rec := lf.entry(slug, name)

	switch {
	case !exists:
		// Nothing at the key: a first link, or a relink over a wiped
		// entry (the record, if any, is refreshed either way).
		return m.writeEntry(lf, prov, configPath, opts, rec, planned, ActionLinked, res)

	case rec == nil:
		if curHash == planned {
			// Identical value: shared-ownership adopt (the server-side
			// apply rule). Nothing to lose, so recording is silent.
			if opts.DryRun {
				res.Action = ActionWouldAdopt
				return res
			}
			m.record(lf, slug, name, configPath, opts, appendHash(nil, curHash), false)
			res.Action = ActionAdopted
			res.Detail = "existing entry matches what gridctl would write; recorded ownership without rewriting"
			return res
		}
		if !opts.Force {
			res.Action = ActionSkippedForeign
			res.Detail = foreignDetail(name, current, prov.NeedsBridge())
			res.Remediation = fmt.Sprintf("adopt it with 'gridctl project adopt --kind wiring --client %s --name %s', or overwrite it with --force", slug, name)
			return res
		}
		return m.writeEntry(lf, prov, configPath, opts, nil, planned, ActionUpdated, res)

	default:
		// An entry identical to what gridctl would write is never drift,
		// even when its hash rotated out of the history (config restored
		// from a backup, or lockfile and config synced independently).
		if curHash == planned {
			res.Action = ActionUnchanged
			if !opts.DryRun {
				m.record(lf, slug, name, configPath, opts, appendHash(rec.Hashes, planned), rec.CreatedByGridctl)
			}
			return res
		}
		if !hashRecorded(rec.Hashes, curHash) && !opts.Force {
			res.Action = ActionSkippedDrift
			res.Detail = fmt.Sprintf("the '%s' entry was edited since gridctl wrote it", name)
			res.Remediation = fmt.Sprintf("keep the edit with 'gridctl project adopt --kind wiring --client %s --name %s', or overwrite it with --force", slug, name)
			return res
		}
		return m.writeEntry(lf, prov, configPath, opts, rec, planned, ActionUpdated, res)
	}
}

// writeEntry performs the provisioner write with ownership resolved and
// records the result. action is the success action for the existing-key
// case; a fresh key reports linked.
func (m *Manager) writeEntry(lf *LockFile, prov provisioner.ClientProvisioner, configPath string, opts provisioner.LinkOptions, rec *Entry, planned, action string, res Result) Result {
	if opts.DryRun {
		if action == ActionLinked {
			res.Action = ActionWouldLink
		} else {
			res.Action = ActionWouldUpdate
		}
		return res
	}
	opts.OwnershipResolved = true
	if err := prov.Link(configPath, opts); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	var history []string
	if rec != nil {
		history = rec.Hashes
	}
	created := rec == nil || rec.CreatedByGridctl
	m.record(lf, prov.Slug(), opts.ServerName, configPath, opts, appendHash(history, planned), created)
	res.Action = action
	return res
}

// record updates the view's entry for (client, name).
func (m *Manager) record(lf *LockFile, client, name, configPath string, opts provisioner.LinkOptions, hashes []string, createdByGridctl bool) {
	lf.set(client, name, &Entry{
		ConfigPath:       configPath,
		Group:            opts.Group,
		ClientID:         opts.ClientID,
		Hashes:           hashes,
		CreatedByGridctl: createdByGridctl,
		SyncedAt:         time.Now().UTC(),
	})
}

// foreignDetail explains a foreign entry, with the one-time migration
// hint when the shape matches what pre-lockfile gridctl versions wrote.
func foreignDetail(name string, current map[string]any, needsBridge bool) string {
	detail := fmt.Sprintf("an entry named '%s' exists but was not recorded by gridctl", name)
	if provisioner.LooksLikeLegacyLink(current, needsBridge) {
		detail += " (likely a link written before ownership recording; adopt it once to migrate)"
	}
	return detail
}

// UnlinkClient removes one owned entry: the key is deleted only when
// its current value is one gridctl recorded (or --force), and the
// record is always purged once the key is gone so a later relink never
// trips over stale bookkeeping. Foreign entries are never deleted, with
// or without force (the Stow invariant: never delete what you do not
// own).
func (m *Manager) UnlinkClient(ctx context.Context, prov provisioner.ClientProvisioner, configPath, name string, force, dryRun bool) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res Result
	err := m.store.Mutate(ctx, dryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		res = m.unlinkLocked(lf, prov, configPath, name, force, dryRun)
		if dryRun {
			return nil
		}
		switch res.Action {
		case ActionRemoved, ActionAlreadyGone:
			return saveView(pl, lf)
		}
		return nil
	})
	return res, err
}

// unlinkLocked makes the removal decision and performs it.
func (m *Manager) unlinkLocked(lf *LockFile, prov provisioner.ClientProvisioner, configPath, name string, force, dryRun bool) Result {
	slug := prov.Slug()
	res := Result{Client: slug, Name: name, Target: configPath}

	current, exists, err := currentValue(prov, configPath, name)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	rec := lf.entry(slug, name)

	if rec == nil {
		if !exists {
			res.Action = ActionNotLinked
			return res
		}
		res.Action = ActionSkippedForeign
		res.Detail = foreignDetail(name, current, prov.NeedsBridge())
		res.Remediation = fmt.Sprintf("gridctl never deletes entries it did not record; adopt it first with 'gridctl project adopt --kind wiring --client %s --name %s' if it is yours", slug, name)
		return res
	}

	if !exists {
		// Key already gone (client wiped its config, or the user removed
		// it): purge the record so relink starts clean.
		if dryRun {
			res.Action = ActionWouldRemove
			return res
		}
		lf.remove(slug, name)
		res.Action = ActionAlreadyGone
		res.Detail = "the entry is already gone; removed the ownership record"
		return res
	}

	curHash, err := ValueHash(current)
	if err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	if !hashRecorded(rec.Hashes, curHash) && !force {
		res.Action = ActionSkippedDrift
		res.Detail = fmt.Sprintf("the '%s' entry was edited since gridctl wrote it", name)
		res.Remediation = fmt.Sprintf("keep the edit with 'gridctl project adopt --kind wiring --client %s --name %s', or remove it anyway with --force", slug, name)
		return res
	}

	if dryRun {
		res.Action = ActionWouldRemove
		return res
	}
	if err := prov.Unlink(configPath, name); err != nil {
		res.Action, res.Error = ActionError, err.Error()
		return res
	}
	lf.remove(slug, name)
	res.Action = ActionRemoved
	return res
}

// Adopt records ownership of the entry's current value without
// rewriting it: the explicit take-ownership verb (terraform import,
// stow --adopt). It works both for foreign entries (pre-lockfile links)
// and for recorded entries that drifted (keep the user's edit).
func (m *Manager) Adopt(ctx context.Context, client, name string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := Result{Client: client, Name: name}

	prov, ok := m.registry.FindBySlug(client)
	if !ok {
		return res, fmt.Errorf("unknown client %q", client)
	}
	configPath, found := prov.Detect()
	if !found {
		return res, fmt.Errorf("%s is not detected on this system", prov.Name())
	}
	res.Target = configPath

	err := m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		current, exists, err := currentValue(prov, configPath, name)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: %s has no '%s' entry", ErrNothingToAdopt, prov.Name(), name)
		}
		curHash, err := ValueHash(current)
		if err != nil {
			return err
		}
		rec := lf.entry(client, name)
		var history []string
		created := false
		if rec != nil {
			history = rec.Hashes
			created = rec.CreatedByGridctl
		}
		// Group and client ID are carried forward from the prior record;
		// a foreign adopt has neither and records the plain shape.
		opts := provisioner.LinkOptions{}
		if rec != nil {
			opts.Group, opts.ClientID = rec.Group, rec.ClientID
		} else if strings.HasPrefix(name, "gridctl-") {
			opts.Group = strings.TrimPrefix(name, "gridctl-")
		}
		m.record(lf, client, name, configPath, opts, appendHash(history, curHash), created)
		res.Action = ActionAdopted
		return saveView(pl, lf)
	})
	return res, err
}

// Sync links every detected client (or the named subset) with
// ownership recorded: the wiring-kind counterpart of `gridctl link
// --all`. Bridge clients without npx are skipped, matching the CLI.
func (m *Manager) Sync(ctx context.Context, opts SyncOptions) ([]Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	selected, err := m.selectClients(opts.Clients)
	if err != nil {
		return nil, err
	}
	hasNpx := provisioner.NpxAvailable()

	var results []Result
	err = m.store.Mutate(ctx, opts.DryRun, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		for _, dc := range selected {
			if err := ctx.Err(); err != nil {
				return err
			}
			if dc.Provisioner.NeedsBridge() && !hasNpx {
				results = append(results, Result{
					Client: dc.Provisioner.Slug(), Name: opts.ServerName, Target: dc.ConfigPath,
					Action: ActionSkippedUnavailable,
					Detail: "'npx' not found (mcp-remote bridge requires Node.js)",
				})
				continue
			}
			res := m.linkLocked(lf, dc.Provisioner, dc.ConfigPath, opts.linkOptions())
			results = append(results, res)
			if !opts.DryRun && linkRecorded(res.Action) {
				if err := saveView(pl, lf); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return results, err
}

// selectClients resolves the sync target set: every detected client, or
// the named subset (which must be known slugs; not-detected named
// clients error rather than silently skip).
func (m *Manager) selectClients(slugs []string) ([]provisioner.DetectedClient, error) {
	if len(slugs) == 0 {
		return m.registry.DetectAll(), nil
	}
	var out []provisioner.DetectedClient
	for _, slug := range slugs {
		prov, ok := m.registry.FindBySlug(slug)
		if !ok {
			return nil, fmt.Errorf("unknown client %q (supported: %s)", slug, strings.Join(m.registry.AllSlugs(), ", "))
		}
		configPath, found := prov.Detect()
		if !found {
			return nil, fmt.Errorf("%s is not detected on this system", prov.Name())
		}
		out = append(out, provisioner.DetectedClient{Provisioner: prov, ConfigPath: configPath})
	}
	return out, nil
}

// Statuses computes the wiring state matrix: every recorded entry, plus
// foreign rows (gridctl-named entries never recorded) and missing rows
// (clients detected with nothing recorded and nothing present) for
// detected clients. Reads are lock-free.
func (m *Manager) Statuses(ctx context.Context, opts StatusOptions) ([]Row, error) {
	if opts.ServerName == "" {
		opts.ServerName = "gridctl"
	}
	lf, err := m.loadView(ctx)
	if err != nil {
		return nil, err
	}

	var rows []Row
	for _, key := range sortedRecordKeys(lf) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows = append(rows, m.statusFor(key.client, key.name, lf.entry(key.client, key.name), opts.Port))
	}

	for _, dc := range m.registry.DetectAll() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slug := dc.Provisioner.Slug()
		entries, lerr := dc.Provisioner.ListServers(dc.ConfigPath)
		if lerr != nil {
			// Recorded rows surface the read error themselves; a client
			// with nothing recorded would otherwise vanish entirely.
			if len(lf.Records[slug]) == 0 {
				rows = append(rows, Row{
					Client: slug, Name: opts.ServerName, Channel: ChannelMergeKey, Target: dc.ConfigPath,
					State:  StateTargetMissing,
					Detail: "config could not be read: " + lerr.Error(),
				})
			}
			continue
		}
		sawGridctlEntry := false
		for _, e := range entries {
			if !gridctlEntryName(e.Name, opts.ServerName) {
				continue
			}
			sawGridctlEntry = true
			if lf.entry(slug, e.Name) != nil {
				continue
			}
			rows = append(rows, Row{
				Client: slug, Name: e.Name, Channel: ChannelMergeKey, Target: dc.ConfigPath,
				State:       StateForeign,
				Detail:      foreignDetail(e.Name, e.Raw, dc.Provisioner.NeedsBridge()),
				Remediation: fmt.Sprintf("adopt it with 'gridctl project adopt --kind wiring --client %s --name %s'", slug, e.Name),
			})
		}
		if !sawGridctlEntry && len(lf.Records[slug]) == 0 {
			rows = append(rows, Row{
				Client: slug, Name: opts.ServerName, Channel: ChannelMergeKey, Target: dc.ConfigPath,
				State:       StateMissing,
				Detail:      "client detected but not linked",
				Remediation: fmt.Sprintf("link it with 'gridctl link %s'", slug),
			})
		}
	}
	return rows, nil
}

// gridctlEntryName reports whether an entry name is a gridctl link
// name: the configured default or a group-scoped gridctl-<group>.
func gridctlEntryName(name, serverName string) bool {
	return name == serverName || strings.HasPrefix(name, "gridctl-")
}

// statusFor computes one recorded entry's row.
func (m *Manager) statusFor(client, name string, e *Entry, port int) Row {
	row := Row{Client: client, Name: name, Channel: ChannelMergeKey, Target: e.ConfigPath}
	syncedAt := e.SyncedAt
	row.SyncedAt = &syncedAt

	prov, ok := m.registry.FindBySlug(client)
	if !ok {
		row.State = StateTargetMissing
		row.Detail = "client is no longer supported; run 'gridctl project unsync --kind wiring --client " + client + "' to clean up"
		return row
	}
	if _, found := prov.Detect(); !found {
		row.State = StateTargetMissing
		row.Detail = "client is not detected on this system"
		return row
	}

	current, exists, err := currentValue(prov, e.ConfigPath, name)
	if err != nil {
		row.State = StateTargetMissing
		row.Detail = err.Error()
		return row
	}
	if !exists {
		row.State = StateTargetMissing
		if fileMissing(e.ConfigPath) {
			row.Detail = fmt.Sprintf("config file no longer exists at %s; gridctl's entry (and everything else in that file) is gone", e.ConfigPath)
			row.Remediation = fmt.Sprintf("recreate it with 'gridctl link %s'", client)
		} else {
			row.Detail = "the recorded entry was removed from the config (file still present)"
			row.Remediation = fmt.Sprintf("relink with 'gridctl link %s', or drop the record with 'gridctl project unsync --kind wiring --client %s'", client, client)
		}
		return row
	}

	curHash, err := ValueHash(current)
	if err != nil {
		row.State = StateDrifted
		row.Detail = err.Error()
		return row
	}
	if !hashRecorded(e.Hashes, curHash) {
		row.State = StateDrifted
		row.Detail = fmt.Sprintf("the '%s' entry was edited since gridctl wrote it", name)
		row.Remediation = fmt.Sprintf("keep the edit with 'gridctl project adopt --kind wiring --client %s --name %s', or rewrite it with 'gridctl project sync --kind wiring --clients %s --force'", client, name, client)
		return row
	}

	planned, err := plannedHash(prov, rebuildOptions(e, name, port))
	if err == nil && planned != curHash {
		row.State = StateStale
		row.Detail = "the entry differs from what gridctl would write now (gateway port or entry shape changed)"
		row.Remediation = fmt.Sprintf("refresh it with 'gridctl link %s'", client)
		return row
	}
	row.State = StateInSync
	return row
}

// DropRecord purges an ownership record without touching any config
// file: the cleanup path for records whose client is no longer
// detected on this machine.
func (m *Manager) DropRecord(ctx context.Context, client, name string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := Result{Client: client, Name: name}
	err := m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		rec := lf.entry(client, name)
		if rec == nil {
			return fmt.Errorf("%w: %s / %s", ErrNotRecorded, client, name)
		}
		res.Target = rec.ConfigPath
		lf.remove(client, name)
		res.Action = ActionRemoved
		res.Detail = "record dropped; no config file was touched"
		return saveView(pl, lf)
	})
	return res, err
}

// RecordedHash returns the newest recorded hash for (client, name), or
// "" when nothing is recorded.
func (m *Manager) RecordedHash(ctx context.Context, client, name string) (string, error) {
	lf, err := m.loadView(ctx)
	if err != nil {
		return "", err
	}
	if e := lf.entry(client, name); e != nil {
		return e.latestHash(), nil
	}
	return "", nil
}

// DriftedClients reports which clients currently have at least one
// recorded entry in the drifted state. Feeds the Connections badge.
func (m *Manager) DriftedClients(ctx context.Context, port int) (map[string]bool, error) {
	lf, err := m.loadView(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, key := range sortedRecordKeys(lf) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := m.statusFor(key.client, key.name, lf.entry(key.client, key.name), port)
		if row.State == StateDrifted {
			out[key.client] = true
		}
	}
	return out, nil
}
