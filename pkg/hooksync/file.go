package hooksync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

// The dedicated-file channel: gridctl owns one generated file per
// (hook, client). Ownership is the raw byte hash of the file, recorded in
// the entry's hash history. The decision table mirrors pkg/agentsync.

const (
	fileRemovedDetail = "the projected file was removed"
	fileBackupTime    = "20060102-150405.000000000"
)

func fileForeignDetail(path string) string {
	return path + " exists but gridctl has no record of writing it; pass --force to claim it after a backup"
}

func fileDriftDetail(path string) string {
	return path + " was edited since gridctl wrote it"
}

// entryPath is the recorded file for a dedicated-file entry. ConfigPath
// survives a rewrite by an older gridctl, which only changes Path.
func entryPath(home string, t Target, name string, entry *Entry) string {
	if entry != nil && entry.ConfigPath != "" {
		return entry.ConfigPath
	}
	return t.PathFor(home, name)
}

func joinDetail(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "; ")
}

func (m *Manager) syncFiles(ctx context.Context, lf *LockFile, t Target, hooks []skills.InstalledHook, opts SyncOptions) ([]SyncResult, error) {
	var results []SyncResult
	for _, h := range hooks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		w := m.planFile(h, t, lf, opts)
		if w.write && !opts.DryRun {
			if err := m.applyFile(t, &w); err != nil {
				w.write = false
				w.res.Action = ActionError
				w.res.Error = err.Error()
			}
		}
		if w.write && opts.DryRun {
			if w.res.Action == ActionCopied {
				w.res.Action = ActionWouldCopy
			} else {
				w.res.Action = ActionWouldUpdate
			}
		}
		if !opts.DryRun && (w.write || w.res.Action == ActionUnchanged) {
			m.record(lf, w, opts.Pack)
		}
		results = append(results, w.res)
	}
	return results, nil
}

func (m *Manager) planFile(h skills.InstalledHook, t Target, lf *LockFile, opts SyncOptions) hookWork {
	path := t.PathFor(m.home, h.Name)
	scriptDir := scriptDirFor(m.home, h.Name, t.Slug)
	w := hookWork{
		hook:      h,
		scriptDir: scriptDir,
		res: SyncResult{
			Hook: h.Name, Client: t.Slug, Channel: t.Channel,
			Target: path, ScriptDir: scriptDir,
		},
	}
	if !m.admitHook(&w) {
		return w
	}
	r, err := renderFor(t, h.Definition.Raw, h.Name, scriptDir)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	if !r.expressible() {
		w.res.Action = ActionSkippedUnexpressible
		w.res.Detail = strings.Join(r.Unexpressible, "; ")
		return w
	}
	w.fileBytes = r.Bytes
	w.plannedHash = fileHash(r.Bytes)
	w.renderNote = r.detail()
	plannedTree, err := plannedTreeHash(h.Dir)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	w.plannedTree = plannedTree
	data, exists, err := readFileIfExists(path)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	w.fileExists = exists
	if exists {
		w.fileCurrent = fileHash(data)
	}
	currentTree, err := treeHash(scriptDir)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	rec := lf.entry(h.Name, t.Slug)
	w.createdFile = !exists
	if rec != nil {
		w.createdFile = rec.CreatedFile
	}

	switch {
	case rec == nil && !exists:
		w.write = true
		w.res.Action = ActionCopied
	case rec == nil:
		if !opts.Force {
			w.res.Action = ActionSkippedForeign
			w.res.Detail = fileForeignDetail(path)
			return w
		}
		w.write = true
		w.res.Action = ActionUpdated
	case !exists:
		w.write = true
		w.res.Action = ActionUpdated
		w.res.Detail = fileRemovedDetail
	case !hashRecorded(rec.Hashes, w.fileCurrent):
		if !opts.Force {
			w.res.Action = ActionSkippedDrift
			w.res.Detail = fileDriftDetail(path)
			return w
		}
		w.write = true
		w.res.Action = ActionUpdated
	default:
		scriptsGone := currentTree == ""
		scriptsDrifted := !scriptsGone && currentTree != rec.TreeHash && currentTree != plannedTree
		if scriptsDrifted && !opts.Force {
			w.res.Action = ActionSkippedDrift
			w.res.Detail = "script copy was edited since gridctl wrote it"
			return w
		}
		if w.fileCurrent == w.plannedHash && currentTree == plannedTree {
			w.res.Action = ActionUnchanged
			w.res.Detail = w.renderNote
			return w
		}
		w.write = true
		w.res.Action = ActionUpdated
		if scriptsGone {
			w.res.Detail = scriptRemovedDetail
		}
	}
	w.res.Detail = joinDetail(w.res.Detail, w.renderNote)
	return w
}

// applyFile writes the script copy, then the file when its bytes differ.
// An existing file is backed up before it is replaced.
func (m *Manager) applyFile(t Target, w *hookWork) error {
	if err := m.writeScripts(*w); err != nil {
		return err
	}
	if w.fileExists && w.fileCurrent == w.plannedHash {
		return nil
	}
	path := w.res.Target
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if w.fileExists {
		backup, err := m.backupFile(t.Slug, w.hook.Name, path)
		if err != nil {
			return err
		}
		w.res.BackupPath = backup
	}
	if err := project.AtomicWriteFile(path, w.fileBytes); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// backupFile copies a projected file out of the client tree to
// ~/.gridctl/project-backups/hook-file/<client>/<name>/<ts>-<basename>
// and keeps project.MaxBackups.
func (m *Manager) backupFile(client, name, path string) (string, error) {
	data, exists, err := readFileIfExists(path)
	if err != nil || !exists {
		return "", err
	}
	dir := filepath.Join(m.home, ".gridctl", "project-backups", "hook-file", client, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating backup directory: %w", err)
	}
	backup := filepath.Join(dir, time.Now().UTC().Format(fileBackupTime)+"-"+filepath.Base(path))
	if err := project.AtomicWriteFile(backup, data); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err == nil {
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
	return backup, nil
}

func readFileIfExists(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- paths come from the target table and the lockfile
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, true, nil
}

// unsyncFiles removes each recorded file whose hash gridctl wrote, or any
// file with Force, after a backup. The script copy the record names is
// backed up and removed with it, and the record is purged on removed and
// already-gone.
func (m *Manager) unsyncFiles(lf *LockFile, t Target, keys []recordKey, opts UnsyncOptions) ([]UnsyncResult, error) {
	var out []UnsyncResult
	for _, key := range keys {
		entry := lf.entry(key.name, key.client)
		path := entryPath(m.home, t, key.name, entry)
		res := UnsyncResult{Hook: key.name, Client: key.client, Target: path}
		data, exists, err := readFileIfExists(path)
		if err != nil {
			res.Action = ActionError
			res.Error = err.Error()
			out = append(out, res)
			continue
		}
		switch {
		case !exists:
			res.Action = ActionAlreadyGone
		case opts.Force || hashRecorded(entry.Hashes, fileHash(data)):
			res.Action = ActionRemoved
		default:
			res.Action = ActionSkippedDrift
			res.Detail = fileDriftDetail(path)
			out = append(out, res)
			continue
		}
		if opts.DryRun {
			if res.Action == ActionRemoved {
				res.Action = ActionWouldRemove
			}
			out = append(out, res)
			continue
		}
		if exists {
			backup, err := m.backupFile(key.client, key.name, path)
			if err != nil {
				return nil, err
			}
			res.BackupPath = backup
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("removing %s: %w", path, err)
			}
		}
		if _, err := backupTree(m.home, entry.ScriptDir); err != nil {
			return nil, err
		}
		if err := os.RemoveAll(entry.ScriptDir); err != nil {
			return nil, err
		}
		lf.remove(key.name, key.client)
		out = append(out, res)
	}
	return out, nil
}

// fileStatus reports one dedicated-file projection. An in-sync row
// carries the planned render's dropped-group and dropped-key details,
// recomputed on every call.
func (m *Manager) fileStatus(name string, t Target, entry *Entry) ProjectionStatus {
	path := entryPath(m.home, t, name, entry)
	ps := ProjectionStatus{
		Hook:      name,
		Client:    t.Slug,
		Channel:   t.Channel,
		Render:    t.RenderClass,
		Target:    path,
		ScriptDir: entry.ScriptDir,
		Pack:      entry.Pack,
		State:     StateInSync,
	}
	synced := entry.SyncedAt
	ps.SyncedAt = &synced
	if !t.available(m.home) {
		ps.State = StateTargetMissing
		ps.Detail = "client not initialized on this machine; script directory " + entry.ScriptDir
		return ps
	}
	data, exists, err := readFileIfExists(path)
	if err != nil {
		ps.State = StateTargetMissing
		ps.Detail = err.Error()
		return ps
	}
	if !exists {
		ps.State = StateTargetMissing
		ps.Detail = "projected file is gone; script directory " + entry.ScriptDir
		return ps
	}
	if _, err := os.Stat(entry.ScriptDir); err != nil {
		ps.State = StateTargetMissing
		ps.Detail = "script directory is gone (" + entry.ScriptDir + ")"
		return ps
	}
	currentTree, err := treeHash(entry.ScriptDir)
	if err != nil {
		ps.State = StateTargetMissing
		ps.Detail = err.Error()
		return ps
	}
	scriptGone := currentTree == ""
	scriptDrift := !scriptGone && currentTree != entry.TreeHash
	fileDrift := !hashRecorded(entry.Hashes, fileHash(data))
	if scriptGone && !fileDrift {
		ps.State = StateTargetMissing
		ps.Detail = "script copy is empty (" + entry.ScriptDir + ")"
		return ps
	}
	if fileDrift || scriptDrift {
		ps.State = StateDrifted
		switch {
		case fileDrift && scriptDrift:
			ps.Detail = path + " and the script copy were edited since gridctl wrote them; script directory " + entry.ScriptDir
		case scriptDrift:
			ps.Detail = "script copy was edited since gridctl wrote it; script directory " + entry.ScriptDir
		default:
			ps.Detail = fileDriftDetail(path) + "; script directory " + entry.ScriptDir
		}
		return ps
	}
	hook, stale := m.registryStale(name)
	note := ""
	if hook != nil && hook.Definition != nil {
		r, rerr := renderFor(t, hook.Definition.Raw, name, entry.ScriptDir)
		switch {
		case rerr != nil:
			stale = append(stale, rerr.Error())
		case !r.expressible():
			stale = append(stale, "planned render has no expressible group: "+strings.Join(r.Unexpressible, "; "))
		case fileHash(r.Bytes) != entry.latestHash():
			stale = append(stale, "planned render differs from the synced file")
		default:
			note = r.detail()
		}
		if plannedTree, terr := plannedTreeHash(hook.Dir); terr == nil && plannedTree != entry.TreeHash {
			stale = append(stale, "registry scripts differ from the synced copy")
		}
	}
	if len(stale) > 0 {
		ps.State = StateStale
		ps.Detail = strings.Join(stale, "; ")
		return ps
	}
	ps.Detail = note
	return ps
}

// registryStale reports the registry and pin reasons a projection is
// stale. The hook is nil when it left the registry.
func (m *Manager) registryStale(name string) (*skills.InstalledHook, []string) {
	var stale []string
	hook, err := skills.GetHook(m.registryDir, name)
	if err != nil {
		return nil, append(stale, "hook is no longer in the registry")
	}
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
	return hook, stale
}
