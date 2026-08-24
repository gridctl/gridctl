package modelsync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"

	"github.com/gridctl/gridctl/pkg/project"
)

// The opencode.json mutation layer. Writes are RFC 6902 patches applied
// through hujson so every byte outside the owned provider subtree
// survives, comments and formatting included. Only the owned pointer is
// ever added, replaced, or removed; the top-level model key is never
// touched.

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// readProviderValue returns the current owned subtree value, or
// (nil, false) when the file, container, or key is absent. A missing
// file is the normal never-linked state, not an error.
func readProviderValue(path, container, id string) (map[string]any, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading %s: %w", path, err)
	}
	data, err := parseJSONC(raw)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	cont, ok := data[container].(map[string]any)
	if !ok {
		return nil, false, nil
	}
	value, ok := cont[id].(map[string]any)
	if !ok {
		return nil, false, nil
	}
	return value, true, nil
}

// readTopLevelString returns a top-level string key from the config
// (e.g. model), or "" when absent or unreadable. Read-only advisory.
func readTopLevelString(path, key string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	data, err := parseJSONC(raw)
	if err != nil {
		return ""
	}
	s, _ := data[key].(string)
	return s
}

// upsertProviderValue writes the owned subtree, creating the file and
// container as needed. Returns the backup path (empty for a new file)
// and whether gridctl created the container object, so unsync knows it
// may remove an emptied container it introduced.
func upsertProviderValue(path, container, id string, value map[string]any) (backup string, containerCreated bool, err error) {
	raw, err := os.ReadFile(path)
	created := false
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("reading %s: %w", path, err)
		}
		raw = []byte("{}\n")
		created = true
	}
	if len(raw) == 0 {
		raw = []byte("{}\n")
	}
	v, err := hujson.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parsing %s: %w", path, err)
	}

	var ops []patchOp
	ptr := "/" + container + "/" + id
	if v.Find("/"+container) == nil {
		containerCreated = true
		ops = append(ops, patchOp{Op: "add", Path: "/" + container, Value: map[string]any{}})
	}
	op := "add"
	if v.Find(ptr) != nil {
		op = "replace"
	}
	ops = append(ops, patchOp{Op: op, Path: ptr, Value: value})

	patch, err := json.Marshal(ops)
	if err != nil {
		return "", false, fmt.Errorf("encoding patch: %w", err)
	}
	if err := v.Patch(patch); err != nil {
		return "", false, fmt.Errorf("patching %s: %w", path, err)
	}

	if !created {
		if backup, err = createBackup(path); err != nil {
			return "", false, err
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", false, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := project.AtomicWriteFile(path, v.Pack()); err != nil {
		return "", false, err
	}
	return backup, containerCreated, nil
}

// removeProviderValue removes the owned subtree. A missing file or key
// reports existed=false and writes nothing. When gridctl created the
// container object (removeContainer) and removing the value empties
// it, the container goes too, restoring the pre-sync shape; a
// container that predates gridctl is always left in place.
func removeProviderValue(path, container, id string, removeContainer bool) (backup string, existed bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("reading %s: %w", path, err)
	}
	v, err := hujson.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parsing %s: %w", path, err)
	}
	ptr := "/" + container + "/" + id
	if v.Find(ptr) == nil {
		return "", false, nil
	}
	patch, err := json.Marshal([]patchOp{{Op: "remove", Path: ptr}})
	if err != nil {
		return "", false, fmt.Errorf("encoding patch: %w", err)
	}
	if err := v.Patch(patch); err != nil {
		return "", false, fmt.Errorf("patching %s: %w", path, err)
	}
	if removeContainer {
		if cont := v.Find("/" + container); cont != nil {
			if obj, ok := cont.Value.(*hujson.Object); ok && len(obj.Members) == 0 {
				drop, derr := json.Marshal([]patchOp{{Op: "remove", Path: "/" + container}})
				if derr == nil {
					if perr := v.Patch(drop); perr != nil {
						return "", true, fmt.Errorf("patching %s: %w", path, perr)
					}
				}
			}
		}
	}
	if backup, err = createBackup(path); err != nil {
		return "", true, err
	}
	if err := project.AtomicWriteFile(path, v.Pack()); err != nil {
		return backup, true, err
	}
	return backup, true, nil
}
