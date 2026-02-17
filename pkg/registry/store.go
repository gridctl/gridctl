package registry

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// ErrNotFound is returned when a skill does not exist in the store.
var ErrNotFound = errors.New("not found")

// Store manages skill files on disk.
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

// Load scans the skills/ subdirectory for SKILL.md files.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.skills = make(map[string]*AgentSkill)

	if err := s.loadSkills(); err != nil {
		return err
	}
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

	dir := filepath.Join(s.baseDir, "skills", sk.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	data, err := RenderSkillMD(sk)
	if err != nil {
		return fmt.Errorf("rendering skill %q: %w", sk.Name, err)
	}

	path := filepath.Join(dir, "SKILL.md")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing skill %q: %w", sk.Name, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	s.skills[sk.Name] = sk
	return nil
}

// DeleteSkill removes a skill directory and cache entry.
func (s *Store) DeleteSkill(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.baseDir, "skills", name)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
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
	return result
}

// loadSkills reads all SKILL.md files from the skills/ subdirectory.
// Each skill lives in its own directory: skills/{name}/SKILL.md
func (s *Store) loadSkills() error {
	dir := filepath.Join(s.baseDir, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading skills directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slog.Warn("skipping skill directory", "path", skillFile, "error", err)
			continue
		}

		sk, err := ParseSkillMD(data)
		if err != nil {
			slog.Warn("skipping skill file", "path", skillFile, "error", err)
			continue
		}

		// Use directory name as skill name if frontmatter doesn't specify one
		if sk.Name == "" {
			sk.Name = entry.Name()
		}

		if err := sk.Validate(); err != nil {
			slog.Warn("skipping invalid skill", "path", skillFile, "error", err)
			continue
		}

		// Count supporting files
		sk.FileCount = countSupportingFiles(filepath.Join(dir, entry.Name()))

		s.skills[sk.Name] = sk
	}
	return nil
}

// countSupportingFiles counts non-SKILL.md files in a skill directory.
func countSupportingFiles(dir string) int {
	count := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if entry.Name() == "SKILL.md" {
			continue
		}
		count++
	}
	return count
}
