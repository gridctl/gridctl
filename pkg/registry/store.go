package registry

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned when a prompt or skill does not exist in the store.
var ErrNotFound = errors.New("not found")

// Store manages prompt and skill YAML files on disk.
type Store struct {
	baseDir string
	mu      sync.RWMutex
	prompts map[string]*Prompt
	skills  map[string]*Skill
}

// NewStore creates a store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir: baseDir,
		prompts: make(map[string]*Prompt),
		skills:  make(map[string]*Skill),
	}
}

// Load scans the prompts/ and skills/ subdirectories for YAML files.
// Call this once at initialization. Individual file parse errors are logged and skipped.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prompts = make(map[string]*Prompt)
	s.skills = make(map[string]*Skill)

	if err := s.loadPrompts(); err != nil {
		return err
	}
	if err := s.loadSkills(); err != nil {
		return err
	}
	return nil
}

// HasContent returns true if there is at least one prompt or skill.
func (s *Store) HasContent() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.prompts) > 0 || len(s.skills) > 0
}

// Status returns registry summary counts.
func (s *Store) Status() RegistryStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := RegistryStatus{
		TotalPrompts: len(s.prompts),
		TotalSkills:  len(s.skills),
	}
	for _, p := range s.prompts {
		if p.State == StateActive {
			st.ActivePrompts++
		}
	}
	for _, sk := range s.skills {
		if sk.State == StateActive {
			st.ActiveSkills++
		}
	}
	return st
}

// ListPrompts returns all prompts (all states). Returned pointers are copies.
func (s *Store) ListPrompts() []*Prompt {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Prompt, 0, len(s.prompts))
	for _, p := range s.prompts {
		cp := *p
		result = append(result, &cp)
	}
	return result
}

// GetPrompt returns a prompt by name.
func (s *Store) GetPrompt(name string) (*Prompt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.prompts[name]
	if !ok {
		return nil, fmt.Errorf("prompt %q: %w", name, ErrNotFound)
	}
	cp := *p
	return &cp, nil
}

// SavePrompt creates or updates a prompt (validates, writes YAML, updates cache).
func (s *Store) SavePrompt(p *Prompt) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := p.Validate(); err != nil {
		return fmt.Errorf("validating prompt: %w", err)
	}

	dir := filepath.Join(s.baseDir, "prompts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating prompts directory: %w", err)
	}

	path := s.promptPath(p.Name)
	if err := atomicWrite(path, p); err != nil {
		return fmt.Errorf("writing prompt %q: %w", p.Name, err)
	}

	s.prompts[p.Name] = p
	return nil
}

// DeletePrompt removes a prompt file and cache entry.
func (s *Store) DeletePrompt(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.promptPath(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting prompt %q: %w", name, err)
	}

	delete(s.prompts, name)
	return nil
}

// ListSkills returns all skills (all states). Returned pointers are copies.
func (s *Store) ListSkills() []*Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		cp := *sk
		result = append(result, &cp)
	}
	return result
}

// GetSkill returns a skill by name.
func (s *Store) GetSkill(name string) (*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sk, ok := s.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill %q: %w", name, ErrNotFound)
	}
	cp := *sk
	return &cp, nil
}

// SaveSkill creates or updates a skill (validates, writes YAML, updates cache).
func (s *Store) SaveSkill(sk *Skill) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := sk.Validate(); err != nil {
		return fmt.Errorf("validating skill: %w", err)
	}

	dir := filepath.Join(s.baseDir, "skills")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating skills directory: %w", err)
	}

	path := s.skillPath(sk.Name)
	if err := atomicWrite(path, sk); err != nil {
		return fmt.Errorf("writing skill %q: %w", sk.Name, err)
	}

	s.skills[sk.Name] = sk
	return nil
}

// DeleteSkill removes a skill file and cache entry.
func (s *Store) DeleteSkill(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.skillPath(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting skill %q: %w", name, err)
	}

	delete(s.skills, name)
	return nil
}

// ActivePrompts returns only prompts with State == "active". Returned pointers are copies.
func (s *Store) ActivePrompts() []*Prompt {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Prompt
	for _, p := range s.prompts {
		if p.State == StateActive {
			cp := *p
			result = append(result, &cp)
		}
	}
	return result
}

// ActiveSkills returns only skills with State == "active". Returned pointers are copies.
func (s *Store) ActiveSkills() []*Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Skill
	for _, sk := range s.skills {
		if sk.State == StateActive {
			cp := *sk
			result = append(result, &cp)
		}
	}
	return result
}

// promptPath returns the file path for a prompt by name.
func (s *Store) promptPath(name string) string {
	return filepath.Join(s.baseDir, "prompts", filepath.Base(name)+".yaml")
}

// skillPath returns the file path for a skill by name.
func (s *Store) skillPath(name string) string {
	return filepath.Join(s.baseDir, "skills", filepath.Base(name)+".yaml")
}

// loadPrompts reads all YAML files from the prompts/ subdirectory.
func (s *Store) loadPrompts() error {
	dir := filepath.Join(s.baseDir, "prompts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading prompts directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skipping prompt file", "path", path, "error", err)
			continue
		}

		var p Prompt
		if err := yaml.Unmarshal(data, &p); err != nil {
			slog.Warn("skipping prompt file", "path", path, "error", err)
			continue
		}

		if err := p.Validate(); err != nil {
			slog.Warn("skipping invalid prompt", "path", path, "error", err)
			continue
		}

		s.prompts[p.Name] = &p
	}
	return nil
}

// loadSkills reads all YAML files from the skills/ subdirectory.
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
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("skipping skill file", "path", path, "error", err)
			continue
		}

		var sk Skill
		if err := yaml.Unmarshal(data, &sk); err != nil {
			slog.Warn("skipping skill file", "path", path, "error", err)
			continue
		}

		if err := sk.Validate(); err != nil {
			slog.Warn("skipping invalid skill", "path", path, "error", err)
			continue
		}

		s.skills[sk.Name] = &sk
	}
	return nil
}

// atomicWrite marshals v to YAML and writes it atomically to path.
func atomicWrite(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
	}

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
