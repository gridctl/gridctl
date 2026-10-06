package skills

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Snapshot caps are whole-repository limits, distinct from the per-skill
// caps in install.go. A pack checkout is the daemon's working tree, so
// the copy has to stay bounded. Vars so tests can lower them; production
// values are 5 MiB, 5000 files, and 64 MiB.
var (
	snapshotMaxFileSize  int64 = 5 << 20
	snapshotMaxFiles           = 5000
	snapshotMaxTotalSize int64 = 64 << 20
)

// SnapshotResult is a materialized pack checkout.
type SnapshotResult struct {
	// CheckoutDir is the absolute directory the copy landed in.
	CheckoutDir string
	// Reused is true when an existing directory for this destination was
	// returned without copying.
	Reused bool
}

// SnapshotWorktree copies repoPath, excluding .git, into destDir. The copy
// skips symlinks, refuses any path that resolves outside the clone root,
// and fails closed on the repository caps. An existing destDir is reused.
// The copy holds the cross-process flock on repoPath+".flock" that
// cloneShallow also acquires, so a cache update cannot rewrite the source
// mid-copy. A failure leaves no partial destDir.
func SnapshotWorktree(ctx context.Context, repoPath, destDir string) (SnapshotResult, error) {
	if err := ctx.Err(); err != nil {
		return SnapshotResult{}, err
	}
	if repoPath == "" || destDir == "" {
		return SnapshotResult{}, fmt.Errorf("snapshot: repository and destination are required")
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot: resolving destination: %w", err)
	}
	if info, statErr := os.Stat(absDest); statErr == nil && info.IsDir() {
		return SnapshotResult{CheckoutDir: absDest, Reused: true}, nil
	}

	var result SnapshotResult
	err = withLockFileFlock(ctx, repoPath, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if info, statErr := os.Stat(absDest); statErr == nil && info.IsDir() {
			result = SnapshotResult{CheckoutDir: absDest, Reused: true}
			return nil
		}
		copied, copyErr := copySnapshot(ctx, repoPath, absDest)
		if copyErr != nil {
			return copyErr
		}
		result = SnapshotResult{CheckoutDir: copied}
		return nil
	})
	if err != nil {
		return SnapshotResult{}, err
	}
	return result, nil
}

func copySnapshot(ctx context.Context, repoPath, destDir string) (string, error) {
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("snapshot: resolving repository: %w", err)
	}
	evaluated, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("snapshot: resolving repository: %w", err)
	}
	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("snapshot: creating checkout parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".snapshot-")
	if err != nil {
		return "", fmt.Errorf("snapshot: creating staging directory: %w", err)
	}
	defer func() {
		if staging != "" {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0o700); err != nil {
		return "", fmt.Errorf("snapshot: restricting staging directory: %w", err)
	}

	var (
		files int
		total int64
	)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return fmt.Errorf("snapshot: walking %s: %w", path, walkErr)
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.Clean(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		resolved, evalErr := filepath.EvalSymlinks(path)
		if evalErr != nil {
			return fmt.Errorf("snapshot: resolving %s: %w", rel, evalErr)
		}
		if err := rejectOutsideRoot(evaluated, resolved); err != nil {
			return err
		}
		target := filepath.Join(staging, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			return os.Chmod(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("snapshot: stating %s: %w", rel, infoErr)
		}
		if info.Size() > snapshotMaxFileSize {
			return &limitError{reason: fmt.Sprintf("snapshot: %s is %d bytes, over the 5 MiB per-file limit", filepath.ToSlash(rel), info.Size())}
		}
		if files >= snapshotMaxFiles {
			return &limitError{reason: "snapshot: more than 5000 files"}
		}
		if total+info.Size() > snapshotMaxTotalSize {
			return &limitError{reason: "snapshot: files exceed the 64 MiB total limit"}
		}
		data, readErr := os.ReadFile(path) // #nosec G304 -- path is a non-symlink file under the evaluated clone root
		if readErr != nil {
			return fmt.Errorf("snapshot: reading %s: %w", rel, readErr)
		}
		if int64(len(data)) > snapshotMaxFileSize {
			return &limitError{reason: fmt.Sprintf("snapshot: %s is %d bytes, over the 5 MiB per-file limit", filepath.ToSlash(rel), len(data))}
		}
		if total+int64(len(data)) > snapshotMaxTotalSize {
			return &limitError{reason: "snapshot: files exceed the 64 MiB total limit"}
		}
		mode := os.FileMode(0o600)
		if info.Mode()&0o111 != 0 {
			mode = 0o700
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, mode); err != nil { // #nosec G306 -- mode is 0600 or 0700, then chmod defeats umask
			return fmt.Errorf("snapshot: writing %s: %w", rel, err)
		}
		if err := os.Chmod(target, mode); err != nil {
			return fmt.Errorf("snapshot: restricting %s: %w", rel, err)
		}
		files++
		total += int64(len(data))
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if err := os.Rename(staging, destDir); err != nil {
		if info, statErr := os.Stat(destDir); statErr == nil && info.IsDir() {
			return destDir, nil
		}
		return "", fmt.Errorf("snapshot: publishing checkout: %w", err)
	}
	staging = ""
	return destDir, nil
}

// rejectOutsideRoot fails when resolved is not inside evaluatedRoot.
func rejectOutsideRoot(evaluatedRoot, resolved string) error {
	rel, err := filepath.Rel(evaluatedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("snapshot: path %s resolves outside the clone root", resolved)
	}
	return nil
}
