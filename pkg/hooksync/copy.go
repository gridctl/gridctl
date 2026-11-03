package hooksync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// copyScripts copies a registry hook directory to dst. .origin.json,
// dot-prefixed entries, and *.pre-* backups are skipped. Directories are
// 0700. Files are 0600, or 0700 when the source is executable.
func copyScripts(src, dst string) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", parent, err)
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	if err := os.Chmod(tmp, 0o700); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	var old string
	if _, lerr := os.Lstat(dst); lerr == nil {
		old = filepath.Join(parent, "."+filepath.Base(dst)+".old-"+time.Now().UTC().Format(backupTimeFormat))
		if err := os.Rename(dst, old); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("moving previous script copy aside: %w", err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		if old != "" {
			_ = os.Rename(old, dst)
		}
		return fmt.Errorf("renaming script copy into place: %w", err)
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		if skipCopyEntry(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		data, rerr := os.ReadFile(path) // #nosec G304 -- walking a managed hook directory; symlinks are not followed
		if rerr != nil {
			return rerr
		}
		mode := os.FileMode(0o600)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o700
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		return os.WriteFile(out, data, mode)
	})
}

func skipCopyEntry(name string) bool {
	if name == ".origin.json" || strings.HasPrefix(name, ".") {
		return true
	}
	return strings.Contains(name, ".pre-")
}

// treeHash fingerprints a script copy: sorted relative paths and per-file
// content hashes. A missing directory hashes as empty.
func treeHash(dir string) (string, error) {
	type manifestEntry struct{ rel, sum string }
	var entries []manifestEntry
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(path) // #nosec G304 -- walking the managed script copy
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		entries = append(entries, manifestEntry{rel: filepath.ToSlash(rel), sum: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%s\n", e.rel, e.sum)
	}
	return project.HashScheme + hex.EncodeToString(h.Sum(nil)), nil
}

// plannedTreeHash hashes the registry tree the way copyScripts would
// write it, without writing.
func plannedTreeHash(src string) (string, error) {
	type manifestEntry struct{ rel, sum string }
	var entries []manifestEntry
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		if skipCopyEntry(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		data, rerr := os.ReadFile(path) // #nosec G304 -- walking a managed hook directory; symlinks are not followed
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		entries = append(entries, manifestEntry{rel: filepath.ToSlash(rel), sum: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", src, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%s\n", e.rel, e.sum)
	}
	return project.HashScheme + hex.EncodeToString(h.Sum(nil)), nil
}

// backupTree copies dir to a timestamped directory under
// ~/.gridctl/project-backups/hook/<name>/ and keeps three.
func backupTree(home, name, dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", nil
	}
	dest := filepath.Join(home, ".gridctl", "project-backups", "hook", name, time.Now().UTC().Format(backupTimeFormat))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	for i := 0; ; i++ {
		candidate := dest
		if i > 0 {
			candidate = dest + "-" + fmt.Sprint(i)
		}
		if _, statErr := os.Stat(candidate); os.IsNotExist(statErr) {
			dest = candidate
			break
		}
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", err
	}
	if err := copyTree(dir, dest); err != nil {
		return "", err
	}
	pruneTreeBackups(filepath.Dir(dest))
	return dest, nil
}

func pruneTreeBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			backups = append(backups, filepath.Join(dir, entry.Name()))
		}
	}
	for _, p := range project.StaleBackups(backups, project.MaxBackups) {
		_ = os.RemoveAll(p)
	}
}
