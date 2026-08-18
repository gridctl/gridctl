package resetops

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/state"
)

// stateLockTimeout bounds the per-stack lock acquisition while stopping
// daemons; a wedged holder must not hang the reset forever.
const stateLockTimeout = 10 * time.Second

// Execute runs the reset cascade: backup, stop daemons, unsync
// projections, unlink wiring, tear down containers, delete state files,
// and (purge) remove <home>/.gridctl. Per-item failures are reported in
// rows and counted, never silently swallowed (Article XI); the pass
// continues so a re-run removes only the remainder (idempotent retry).
//
// Ordering is load-bearing: daemons die FIRST because a live daemon's
// registry-refresh reconcile re-projects skills and agents into client
// trees, racing the removals. The projection lockfile is consumed last,
// entry by entry, through the managers that own it.
func (m *Managers) Execute(ctx context.Context, opts Options, progress Progress) (*Doc, error) {
	if progress == nil {
		progress = func(string, *Row) {}
	}
	emit := func(r Row) Row { progress("", &r); return r }

	inv, err := m.collect(ctx, opts)
	if err != nil {
		return nil, err
	}
	previewRows, kept := inv.rows(opts)
	doc := &Doc{
		SchemaVersion: SchemaVersion,
		Home:          m.Home,
		Purge:         opts.Purge,
		Kept:          kept,
	}
	if opts.Purge {
		doc.Stats = m.purgeStats()
	}

	// Phase 0: backup, fail closed. The preview rows carry every path
	// about to be touched; nothing is destroyed if the archive fails.
	progress("backup", nil)
	doc.Rows = previewRows // temporarily, for backupSet path resolution
	backupPath, err := m.Backup(ctx, doc, time.Now())
	if err != nil {
		return nil, err
	}
	doc.BackupPath = backupPath
	doc.Rows = nil

	// The backup row streams FIRST so an interrupted run still told the
	// user where the safety copy landed.
	doc.Rows = append(doc.Rows, emit(Row{Kind: "backup", Name: "pre-reset archive", Path: backupPath, Action: "written", Detail: doc.BackupNote}))

	// Surfaces whose manager failed to construct are reported and
	// counted up front: a "clean" exit 0 that silently skipped every
	// projection would be a lie.
	for _, name := range m.Missing {
		doc.Failed++
		doc.Rows = append(doc.Rows, emit(Row{Kind: name, Name: name, Action: ActionSkipped,
			Detail: "manager unavailable; nothing on this surface was removed (re-run reset once it is back)"}))
	}

	fail := func(r Row, err error) {
		r.Action = ActionFailed
		r.Error = err.Error()
		doc.Failed++
		doc.Rows = append(doc.Rows, emit(r))
	}
	ok := func(r Row) {
		doc.Rows = append(doc.Rows, emit(r))
	}

	// Phase 1: stop daemons (self deferred when SelfPID matches).
	progress("daemons", nil)
	var selfStack *state.DaemonState
	for i := range inv.stacks {
		s := inv.stacks[i]
		if err := ctx.Err(); err != nil {
			return doc, err
		}
		if opts.SelfPID != 0 && s.PID == opts.SelfPID {
			sCopy := s
			selfStack = &sCopy
			ok(Row{Kind: "daemon", Name: s.StackName, Action: ActionSkipped,
				Detail: "this process; stopped after the response is delivered"})
			continue
		}
		row := Row{Kind: "daemon", Name: s.StackName, Detail: fmt.Sprintf("pid %d", s.PID)}
		err := state.WithLock(s.StackName, stateLockTimeout, func() error {
			return state.KillDaemon(&s)
		})
		if err != nil {
			fail(row, err)
			continue
		}
		row.Action = ActionStopped
		ok(row)
	}
	if inv.orphanPID != 0 && inv.orphanPID != opts.SelfPID {
		// Foreground process under OUR home with no state file: kill it
		// so its reconcile loop cannot re-project mid-reset. No state
		// lock to take and no state file to delete; its containers keep
		// their stack name and are destroyed by name if any remain.
		row := Row{Kind: "daemon", Name: "gridctl (foreground)", Detail: fmt.Sprintf("pid %d", inv.orphanPID)}
		if err := state.KillDaemon(&state.DaemonState{StackName: "gridctl", PID: inv.orphanPID}); err != nil {
			fail(row, err)
		} else {
			row.Action = ActionStopped
			ok(row)
		}
	}
	if inv.foreignDaemonHome != "" {
		ok(Row{Kind: "daemon", Name: "gridctl (other home)", Action: ActionSkipped,
			Detail: fmt.Sprintf("daemon on port %d runs under home %s; not touched", defaultGatewayPort, inv.foreignDaemonHome)})
	}

	// A canceled context aborts the cascade here rather than letting a
	// later phase (state-file deletion, purge) outrun the removals it
	// depends on and orphan lockfile-attested files.
	if err := ctx.Err(); err != nil {
		return doc, err
	}
	// Phase 2: skill and agent projections, drift pre-filtered.
	progress("projections", nil)
	if m.Skills != nil {
		if names := removableNames(uniqueSkillNames(inv.skills), inv.keptSkills); len(names) > 0 {
			results, err := m.Skills.Unsync(ctx, names, skillsync.UnsyncOptions{})
			if err != nil {
				fail(Row{Kind: "skill", Name: fmt.Sprintf("%d skills", len(names))}, err)
			} else {
				for _, r := range results {
					ok(Row{Kind: "skill", Name: r.Skill, Client: r.Client, Path: r.Target, Action: r.Action})
				}
			}
		}
	}
	if m.Agents != nil {
		if names := removableNames(uniqueAgentNames(inv.agents), inv.keptAgents); len(names) > 0 {
			results, err := m.Agents.Unsync(ctx, names, agentsync.UnsyncOptions{})
			if err != nil {
				fail(Row{Kind: "agent", Name: fmt.Sprintf("%d agents", len(names))}, err)
			} else {
				for _, r := range results {
					ok(Row{Kind: "agent", Name: r.Agent, Client: r.Client, Path: r.Target, Action: r.Action})
				}
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return doc, err
	}
	// Phase 3: context artifacts, per client, drift pre-filtered.
	progress("contexts", nil)
	if m.Contexts != nil {
		for _, c := range inv.contextRows {
			if !contextSynced(c) {
				continue
			}
			if inv.keptContexts[c.Slug] {
				ok(Row{Kind: "context", Name: c.Slug, Client: c.Slug, Path: c.TargetPath, Action: ActionKeptDrift,
					Detail: contextKeptDetail})
				continue
			}
			results, err := m.Contexts.Unsync(ctx, c.Slug)
			if err != nil {
				fail(Row{Kind: "context", Name: c.Slug, Client: c.Slug, Path: c.TargetPath}, err)
				continue
			}
			for _, r := range results {
				ok(Row{Kind: "context", Name: r.Slug, Client: r.Slug, Path: r.TargetPath, Action: r.Action})
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return doc, err
	}
	// Phase 4: wiring entries through the ownership manager. Foreign is
	// never removed; drift needs force; undetected clients drop only the
	// record. Never raw provisioner Unlink.
	progress("wiring", nil)
	if m.Wiring != nil {
		reg := m.Wiring.Registry()
		for _, w := range inv.wiringRows {
			row := Row{Kind: "wiring", Name: w.Name, Client: w.Client, Path: w.Target}
			action, detail, proceed := wiringDisposition(w.State, opts.Force)
			if action == "" {
				continue // advisory: nothing recorded, nothing to do
			}
			if !proceed {
				row.Action = action
				row.Detail = detail
				ok(row)
				continue
			}
			if action == ActionDropRecord {
				if _, dropErr := m.Wiring.DropRecord(ctx, w.Client, w.Name); dropErr != nil {
					fail(row, dropErr)
					continue
				}
				row.Action = ActionDropRecord
				row.Detail = detail
				ok(row)
				continue
			}
			prov, found := reg.FindBySlug(w.Client)
			if !found {
				if _, dropErr := m.Wiring.DropRecord(ctx, w.Client, w.Name); dropErr != nil {
					fail(row, dropErr)
					continue
				}
				row.Action = ActionDropRecord
				row.Detail = "client no longer supported; record removed"
				ok(row)
				continue
			}
			res, err := m.Wiring.UnlinkClient(ctx, prov, w.Target, w.Name, opts.Force, false)
			if err != nil {
				fail(row, err)
				continue
			}
			row.Action = res.Action
			row.Detail = res.Detail
			ok(row)
		}
	}

	if err := ctx.Err(); err != nil {
		return doc, err
	}
	// Phase 5: containers and networks, per stack from OUR state files.
	// Never an engine-wide label sweep: labels carry no home, and an
	// empty-stack Down would tear down other GRIDCTL_HOME instances.
	// Known window (self-reset only): the self daemon's autoscaler is
	// still ticking while its stack is downed and could recreate a
	// replica between Down and process exit; the re-run path cleans it
	// up, and stopping the autoscaler pre-Down is a follow-up.
	progress("containers", nil)
	for _, s := range inv.stacks {
		row := Row{Kind: "containers", Name: s.StackName}
		if m.Runtime == nil {
			row.Action = ActionSkipped
			row.Detail = "container runtime unavailable; re-run reset once it is back"
			doc.Failed++
			ok(row)
			continue
		}
		if err := m.Runtime.Down(ctx, s.StackName); err != nil {
			fail(row, err)
			continue
		}
		row.Action = ActionRemoved
		ok(row)
	}

	if err := ctx.Err(); err != nil {
		return doc, err
	}
	// Phase 6: per-stack state files (self deferred).
	progress("state", nil)
	for _, s := range inv.stacks {
		if selfStack != nil && s.StackName == selfStack.StackName {
			continue
		}
		row := Row{Kind: "state-file", Name: s.StackName}
		if err := state.Delete(s.StackName); err != nil {
			fail(row, err)
			continue
		}
		row.Action = ActionRemoved
		ok(row)
	}

	if err := ctx.Err(); err != nil {
		return doc, err
	}
	// Phase 7: purge (self-deferred variant returns it in Finalize).
	finalize := func() error {
		if selfStack != nil {
			if err := state.Delete(selfStack.StackName); err != nil {
				return err
			}
		}
		if opts.Purge {
			// The resolver never returns a relative path (it errors
			// instead), so this RemoveAll cannot aim at the cwd.
			return os.RemoveAll(m.GridctlDir())
		}
		return nil
	}
	if opts.SelfPID != 0 {
		// Only hand back a Finalize when something was actually
		// deferred; a nil Finalize tells the caller its process is not
		// in the blast radius and nothing remains to do.
		if selfStack != nil || opts.Purge {
			doc.Finalize = finalize
		}
	} else if opts.Purge {
		progress("purge", nil)
		row := Row{Kind: "gridctl-dir", Name: m.GridctlDir(), Path: m.GridctlDir()}
		if err := finalize(); err != nil {
			fail(row, err)
		} else {
			row.Action = ActionRemoved
			ok(row)
		}
	}

	return doc, nil
}

func uniqueSkillNames(rows []skillsync.ProjectionStatus) []string {
	return uniqueNames(len(rows), func(i int) string { return rows[i].Skill })
}

func uniqueAgentNames(rows []agentsync.ProjectionStatus) []string {
	return uniqueNames(len(rows), func(i int) string { return rows[i].Agent })
}

func uniqueNames(n int, name func(int) string) []string {
	seen := map[string]bool{}
	var out []string
	for i := 0; i < n; i++ {
		if s := name(i); !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func removableNames(names []string, kept map[string]bool) []string {
	var out []string
	for _, n := range names {
		if !kept[n] {
			out = append(out, n)
		}
	}
	return out
}
