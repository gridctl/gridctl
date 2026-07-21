package skillsync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	backupTimeFormat = "20060102-150405"
	maxBackups       = 3
)

// hashScheme prefixes every stored hash so a future scheme change never
// presents as false drift (the pkg/pins lesson).
const hashScheme = "sha256:"

// treeHash fingerprints a skill directory: a hash over the sorted
// manifest of relative paths and per-file content hashes, so any added,
// removed, renamed, or edited file changes the result. Symlinks inside
// the tree hash their link target. Generalizes the single-file
// InstalledHash used by pkg/skills drift detection.
func treeHash(dir string) (string, error) {
	type manifestEntry struct{ rel, sum string }
	var entries []manifestEntry
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			sum := sha256.Sum256([]byte("symlink:" + link))
			entries = append(entries, manifestEntry{rel: rel, sum: hex.EncodeToString(sum[:])})
			return nil
		}
		data, rerr := os.ReadFile(path) // #nosec G304 -- path comes from walking the managed tree
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		entries = append(entries, manifestEntry{rel: rel, sum: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%s\n", filepath.ToSlash(e.rel), e.sum)
	}
	return hashScheme + hex.EncodeToString(h.Sum(nil)), nil
}

// copyDirAtomic copies src to dst via a temp sibling directory renamed
// into place, so a crash never leaves a half-copied skill at dst. An
// existing dst is renamed away first (rename over a non-empty directory
// fails) and removed after the swap. Temp and old names are dot-prefixed
// so a client scanning the skills root mid-swap never discovers them as
// phantom skills.
func copyDirAtomic(src, dst string) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", parent, err)
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	var old string
	if _, lerr := os.Lstat(dst); lerr == nil {
		old = filepath.Join(parent, "."+filepath.Base(dst)+".old-"+time.Now().Format(backupTimeFormat))
		if err := os.Rename(dst, old); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("moving previous %s aside: %w", dst, err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		if old != "" {
			_ = os.Rename(old, dst)
		}
		return fmt.Errorf("renaming temp dir into place: %w", err)
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}

// copyTree recursively copies src into dst (which already exists),
// preserving file modes. Symlinks are recreated as symlinks.
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
		out := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			return os.MkdirAll(out, info.Mode().Perm())
		case d.Type()&fs.ModeSymlink != 0:
			link, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			return os.Symlink(link, out)
		default:
			data, rerr := os.ReadFile(path) // #nosec G304 -- path comes from walking the source tree
			if rerr != nil {
				return rerr
			}
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			return os.WriteFile(out, data, info.Mode().Perm())
		}
	})
}

// replaceSymlink points linkPath at target via a temp link + rename, so
// the swap is atomic. Rename replaces an existing symlink or file but
// not a directory; callers remove directories first.
func replaceSymlink(linkPath, target string) error {
	parent := filepath.Dir(linkPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", parent, err)
	}
	tmp := filepath.Join(parent, "."+filepath.Base(linkPath)+".tmp-"+time.Now().Format(backupTimeFormat))
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("creating symlink: %w", err)
	}
	if err := os.Rename(tmp, linkPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming symlink into place: %w", err)
	}
	return nil
}

// backupProjection copies an existing destination aside before a
// replace or removal, pruning to maxBackups. Symlinks need no backup
// (the content lives in the registry) and yield "". Backups live under
// <home>/.gridctl/skillsync-backups/<client>/<skill>/, never inside the
// client's scanned skills directory: a sibling backup containing a
// SKILL.md would surface in the client as a phantom skill and keep a
// removed skill alive.
func (m *Manager) backupProjection(client, skill, path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("inspecting %s for backup: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return "", nil
	}
	root := filepath.Join(m.home, ".gridctl", "skillsync-backups", client, skill)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("creating backup root: %w", err)
	}
	// MkdirTemp guarantees a unique name even for two backups within the
	// same timestamp second.
	backupPath, err := os.MkdirTemp(root, time.Now().Format(backupTimeFormat)+"-*")
	if err != nil {
		return "", fmt.Errorf("creating backup dir: %w", err)
	}
	if info.IsDir() {
		if err := copyTree(path, backupPath); err != nil {
			return "", fmt.Errorf("backing up %s: %w", path, err)
		}
	} else {
		data, rerr := os.ReadFile(path) // #nosec G304 -- backing up the projection destination itself
		if rerr != nil {
			return "", fmt.Errorf("backing up %s: %w", path, rerr)
		}
		if err := os.WriteFile(filepath.Join(backupPath, filepath.Base(path)), data, 0o644); err != nil {
			return "", fmt.Errorf("writing backup: %w", err)
		}
	}
	pruneBackups(root)
	return backupPath, nil
}

// pruneBackups keeps only the most recent maxBackups entries in one
// skill's backup root. Best-effort: a failed prune never fails the
// write it follows.
func pruneBackups(root string) {
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var backups []string
	for _, entry := range dirEntries {
		backups = append(backups, filepath.Join(root, entry.Name()))
	}
	if len(backups) <= maxBackups {
		return
	}
	sort.Strings(backups)
	for _, p := range backups[:len(backups)-maxBackups] {
		_ = os.RemoveAll(p)
	}
}

// removeProjection deletes the projected artifact at path: a symlink is
// unlinked, a copied directory is removed recursively.
func removeProjection(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspecting %s: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}

// atomicWriteFile writes data via a uniquely named temp file + rename in
// the target dir (mirrors pkg/contexts).
func atomicWriteFile(path string, data []byte) error {
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
