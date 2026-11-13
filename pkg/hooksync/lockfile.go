package hooksync

import (
	"context"
	"sort"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// LockFile is the hook-kind view over the unified project lockfile,
// keyed hook name → client slug. The engine owns the on-disk schema.
type LockFile struct {
	Records map[string]map[string]*Entry
}

// Entry is one recorded (hook, client) ownership record.
type Entry struct {
	// ConfigPath is the client file. For a merge-key target, Path on the
	// engine entry is the composite "<ConfigPath>#hooks/<name>" so several
	// hooks share one file; for a dedicated-file target Path is ConfigPath.
	ConfigPath string
	// Channel is display only. saveView writes it from the target table,
	// and every behavior branch reads the channel from FindTarget, so an
	// entry rewritten by an older gridctl behaves the same after upgrade.
	Channel string
	// ScriptDir is the absolute script copy.
	ScriptDir string
	// Hashes is the canonical hash history of the owned groups, newest last.
	Hashes []string
	// TreeHash is the hash of the script copy at sync time.
	TreeHash string
	// CreatedFile is true when gridctl created the settings file.
	CreatedFile bool
	Pack        string
	SyncedAt    time.Time
}

func (e *Entry) latestHash() string {
	if e == nil || len(e.Hashes) == 0 {
		return ""
	}
	return e.Hashes[len(e.Hashes)-1]
}

func compositePath(configPath, name string) string {
	return configPath + "#hooks/" + name
}

func newLockFile() *LockFile {
	return &LockFile{Records: map[string]map[string]*Entry{}}
}

func (lf *LockFile) entry(name, client string) *Entry {
	if lf == nil {
		return nil
	}
	return lf.Records[name][client]
}

func (lf *LockFile) set(name, client string, e *Entry) {
	if lf.Records[name] == nil {
		lf.Records[name] = map[string]*Entry{}
	}
	lf.Records[name][client] = e
}

func (lf *LockFile) remove(name, client string) {
	delete(lf.Records[name], client)
	if len(lf.Records[name]) == 0 {
		delete(lf.Records, name)
	}
}

func viewFromLock(pl *project.Lock) *LockFile {
	lf := newLockFile()
	for _, e := range pl.Entries(project.KindHook) {
		lf.set(e.Source, e.Client, &Entry{
			ConfigPath:  e.ConfigPath,
			Channel:     e.Channel,
			ScriptDir:   e.ScriptDir,
			Hashes:      append([]string(nil), e.Hashes...),
			TreeHash:    e.TreeHash,
			CreatedFile: e.CreatedFile,
			Pack:        e.Pack,
			SyncedAt:    e.SyncedAt,
		})
	}
	return lf
}

func saveView(pl *project.Lock, lf *LockFile) error {
	var entries []*project.Entry
	for name, clients := range lf.Records {
		for client, e := range clients {
			channel := e.Channel
			if t, ok := FindTarget(client); ok {
				channel = t.Channel
			}
			if channel == "" {
				channel = ChannelMergeKey
			}
			path := compositePath(e.ConfigPath, name)
			if channel == ChannelDedicatedFile {
				path = e.ConfigPath
			}
			entries = append(entries, &project.Entry{
				Kind:             project.KindHook,
				Client:           client,
				Source:           name,
				Path:             path,
				Channel:          channel,
				ConfigPath:       e.ConfigPath,
				ScriptDir:        e.ScriptDir,
				Hashes:           e.Hashes,
				TreeHash:         e.TreeHash,
				CreatedFile:      e.CreatedFile,
				CreatedByGridctl: e.CreatedFile,
				Pack:             e.Pack,
				SyncedAt:         e.SyncedAt,
			})
		}
	}
	if err := pl.ReplaceKind(project.KindHook, entries); err != nil {
		return err
	}
	return pl.Save()
}

func (m *Manager) loadView(ctx context.Context) (*LockFile, error) {
	l, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return viewFromLock(l), nil
}

type recordKey struct{ name, client string }

func sortedRecordKeys(lf *LockFile) []recordKey {
	var keys []recordKey
	for name, clients := range lf.Records {
		for client := range clients {
			keys = append(keys, recordKey{name: name, client: client})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		return keys[i].client < keys[j].client
	})
	return keys
}
