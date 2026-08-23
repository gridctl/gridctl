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
// container as needed. Returns the backup path (empty for a new file).
func upsertProviderValue(path, container, id string, value map[string]any) (string, error) {
	raw, err := os.ReadFile(path)
	created := false
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		raw = []byte("{}\n")
		created = true
	}
	if len(raw) == 0 {
		raw = []byte("{}\n")
	}
	v, err := hujson.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}

	var ops []patchOp
	ptr := "/" + container + "/" + id
	if v.Find("/"+container) == nil {
		ops = append(ops, patchOp{Op: "add", Path: "/" + container, Value: map[string]any{}})
	}
	op := "add"
	if v.Find(ptr) != nil {
		op = "replace"
	}
	ops = append(ops, patchOp{Op: op, Path: ptr, Value: value})

	patch, err := json.Marshal(ops)
	if err != nil {
		return "", fmt.Errorf("encoding patch: %w", err)
	}
	if err := v.Patch(patch); err != nil {
		return "", fmt.Errorf("patching %s: %w", path, err)
	}

	var backup string
	if !created {
		if backup, err = createBackup(path); err != nil {
			return "", err
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := project.AtomicWriteFile(path, v.Pack()); err != nil {
		return "", err
	}
	return backup, nil
}

// removeProviderValue removes the owned subtree. A missing file or key
// reports existed=false and writes nothing; an emptied container object
// is left in place (harmless, and it may predate gridctl).
func removeProviderValue(path, container, id string) (backup string, existed bool, err error) {
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
	if backup, err = createBackup(path); err != nil {
		return "", true, err
	}
	if err := project.AtomicWriteFile(path, v.Pack()); err != nil {
		return backup, true, err
	}
	return backup, true, nil
}
