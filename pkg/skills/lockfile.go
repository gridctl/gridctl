package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ImportLockVersion is the highest skills.lock.yaml schema version this
// gridctl reads. Version 1 added the version field itself and
// per-source agents; version 2 added per-source pack records. Files are
// written at the lowest version that can represent them (see
// WriteLockFile), so users without packs keep downgrade freedom.
const ImportLockVersion = 2

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
	// Pack records the pack manifest this source was imported through,
	// with its resolved selection. Nil for plain skill/agent sources.
	Pack *LockedPack `yaml:"pack,omitempty"`
}

// LockedPack is the recorded state of an imported pack: the manifest
// identity plus the selection as resolved against discovery at import
// time (never the empty-means-all shorthand).
type LockedPack struct {
	Name    string   `yaml:"name"`
	Version string   `yaml:"version,omitempty"`
	Wiring  bool     `yaml:"wiring,omitempty"`
	Clients []string `yaml:"clients,omitempty"`
	Skills  []string `yaml:"skills,omitempty"`
	Agents  []string `yaml:"agents,omitempty"`
	// Rules lists context rule fragments imported from the pack repo.
	Rules []string `yaml:"rules,omitempty"`
	// Unresolved lists manifest-selected names discovery could not find,
	// so status can keep reporting them until the upstream repo (or the
	// manifest) is fixed.
	Unresolved []string `yaml:"unresolved,omitempty"`
}

// FindPackSource finds the source carrying a pack by pack name.
func (lf *LockFile) FindPackSource(packName string) (string, *LockedSource, bool) {
	for srcName, src := range lf.Sources {
		if src.Pack != nil && src.Pack.Name == packName {
			return srcName, &src, true
		}
	}
	return "", nil, false
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
	// Stamp the lowest version that can represent the file: pack records
	// need version 2 (an older binary would silently drop them on
	// rewrite); everything else stays readable by version-1 binaries.
	lf.Version = 1
	for _, src := range lf.Sources {
		if src.Pack != nil {
			lf.Version = ImportLockVersion
			break
		}
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

