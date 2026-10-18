package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/state"
)

// Manager owns service lock entries and the unit files they point at.
type Manager struct {
	store *project.Store
	sup   Supervisor
	home  string
	now   func() time.Time
}

// NewManager builds a manager for home. sup may be nil only in tests that
// never call the supervisor; production callers pass NewSupervisor.
func NewManager(home string, sup Supervisor) *Manager {
	return &Manager{store: project.NewStore(home), sup: sup, home: home, now: time.Now}
}

// InstallOptions configures one install.
type InstallOptions struct {
	Spec           Spec
	DryRun         bool
	Force          bool
	NoEnable       bool
	Replace        bool
	HomeOverridden bool
	Notes          []string
}

// InstallResult is the machine-readable install document.
type InstallResult struct {
	Name        string            `json:"name"`
	Platform    string            `json:"platform"`
	UnitPath    string            `json:"unit_path"`
	Label       string            `json:"label"`
	ExecStart   []string          `json:"exec_start"`
	Environment map[string]string `json:"environment"`
	Enabled     bool              `json:"enabled"`
	Running     bool              `json:"running"`
	PID         int               `json:"pid"`
	Linger      string            `json:"linger,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
	DryRun      bool              `json:"dry_run"`
	Text        string            `json:"text,omitempty"`
	WouldEnable bool              `json:"would_enable,omitempty"`
}

// Row is one status line.
type Row struct {
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	State       string `json:"state"`
	Enabled     string `json:"enabled"`
	PID         int    `json:"pid,omitempty"`
	Port        int    `json:"port,omitempty"`
	Stack       string `json:"stack"`
	Unit        string `json:"unit"`
	Linger      string `json:"linger,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Label       string `json:"label,omitempty"`
	Pack        string `json:"pack,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// UninstallOptions configures uninstall.
type UninstallOptions struct {
	Force  bool
	DryRun bool
}

// UninstallResult is one uninstall outcome.
type UninstallResult struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	UnitPath string `json:"unit_path,omitempty"`
	Detail   string `json:"detail,omitempty"`
	DryRun   bool   `json:"dry_run"`
}

// ResetRow is the reset adapter's view of one unit. resetops converts it.
type ResetRow struct {
	Name   string
	Client string
	Path   string
	Action string
	Detail string
	Error  string
}

// Install records the unit in the lockfile and writes it. Dry-run checks
// conflict and drift against the read-only lock and returns before Set,
// Save, or the file write.
func (m *Manager) Install(ctx context.Context, opts InstallOptions) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	spec := opts.Spec
	text, err := Render(spec)
	if err != nil {
		return InstallResult{}, err
	}
	result := InstallResult{
		Name:        spec.StackName,
		Platform:    string(spec.Platform),
		UnitPath:    spec.UnitPath,
		Label:       spec.Label,
		ExecStart:   append([]string(nil), spec.Argv...),
		Environment: envMap(spec.Env),
		Notes:       append([]string(nil), opts.Notes...),
		DryRun:      opts.DryRun,
		WouldEnable: !opts.NoEnable,
		Linger:      "unknown",
	}
	if opts.DryRun {
		result.Text = text
	}
	// Conflict, drift, and a foreign file are refused before any process
	// is stopped. A later refusal must not have already killed the gateway.
	if err := m.preflight(ctx, spec, text, opts.Force); err != nil {
		return result, err
	}
	restartOwned := false
	if !opts.DryRun {
		var stopErr error
		restartOwned, stopErr = m.stopReplacedDaemon(ctx, spec, opts.Replace)
		if stopErr != nil {
			return result, stopErr
		}
	}

	client := string(spec.Platform)
	source := spec.StackName
	err = m.store.Mutate(ctx, opts.DryRun, func(l *project.Lock) error {
		if err := checkInstall(l, spec, text, opts.Force); err != nil {
			return err
		}
		if opts.DryRun {
			return nil
		}
		prev := l.Get(project.KindService, client, source)
		if drifted, driftErr := unitDrifted(prev); driftErr != nil {
			return driftErr
		} else if drifted && opts.Force {
			if err := backupUnit(prev.Path, m.now()); err != nil {
				return err
			}
		}
		exists, identical, fileErr := classifyUnitFile(spec.UnitPath, text)
		if fileErr != nil {
			return fileErr
		}
		if prev == nil && exists && !identical && opts.Force {
			if err := backupUnit(spec.UnitPath, m.now()); err != nil {
				return err
			}
		}
		// An identical pre-existing file is adopted, not created. A
		// --force replacement is our bytes; CreatedByGridctl stays true
		// so reset can remove the unit. The backup holds the foreign content.
		created := !identical || (prev != nil && prev.CreatedByGridctl)
		entry := &project.Entry{
			Kind:             project.KindService,
			Client:           client,
			Source:           source,
			Path:             spec.UnitPath,
			CreatedByGridctl: created,
			InstalledHash:    Hash(text),
			Pack:             spec.Pack,
			SyncedAt:         m.now(),
			Extra: map[string]any{
				"stack_file":   spec.StackFile,
				"port":         spec.Port,
				"base_port":    spec.BasePort,
				"binary":       spec.Binary,
				"label":        spec.Label,
				"service_name": spec.Name,
			},
		}
		if err := l.Set(entry); err != nil {
			return err
		}
		if err := l.Save(); err != nil {
			return err
		}
		if err := writeUnit(spec.UnitPath, []byte(text)); err != nil {
			l.Remove(project.KindService, client, source)
			if saveErr := l.Save(); saveErr != nil {
				return fmt.Errorf("%w (also failed to roll back the lock entry: %v)", err, saveErr)
			}
			return err
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if !opts.DryRun {
		if err := ensureServiceLogDir(spec); err != nil {
			return result, err
		}
	}
	if opts.DryRun || (opts.NoEnable && !restartOwned) {
		if !opts.DryRun && spec.Platform == PlatformSystemd {
			result.Linger = m.linger(ctx)
		}
		return result, nil
	}
	if m.sup == nil {
		return result, &InfraError{Msg: "no service supervisor is available"}
	}
	start := m.sup.Enable
	if restartOwned {
		// The supervisor already owns this PID. Restart through it so
		// the new unit text is what runs. KillDaemon would trip
		// Restart=on-failure.
		start = m.sup.Restart
	}
	if err := start(ctx, spec); err != nil {
		return result, enableFailure(spec, opts.HomeOverridden, err)
	}
	result.Enabled = true
	if rt, qerr := m.sup.Query(ctx, spec); qerr == nil {
		result.Running = rt.State == "running"
		result.PID = rt.PID
		if !rt.Enabled && opts.NoEnable {
			result.Enabled = false
		}
	}
	if spec.Platform == PlatformSystemd {
		result.Linger = m.linger(ctx)
	}
	return result, nil
}

// Status returns one row per service entry, or the named stack.
func (m *Manager) Status(ctx context.Context, name string) ([]Row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	entries := l.Entries(project.KindService)
	linger := ""
	if len(entries) > 0 && m.sup != nil {
		linger = m.linger(ctx)
	}
	var rows []Row
	for _, e := range entries {
		if name != "" && !entryMatches(e, name) {
			continue
		}
		row, rowErr := m.rowFor(ctx, e, linger)
		if rowErr != nil {
			return nil, rowErr
		}
		rows = append(rows, row)
	}
	if name != "" && len(rows) == 0 {
		return nil, &AttentionError{Msg: fmt.Sprintf("no installed service %q", name)}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

// Uninstall disables and stops the unit, removes the file when the hash
// matches (or Force), and drops the lock entry. A drifted unit without
// Force is kept and reported. A stop failure leaves the record so the
// command can be re-run.
func (m *Manager) Uninstall(ctx context.Context, name string, opts UninstallOptions) (UninstallResult, error) {
	if err := ctx.Err(); err != nil {
		return UninstallResult{}, err
	}
	l, err := m.store.Load(ctx)
	if err != nil {
		return UninstallResult{}, err
	}
	entry := findEntry(l.Entries(project.KindService), name)
	if entry == nil {
		return UninstallResult{}, &AttentionError{Msg: fmt.Sprintf("no installed service %q", name)}
	}
	res := UninstallResult{Name: entry.Source, UnitPath: entry.Path, DryRun: opts.DryRun}
	drifted, err := unitDrifted(entry)
	if err != nil {
		return res, err
	}
	if drifted && !opts.Force {
		res.Action = "kept-drift"
		res.Detail = "the unit was edited after gridctl wrote it; kept (re-run with --force to remove)"
		return res, &AttentionError{Msg: res.Detail + ": " + entry.Path}
	}
	missing := false
	if _, statErr := os.Stat(entry.Path); os.IsNotExist(statErr) {
		missing = true
	}
	if opts.DryRun {
		if missing {
			res.Action = "already-gone"
			res.Detail = "unit file is already gone; the ownership record would be removed"
		} else {
			res.Action = "would-remove"
		}
		return res, nil
	}
	spec := specFromEntry(entry)
	if m.sup != nil {
		if err := m.sup.Disable(ctx, spec); err != nil {
			if isInfra(err) {
				return res, err
			}
			return res, &AttentionError{Msg: fmt.Sprintf("could not stop %s: %v; the record was left in place so the command can be re-run", entry.Path, err)}
		}
	}
	err = m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		current := findEntry(pl.Entries(project.KindService), name)
		if current == nil {
			return nil
		}
		if !missing && (opts.Force || !mustKeepDrift(current)) {
			if rmErr := os.Remove(current.Path); rmErr != nil && !os.IsNotExist(rmErr) {
				return rmErr
			}
		}
		pl.Remove(current.Kind, current.Client, current.Source)
		return pl.Save()
	})
	if err != nil {
		return res, err
	}
	if m.sup != nil {
		if reloadErr := m.sup.DaemonReload(ctx); reloadErr != nil {
			res.Detail = "unit removed; daemon-reload failed: " + reloadErr.Error()
		}
	}
	if missing {
		res.Action = "already-gone"
		if res.Detail == "" {
			res.Detail = "unit file was already gone; the ownership record was removed"
		}
	} else {
		res.Action = "removed"
	}
	return res, nil
}

// StopReport is what destroy needs after asking the supervisor to stop.
type StopReport struct {
	// Present is true when a service lock entry exists.
	Present bool
	Path    string
	// OwnedPID is the supervisor's main PID when that process is the
	// recorded daemon, or the unit was active. Zero means the supervisor
	// does not own the recorded process. Destroy must not KillDaemon an
	// owned PID: Restart=on-failure would start it again.
	OwnedPID int
	// StoppedUnit is true when a loaded unit was stopped. Destroy prints
	// the re-enable message only then.
	StoppedUnit bool
	// NotLoaded means the user manager does not have the unit, or no
	// supervisor binary is installed. Destroy continues as unsupervised.
	NotLoaded bool
}

// Stop stops a loaded unit without disabling it and without KillDaemon.
// A missing entry reports Present false. A unit that is not loaded is
// not a stop failure: the supervisor does not own the process.
func (m *Manager) Stop(ctx context.Context, name string) (StopReport, error) {
	entry, err := Lookup(ctx, m.home, name)
	if err != nil || entry == nil {
		return StopReport{}, err
	}
	res := StopReport{Present: true, Path: entry.Path}
	if m.sup == nil {
		return res, &InfraError{Msg: "no service supervisor is available"}
	}
	spec := specFromEntry(entry)
	rt, qerr := m.sup.Query(ctx, spec)
	if qerr != nil {
		if isInfra(qerr) {
			// No user manager is installed, so nothing can restart a
			// killed process. Destroy takes the unsupervised path.
			res.NotLoaded = true
			return res, nil
		}
		return res, qerr
	}
	if rt.State == "not-loaded" {
		res.NotLoaded = true
		return res, nil
	}
	st, _ := state.Load(name)
	ownsPID := rt.PID > 0 && st != nil && rt.PID == st.PID
	if rt.State == "running" || ownsPID {
		if rt.PID > 0 {
			res.OwnedPID = rt.PID
		} else if st != nil && st.PID > 0 {
			res.OwnedPID = st.PID
		}
	}
	if err := m.sup.Stop(ctx, spec); err != nil {
		if errors.Is(err, ErrUnitNotLoaded) || isInfra(err) {
			res.NotLoaded = true
			res.OwnedPID = 0
			return res, nil
		}
		return res, err
	}
	res.StoppedUnit = true
	return res, nil
}

// Linger reports the user-manager linger state.
func (m *Manager) Linger(ctx context.Context) (string, error) {
	if m.sup == nil {
		return "unknown", nil
	}
	return m.sup.Linger(ctx)
}

// Preview lists the units reset would touch. force matches Remove, so a
// dry-run with --force does not report kept-drift for a unit Remove
// would delete. It does not call the supervisor.
func (m *Manager) Preview(ctx context.Context, force bool) ([]ResetRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	var rows []ResetRow
	for _, e := range l.Entries(project.KindService) {
		rows = append(rows, previewResetRow(e, force))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

// Remove disables and stops each unit, removes the file when the hash
// matches (or force), and drops the record. Per-item failures are rows.
func (m *Manager) Remove(ctx context.Context, force bool) ([]ResetRow, error) {
	preview, err := m.Preview(ctx, force)
	if err != nil {
		return nil, err
	}
	var rows []ResetRow
	removed := false
	for _, row := range preview {
		got := m.removeOne(ctx, row, force)
		if got.Action == "removed" || got.Action == "already-gone" || got.Action == "dropped-record" {
			removed = true
		}
		rows = append(rows, got)
	}
	if removed && m.sup != nil {
		_ = m.sup.DaemonReload(ctx)
	}
	return rows, nil
}

func (m *Manager) removeOne(ctx context.Context, row ResetRow, force bool) ResetRow {
	entry, err := Lookup(ctx, m.home, row.Name)
	if err != nil {
		row.Action = "failed"
		row.Error = err.Error()
		return row
	}
	if entry == nil {
		row.Action = "already-gone"
		return row
	}
	// Stop even a drifted unit. Reset's job is to stop daemons; the file
	// and the record stay unless the hash matches or force is set.
	if m.sup != nil {
		if err := m.sup.Disable(ctx, specFromEntry(entry)); err != nil {
			row.Action = "failed"
			row.Error = err.Error()
			row.Detail = "stop failed; the record was left in place"
			return row
		}
	}
	missing := false
	if _, statErr := os.Stat(entry.Path); os.IsNotExist(statErr) {
		missing = true
	}
	drifted, _ := unitDrifted(entry)
	if drifted && !force {
		row.Action = "kept-drift"
		row.Detail = "the unit was edited after gridctl wrote it; kept (re-run with --force to remove)"
		return row
	}
	err = m.store.Mutate(ctx, false, func(l *project.Lock) error {
		current := l.Get(entry.Kind, entry.Client, entry.Source)
		if current == nil {
			return nil
		}
		if !missing {
			if rmErr := os.Remove(current.Path); rmErr != nil && !os.IsNotExist(rmErr) {
				return rmErr
			}
		}
		l.Remove(current.Kind, current.Client, current.Source)
		return l.Save()
	})
	if err != nil {
		row.Action = "failed"
		row.Error = err.Error()
		return row
	}
	if missing {
		row.Action = "already-gone"
		row.Detail = "unit file was already gone; the ownership record was removed"
		return row
	}
	row.Action = "removed"
	return row
}

// Lookup returns the service entry for the stack name, or nil.
func Lookup(ctx context.Context, home, name string) (*project.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l, err := project.NewStore(home).Load(ctx)
	if err != nil {
		return nil, err
	}
	return findEntry(l.Entries(project.KindService), name), nil
}

// HasEntries reports whether any service entry exists.
func HasEntries(ctx context.Context, home string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	l, err := project.NewStore(home).Load(ctx)
	if err != nil {
		return false, err
	}
	return len(l.Entries(project.KindService)) > 0, nil
}

// stopReplacedDaemon reports whether the supervisor already owns the
// running process. That process is restarted after the write. KillDaemon
// is only for a daemon the supervisor does not own.
func (m *Manager) stopReplacedDaemon(ctx context.Context, spec Spec, replace bool) (bool, error) {
	st, err := state.Load(spec.StackName)
	if err != nil || st == nil || !state.IsRunning(st) {
		return false, nil
	}
	home, homeErr := state.Home()
	if homeErr == nil && st.Home != "" && st.Home != home {
		return false, &AttentionError{Msg: fmt.Sprintf("stack %q is running under a different home (%s, pid %d, port %d); not touched", spec.StackName, st.Home, st.PID, st.Port)}
	}
	if m.supervisorOwns(ctx, spec, st.PID) {
		return true, nil
	}
	if !replace {
		return false, &AttentionError{Msg: fmt.Sprintf("stack %q is already running (pid %d, port %d); re-run with --replace to stop it first", spec.StackName, st.PID, st.Port)}
	}
	return false, state.WithLock(spec.StackName, 5*time.Second, func() error {
		current, loadErr := state.Load(spec.StackName)
		if loadErr != nil || current == nil || !state.IsRunning(current) {
			return nil
		}
		if homeErr == nil && current.Home != "" && current.Home != home {
			return &AttentionError{Msg: fmt.Sprintf("stack %q is running under a different home (%s, pid %d, port %d); not touched", spec.StackName, current.Home, current.PID, current.Port)}
		}
		if m.supervisorOwns(ctx, spec, current.PID) {
			return nil
		}
		if err := state.KillDaemon(current); err != nil {
			return fmt.Errorf("stopping running stack %q: %w", spec.StackName, err)
		}
		return state.Delete(spec.StackName)
	})
}

func (m *Manager) supervisorOwns(ctx context.Context, spec Spec, pid int) bool {
	if m.sup == nil || pid <= 0 {
		return false
	}
	rt, err := m.sup.Query(ctx, spec)
	if err != nil || rt.State == "not-loaded" || rt.PID <= 0 {
		return false
	}
	return rt.PID == pid
}

func (m *Manager) linger(ctx context.Context) string {
	if m.sup == nil {
		return "unknown"
	}
	value, err := m.sup.Linger(ctx)
	if err != nil || value == "" {
		return "unknown"
	}
	return value
}

func (m *Manager) rowFor(ctx context.Context, e *project.Entry, linger string) (Row, error) {
	spec := specFromEntry(e)
	row := Row{
		Name:     e.Source,
		Platform: e.Client,
		Stack:    extraString(e.Extra["stack_file"]),
		Unit:     e.Path,
		Port:     extraInt(e.Extra["port"]),
		Label:    spec.Label,
		Pack:     e.Pack,
		Linger:   linger,
		Enabled:  "no",
	}
	if _, err := os.Stat(e.Path); os.IsNotExist(err) {
		row.State = "target-missing"
		row.Remediation = reinstallRemediation(e, m.manualDaemon(e.Source, 0))
		row.Detail = "unit file is gone"
		return row, nil
	}
	if drifted, err := unitDrifted(e); err != nil {
		row.State = "unknown"
		row.Detail = err.Error()
		return row, nil
	} else if drifted {
		row.State = "drifted"
		row.Remediation = "the unit was edited after gridctl wrote it; re-run 'gridctl service install' with --force"
		return row, nil
	}
	if e.Pack != "" {
		if stale, detail := packStale(e); stale {
			row.State = "stale"
			row.Detail = detail
			owned := 0
			if m.sup != nil {
				if rt, qerr := m.sup.Query(ctx, spec); qerr == nil {
					owned = rt.PID
				} else if isInfra(qerr) {
					return row, qerr
				}
			}
			row.Remediation = reinstallRemediation(e, m.manualDaemon(e.Source, owned))
			return row, nil
		}
	}
	if m.sup == nil {
		row.State = "unknown"
		row.Detail = "no service supervisor is available"
		return row, nil
	}
	rt, err := m.sup.Query(ctx, spec)
	if err != nil {
		if isInfra(err) {
			return row, err
		}
		row.State = "unknown"
		row.Detail = err.Error()
		row.Remediation = reinstallRemediation(e, m.manualDaemon(e.Source, 0))
		return row, nil
	}
	row.State = rt.State
	if row.State == "" {
		row.State = "unknown"
	}
	row.PID = rt.PID
	if rt.Enabled {
		row.Enabled = "yes"
	}
	if row.State != "running" || row.Enabled != "yes" {
		row.Remediation = reinstallRemediation(e, m.manualDaemon(e.Source, rt.PID))
	}
	return row, nil
}

func previewResetRow(e *project.Entry, force bool) ResetRow {
	row := ResetRow{
		Name:   e.Source,
		Client: e.Client,
		Path:   e.Path,
		Action: "would-remove",
	}
	if _, err := os.Stat(e.Path); os.IsNotExist(err) {
		row.Action = "would-remove"
		row.Detail = "unit file is already gone; the ownership record would be removed"
		return row
	}
	if drifted, err := unitDrifted(e); err == nil && drifted {
		if force {
			row.Action = "would-remove"
			row.Detail = "the unit was edited after gridctl wrote it; --force removes it"
			return row
		}
		row.Action = "kept-drift"
		row.Detail = "the unit was edited after gridctl wrote it; kept (re-run with --force to remove)"
	}
	return row
}

func packStale(e *project.Entry) (bool, string) {
	locked, err := packops.LoadLockedPack(e.Pack)
	if err != nil || locked == nil || locked.Stack == nil {
		return true, "pack record has no pinned stack"
	}
	pinned := packops.PinnedStackPath(locked.Stack)
	recorded := extraString(e.Extra["stack_file"])
	if !sameCleanPath(recorded, pinned) {
		return true, fmt.Sprintf("pinned stack is %s", pinned)
	}
	return false, ""
}

func sameCleanPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func reinstallRemediation(e *project.Entry, manual bool) string {
	extra := ""
	if manual {
		extra = " --replace"
	}
	if e.Pack != "" {
		return fmt.Sprintf("re-run 'gridctl service install --pack %s%s'", e.Pack, extra)
	}
	stack := extraString(e.Extra["stack_file"])
	if stack == "" {
		return fmt.Sprintf("re-run 'gridctl service install' for %s%s", e.Source, extra)
	}
	return fmt.Sprintf("re-run 'gridctl service install --stack %s%s'", stack, extra)
}

func (m *Manager) manualDaemon(name string, ownedPID int) bool {
	st, err := state.Load(name)
	if err != nil || st == nil || !state.IsRunning(st) {
		return false
	}
	if ownedPID > 0 && st.PID == ownedPID {
		return false
	}
	return true
}

func (m *Manager) preflight(ctx context.Context, spec Spec, text string, force bool) error {
	l, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	return checkInstall(l, spec, text, force)
}

func checkInstall(l *project.Lock, spec Spec, text string, force bool) error {
	client := string(spec.Platform)
	if err := rejectPathConflict(l, spec.UnitPath, client, spec.StackName); err != nil {
		return err
	}
	prev := l.Get(project.KindService, client, spec.StackName)
	drifted, err := unitDrifted(prev)
	if err != nil {
		return err
	}
	if drifted && !force {
		return &AttentionError{Msg: fmt.Sprintf("unit %s was edited after gridctl wrote it; re-run with --force to replace it", prev.Path)}
	}
	if prev != nil {
		return nil
	}
	exists, identical, err := classifyUnitFile(spec.UnitPath, text)
	if err != nil {
		return err
	}
	if exists && !identical && !force {
		return &AttentionError{Msg: fmt.Sprintf("unit %s exists but was not recorded by gridctl; kept (re-run with --force to back it up and replace it)", spec.UnitPath)}
	}
	return nil
}

func enableFailure(spec Spec, homeOverridden bool, err error) error {
	msg := fmt.Sprintf("wrote %s but enable failed: %v", spec.UnitPath, err)
	if !isInfra(err) {
		msg += "\nFinish by hand: " + manualEnableCommand(spec)
	}
	if spec.Platform == PlatformSystemd && homeOverridden {
		msg += "\nThe unit is outside the user manager's search path because the home is overridden."
	}
	if isInfra(err) {
		return &InfraError{Msg: msg}
	}
	return &AttentionError{Msg: msg}
}

func ensureServiceLogDir(spec Spec) error {
	if spec.Platform != PlatformLaunchd || spec.LogPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(spec.LogPath), 0o755); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}
	return nil
}

func rejectPathConflict(l *project.Lock, path, client, source string) error {
	owner := l.Owner(path)
	if owner == nil {
		return nil
	}
	if owner.Kind == project.KindService && owner.Client == client && owner.Source == source {
		return nil
	}
	return fmt.Errorf("%w: %s is recorded for %s %q (client %s)", project.ErrPathConflict, path, owner.Kind, owner.Source, owner.Client)
}

func unitDrifted(e *project.Entry) (bool, error) {
	if e == nil || e.Path == "" || e.InstalledHash == "" {
		return false, nil
	}
	data, err := os.ReadFile(e.Path) // #nosec G304 -- path is the lockfile-recorded unit
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return Hash(string(data)) != e.InstalledHash, nil
}

func mustKeepDrift(e *project.Entry) bool {
	drifted, err := unitDrifted(e)
	return err == nil && drifted
}

func classifyUnitFile(path, text string) (exists, identical bool, err error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the unit being installed
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, string(data) == text, nil
}

func writeUnit(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating unit directory: %w", err)
	}
	if err := project.AtomicWriteFile(path, data); err != nil {
		return fmt.Errorf("writing unit: %w", err)
	}
	return nil
}

func backupUnit(path string, now time.Time) error {
	data, err := os.ReadFile(path) // #nosec G304 -- backing up the unit being replaced
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	dest := path + ".gridctl-backup-" + now.UTC().Format("20060102-150405")
	if _, statErr := os.Stat(dest); statErr == nil {
		dest = dest + "-" + strconv.FormatInt(now.UnixNano(), 10)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil { // #nosec G306 -- backup of a 0644 unit file
		return fmt.Errorf("backing up drifted unit: %w", err)
	}
	return nil
}

func specFromEntry(e *project.Entry) Spec {
	name := extraString(e.Extra["service_name"])
	if name == "" {
		name = SanitizeName(e.Source)
	}
	label := extraString(e.Extra["label"])
	if label == "" {
		label = Label(name)
	}
	return Spec{
		Platform:  Platform(e.Client),
		Name:      name,
		StackName: e.Source,
		Label:     label,
		Binary:    extraString(e.Extra["binary"]),
		UnitPath:  e.Path,
		StackFile: extraString(e.Extra["stack_file"]),
		Pack:      e.Pack,
		Port:      extraInt(e.Extra["port"]),
		BasePort:  extraInt(e.Extra["base_port"]),
	}
}

func findEntry(entries []*project.Entry, name string) *project.Entry {
	var fallback *project.Entry
	for _, e := range entries {
		if e.Source == name {
			return e
		}
		if extraString(e.Extra["service_name"]) == name {
			fallback = e
		}
	}
	return fallback
}

func entryMatches(e *project.Entry, name string) bool {
	return e.Source == name || extraString(e.Extra["service_name"]) == name
}

func envMap(pairs []KV) map[string]string {
	out := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		out[kv.Key] = kv.Value
	}
	return out
}

func extraString(v any) string {
	s, _ := v.(string)
	return s
}

func extraInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}
