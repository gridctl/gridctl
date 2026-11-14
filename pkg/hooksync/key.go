package hooksync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/skills"
)

// The keyed merge-key channel: gridctl owns one top-level key per hook in
// a shared hooks file (Antigravity's ~/.gemini/config/hooks.json). The key
// name is the owned region and the canonical hash of its value is the
// ownership hash. Keys gridctl did not record are never touched, and every
// write is an RFC 6902 patch at the key's pointer on the hujson tree, so
// sibling keys and comments keep their bytes.

const keyRemovedDetail = "the recorded key was removed from the hooks file"

// errRootNotObject marks a parsed hooks file whose top-level value is not
// an object. It is reported the same way as a file that does not parse.
var errRootNotObject = errors.New("top-level value is not an object")

func keyForeignDetail(key, path string) string {
	return fmt.Sprintf("key %q in %s exists but gridctl has no record of writing it; pass --force to claim it after a backup", key, path)
}

func keyDriftDetail(key, path string) string {
	return fmt.Sprintf("key %q in %s was edited since gridctl wrote it", key, path)
}

// readKeyedFile reads a keyed target's shared file. A parsed file whose
// top-level value is not an object carries errRootNotObject as its parse
// error, so it is never patched.
func readKeyedFile(path string) (*settingsFile, error) {
	sf, err := readSettings(path)
	if err != nil {
		return nil, err
	}
	if sf.Parsed && !rootIsObject(sf) {
		sf.Parsed = false
		sf.ParseErr = errRootNotObject
	}
	return sf, nil
}

func (m *Manager) syncKeys(ctx context.Context, lf *LockFile, t Target, hooks []skills.InstalledHook, opts SyncOptions) ([]SyncResult, error) {
	path := t.PathFor(m.home, "")
	sf, err := readKeyedFile(path)
	if err != nil {
		return []SyncResult{{Client: t.Slug, Target: path, Action: ActionError, Error: err.Error()}}, nil
	}
	work := make([]hookWork, 0, len(hooks))
	for _, h := range hooks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		work = append(work, m.planKey(h, t, sf, lf, opts))
	}
	if !opts.DryRun {
		if err := m.applyKeys(sf, work); err != nil {
			return nil, err
		}
	}
	results := make([]SyncResult, 0, len(work))
	for _, w := range work {
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

func (m *Manager) planKey(h skills.InstalledHook, t Target, sf *settingsFile, lf *LockFile, opts SyncOptions) hookWork {
	path := sf.Path
	key := t.keyFor(h.Name)
	scriptDir := scriptDirFor(m.home, h.Name, t.Slug)
	w := hookWork{
		hook:      h,
		scriptDir: scriptDir,
		key:       key,
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
	plannedHash, err := valueHash(r.Value)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	w.keyValue = r.Value
	w.plannedHash = plannedHash
	w.renderNote = r.detail()
	plannedTree, err := plannedTreeHash(h.Dir)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	w.plannedTree = plannedTree
	if sf.ParseErr != nil {
		w.res.Action = ActionSkippedUnmanaged
		w.res.Detail = SettingsUnparsableDetail + sf.ParseErr.Error()
		return w
	}
	current, exists, err := valueAt(sf, keyPointer(key))
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	w.fileExists = exists
	if exists {
		if w.fileCurrent, err = valueHash(current); err != nil {
			w.res.Action = ActionError
			w.res.Error = err.Error()
			return w
		}
	}
	currentTree, err := treeHash(scriptDir)
	if err != nil {
		w.res.Action = ActionError
		w.res.Error = err.Error()
		return w
	}
	rec := lf.entry(h.Name, t.Slug)
	w.createdFile = sf.Missing
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
			w.res.Detail = keyForeignDetail(key, path)
			return w
		}
		w.write = true
		w.claimed = true
		w.scriptsOnly = w.fileCurrent == w.plannedHash
		w.res.Action = ActionUpdated
	case !exists:
		w.write = true
		w.res.Action = ActionUpdated
		w.res.Detail = keyRemovedDetail
	case !hashRecorded(rec.Hashes, w.fileCurrent):
		if !opts.Force {
			w.res.Action = ActionSkippedDrift
			w.res.Detail = keyDriftDetail(key, path)
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
		w.scriptsOnly = w.fileCurrent == w.plannedHash
		w.res.Action = ActionUpdated
		if scriptsGone {
			w.res.Detail = scriptRemovedDetail
		}
	}
	w.res.Detail = joinDetail(w.res.Detail, w.renderNote)
	return w
}

// applyKeys writes each planned hook's script copy, then patches every
// changed key into the shared file in one write. An existing file is
// backed up beside itself first. A missing file is created holding only
// the planned keys.
func (m *Manager) applyKeys(sf *settingsFile, work []hookWork) error {
	var patching []*hookWork
	for i := range work {
		w := &work[i]
		if !w.write {
			continue
		}
		if err := m.writeScripts(*w); err != nil {
			return err
		}
		if !w.scriptsOnly {
			patching = append(patching, w)
		}
	}
	if len(patching) == 0 {
		return nil
	}
	if sf.Missing || bareEmptyObject(sf.Raw) {
		// A missing file, or one that is only "{}" with no comment, is
		// written whole so the first key is not glued to the brace.
		doc := map[string]any{}
		for _, w := range patching {
			doc[w.key] = w.keyValue
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding %s: %w", sf.Path, err)
		}
		backup := ""
		if !sf.Missing {
			if backup, err = createBackup(sf.Path); err != nil {
				return err
			}
		}
		if err := writeSettings(sf.Path, append(data, '\n')); err != nil {
			return err
		}
		for _, w := range patching {
			w.res.BackupPath = backup
		}
		return nil
	}
	indent := sf.indentOrDefault()
	ops := make([]patchOp, 0, len(patching))
	for _, w := range patching {
		value, err := prettyPrefixed(w.keyValue, indent, indent)
		if err != nil {
			return fmt.Errorf("encoding key %s: %w", w.key, err)
		}
		op := "add"
		if w.fileExists {
			op = "replace"
		}
		ops = append(ops, patchOp{Op: op, Path: keyPointer(w.key), Value: value})
	}
	backup, err := createBackup(sf.Path)
	if err != nil {
		return err
	}
	packed, err := applyPatch(sf.Raw, ops)
	if err != nil {
		return err
	}
	packed = separateInsertedKeys(sf.Raw, packed, func(string) string { return indent })
	if err := writeSettings(sf.Path, packed); err != nil {
		return err
	}
	for _, w := range patching {
		w.res.BackupPath = backup
	}
	return nil
}

// unsyncKeyed removes each recorded key whose hash gridctl wrote, or any
// present key with Force, in one patch after a backup. Sibling keys stay,
// and a file left with no keys is an empty object. The script copy the
// record names is backed up and removed, and the record is purged on
// removed and already-gone.
func (m *Manager) unsyncKeyed(lf *LockFile, t Target, keys []recordKey, opts UnsyncOptions) ([]UnsyncResult, error) {
	path := t.PathFor(m.home, "")
	sf, err := readKeyedFile(path)
	if err != nil {
		return nil, err
	}
	type item struct {
		key    recordKey
		entry  *Entry
		owned  string
		remove bool
		purge  bool
		res    UnsyncResult
	}
	items := make([]item, 0, len(keys))
	for _, k := range keys {
		it := item{key: k, entry: lf.entry(k.name, k.client), owned: t.keyFor(k.name)}
		it.res = UnsyncResult{Hook: k.name, Client: k.client, Target: path}
		switch {
		case sf.Missing:
			it.purge = true
			it.res.Action = ActionAlreadyGone
		case sf.ParseErr != nil:
			it.res.Action = ActionSkippedUnmanaged
			it.res.Detail = SettingsUnparsableDetail + sf.ParseErr.Error()
		default:
			current, exists, verr := valueAt(sf, keyPointer(it.owned))
			if verr != nil {
				it.res.Action = ActionError
				it.res.Error = verr.Error()
				break
			}
			if !exists {
				it.purge = true
				it.res.Action = ActionAlreadyGone
				break
			}
			hash, herr := valueHash(current)
			if herr != nil {
				it.res.Action = ActionError
				it.res.Error = herr.Error()
				break
			}
			if !opts.Force && !hashRecorded(it.entry.Hashes, hash) {
				it.res.Action = ActionSkippedDrift
				it.res.Detail = keyDriftDetail(it.owned, path)
				break
			}
			it.remove = true
			it.purge = true
			it.res.Action = ActionRemoved
		}
		items = append(items, it)
	}
	if opts.DryRun {
		out := make([]UnsyncResult, 0, len(items))
		for _, it := range items {
			if it.res.Action == ActionRemoved {
				it.res.Action = ActionWouldRemove
			}
			out = append(out, it.res)
		}
		return out, nil
	}
	var ops []patchOp
	for _, it := range items {
		if it.remove {
			ops = append(ops, patchOp{Op: "remove", Path: keyPointer(it.owned)})
		}
	}
	if len(ops) > 0 {
		backup, err := createBackup(sf.Path)
		if err != nil {
			return nil, err
		}
		packed, err := applyPatch(sf.Raw, ops)
		if err != nil {
			return nil, err
		}
		if err := writeSettings(sf.Path, compactEmptyObject(packed)); err != nil {
			return nil, err
		}
		for i := range items {
			if items[i].remove {
				items[i].res.BackupPath = backup
			}
		}
	}
	out := make([]UnsyncResult, 0, len(items))
	for _, it := range items {
		if it.purge {
			if _, err := backupTree(m.home, it.entry.ScriptDir); err != nil {
				return nil, err
			}
			if err := os.RemoveAll(it.entry.ScriptDir); err != nil {
				return nil, err
			}
			lf.remove(it.key.name, it.key.client)
		}
		out = append(out, it.res)
	}
	return out, nil
}

// compactEmptyObject turns a file that is only an empty object with
// whitespace inside the braces into "{}", keeping the trailing newline.
// A file with a comment anywhere is returned unchanged.
func compactEmptyObject(data []byte) []byte {
	if !bareEmptyObject(data) {
		return data
	}
	out := []byte("{}")
	if bytes.HasSuffix(data, []byte("\n")) {
		out = append(out, '\n')
	}
	return out
}

// bareEmptyObject reports whether data is an empty object with nothing
// but whitespace around and inside the braces.
func bareEmptyObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return false
	}
	return len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) == 0
}

// keyStatus reports one keyed projection. An in-sync row carries the
// planned render's dropped-group and dropped-key details, recomputed on
// every call.
func (m *Manager) keyStatus(name string, t Target, entry *Entry) ProjectionStatus {
	path := entryPath(m.home, t, name, entry)
	key := t.keyFor(name)
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
	sf, err := readKeyedFile(path)
	if err != nil {
		ps.State = StateTargetMissing
		ps.Detail = err.Error()
		return ps
	}
	if sf.Missing {
		ps.State = StateTargetMissing
		ps.Detail = "hooks file is gone; script directory " + entry.ScriptDir
		return ps
	}
	if sf.ParseErr != nil {
		ps.State = StateDrifted
		ps.Detail = SettingsUnparsableDetail + sf.ParseErr.Error()
		return ps
	}
	if _, err := os.Stat(entry.ScriptDir); err != nil {
		ps.State = StateTargetMissing
		ps.Detail = "script directory is gone (" + entry.ScriptDir + ")"
		return ps
	}
	current, exists, err := valueAt(sf, keyPointer(key))
	if err != nil {
		ps.State = StateDrifted
		ps.Detail = err.Error()
		return ps
	}
	if !exists {
		ps.State = StateTargetMissing
		ps.Detail = keyRemovedDetail + "; script directory " + entry.ScriptDir
		return ps
	}
	currentTree, err := treeHash(entry.ScriptDir)
	if err != nil {
		ps.State = StateTargetMissing
		ps.Detail = err.Error()
		return ps
	}
	currentHash, _ := valueHash(current)
	scriptGone := currentTree == ""
	scriptDrift := !scriptGone && currentTree != entry.TreeHash
	keyDrift := !hashRecorded(entry.Hashes, currentHash)
	if scriptGone && !keyDrift {
		ps.State = StateTargetMissing
		ps.Detail = "script copy is empty (" + entry.ScriptDir + ")"
		return ps
	}
	if keyDrift || scriptDrift {
		ps.State = StateDrifted
		switch {
		case keyDrift && scriptDrift:
			ps.Detail = fmt.Sprintf("key %q in %s and the script copy were edited since gridctl wrote them; script directory %s", key, path, entry.ScriptDir)
		case scriptDrift:
			ps.Detail = "script copy was edited since gridctl wrote it; script directory " + entry.ScriptDir
		default:
			ps.Detail = keyDriftDetail(key, path) + "; script directory " + entry.ScriptDir
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
		default:
			if planned, herr := valueHash(r.Value); herr != nil {
				stale = append(stale, herr.Error())
			} else if planned != entry.latestHash() {
				stale = append(stale, "planned render differs from the synced key")
			} else {
				note = r.detail()
			}
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
