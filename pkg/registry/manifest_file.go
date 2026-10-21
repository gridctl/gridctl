package registry

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ReadManifestFile reads a regular supporting file within a rooted skill
// directory. Rooted operations keep concurrent path replacements confined.
func (s *Store) ReadManifestFile(ctx context.Context, name, file string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.safeFilePath(name, file); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Join(s.baseDir, "skills"))
	if err != nil {
		return nil, fmt.Errorf("open registry root: %w", err)
	}
	defer root.Close()
	rel, err := filepath.Rel(filepath.Join(s.baseDir, "skills"), s.skillDirPath(name))
	if err != nil {
		return nil, err
	}
	// Open each directory under the previously opened root, never an absolute
	// path that could be redirected between validation and opening.
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		info, err := root.Lstat(part)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("skill directory unavailable: %w", ErrNotFound)
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, fmt.Errorf("open skill directory: %w", err)
		}
		defer next.Close()
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			return nil, fmt.Errorf("skill directory changed: %w", ErrNotFound)
		}
		root = next
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(file)), "/")
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := root.Lstat(part)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("skill file unavailable: %w", ErrNotFound)
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("skill file is not regular: %w", ErrNotFound)
			}
			file, err := root.OpenFile(part, manifestReadFlags, 0)
			if err != nil {
				return nil, fmt.Errorf("open skill file: %w", err)
			}
			defer file.Close()
			opened, err := file.Stat()
			if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
				return nil, fmt.Errorf("skill file changed: %w", ErrNotFound)
			}
			data, err := io.ReadAll(file)
			if err != nil {
				return nil, fmt.Errorf("read skill file: %w", err)
			}
			return data, ctx.Err()
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("skill directory unavailable: %w", ErrNotFound)
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, fmt.Errorf("open supporting directory: %w", err)
		}
		defer next.Close()
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			return nil, fmt.Errorf("supporting directory changed: %w", ErrNotFound)
		}
		root = next
	}
	return nil, ErrNotFound
}
