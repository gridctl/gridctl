// Package resetops implements the machine-wide reset cascade behind
// `gridctl reset` and POST /api/reset. It composes the kind managers
// that own every write (skillsync, agentsync, contexts, wiring) plus
// the daemon/runtime state helpers; like pkg/packops it owns only the
// orchestration, never a write path of its own.
//
// Removal is lockfile-driven per Constitution Article XVI: only
// artifacts the projection lockfile or wiring records attest gridctl
// created are touched, drifted (hand-edited) artifacts are kept unless
// forced, and foreign entries in shared client configs are never
// deleted. The cascade order is load-bearing: daemons stop first
// (a live daemon's reconcile loop re-projects skills into client trees
// mid-removal), the projection lockfile is consumed last.
package resetops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// SchemaVersion versions every reset document (Article X / XVII).
const SchemaVersion = 1

// Row actions. Preview uses the would- forms; execution reports what
// actually happened.
const (
	ActionWouldRemove = "would-remove"
	ActionWouldStop   = "would-stop"
	ActionRemoved     = "removed"
	ActionStopped     = "stopped"
	ActionKeptDrift   = "kept-drift"
	ActionKeptForeign = "kept-foreign"
	ActionDropRecord  = "dropped-record"
	ActionAlreadyGone = "already-gone"
	ActionFailed      = "failed"
	ActionSkipped     = "skipped"
)

// Options configure a reset pass.
type Options struct {
	// Purge additionally deletes <home>/.gridctl after the cascade.
	Purge bool
	// Force removes drifted (hand-edited) projections too. Foreign
	// wiring entries are never removed, force or not.
	Force bool
	// SelfPID, when non-zero, defers actions that would kill the calling
	// process: the daemon with this PID is not stopped, its state file
	// is not deleted, and the purge RemoveAll is deferred. Execute then
	// returns a non-nil Finalize on the Doc; the caller runs it after
	// its HTTP response is flushed (see FR12a self-termination).
	SelfPID int
}

// Row is one artifact line in reset output.
type Row struct {
	Kind   string `json:"kind"` // skill | agent | context | wiring | daemon | containers | state-file | gridctl-dir
	Name   string `json:"name"`
	Client string `json:"client,omitempty"`
	Path   string `json:"path,omitempty"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// PurgeStats enumerates what --purge destroys beyond the cascade, so
// the confirmation can show real numbers. Counts of -1 render as
// "unknown" (e.g. a locked vault).
type PurgeStats struct {
	GridctlDir     string `json:"gridctl_dir"`
	VaultVariables int    `json:"vault_variables"`
	OAuthServers   int    `json:"oauth_servers"`
	PinFiles       int    `json:"pin_files"`
	TelemetryBytes int64  `json:"telemetry_bytes"`
}

// Doc is the machine-readable reset document: the same shape backs
// --dry-run, the interactive preview, and the execution result, all
// computed from one inventory so the preview cannot lie.
type Doc struct {
	SchemaVersion int         `json:"schema_version"`
	Home          string      `json:"home"`
	Purge         bool        `json:"purge"`
	DryRun        bool        `json:"dry_run"`
	BackupPath    string      `json:"backup_path,omitempty"`
	BackupNote    string      `json:"backup_note,omitempty"`
	Rows          []Row       `json:"rows"`
	Kept          []string    `json:"kept,omitempty"`
	Failed        int         `json:"failed"`
	Stats         *PurgeStats `json:"purge_stats,omitempty"`

	// Finalize is set by Execute when Options.SelfPID deferred the
	// caller's own teardown; run it after the response is flushed.
	// Never serialized.
	Finalize func() error `json:"-"`
}

// Progress streams execution to the caller: a phase transition
// (row == nil) or one completed row. Never called concurrently.
type Progress func(phase string, row *Row)

// skillSyncer, agentSyncer, contextSyncer, and wireManager are the
// slices of the kind managers reset consumes; interfaces so unit tests
// exercise the cascade without real client trees (mocks are unit-test
// only per Article IV).
type skillSyncer interface {
	Statuses(ctx context.Context) ([]skillsync.ProjectionStatus, error)
	Unsync(ctx context.Context, names []string, opts skillsync.UnsyncOptions) ([]skillsync.UnsyncResult, error)
}

type agentSyncer interface {
	Statuses(ctx context.Context) ([]agentsync.ProjectionStatus, error)
	Unsync(ctx context.Context, names []string, opts agentsync.UnsyncOptions) ([]agentsync.UnsyncResult, error)
}

type contextSyncer interface {
	Statuses(ctx context.Context) ([]contexts.ClientStatus, error)
	Unsync(ctx context.Context, slug string) ([]contexts.UnsyncResult, error)
}

type wireManager interface {
	Statuses(ctx context.Context, opts wiring.StatusOptions) ([]wiring.Row, error)
	UnlinkClient(ctx context.Context, prov provisioner.ClientProvisioner, configPath, name string, force, dryRun bool) (wiring.Result, error)
	DropRecord(ctx context.Context, client, name string) (wiring.Result, error)
	Registry() *provisioner.Registry
}

// Runtime is the container-teardown slice of pkg/runtime. Nil in
// Managers means container cleanup is skipped with a reported row
// (e.g. Docker unavailable).
type Runtime interface {
	Down(ctx context.Context, stackName string) error
}

// Managers bundles everything the reset cascade drives. Construct with
// the real managers in cmd / internal/api; tests inject fakes.
type Managers struct {
	Skills   skillSyncer
	Agents   agentSyncer
	Contexts contextSyncer
	Wiring   wireManager
	Runtime  Runtime
	Home     string
}

// inventory is the single source both Preview and Execute act on.
type inventory struct {
	skills       []skillsync.ProjectionStatus
	agents       []agentsync.ProjectionStatus
	contextRows  []contexts.ClientStatus
	wiringRows   []wiring.Row
	stacks       []state.DaemonState
	keptSkills   map[string]bool
	keptAgents   map[string]bool
	keptContexts map[string]bool
}

// collect reads every surface once. Read-only; safe for dry runs.
func (m *Managers) collect(ctx context.Context, opts Options) (*inventory, error) {
	inv := &inventory{
		keptSkills:   map[string]bool{},
		keptAgents:   map[string]bool{},
		keptContexts: map[string]bool{},
	}
	var err error
	if m.Skills != nil {
		if inv.skills, err = m.Skills.Statuses(ctx); err != nil {
			return nil, fmt.Errorf("reading skill projections: %w", err)
		}
	}
	if m.Agents != nil {
		if inv.agents, err = m.Agents.Statuses(ctx); err != nil {
			return nil, fmt.Errorf("reading agent projections: %w", err)
		}
	}
	if m.Contexts != nil {
		if inv.contextRows, err = m.Contexts.Statuses(ctx); err != nil {
			return nil, fmt.Errorf("reading context state: %w", err)
		}
	}
	if m.Wiring != nil {
		if inv.wiringRows, err = m.Wiring.Statuses(ctx, wiring.StatusOptions{}); err != nil {
			return nil, fmt.Errorf("reading wiring records: %w", err)
		}
	}
	if inv.stacks, err = state.List(); err != nil {
		return nil, fmt.Errorf("reading daemon state: %w", err)
	}

	// Drift pre-filter (the packops splitKept rule, extended to contexts):
	// skillsync/agentsync/contexts unsync is NOT drift-aware, so a drifted
	// resource must be excluded before Unsync or the hand-edit is deleted.
	if !opts.Force {
		for _, s := range inv.skills {
			if s.State == skillsync.StateDrifted {
				inv.keptSkills[s.Skill] = true
			}
		}
		for _, a := range inv.agents {
			if a.State == agentsync.StateDrifted {
				inv.keptAgents[a.Agent] = true
			}
		}
		for _, c := range inv.contextRows {
			if c.State == contexts.StateDrifted {
				inv.keptContexts[c.Slug] = true
			}
		}
	}
	return inv, nil
}

// rows renders the inventory as preview rows (would- actions).
func (inv *inventory) rows(opts Options) ([]Row, []string) {
	var rows []Row
	var kept []string

	for _, s := range inv.skills {
		if inv.keptSkills[s.Skill] {
			continue // one kept row per resource, below
		}
		rows = append(rows, Row{Kind: "skill", Name: s.Skill, Client: s.Client, Path: s.Target, Action: ActionWouldRemove})
	}
	for name := range inv.keptSkills {
		rows = append(rows, Row{Kind: "skill", Name: name, Action: ActionKeptDrift,
			Detail: "a projection of this skill was hand-edited; kept (re-run with --force to remove)"})
		kept = append(kept, "skill/"+name)
	}

	for _, a := range inv.agents {
		if inv.keptAgents[a.Agent] {
			continue
		}
		rows = append(rows, Row{Kind: "agent", Name: a.Agent, Client: a.Client, Path: a.Target, Action: ActionWouldRemove})
	}
	for name := range inv.keptAgents {
		rows = append(rows, Row{Kind: "agent", Name: name, Action: ActionKeptDrift,
			Detail: "a projection of this agent was hand-edited; kept (re-run with --force to remove)"})
		kept = append(kept, "agent/"+name)
	}

	for _, c := range inv.contextRows {
		if !contextSynced(c) {
			continue
		}
		if inv.keptContexts[c.Slug] {
			rows = append(rows, Row{Kind: "context", Name: c.Slug, Client: c.Slug, Path: c.TargetPath, Action: ActionKeptDrift,
				Detail: contextKeptDetail})
			kept = append(kept, "context/"+c.Slug)
			continue
		}
		rows = append(rows, Row{Kind: "context", Name: c.Slug, Client: c.Slug, Path: c.TargetPath, Action: ActionWouldRemove})
	}

	for _, w := range inv.wiringRows {
		action, detail, _ := wiringDisposition(w.State, opts.Force)
		if action == "" {
			continue // advisory rows carry nothing to remove
		}
		rows = append(rows, Row{Kind: "wiring", Name: w.Name, Client: w.Client, Path: w.Target, Action: action, Detail: detail})
		if action == ActionKeptForeign || action == ActionKeptDrift {
			kept = append(kept, "wiring/"+w.Client)
		}
	}

	for _, s := range inv.stacks {
		action := ActionWouldStop
		if !state.IsRunning(&s) {
			action = ActionAlreadyGone
		}
		rows = append(rows, Row{Kind: "daemon", Name: s.StackName, Action: action,
			Detail: fmt.Sprintf("pid %d, port %d", s.PID, s.Port)})
		rows = append(rows, Row{Kind: "containers", Name: s.StackName, Action: ActionWouldRemove,
			Detail: "all containers and networks of this stack"})
		statePath, _ := state.StatePath(s.StackName)
		rows = append(rows, Row{Kind: "state-file", Name: s.StackName, Path: statePath, Action: ActionWouldRemove})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].Client != rows[j].Client {
			return rows[i].Client < rows[j].Client
		}
		return rows[i].Name < rows[j].Name
	})
	sort.Strings(kept)
	return rows, kept
}

// contextKeptDetail is the shared drift-kept wording for context rows,
// used by both the preview and the executor so they cannot diverge.
const contextKeptDetail = "the context artifact was hand-edited; kept (re-run with --force to remove)"

// wiringDisposition classifies one wiring record for both the preview
// and the executor: the row action (preview form), the shared detail
// string, and whether the executor proceeds (UnlinkClient for
// would-remove, DropRecord for dropped-record). One classifier, two
// consumers, so the preview cannot promise what execution refuses.
func wiringDisposition(rowState string, force bool) (action, detail string, proceed bool) {
	switch rowState {
	case wiring.StateForeign:
		return ActionKeptForeign, "not created by gridctl; never removed", false
	case wiring.StateMissing:
		return "", "", false // advisory: nothing recorded, nothing to do
	case wiring.StateDrifted:
		if !force {
			return ActionKeptDrift, "the entry was edited after gridctl wrote it; kept (re-run with --force to remove)", false
		}
		return ActionWouldRemove, "", true
	case wiring.StateTargetMissing:
		return ActionDropRecord, "entry or client already gone; only the ownership record is removed", true
	default: // in-sync, stale
		return ActionWouldRemove, "", true
	}
}

// contextSynced reports whether a context client row has anything on
// disk that reset would touch.
func contextSynced(c contexts.ClientStatus) bool {
	switch c.State {
	case contexts.StateNeverSynced, contexts.StateUnsupported:
		return false
	}
	return c.TargetPath != ""
}

// Preview computes the reset document without writing anything.
func (m *Managers) Preview(ctx context.Context, opts Options) (*Doc, error) {
	inv, err := m.collect(ctx, opts)
	if err != nil {
		return nil, err
	}
	rows, kept := inv.rows(opts)
	doc := &Doc{
		SchemaVersion: SchemaVersion,
		Home:          m.Home,
		Purge:         opts.Purge,
		DryRun:        true,
		Rows:          rows,
		Kept:          kept,
	}
	if opts.Purge {
		doc.Stats = m.purgeStats()
	}
	return doc, nil
}

// GridctlDir returns the state directory reset --purge deletes.
func (m *Managers) GridctlDir() string {
	return filepath.Join(m.Home, ".gridctl")
}

// purgeStats gathers what --purge destroys. Best-effort: counting must
// never block a reset, so failures degrade to -1 (rendered "unknown").
func (m *Managers) purgeStats() *PurgeStats {
	dir := m.GridctlDir()
	stats := &PurgeStats{GridctlDir: dir, VaultVariables: -1}

	stats.OAuthServers = countFiles(filepath.Join(dir, "oauth"), func(name string) bool {
		return name != "key" && name != "lock"
	})
	stats.PinFiles = countFiles(filepath.Join(dir, "pins"), nil) +
		countFiles(filepath.Join(dir, "pins", "skills"), nil)

	if entries := telemetrySize(filepath.Join(dir, "telemetry")); entries >= 0 {
		stats.TelemetryBytes = entries
	}
	if n, ok := vaultCount(filepath.Join(dir, "vault")); ok {
		stats.VaultVariables = n
	}
	return stats
}

func countFiles(dir string, keep func(string) bool) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if keep == nil || keep(e.Name()) {
			n++
		}
	}
	return n
}

// vaultCount reports how many variables the vault holds. A locked or
// unreadable vault reports not-ok; the caller renders "unknown".
func vaultCount(dir string) (int, bool) {
	st := vault.NewStore(dir)
	if err := st.Load(); err != nil {
		return 0, false
	}
	if st.IsLocked() {
		return 0, false
	}
	return len(st.List()), true
}

func telemetrySize(dir string) int64 {
	var total int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort sizing
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0
	}
	return total
}
