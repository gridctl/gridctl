package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// MaxBackups is the shared keep-newest retention for projection
// backups, whatever their per-kind placement policy.
const MaxBackups = 3

// AtomicWriteFile writes data via a uniquely named temp file + rename
// in the target dir. Unique names keep concurrent writers from
// clobbering each other's in-flight temp file. Absorbed from the
// byte-identical copies in pkg/contexts and pkg/skillsync.
func AtomicWriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// StaleBackups returns the entries of a timestamped backup set beyond
// the newest keep, oldest first, for the caller to delete. Lexicographic
// order is chronological because backup names lead with a zero-padded
// "20060102-150405" timestamp. Placement and removal policy stay with
// the kind: contexts removes sibling files, skillsync removes
// out-of-tree directories, and both treat deletion as best-effort.
func StaleBackups(backups []string, keep int) []string {
	if len(backups) <= keep {
		return nil
	}
	sort.Strings(backups)
	return backups[:len(backups)-keep]
}
