package provisioner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	backupSuffix    = ".gridctl-backup-"
	backupTimeFormat = "20060102-150405"
	maxBackups      = 3
)

// createBackup copies the original file to a timestamped backup.
// Returns the backup path, or empty string if the source file doesn't exist.
func createBackup(path string) (string, error) {
	if !fileExists(path) {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file for backup: %w", err)
	}

	backupPath := path + backupSuffix + time.Now().Format(backupTimeFormat)
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}

	// Prune old backups
	if err := pruneBackups(path); err != nil {
		// Non-fatal: log but don't fail
		return backupPath, nil
	}

	return backupPath, nil
}

// pruneBackups keeps only the most recent maxBackups backup files.
func pruneBackups(originalPath string) error {
	dir := filepath.Dir(originalPath)
	base := filepath.Base(originalPath)
	prefix := base + backupSuffix

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var backups []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, filepath.Join(dir, entry.Name()))
		}
	}

	if len(backups) <= maxBackups {
		return nil
	}

	// Sort oldest first (timestamp in filename makes lexicographic sort work)
	sort.Strings(backups)

	// Remove oldest, keeping the most recent maxBackups
	for _, path := range backups[:len(backups)-maxBackups] {
		os.Remove(path)
	}

	return nil
}
