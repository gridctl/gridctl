package wiring

import (
	"sort"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// ChannelMergeKey is the only wiring channel: gridctl owns one key
// inside a shared client config file, never the file itself.
const ChannelMergeKey = "merge-key"

// LockFile is the wiring-kind view over the unified project lockfile,
// keyed client slug → entry name. The engine owns the on-disk schema,
// versioning, and locking; this view keeps the ops code in the same
// shape as the other kind packages.
type LockFile struct {
	// Records maps client slug → entry name → record.
	Records map[string]map[string]*Entry
}

// Entry is one recorded (client, entry name) ownership record.
type Entry struct {
	// ConfigPath is the client config file holding the owned key.
	ConfigPath string
	// Group and ClientID reproduce the link's endpoint composition so
	// status can rebuild the planned value against the current port.
	Group    string
	ClientID string
	// Hashes is the canonical value-hash history, newest last. The
	// current value matching any of them means gridctl wrote it.
	Hashes []string
	// CreatedByGridctl is false for adopted entries: gridctl owns them
	// now but did not author their current value.
	CreatedByGridctl bool
	// Pack tags the record with the pack that applied it (empty = not
	// pack-managed).
	Pack     string
	SyncedAt time.Time
}

// latestHash returns the newest recorded hash, or "".
func (e *Entry) latestHash() string {
	if len(e.Hashes) == 0 {
		return ""
	}
	return e.Hashes[len(e.Hashes)-1]
}

// compositePath builds the engine-side ownership path: one config file
// legitimately holds several owned entries, and the engine's one-owner
// invariant keys on (client, Path), so the entry name is folded in.
// Consumers never parse this; the real path lives in Entry.ConfigPath.
func compositePath(configPath, name string) string {
	return configPath + "#" + name
}

// newLockFile returns an empty view.
func newLockFile() *LockFile {
	return &LockFile{Records: map[string]map[string]*Entry{}}
}

// entry returns the record for (client, name), or nil.
func (lf *LockFile) entry(client, name string) *Entry {
	return lf.Records[client][name]
}

// set records an entry for (client, name).
func (lf *LockFile) set(client, name string, e *Entry) {
	if lf.Records[client] == nil {
		lf.Records[client] = map[string]*Entry{}
	}
	lf.Records[client][name] = e
}

// remove deletes the record for (client, name), dropping the client key
// when its last entry goes.
func (lf *LockFile) remove(client, name string) {
	delete(lf.Records[client], name)
	if len(lf.Records[client]) == 0 {
		delete(lf.Records, client)
	}
}

// viewFromLock projects the engine lock's wiring entries into the view
// the ops code works on.
func viewFromLock(pl *project.Lock) *LockFile {
	lf := newLockFile()
	for _, e := range pl.Entries(project.KindWiring) {
		lf.set(e.Client, e.Source, &Entry{
			ConfigPath:       e.ConfigPath,
			Group:            e.Group,
			ClientID:         e.ClientID,
			Hashes:           append([]string(nil), e.Hashes...),
			CreatedByGridctl: e.CreatedByGridctl,
			Pack:             e.Pack,
			SyncedAt:         e.SyncedAt,
		})
	}
	return lf
}

// saveView flushes the view back into the engine lock and persists it.
// Records dropped from the view are removed as explicit engine-driven
// deletes; entries of other kinds and unknown fields ride along.
func saveView(pl *project.Lock, lf *LockFile) error {
	var entries []*project.Entry
	for client, names := range lf.Records {
		for name, e := range names {
			entries = append(entries, &project.Entry{
				Kind:             project.KindWiring,
				Client:           client,
				Source:           name,
				Path:             compositePath(e.ConfigPath, name),
				Channel:          ChannelMergeKey,
				ConfigPath:       e.ConfigPath,
				Group:            e.Group,
				ClientID:         e.ClientID,
				Hashes:           e.Hashes,
				InstalledHash:    e.latestHash(),
				CreatedByGridctl: e.CreatedByGridctl,
				Pack:             e.Pack,
				SyncedAt:         e.SyncedAt,
			})
		}
	}
	if err := pl.ReplaceKind(project.KindWiring, entries); err != nil {
		return err
	}
	return pl.Save()
}

// recordKey identifies one (client, name) record.
type recordKey struct{ client, name string }

// sortedRecordKeys returns the view's records in deterministic
// client-then-name order.
func sortedRecordKeys(lf *LockFile) []recordKey {
	var keys []recordKey
	for client, names := range lf.Records {
		for name := range names {
			keys = append(keys, recordKey{client: client, name: name})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].client != keys[j].client {
			return keys[i].client < keys[j].client
		}
		return keys[i].name < keys[j].name
	})
	return keys
}
