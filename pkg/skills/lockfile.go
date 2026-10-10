package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
	"gopkg.in/yaml.v3"
)

// Per-feature lockfile schema versions. WriteLockFile stamps the highest
// version any source requires, which is also the lowest version that can
// represent the file, so users without those features keep downgrade freedom.
const (
	lockVersionPacks       = 2
	lockVersionVariables   = 3
	lockVersionSSHAuth     = 4
	lockVersionStack       = 5
	lockVersionLocal       = 6
	lockVersionPackSources = 7
)

// ImportLockVersion is the highest skills.lock.yaml schema version this
// gridctl reads. Version 1 added the version field itself and per-source
// agents; version 2 added per-source pack records; version 3 added pack
// variable declarations; version 4 added ssh-key auth fields (auth_method,
// ssh_user, ssh_key_path); version 5 added pack stack records and
// unresolved-detail maps; version 6 added local directory sources
// (kind: local and per-skill tree_hash); version 7 added pack external
// sources (LockedPack.Sources and LockedSource.PackMember). Files are
// written at the lowest version that can represent them (see WriteLockFile).
// A reader whose maximum is 4 understands SSH fields only and must refuse a
// file that carries a stack record or an unresolved-detail map rather than
// drop those keys on the next write. A reader whose maximum is 5 must refuse
// a file that carries a local source. A reader whose maximum is 6 must refuse
// a file that carries pack sources or a pack member marker.
const ImportLockVersion = lockVersionPackSources

// ErrNewerImportLockVersion signals a skills.lock.yaml written by a
// newer gridctl. Callers must never paper over it: acting on state a
// newer version wrote risks silent data loss (the pkg/project lesson).
var ErrNewerImportLockVersion = errors.New("import lockfile was written by a newer gridctl version")

// ErrImportLockBusy signals contention on the cross-process import lock:
// the operation should be retried, nothing is corrupted.
var ErrImportLockBusy = errors.New("timeout acquiring import lock")

// LockFile represents skills.lock.yaml — pins exact versions of imported skills.
type LockFile struct {
	Version int                     `yaml:"version"`
	Sources map[string]LockedSource `yaml:"sources"`
}

// LockedSource records the resolved state of a skill source.
type LockedSource struct {
	Repo string `yaml:"repo"`
	// Kind is "local" for a directory source and empty for a git source.
	Kind        string                 `yaml:"kind,omitempty"`
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
	// AuthMethod, SSHUser, and SSHKeyPath persist ssh-key authentication.
	// Only the path is stored, never key material or a passphrase.
	AuthMethod string `yaml:"auth_method,omitempty"`
	SSHUser    string `yaml:"ssh_user,omitempty"`
	SSHKeyPath string `yaml:"ssh_key_path,omitempty"`
	// Pack records the pack manifest this source was imported through,
	// with its resolved selection. Nil for plain skill/agent sources.
	// A member source keeps Pack nil so FindPackSource never treats it
	// as the pack record.
	Pack *LockedPack `yaml:"pack,omitempty"`
	// PackMember is the pack name when this source was imported as a
	// named external source of that pack. Empty for every other source.
	// <pack>/<source> cannot collide with RepoToName output, which is a
	// basename and never contains a slash. GuardSourceKey is what stops
	// an explicit --source-name from re-keying the member to another repo.
	PackMember string `yaml:"pack_member,omitempty"`
}

// LockedPack is the recorded state of an imported pack: the manifest
// identity plus the selection as resolved against discovery at import
// time (never the empty-means-all shorthand).
type LockedPack struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version,omitempty"`
	// Description and Author persist the manifest metadata a list view
	// needs, so no consumer ever has to re-clone the repo to show it.
	// Additive fields: files without them keep loading, and version 2
	// (which every pack record already stamps) covers them.
	Description string   `yaml:"description,omitempty"`
	Author      string   `yaml:"author,omitempty"`
	Wiring      bool     `yaml:"wiring,omitempty"`
	Clients     []string `yaml:"clients,omitempty"`
	Skills      []string `yaml:"skills,omitempty"`
	Agents      []string `yaml:"agents,omitempty"`
	// Rules lists context rule fragments imported from the pack repo.
	// Superseded by RuleFiles, which adds per-rule provenance; retained so
	// lockfiles written before that keep loading, and kept in sync on write
	// so a downgrade still sees the selection.
	Rules []string `yaml:"rules,omitempty"`
	// RuleFiles records per-rule provenance keyed by fragment name. An entry
	// with an empty ContentHash means provenance is unknown (migrated from a
	// Rules-only lockfile), and callers must fall back to byte comparison
	// rather than treating the empty hash as a match.
	RuleFiles map[string]LockedRule `yaml:"rule_files,omitempty"`
	// Unresolved lists manifest-selected names discovery could not find,
	// so status can keep reporting them until the upstream repo (or the
	// manifest) is fixed.
	Unresolved []string                             `yaml:"unresolved,omitempty"`
	Variables  map[string]LockedVariableDeclaration `yaml:"variables,omitempty"`
	// Stack records a pinned checkout of a pack-carried stack. Nil when
	// the manifest has no stack, or the stack did not resolve at import.
	Stack *LockedStack `yaml:"stack,omitempty"`
	// UnresolvedDetails maps an unresolved token (for example
	// "stack:stack.yaml") to the reason it did not resolve, so status can
	// show the export or escape error without re-reading the repository.
	// A non-empty map stamps lockVersionStack. An older reader has no
	// field for the key and would drop it on the next write.
	UnresolvedDetails map[string]string `yaml:"unresolved_details,omitempty"`
	// Sources records each external git source pinned at import. A
	// non-empty map stamps lockVersionPackSources. An older reader has no
	// field for the key and would drop it on the next write.
	Sources map[string]LockedPackSource `yaml:"sources,omitempty"`
}

// LockedPackSource is one external repository pinned by a pack import.
// Skills and Agents are the names that actually imported, not the
// manifest shorthand and not skipped or unresolved selections.
type LockedPackSource struct {
	// Repo is the authored URL, stored as written, matching the primary
	// pack source. Printed lines and source summaries redact userinfo.
	Repo      string    `yaml:"repo"`
	Ref       string    `yaml:"ref,omitempty"`
	Path      string    `yaml:"path,omitempty"`
	CommitSHA string    `yaml:"commit_sha,omitempty"`
	FetchedAt time.Time `yaml:"fetched_at,omitempty"`
	// SourceKey is the member lock source key (<pack>/<source>).
	SourceKey string   `yaml:"source_key,omitempty"`
	Skills    []string `yaml:"skills,omitempty"`
	Agents    []string `yaml:"agents,omitempty"`
}

// LockedStack is the pinned checkout of a pack-carried stack file.
type LockedStack struct {
	Path        string `yaml:"path"`
	Name        string `yaml:"name"`
	ContentHash string `yaml:"content_hash"`
	CheckoutDir string `yaml:"checkout_dir"`
}

// LockedVariableDeclaration persists one value-free pack prerequisite.
type LockedVariableDeclaration struct {
	Required    *bool  `yaml:"required,omitempty"`
	Secret      *bool  `yaml:"secret,omitempty"`
	Type        string `yaml:"type,omitempty"`
	Description string `yaml:"description,omitempty"`
	Docs        string `yaml:"docs,omitempty"`
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

// MemberSources returns the lock source keys imported as members of packName,
// sorted by key. A member source has Pack == nil, so FindPackSource does not
// return it.
func (lf *LockFile) MemberSources(packName string) []string {
	if lf == nil || packName == "" {
		return nil
	}
	var keys []string
	for name, src := range lf.Sources {
		if src.PackMember == packName {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// StoredAuth returns the authentication this source recorded at import.
func (s LockedSource) StoredAuth() StoredAuth {
	return StoredAuth{
		Method:        s.AuthMethod,
		SSHUser:       s.SSHUser,
		SSHKeyPath:    s.SSHKeyPath,
		CredentialRef: s.CredentialRef,
	}
}

// LockedSkill records per-skill metadata within a source.
type LockedSkill struct {
	Path        string `yaml:"path"`
	ContentHash string `yaml:"content_hash"`
	// TreeHash is the sha256:-prefixed hash of SKILL.md plus allowlisted
	// supporting files. Set only for local sources.
	TreeHash    string       `yaml:"tree_hash,omitempty"`
	Fingerprint *Fingerprint `yaml:"fingerprint,omitempty"`
}

// IsLocal reports whether this source is a local directory import.
func (s LockedSource) IsLocal() bool {
	return s.Kind == SourceKindLocal
}

// LockedAgent records per-agent metadata within a source.
type LockedAgent struct {
	Path        string `yaml:"path"`
	ContentHash string `yaml:"content_hash"`
}

// LockedRule records per-rule-fragment metadata within a pack source. The
// content hash is what lets a later install tell an upstream change apart
// from a local edit; without it the only available comparison is raw bytes
// against disk, which conflates the two.
type LockedRule struct {
	Path        string `yaml:"path"`
	ContentHash string `yaml:"content_hash"`
}

// LockFilePath returns the default path to skills.lock.yaml.
func LockFilePath() string {
	home, err := state.Home()
	if err != nil {
		// No resolvable home: return "" so callers fail on a missing
		// file instead of writing a relative path in the cwd.
		return ""
	}
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
	migrateRuleFiles(&lf)

	return &lf, nil
}

// migrateRuleFiles backfills RuleFiles from a Rules-only lockfile written
// before per-rule provenance existed. Entries get an empty ContentHash,
// which callers must read as "unknown" and handle by falling back to byte
// comparison — never as a hash that could match content.
func migrateRuleFiles(lf *LockFile) {
	for name, src := range lf.Sources {
		if src.Pack == nil || len(src.Pack.Rules) == 0 || src.Pack.RuleFiles != nil {
			continue
		}
		src.Pack.RuleFiles = make(map[string]LockedRule, len(src.Pack.Rules))
		for _, rule := range src.Pack.Rules {
			src.Pack.RuleFiles[rule] = LockedRule{}
		}
		lf.Sources[name] = src
	}
}

// WriteLockFile writes skills.lock.yaml atomically. Keys are sorted for
// minimal merge conflicts.
func WriteLockFile(path string, lf *LockFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating lock file directory: %w", err)
	}
	// Stamp the lowest version that can represent the file: the highest
	// version any source requires. A file with neither packs nor ssh-key
	// fields stays at 1. A stack record or an unresolved-detail map stamps
	// lockVersionStack so a reader that only understands SSH fields refuses
	// the file instead of dropping the new key.
	lf.Version = 1
	for _, src := range lf.Sources {
		if src.Pack != nil && lf.Version < lockVersionPacks {
			lf.Version = lockVersionPacks
		}
		if src.Pack != nil && len(src.Pack.Variables) > 0 && lf.Version < lockVersionVariables {
			lf.Version = lockVersionVariables
		}
		if src.SSHKeyPath != "" && lf.Version < lockVersionSSHAuth {
			lf.Version = lockVersionSSHAuth
		}
		if src.Pack != nil && lf.Version < lockVersionStack &&
			(src.Pack.Stack != nil || len(src.Pack.UnresolvedDetails) > 0) {
			lf.Version = lockVersionStack
		}
		if src.IsLocal() && lf.Version < lockVersionLocal {
			lf.Version = lockVersionLocal
		}
		if src.PackMember != "" && lf.Version < lockVersionPackSources {
			lf.Version = lockVersionPackSources
		}
		if src.Pack != nil && len(src.Pack.Sources) > 0 && lf.Version < lockVersionPackSources {
			lf.Version = lockVersionPackSources
		}
	}

	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling lock file: %w", err)
	}

	return atomicWriteBytes(path, data)
}

// MutateLockFile runs one read-modify-write cycle over skills.lock.yaml
// while holding the cross-process import lock, so concurrent operations
// serialize instead of losing each other's updates. fn returns whether
// its changes should be written; false skips the write and succeeds.
func MutateLockFile(ctx context.Context, path string, fn func(*LockFile) (bool, error)) error {
	return withLockFileFlock(ctx, path, func() error {
		lf, err := ReadLockFile(path)
		if err != nil {
			return err
		}
		write, err := fn(lf)
		if err != nil || !write {
			return err
		}
		return WriteLockFile(path, lf)
	})
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
				if src.IsLocal() {
					src.ContentHash = CombineTrackedSourceHash(src.Skills, src.Agents)
				}
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
				if src.IsLocal() {
					src.ContentHash = CombineTrackedSourceHash(src.Skills, src.Agents)
				}
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
