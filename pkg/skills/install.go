package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Supporting content is installed from an allowlist rather than by copying
// the discovered skill directory and excluding known-bad paths. The allowlist
// is what keeps a repo-root skill (Path == ".") from installing .git, and what
// keeps a parent skill from absorbing a nested skill that lives beside it.
var supportingDirs = []string{"scripts", "references", "assets"}

// metadataFilePattern matches top-level package metadata copied verbatim
// alongside the supporting trees. Deliberately an exact basename match with
// an optional extension: a prefix match would both let a repo smuggle
// arbitrary files into the skill root by naming them "license-*" and make
// pruning delete a user's own "license-notes.md" on every sync.
var metadataFilePattern = regexp.MustCompile(`^(LICENSE|LICENCE|NOTICE|COPYING)(\.[A-Za-z0-9]+)?$`)

const (
	// maxSupportingFileSize caps a single copied file. Skill scripts are
	// source, not payloads; anything larger is a packaging mistake or an
	// attempt to fill the disk.
	maxSupportingFileSize = 5 << 20 // 5 MiB

	// maxSupportingFiles caps the per-skill file count.
	maxSupportingFiles = 500

	// maxSupportingTotalSize caps the aggregate. Without it the two limits
	// above multiply to gigabytes of resident content, and import is
	// reachable from the long-running daemon, not just the CLI.
	maxSupportingTotalSize = 50 << 20 // 50 MiB

	// installTmpDir stages a new tree inside the skill directory so the
	// existing one is only removed once the replacement is fully written.
	installTmpDir = ".gridctl-install-tmp"
)

// supportingFile is one file selected for installation, carrying everything
// needed to scan and copy it without re-walking the source tree.
type supportingFile struct {
	rel     string // path relative to the skill directory, slash-separated
	mode    fs.FileMode
	content []byte
}

// limitError marks content that exceeds an install limit. It is a skip
// reason rather than a hard error, so one oversized package cannot fail a
// whole multi-skill import.
type limitError struct{ reason string }

func (e *limitError) Error() string { return e.reason }

// collectSupportingFiles walks the allowlisted subtrees under srcDir and
// returns the files that should be installed alongside SKILL.md.
//
// The traversal is deliberately conservative because srcDir is untrusted
// remote content:
//   - only supportingDirs and top-level metadata files are considered
//   - any directory carrying its own SKILL.md is skipped, so a parent skill
//     never absorbs a nested one, and a stray SKILL.md is never copied
//   - symlinks are skipped entirely; remote skills have no legitimate need
//     for them, and following one is how a copy escapes the skill directory
//   - per-file, per-skill, and aggregate size limits are enforced
//
// Warnings describe content that was deliberately not installed. A limitError
// means the skill should be skipped rather than partially installed.
func collectSupportingFiles(srcDir string) ([]supportingFile, []string, error) {
	var (
		files    []supportingFile
		warnings []string
		total    int64
	)

	add := func(rel, abs string, d fs.DirEntry) error {
		info, err := d.Info()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: cannot stat: %v", filepath.ToSlash(rel), err))
			return nil
		}
		if info.Size() > maxSupportingFileSize {
			return &limitError{reason: fmt.Sprintf("%s is %d bytes, over the %d byte per-file limit", filepath.ToSlash(rel), info.Size(), maxSupportingFileSize)}
		}
		if len(files) >= maxSupportingFiles {
			return &limitError{reason: fmt.Sprintf("more than %d supporting files", maxSupportingFiles)}
		}
		if total+info.Size() > maxSupportingTotalSize {
			return &limitError{reason: fmt.Sprintf("supporting files exceed the %d byte total limit", maxSupportingTotalSize)}
		}
		data, err := os.ReadFile(abs) // #nosec G304 -- abs is produced by walking srcDir, and symlinks are skipped before this point
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: cannot read: %v", filepath.ToSlash(rel), err))
			return nil
		}
		total += int64(len(data))
		files = append(files, supportingFile{
			rel:     filepath.ToSlash(rel),
			mode:    info.Mode().Perm(),
			content: data,
		})
		return nil
	}

	for _, sub := range supportingDirs {
		root, ok, why := managedTreeRoot(srcDir, sub)
		if !ok {
			if why != "" {
				warnings = append(warnings, why)
			}
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				rel, _ := filepath.Rel(srcDir, path)
				warnings = append(warnings, fmt.Sprintf("%s: cannot walk: %v", filepath.ToSlash(rel), err))
				return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
			}
			rel, rerr := filepath.Rel(srcDir, path)
			if rerr != nil {
				return rerr
			}
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				warnings = append(warnings, fmt.Sprintf("%s: skipped symlink", filepath.ToSlash(rel)))
				return nil
			case d.IsDir():
				// Any directory carrying its own SKILL.md is a separate
				// skill, including the tree root itself.
				if _, serr := os.Lstat(filepath.Join(path, "SKILL.md")); serr == nil {
					warnings = append(warnings, fmt.Sprintf("%s: skipped nested skill directory", filepath.ToSlash(rel)))
					return fs.SkipDir
				}
				return nil
			case !d.Type().IsRegular():
				warnings = append(warnings, fmt.Sprintf("%s: skipped irregular file", filepath.ToSlash(rel)))
				return nil
			case d.Name() == "SKILL.md":
				// The rendered SKILL.md is written by the store; a stray one
				// inside a managed tree is never carried over.
				return nil
			default:
				return add(rel, path, d)
			}
		})
		if err != nil {
			return nil, warnings, err
		}
	}

	// Top-level metadata files.
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, warnings, fmt.Errorf("reading %s: %w", srcDir, err)
	}
	for _, e := range entries {
		if !metadataFilePattern.MatchString(e.Name()) {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 || !e.Type().IsRegular() {
			warnings = append(warnings, fmt.Sprintf("%s: skipped non-regular metadata file", e.Name()))
			continue
		}
		if err := add(e.Name(), filepath.Join(srcDir, e.Name()), e); err != nil {
			return nil, warnings, err
		}
	}

	return files, warnings, nil
}

// managedTreeRoot resolves one supporting directory under srcDir, reporting
// why it was skipped when it is not a usable directory. The name is matched
// exactly against the directory listing rather than trusted to the
// filesystem, so a case-insensitive volume cannot silently install a
// "Scripts/" tree as "scripts/" while a case-sensitive one installs nothing.
func managedTreeRoot(srcDir, sub string) (root string, ok bool, warning string) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", false, ""
	}
	for _, e := range entries {
		if e.Name() != sub {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 {
			return "", false, fmt.Sprintf("%s: skipped symlinked directory", sub)
		}
		if !e.IsDir() {
			return "", false, fmt.Sprintf("%s: skipped, not a directory", sub)
		}
		return filepath.Join(srcDir, sub), true, ""
	}
	return "", false, ""
}

// installSupportingFiles replaces the managed content of dstDir with files.
//
// The replacement is staged: everything is written into a temp directory
// inside dstDir first, and the existing managed content is only removed once
// the new tree is complete. A failure partway through therefore leaves the
// previous install intact rather than a half-deleted skill whose SKILL.md
// still references scripts that no longer exist.
//
// Pruning is allowlist-scoped: the supportingDirs are replaced wholesale and
// managed metadata basenames are removed, so content deleted upstream does
// not linger. Nothing outside those paths is touched — .origin.json,
// SKILL.md, and SKILL.md.pre-<sha> backups all survive. The registry store
// has no provenance to tell a user-authored scripts/helper.sh from an
// imported one, so files a user adds under a managed directory are replaced
// on the next import; that is the documented trade-off rather than an
// oversight.
func installSupportingFiles(dstDir string, files []supportingFile) (int, error) {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, fmt.Errorf("creating %s: %w", dstDir, err)
	}

	staging := filepath.Join(dstDir, installTmpDir)
	if err := os.RemoveAll(staging); err != nil {
		return 0, fmt.Errorf("clearing staging dir: %w", err)
	}
	if len(files) > 0 {
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return 0, fmt.Errorf("creating staging dir: %w", err)
		}
	}
	defer func() { _ = os.RemoveAll(staging) }()

	for _, f := range files {
		out := filepath.Join(staging, filepath.FromSlash(f.rel))
		// Defense in depth: the walk cannot produce an escaping path, but the
		// copy target is derived from remote input so verify containment.
		if !withinDir(staging, out) {
			return 0, fmt.Errorf("refusing to write outside skill directory: %s", f.rel)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return 0, fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
		}
		if err := os.WriteFile(out, f.content, f.mode); err != nil {
			return 0, fmt.Errorf("writing %s: %w", f.rel, err)
		}
	}

	// Staging succeeded; now it is safe to replace the live content.
	if err := pruneManagedContent(dstDir); err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	staged, err := os.ReadDir(staging)
	if err != nil {
		return 0, fmt.Errorf("reading staging dir: %w", err)
	}
	for _, e := range staged {
		if err := os.Rename(filepath.Join(staging, e.Name()), filepath.Join(dstDir, e.Name())); err != nil {
			return 0, fmt.Errorf("installing %s: %w", e.Name(), err)
		}
	}
	return len(files), nil
}

// pruneManagedContent removes the managed subtrees and metadata files from
// dstDir. Missing paths are not an error.
func pruneManagedContent(dstDir string) error {
	for _, sub := range supportingDirs {
		if err := os.RemoveAll(filepath.Join(dstDir, sub)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("pruning %s: %w", sub, err)
		}
	}
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", dstDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !metadataFilePattern.MatchString(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dstDir, e.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("pruning %s: %w", e.Name(), err)
		}
	}
	return nil
}

// withinDir reports whether path is dir itself or lies beneath it.
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
