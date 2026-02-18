package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ErrNotFound is returned when a skill does not exist in the store.
var ErrNotFound = errors.New("not found")

// Store manages skill directories on disk.
// Each skill is a directory containing a required SKILL.md and optional
// supporting files (scripts/, references/, assets/).
type Store struct {
	baseDir string
	mu      sync.RWMutex
	skills  map[string]*AgentSkill
}

// NewStore creates a store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir: baseDir,
		skills:  make(map[string]*AgentSkill),
	}
}

// Load scans the skills/ subdirectory for SKILL.md files and checks for
// legacy YAML registry files.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.skills = make(map[string]*AgentSkill)

	if err := s.loadSkills(); err != nil {
		return err
	}

	s.checkLegacyFiles()

	return nil
}

// HasContent returns true if there is at least one skill.
func (s *Store) HasContent() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.skills) > 0
}

// Status returns registry summary counts.
func (s *Store) Status() RegistryStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := RegistryStatus{
		TotalSkills: len(s.skills),
	}
	for _, sk := range s.skills {
		if sk.State == StateActive {
			st.ActiveSkills++
		}
	}
	return st
}

// ListSkills returns all skills (all states). Returned pointers are copies.
func (s *Store) ListSkills() []*AgentSkill {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*AgentSkill, 0, len(s.skills))
	for _, sk := range s.skills {
		cp := *sk
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// GetSkill returns a skill by name.
func (s *Store) GetSkill(name string) (*AgentSkill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sk, ok := s.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill %q: %w", name, ErrNotFound)
	}
	cp := *sk
	return &cp, nil
}

// SaveSkill creates or updates a skill (validates, writes SKILL.md, updates cache).
func (s *Store) SaveSkill(sk *AgentSkill) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := sk.Validate(); err != nil {
		return fmt.Errorf("validating skill: %w", err)
	}

	// Preserve existing Dir for updates; default to name for new skills
	if sk.Dir == "" {
		if existing, ok := s.skills[sk.Name]; ok {
			sk.Dir = existing.Dir
		} else {
			sk.Dir = sk.Name
		}
	}

	skillDir := filepath.Join(s.baseDir, "skills", sk.Dir)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	data, err := RenderSkillMD(sk)
	if err != nil {
		return fmt.Errorf("rendering SKILL.md: %w", err)
	}

	path := filepath.Join(skillDir, "SKILL.md")
	if err := atomicWriteBytes(path, data); err != nil {
		return fmt.Errorf("writing SKILL.md for %q: %w", sk.Name, err)
	}

	sk.FileCount = countSupportingFiles(skillDir)
	cp := *sk
	s.skills[cp.Name] = &cp
	return nil
}

// DeleteSkill removes a skill directory and cache entry.
func (s *Store) DeleteSkill(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	skillDir := s.skillDirPath(name)
	if err := os.RemoveAll(skillDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting skill %q: %w", name, err)
	}

	delete(s.skills, name)
	return nil
}

// ActiveSkills returns only skills with State == "active". Returned pointers are copies.
func (s *Store) ActiveSkills() []*AgentSkill {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*AgentSkill
	for _, sk := range s.skills {
		if sk.State == StateActive {
			cp := *sk
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// RenameSkill renames a skill directory and updates its frontmatter.
func (s *Store) RenameSkill(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ValidateSkillName(newName); err != nil {
		return fmt.Errorf("invalid new name: %w", err)
	}

	if _, ok := s.skills[newName]; ok {
		return fmt.Errorf("skill %q already exists", newName)
	}

	sk, ok := s.skills[oldName]
	if !ok {
		return fmt.Errorf("skill %q: %w", oldName, ErrNotFound)
	}

	oldDir := s.skillDirPath(oldName)
	// New dir is a sibling of old dir (same parent)
	newDir := filepath.Join(filepath.Dir(oldDir), filepath.Base(newName))

	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("renaming directory: %w", err)
	}

	// Update frontmatter in SKILL.md
	sk.Name = newName
	data, err := RenderSkillMD(sk)
	if err != nil {
		// Rollback directory rename on render failure
		_ = os.Rename(newDir, oldDir)
		sk.Name = oldName
		return fmt.Errorf("rendering SKILL.md: %w", err)
	}

	path := filepath.Join(newDir, "SKILL.md")
	if err := atomicWriteBytes(path, data); err != nil {
		_ = os.Rename(newDir, oldDir)
		sk.Name = oldName
		return fmt.Errorf("writing SKILL.md: %w", err)
	}

	// Update Dir to reflect new directory location
	skillsDir := filepath.Join(s.baseDir, "skills")
	newRelDir, _ := filepath.Rel(skillsDir, newDir)
	sk.Dir = newRelDir

	delete(s.skills, oldName)
	s.skills[newName] = sk
	return nil
}

// ListFiles returns all files in a skill directory (excluding SKILL.md).
func (s *Store) ListFiles(skillName string) ([]SkillFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	skillDir := s.skillDirPath(skillName)
	var files []SkillFile

	err := filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(skillDir, path)
		if rel == "." || rel == "SKILL.md" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // skip files we can't stat
		}
		files = append(files, SkillFile{
			Path:  rel,
			Size:  info.Size(),
			IsDir: d.IsDir(),
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("skill %q: %w", skillName, ErrNotFound)
		}
		return nil, fmt.Errorf("listing files for %q: %w", skillName, err)
	}
	return files, nil
}

// ReadFile reads a specific file from a skill directory.
func (s *Store) ReadFile(skillName, filePath string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	absPath, err := s.safeFilePath(skillName, filePath)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file %q in skill %q: %w", filePath, skillName, ErrNotFound)
		}
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return data, nil
}

// WriteFile writes a file to a skill directory, creating parent directories as needed.
func (s *Store) WriteFile(skillName, filePath string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	absPath, err := s.safeFilePath(skillName, filePath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	if err := os.WriteFile(absPath, data, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	// Update file count in cache
	if sk, ok := s.skills[skillName]; ok {
		sk.FileCount = countSupportingFiles(s.skillDirPath(skillName))
	}
	return nil
}

// DeleteFile removes a file from a skill directory.
func (s *Store) DeleteFile(skillName, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	absPath, err := s.safeFilePath(skillName, filePath)
	if err != nil {
		return err
	}

	if err := os.Remove(absPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file %q in skill %q: %w", filePath, skillName, ErrNotFound)
		}
		return fmt.Errorf("deleting file: %w", err)
	}

	// Update file count in cache
	if sk, ok := s.skills[skillName]; ok {
		sk.FileCount = countSupportingFiles(s.skillDirPath(skillName))
	}
	return nil
}

// skillDirPath returns the absolute directory path for a skill.
// If the skill is loaded and has a Dir set, it uses that. Otherwise it falls
// back to using the skill name as the directory name (flat layout).
func (s *Store) skillDirPath(name string) string {
	if sk, ok := s.skills[name]; ok && sk.Dir != "" {
		return filepath.Join(s.baseDir, "skills", sk.Dir)
	}
	return filepath.Join(s.baseDir, "skills", filepath.Base(name))
}

// safeFilePath validates and resolves a file path within a skill directory,
// preventing directory traversal attacks.
func (s *Store) safeFilePath(skillName, filePath string) (string, error) {
	// Reject skill names with path traversal components
	if strings.Contains(skillName, "..") || filepath.IsAbs(skillName) {
		return "", fmt.Errorf("invalid skill name: %q", skillName)
	}

	cleanPath := filepath.Clean(filePath)
	if filepath.IsAbs(cleanPath) || strings.HasPrefix(cleanPath, "..") {
		return "", fmt.Errorf("invalid file path: %q", filePath)
	}

	skillDir := s.skillDirPath(skillName)
	fullPath := filepath.Join(skillDir, cleanPath)

	// Defense in depth: verify the resolved path is under the skill directory
	resolved, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	base, err := filepath.Abs(skillDir)
	if err != nil {
		return "", fmt.Errorf("resolving base: %w", err)
	}
	if !strings.HasPrefix(resolved, base+string(filepath.Separator)) && resolved != base {
		return "", fmt.Errorf("path escapes skill directory: %q", filePath)
	}

	return fullPath, nil
}

// loadSkills recursively walks the skills/ subdirectory to find all SKILL.md files.
// Skills can be organized flat (skills/deploy/SKILL.md) or nested in groups
// (skills/git-workflow/branch-fork/SKILL.md).
func (s *Store) loadSkills() error {
	dir := filepath.Join(s.baseDir, "skills")

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skipping skill file", "path", path, "error", err)
			return nil
		}

		sk, err := ParseSkillMD(data)
		if err != nil {
			slog.Warn("skipping skill file", "path", path, "error", err)
			return nil
		}

		// Relative path from skills/ root to the skill directory
		skillDir := filepath.Dir(path)
		relDir, _ := filepath.Rel(dir, skillDir)
		dirName := filepath.Base(skillDir)

		// Name mismatch: directory name takes precedence over frontmatter
		if sk.Name == "" {
			sk.Name = dirName
		} else if sk.Name != dirName {
			slog.Warn("skill name mismatch, using directory name",
				"directory", dirName,
				"frontmatter", sk.Name,
			)
			sk.Name = dirName
		}

		if err := sk.Validate(); err != nil {
			slog.Warn("skipping invalid skill", "path", path, "error", err)
			return nil
		}

		// Check for duplicate names
		if existing, ok := s.skills[sk.Name]; ok {
			slog.Warn("duplicate skill name, keeping first occurrence",
				"name", sk.Name,
				"kept", existing.Dir,
				"skipped", relDir,
			)
			return nil
		}

		sk.Dir = relDir
		sk.FileCount = countSupportingFiles(skillDir)
		s.skills[sk.Name] = sk
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("walking skills directory: %w", err)
	}
	return nil
}

// checkLegacyFiles logs a warning if legacy YAML registry files are detected.
func (s *Store) checkLegacyFiles() {
	for _, dir := range []string{"prompts", "skills"} {
		legacyDir := filepath.Join(s.baseDir, dir)
		entries, err := os.ReadDir(legacyDir)
		if err != nil || len(entries) == 0 {
			continue
		}
		hasYAML := false
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
				hasYAML = true
				break
			}
		}
		if hasYAML {
			slog.Warn("legacy YAML registry files detected",
				"directory", legacyDir,
				"hint", "migrate to SKILL.md format; see https://agentskills.io/specification",
			)
		}
	}
}

// countSupportingFiles counts files in the scripts/, references/, and assets/
// subdirectories of a skill directory.
func countSupportingFiles(skillDir string) int {
	count := 0
	for _, subdir := range []string{"scripts", "references", "assets"} {
		dir := filepath.Join(skillDir, subdir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				count++
			}
		}
	}
	return count
}

// atomicWriteBytes writes data atomically to path via temp file + rename.
func atomicWriteBytes(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}
