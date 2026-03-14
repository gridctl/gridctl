package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// LockFile represents skills.lock.yaml — pins exact versions of imported skills.
type LockFile struct {
	Sources map[string]LockedSource `yaml:"sources"`
}

// LockedSource records the resolved state of a skill source.
type LockedSource struct {
	Repo        string                `yaml:"repo"`
	Ref         string                `yaml:"ref"`
	ResolvedRef string                `yaml:"resolved_ref,omitempty"`
	CommitSHA   string                `yaml:"commit_sha"`
	FetchedAt   time.Time             `yaml:"fetched_at"`
	ContentHash string                `yaml:"content_hash"`
	Skills      map[string]LockedSkill `yaml:"skills"`
}

// LockedSkill records per-skill metadata within a source.
type LockedSkill struct {
	Path        string       `yaml:"path"`
	ContentHash string       `yaml:"content_hash"`
	Fingerprint *Fingerprint `yaml:"fingerprint,omitempty"`
}

// LockFilePath returns the default path to skills.lock.yaml.
func LockFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gridctl", "skills.lock.yaml")
}

// ReadLockFile reads and parses skills.lock.yaml.
func ReadLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LockFile{Sources: make(map[string]LockedSource)}, nil
		}
		return nil, fmt.Errorf("reading lock file: %w", err)
	}

	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing lock file: %w", err)
	}

	if lf.Sources == nil {
		lf.Sources = make(map[string]LockedSource)
	}

	return &lf, nil
}

// WriteLockFile writes skills.lock.yaml atomically. Keys are sorted for
// minimal merge conflicts.
func WriteLockFile(path string, lf *LockFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating lock file directory: %w", err)
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling lock file: %w", err)
	}

	return atomicWriteBytes(path, data)
}

// SetSource updates or adds a source in the lock file.
func (lf *LockFile) SetSource(name string, src LockedSource) {
	if lf.Sources == nil {
		lf.Sources = make(map[string]LockedSource)
	}
	lf.Sources[name] = src
}

// RemoveSource removes a source from the lock file.
func (lf *LockFile) RemoveSource(name string) {
	delete(lf.Sources, name)
}

// RemoveSkill removes a single skill from the lock file, cleaning up the source if empty.
func (lf *LockFile) RemoveSkill(skillName string) {
	for srcName, src := range lf.Sources {
		if _, ok := src.Skills[skillName]; ok {
			delete(src.Skills, skillName)
			if len(src.Skills) == 0 {
				delete(lf.Sources, srcName)
			} else {
				lf.Sources[srcName] = src
			}
			return
		}
	}
}

// FindSkillSource finds the source name for a given skill.
func (lf *LockFile) FindSkillSource(skillName string) (string, *LockedSource, bool) {
	for srcName, src := range lf.Sources {
		if _, ok := src.Skills[skillName]; ok {
			return srcName, &src, true
		}
	}
	return "", nil, false
}

