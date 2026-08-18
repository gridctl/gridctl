package resetops

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// backupNoteDefault and backupNotePurge state, verbatim in output, what
// the archive does and does not capture. Silence about secret handling
// is the anti-pattern this wording exists to avoid.
const (
	backupNoteDefault = "captures every file this reset modifies or removes, plus the projection lockfile"
	backupNotePurge   = "captures vault, pins, registry, context store, saved stacks, and lockfiles; excludes oauth tokens and daemon state (re-obtainable, and their sealing key must not travel with them) and cache/logs/telemetry (recreatable)"
)

// Backup writes the pre-destruction archive for doc and returns its
// path. Fail-closed: any error here must abort the reset; a partial
// backup is worse than none. The purge archive lives OUTSIDE the tree
// being purged, or it would delete itself.
func (m *Managers) Backup(ctx context.Context, doc *Doc, now time.Time) (string, error) {
	stamp := now.UTC().Format("2006-01-02T15-04-05Z")
	gridctlDir := m.GridctlDir()

	var archivePath string
	if doc.Purge {
		archivePath = filepath.Join(m.Home, ".gridctl-backup-reset-"+stamp+".tar.gz")
	} else {
		dir := filepath.Join(gridctlDir, "backups")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("creating backup directory: %w", err)
		}
		archivePath = filepath.Join(dir, "reset-"+stamp+".tar.gz")
	}

	files, err := m.backupSet(doc)
	if err != nil {
		return "", err
	}
	if err := writeTarGz(ctx, archivePath, m.Home, files); err != nil {
		// Fail closed: remove the partial archive so nothing mistakes
		// it for a complete safety copy.
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("writing backup (nothing was deleted): %w", err)
	}
	if doc.Purge {
		doc.BackupNote = backupNotePurge
	} else {
		doc.BackupNote = backupNoteDefault
	}
	pruneBackups(gridctlDir, m.Home)
	return archivePath, nil
}

// backupSet resolves the file list for the archive from the same rows
// the executor consumes.
func (m *Managers) backupSet(doc *Doc) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	var addErr error
	addFile := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		files = append(files, p)
	}
	// add captures a projection target completely: copy-channel skill
	// projections and multi-file context targets are DIRECTORIES, and a
	// header-only dir entry in the tar would silently break the
	// "captures every file this reset removes" guarantee.
	add := func(p string) {
		if p == "" || addErr != nil {
			return
		}
		info, err := os.Lstat(p)
		if err != nil {
			return // nothing on disk to save
		}
		if !info.IsDir() {
			addFile(p)
			return
		}
		addErr = filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !fi.IsDir() {
				addFile(path)
			}
			return nil
		})
	}

	for _, r := range doc.Rows {
		if doc.Purge && r.Kind == "state-file" {
			continue // purge archives exclude daemon state (resolved tokens)
		}
		switch r.Action {
		case ActionWouldRemove, ActionDropRecord:
			add(r.Path)
		}
	}
	// The projection lockfile is rewritten by every unsync.
	add(filepath.Join(m.GridctlDir(), "project.lock.yaml"))

	if doc.Purge {
		// The whole tree goes; archive the irreplaceable parts. oauth/
		// and state/ are excluded deliberately: tokens are re-obtainable
		// via re-auth, and archiving sealed tokens next to their machine
		// keyfile is plaintext-equivalent. cache/, logs/, telemetry/,
		// and backups/ are recreatable or self-referential bulk.
		skip := map[string]bool{
			"oauth": true, "state": true, "backups": true,
			"cache": true, "logs": true, "telemetry": true,
		}
		entries, err := os.ReadDir(m.GridctlDir())
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading %s: %w", m.GridctlDir(), err)
		}
		for _, e := range entries {
			if skip[e.Name()] {
				continue
			}
			add(filepath.Join(m.GridctlDir(), e.Name()))
		}
	}
	if addErr != nil {
		return nil, fmt.Errorf("collecting backup set: %w", addErr)
	}
	sort.Strings(files)
	return files, nil
}

// writeTarGz archives files (absolute paths) into a 0600 tar.gz, with
// names relative to root so a restore-by-hand lands files back where
// they came from. Symlinks are archived as symlinks.
func writeTarGz(ctx context.Context, archivePath, root string, files []string) error {
	f, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	fail := func(err error) error {
		_ = tw.Close()
		_ = gz.Close()
		_ = f.Close()
		return err
	}

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fail(err)
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return fail(err)
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return fail(err)
		}
		name, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(name, "..") {
			name = strings.TrimPrefix(path, string(filepath.Separator))
		}
		hdr.Name = filepath.ToSlash(name)
		if err := tw.WriteHeader(hdr); err != nil {
			return fail(err)
		}
		if info.Mode().IsRegular() {
			src, err := os.Open(path)
			if err != nil {
				return fail(err)
			}
			_, cerr := io.Copy(tw, src)
			src.Close()
			if cerr != nil {
				return fail(cerr)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return fail(err)
	}
	if err := gz.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// pruneBackups enforces keep-newest-3 on both archive locations.
func pruneBackups(gridctlDir, home string) {
	pruneGlob(filepath.Join(gridctlDir, "backups", "reset-*.tar.gz"))
	pruneGlob(filepath.Join(home, ".gridctl-backup-reset-*.tar.gz"))
}

func pruneGlob(pattern string) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	// Timestamped names sort chronologically; the shared keep-newest
	// policy lives in pkg/project.
	for _, old := range project.StaleBackups(matches, project.MaxBackups) {
		_ = os.Remove(old)
	}
}
