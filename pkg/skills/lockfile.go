package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ImportLockVersion is the current skills.lock.yaml schema version.
// Version 1 adds the version field itself and per-source agents;
// version-less files predate both and migrate on read.
const ImportLockVersion = 1

// ErrNewerImportLockVersion signals a skills.lock.yaml written by a
// newer gridctl. Callers must never paper over it: acting on state a
// newer version wrote risks silent data loss (the pkg/project lesson).
var ErrNewerImportLockVersion = errors.New("import lockfile was written by a newer gridctl version")

// LockFile represents skills.lock.yaml — pins exact versions of imported skills.
type LockFile struct {
	Version int                     `yaml:"version"`
	Sources map[string]LockedSource `yaml:"sources"`
}

// LockedSource records the resolved state of a skill source.
type LockedSource struct {
	Repo        string                 `yaml:"repo"`
	Ref         string                 `yaml:"ref"`
	ResolvedRef string                 `yaml:"resolved_ref,omitempty"`
	CommitSHA   string                 `yaml:"commit_sha"`
	FetchedAt   time.Time              `yaml:"fetched_at"`
	ContentHash string                 `yaml:"content_hash"`
	Skills      map[string]LockedSkill `yaml:"skills"`
	// Agents records agent definitions imported from this source.
	Agents map[string]LockedAgent `yaml:"agents,omitempty"`
	// CredentialRef is an opaque reference like "${vault:GIT_TOKEN}" used to
	// re-resolve credentials on source update. Raw tokens are never stored.
	CredentialRef string `yaml:"credential_ref,omitempty"`
}

// LockedSkill records per-skill metadata within a source.
type LockedSkill struct {
	Path        string       `yaml:"path"`
	ContentHash string       `yaml:"content_hash"`
	Fingerprint *Fingerprint `yaml:"fingerprint,omitempty"`
}

// LockedAgent records per-agent metadata within a source.
type LockedAgent struct {
	Path        string `yaml:"path"`
	ContentHash string `yaml:"content_hash"`
}

// LockFilePath returns the default path to skills.lock.yaml.
func LockFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gridctl", "skills.lock.yaml")
}

// ReadLockFile reads and parses skills.lock.yaml. Version-less files
// (written before the schema carried a version) migrate to the current
// version on read; files from a newer gridctl are rejected with
// ErrNewerImportLockVersion.
func ReadLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- lockfile path fixed by the caller
	if err != nil {
		if os.IsNotExist(err) {
			return &LockFile{Version: ImportLockVersion, Sources: make(map[string]LockedSource)}, nil
		}
		return nil, fmt.Errorf("reading lock file: %w", err)
	}

	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing lock file: %w", err)
	}

	if lf.Version > ImportLockVersion {
		return nil, fmt.Errorf("%w (%s is version %d, this gridctl supports %d; upgrade gridctl)",
			ErrNewerImportLockVersion, path, lf.Version, ImportLockVersion)
	}
	lf.Version = ImportLockVersion

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
	lf.Version = ImportLockVersion

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

// RemoveSkill removes a single skill from the lock file, cleaning up the
// source when neither skills nor agents remain under it.
func (lf *LockFile) RemoveSkill(skillName string) {
	for srcName, src := range lf.Sources {
		if _, ok := src.Skills[skillName]; ok {
			delete(src.Skills, skillName)
			if len(src.Skills) == 0 && len(src.Agents) == 0 {
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

// RemoveAgent removes a single agent from the lock file, cleaning up the
// source when neither skills nor agents remain under it.
func (lf *LockFile) RemoveAgent(agentName string) {
	for srcName, src := range lf.Sources {
		if _, ok := src.Agents[agentName]; ok {
			delete(src.Agents, agentName)
			if len(src.Skills) == 0 && len(src.Agents) == 0 {
				delete(lf.Sources, srcName)
			} else {
				lf.Sources[srcName] = src
			}
			return
		}
	}
}

// FindAgentSource finds the source name for a given agent.
func (lf *LockFile) FindAgentSource(agentName string) (string, *LockedSource, bool) {
	for srcName, src := range lf.Sources {
		if _, ok := src.Agents[agentName]; ok {
			return srcName, &src, true
		}
	}
	return "", nil, false
}

