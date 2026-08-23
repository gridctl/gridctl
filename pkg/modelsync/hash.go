package modelsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gowebpki/jcs"

	"github.com/gridctl/gridctl/pkg/project"
)

// contentHash fingerprints text content with the engine's scheme,
// CRLF-normalized so editor line-ending churn never reads as drift.
func contentHash(content []byte) string {
	sum := sha256.Sum256([]byte(normalizeNewlines(string(content))))
	return project.HashScheme + hex.EncodeToString(sum[:])
}

// valueHash canonicalizes a JSON-shaped value per RFC 8785 and hashes
// it with the engine's scheme (the wiring pattern: serialization style
// never reads as drift, and only the hash is ever stored because
// values can carry secrets).
func valueHash(value map[string]any) (string, error) {
	data, err := json.Marshal(normalizeValue(value))
	if err != nil {
		return "", fmt.Errorf("encoding provider value: %w", err)
	}
	canonical, err := jcs.Transform(data)
	if err != nil {
		return "", fmt.Errorf("canonicalizing provider value: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return project.HashScheme + hex.EncodeToString(sum[:]), nil
}

// normalizeValue converts decoder-specific map shapes (yaml-style
// map[any]any keys) into JSON-encodable ones.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = normalizeValue(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[fmt.Sprintf("%v", k)] = normalizeValue(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalizeValue(item)
		}
		return out
	}
	return v
}

// maxHashHistory bounds the per-entry list of canonical hashes gridctl
// has written; history exists so a newer gridctl changing its own
// written shape never presents the old shape as user drift.
const maxHashHistory = 5

// appendHash dedupes, appends newest-last, and truncates to the newest
// maxHashHistory entries.
func appendHash(hashes []string, hash string) []string {
	kept := make([]string, 0, len(hashes)+1)
	for _, h := range hashes {
		if h != hash {
			kept = append(kept, h)
		}
	}
	kept = append(kept, hash)
	if len(kept) > maxHashHistory {
		kept = kept[len(kept)-maxHashHistory:]
	}
	return kept
}

// hashRecorded reports whether hash appears in the recorded history.
func hashRecorded(hashes []string, hash string) bool {
	for _, h := range hashes {
		if h == hash {
			return true
		}
	}
	return false
}

// normalizeNewlines converts CRLF to LF so hashing and line edits
// operate on one form.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// restoreCRLF converts back to CRLF only when the original content
// used it, so gridctl never imposes its own line-ending choice.
func restoreCRLF(original, edited string) string {
	if strings.Contains(original, "\r\n") {
		return strings.ReplaceAll(edited, "\n", "\r\n")
	}
	return edited
}

// backupPrefix marks modelsync's sibling backups.
const backupPrefix = ".gridctl-backup-"

// createBackup writes a timestamped sibling copy of path before a
// mutation, pruning old backups beyond the engine cap. A missing path
// is a no-op. A copy of the contexts helper: the provisioner's
// equivalents are unexported by design.
func createBackup(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("checking %s: %w", path, err)
	}
	if info.IsDir() {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s for backup: %w", path, err)
	}
	backup := path + backupPrefix + time.Now().Format("20060102-150405")
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return "", fmt.Errorf("writing backup %s: %w", backup, err)
	}
	pruneBackups(path)
	return backup, nil
}

// pruneBackups best-effort deletes backups beyond the engine cap.
func pruneBackups(path string) {
	dir := filepath.Dir(path)
	prefix := filepath.Base(path) + backupPrefix
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			backups = append(backups, e.Name())
		}
	}
	sort.Strings(backups)
	for _, stale := range project.StaleBackups(backups, project.MaxBackups) {
		_ = os.Remove(filepath.Join(dir, stale))
	}
}
