package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Supporting files are selected by layout, not by one allowlist for every
// skill.
//
// layoutOwnDirectory copies the skill directory whole. It applies when the
// discovered path is not the clone root, and to every local-directory import
// even when that path is ".". Dot-prefixed entries, nested skill directories,
// symlinks, irregular files, and reserved root names (SKILL.md.*) are
// excluded. That is what keeps .git out of a normal package and what keeps a
// parent from absorbing a nested skill.
//
// layoutRoot keeps the three-directory allowlist. It applies only when a
// non-local import discovers SKILL.md at the clone root, because that
// directory is the repository, not the skill. Dot-prefixed entries are
// excluded in both layouts.
var supportingDirs = []string{"scripts", "references", "assets"}

// metadataFilePattern matches top-level package metadata copied verbatim
// alongside the supporting trees in the root layout. Deliberately an exact
// basename match with an optional extension: a prefix match would both let a
// repo smuggle arbitrary files into the skill root by naming them "license-*"
// and make pruning delete a user's own "license-notes.md" on every sync.
// Own-directory imports copy these files as ordinary root files; the pattern
// is not widened for that layout.
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

	// maxRootOmissionNames caps the repository-root omission warning so a
	// large clone does not dump every leftover path into one line.
	maxRootOmissionNames = 5
)

// treeLayout selects how supporting files are collected and pruned.
type treeLayout int

const (
	// layoutOwnDirectory copies the skill directory, minus the exclusions
	// documented above. Local-directory imports always use this layout.
	layoutOwnDirectory treeLayout = iota
	// layoutRoot keeps the scripts/, references/, and assets/ allowlist.
	// It applies only to a non-local import whose SKILL.md is at the clone root.
	layoutRoot
)

// layoutFor returns layoutRoot only when the source is not a local directory
// and the discovered skill path is the clone root. kind is ImportOptions.Kind
// (empty for git and for pack preview). discoveredPath is DiscoveredSkill.Path.
// filepath.Clean("") is ".", so an empty path is the clone root.
func layoutFor(kind, discoveredPath string) treeLayout {
	if kind != SourceKindLocal && filepath.Clean(discoveredPath) == "." {
		return layoutRoot
	}
	return layoutOwnDirectory
}

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

// collectSupportingFiles returns the files that should be installed alongside
// SKILL.md. layout selects the walk; see treeLayout.
//
// The traversal is deliberately conservative because srcDir is untrusted
// remote content:
//   - root layout considers only supportingDirs, top-level metadata files,
//     and reports other root entries in one warning
//   - own-directory layout considers every regular file, with one warning
//     per excluded entry
//   - any directory other than the walk root that carries its own SKILL.md
//     is skipped, so a parent skill never absorbs a nested one
//   - symlinks are skipped entirely; remote skills have no legitimate need
//     for them, and following one is how a copy escapes the skill directory
//   - dot-prefixed entries are skipped in both layouts
//   - per-file, per-skill, and aggregate size limits are enforced
//
// Warnings describe content that was deliberately not installed. A limitError
// means the skill should be skipped rather than partially installed. Files
// outside the old allowlist count toward the caps in the own-directory layout.
func collectSupportingFiles(srcDir string, layout treeLayout) ([]supportingFile, []string, error) {
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

	var err error
	if layout == layoutRoot {
		err = collectRootLayout(srcDir, add, &warnings)
	} else {
		err = collectOwnDirectory(srcDir, add, &warnings)
	}
	if err != nil {
		return nil, warnings, err
	}
	return files, warnings, nil
}

// collectRootLayout walks the allowlisted subtrees and top-level metadata,
// then reports other non-dot root entries in one warning.
func collectRootLayout(srcDir string, add func(rel, abs string, d fs.DirEntry) error, warnings *[]string) error {
	for _, sub := range supportingDirs {
		root, ok, why := managedTreeRoot(srcDir, sub)
		if !ok {
			if why != "" {
				*warnings = append(*warnings, why)
			}
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				rel, _ := filepath.Rel(srcDir, path)
				*warnings = append(*warnings, fmt.Sprintf("%s: cannot walk: %v", filepath.ToSlash(rel), err))
				return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
			}
			rel, rerr := filepath.Rel(srcDir, path)
			if rerr != nil {
				return rerr
			}
			switch {
			case strings.HasPrefix(d.Name(), "."):
				*warnings = append(*warnings, fmt.Sprintf("%s: skipped dot entry", filepath.ToSlash(rel)))
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			case d.Type()&fs.ModeSymlink != 0:
				*warnings = append(*warnings, fmt.Sprintf("%s: skipped symlink", filepath.ToSlash(rel)))
				return nil
			case d.IsDir():
				// Any directory carrying its own SKILL.md is a separate
				// skill, including the tree root itself. The own-directory
				// walk cannot reuse this branch unchanged: its root is the
				// skill directory, which always contains SKILL.md.
				if _, serr := os.Lstat(filepath.Join(path, "SKILL.md")); serr == nil {
					*warnings = append(*warnings, fmt.Sprintf("%s: skipped nested skill directory", filepath.ToSlash(rel)))
					return fs.SkipDir
				}
				return nil
			case !d.Type().IsRegular():
				*warnings = append(*warnings, fmt.Sprintf("%s: skipped irregular file", filepath.ToSlash(rel)))
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
			return err
		}
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", srcDir, err)
	}
	for _, e := range entries {
		if !metadataFilePattern.MatchString(e.Name()) {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 || !e.Type().IsRegular() {
			*warnings = append(*warnings, fmt.Sprintf("%s: skipped non-regular metadata file", e.Name()))
			continue
		}
		if err := add(e.Name(), filepath.Join(srcDir, e.Name()), e); err != nil {
			return err
		}
	}
	if w := rootOmissionWarning(entries); w != "" {
		*warnings = append(*warnings, w)
	}
	return nil
}

// rootOmissionWarning reports root entries the allowlist did not install.
// Dot entries, SKILL.md, the allowlisted directory names, and metadata files
// are excluded; each of those has its own rule. At most five names are listed.
func rootOmissionWarning(entries []os.DirEntry) string {
	allow := make(map[string]struct{}, len(supportingDirs))
	for _, sub := range supportingDirs {
		allow[sub] = struct{}{}
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if name == "SKILL.md" || strings.HasPrefix(name, ".") {
			continue
		}
		if _, ok := allow[name]; ok {
			continue
		}
		if metadataFilePattern.MatchString(name) {
			continue
		}
		if e.IsDir() && e.Type()&fs.ModeSymlink == 0 {
			names = append(names, name+"/")
		} else {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	shown := names
	extra := ""
	if len(shown) > maxRootOmissionNames {
		shown = shown[:maxRootOmissionNames]
		extra = ", ..."
	}
	return fmt.Sprintf("repository-root skill: %d entries outside scripts/, references/, and assets/ were not installed (%s%s)",
		len(names), strings.Join(shown, ", "), extra)
}

// collectOwnDirectory walks srcDir and selects every regular file that is
// part of this skill. The walk root is the skill directory, so the first
// callback (rel == ".") must return nil and never SkipDir.
func collectOwnDirectory(srcDir string, add func(rel, abs string, d fs.DirEntry) error, warnings *[]string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == srcDir {
				return fmt.Errorf("reading %s: %w", srcDir, err)
			}
			rel, _ := filepath.Rel(srcDir, path)
			*warnings = append(*warnings, fmt.Sprintf("%s: cannot walk: %v", filepath.ToSlash(rel), err))
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		// The skill directory itself contains SKILL.md. Skipping it would
		// select nothing.
		if rel == "." {
			return nil
		}
		base := d.Name()
		if rel == "SKILL.md" {
			// The store writes the rendered SKILL.md.
			return nil
		}
		if strings.HasPrefix(base, ".") {
			*warnings = append(*warnings, fmt.Sprintf("%s: skipped dot entry", rel))
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.Contains(rel, "/") && strings.HasPrefix(base, "SKILL.md.") {
			*warnings = append(*warnings, fmt.Sprintf("%s: skipped reserved name", rel))
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			*warnings = append(*warnings, fmt.Sprintf("%s: skipped symlink", rel))
			return nil
		}
		if d.IsDir() {
			if _, serr := os.Lstat(filepath.Join(path, "SKILL.md")); serr == nil {
				*warnings = append(*warnings, fmt.Sprintf("%s: skipped nested skill directory", rel))
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			*warnings = append(*warnings, fmt.Sprintf("%s: skipped irregular file", rel))
			return nil
		}
		return add(rel, path, d)
	})
}

// managedTreeRoot resolves one supporting directory under srcDir, reporting
// why it was skipped when it is not a usable directory. The name is matched
// exactly against the directory listing rather than trusted to the
// filesystem, so a case-insensitive volume cannot silently install a
// "Scripts/" tree as "scripts/" while a case-sensitive one installs nothing.
// Used by the root layout only.
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

// installSupportingFiles replaces the managed content of dstDir with files
// and returns the relative paths it removed that are not in the new set.
//
// The replacement is staged: everything is written into a temp directory
// inside dstDir first, and the existing managed content is only removed once
// the new tree is complete. A failure partway through therefore leaves the
// previous install intact rather than a half-deleted skill whose SKILL.md
// still references scripts that no longer exist.
//
// Root layout pruning is allowlist-scoped: the supportingDirs are replaced
// wholesale and managed metadata basenames are removed. Nothing outside those
// paths is touched, so .origin.json, SKILL.md, and SKILL.md.pre-<sha> backups
// survive, as does a hand-added file beside them.
//
// Own-directory pruning replaces the whole directory except SKILL.md, names
// starting with SKILL.md., and dot-prefixed entries. Those dot entries include
// .origin.json and the staging directory, which is created before the prune
// and must not be deleted out from under the rename. The registry has no
// per-file manifest, so a hand-added file in an own-directory skill is
// replaced on the next import; that is the documented trade-off rather than
// an oversight.
func installSupportingFiles(dstDir string, files []supportingFile, layout treeLayout) (int, []string, error) {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, nil, fmt.Errorf("creating %s: %w", dstDir, err)
	}

	staging := filepath.Join(dstDir, installTmpDir)
	if err := os.RemoveAll(staging); err != nil {
		return 0, nil, fmt.Errorf("clearing staging dir: %w", err)
	}
	if len(files) > 0 {
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return 0, nil, fmt.Errorf("creating staging dir: %w", err)
		}
	}
	defer func() { _ = os.RemoveAll(staging) }()

	for _, f := range files {
		out := filepath.Join(staging, filepath.FromSlash(f.rel))
		// Defense in depth: the walk cannot produce an escaping path, but the
		// copy target is derived from remote input so verify containment.
		if !withinDir(staging, out) {
			return 0, nil, fmt.Errorf("refusing to write outside skill directory: %s", f.rel)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return 0, nil, fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
		}
		if err := os.WriteFile(out, f.content, f.mode); err != nil {
			return 0, nil, fmt.Errorf("writing %s: %w", f.rel, err)
		}
	}

	keep := make(map[string]struct{}, len(files))
	for _, f := range files {
		keep[f.rel] = struct{}{}
	}

	// Staging succeeded; now it is safe to replace the live content.
	var (
		removed []string
		err     error
	)
	if layout == layoutRoot {
		removed, err = removedByRootPrune(dstDir, keep)
		if err != nil {
			return 0, nil, err
		}
		if err := pruneManagedContent(dstDir); err != nil {
			return 0, nil, err
		}
	} else {
		removed, err = pruneOwnDirectory(dstDir, keep)
		if err != nil {
			return 0, nil, err
		}
	}
	if len(files) == 0 {
		return 0, removed, nil
	}
	staged, err := os.ReadDir(staging)
	if err != nil {
		return 0, nil, fmt.Errorf("reading staging dir: %w", err)
	}
	for _, e := range staged {
		if err := os.Rename(filepath.Join(staging, e.Name()), filepath.Join(dstDir, e.Name())); err != nil {
			return 0, nil, fmt.Errorf("installing %s: %w", e.Name(), err)
		}
	}
	return len(files), removed, nil
}

// removedByRootPrune lists regular files pruneManagedContent will delete that
// are not in the replacement set. It does not delete; the caller still calls
// pruneManagedContent so the root-layout deletion set stays in one place.
func removedByRootPrune(dstDir string, keep map[string]struct{}) ([]string, error) {
	var removed []string
	for _, sub := range supportingDirs {
		files, err := regularRelFiles(dstDir, filepath.Join(dstDir, sub))
		if err != nil {
			return nil, fmt.Errorf("pruning %s: %w", sub, err)
		}
		for _, rel := range files {
			if _, ok := keep[rel]; !ok {
				removed = append(removed, rel)
			}
		}
	}
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		if os.IsNotExist(err) {
			sort.Strings(removed)
			return removed, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dstDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !metadataFilePattern.MatchString(e.Name()) {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 || !e.Type().IsRegular() {
			continue
		}
		if _, ok := keep[e.Name()]; !ok {
			removed = append(removed, e.Name())
		}
	}
	sort.Strings(removed)
	return removed, nil
}

// pruneOwnDirectory removes every top-level entry except SKILL.md, SKILL.md.*
// backups, and dot-prefixed names, and returns the regular files that deletion
// dropped that are not in the replacement set.
func pruneOwnDirectory(dstDir string, keep map[string]struct{}) ([]string, error) {
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dstDir, err)
	}
	var removed []string
	for _, e := range entries {
		if sparedInstallName(e.Name()) {
			continue
		}
		abs := filepath.Join(dstDir, e.Name())
		files, err := regularRelFiles(dstDir, abs)
		if err != nil {
			return nil, fmt.Errorf("pruning %s: %w", e.Name(), err)
		}
		for _, rel := range files {
			if _, ok := keep[rel]; !ok {
				removed = append(removed, rel)
			}
		}
		if err := os.RemoveAll(abs); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("pruning %s: %w", e.Name(), err)
		}
	}
	sort.Strings(removed)
	return removed, nil
}

// sparedInstallName reports names the own-directory prune must not delete.
// Dot-prefixed names include .origin.json and the staging directory.
func sparedInstallName(name string) bool {
	return name == "SKILL.md" || strings.HasPrefix(name, "SKILL.md.") || strings.HasPrefix(name, ".")
}

// regularRelFiles lists slash-separated regular files at abs or beneath it,
// relative to dstDir. Symlinks are not followed and are not listed. A missing
// path contributes nothing.
func regularRelFiles(dstDir, abs string) ([]string, error) {
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if info.Mode().IsRegular() {
		rel, rerr := filepath.Rel(dstDir, abs)
		if rerr != nil {
			return nil, rerr
		}
		return []string{filepath.ToSlash(rel)}, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, nil
	}
	var out []string
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(dstDir, path)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// pruneManagedContent removes the managed subtrees and metadata files from
// dstDir. Missing paths are not an error. Root layout only.
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
