package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// Store manages variables in a JSON file with mutex-protected access.
// It serves both secrets (IsSecret=true, redacted in logs) and non-sensitive
// configuration (IsSecret=false, plaintext in logs).
type Store struct {
	baseDir    string
	mu         sync.RWMutex
	variables  map[string]Variable
	sets       map[string]Set
	locked     bool   // true when secrets.enc exists and vault is not unlocked
	encrypted  bool   // true when vault was loaded from secrets.enc (re-encrypt on save)
	passphrase string // held in memory for re-encryption after modifications
	// Cached mtime/size of the backing file; gates reload-on-read so external
	// writes (e.g. CLI vault commands while the daemon is up) are picked up.
	mtime time.Time
	size  int64
}

// NewStore creates a variable store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir:   baseDir,
		variables: make(map[string]Variable),
		sets:      make(map[string]Set),
	}
}

// Load reads variables into memory. Checks for secrets.enc first (encrypted),
// then falls back to secrets.json (plaintext). If the file doesn't exist, starts empty.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.variables = make(map[string]Variable)
	s.sets = make(map[string]Set)
	s.locked = false
	s.encrypted = false
	s.mtime = time.Time{}
	s.size = 0

	// Check for encrypted vault first
	encPath := s.encryptedPath()
	if info, err := os.Stat(encPath); err == nil {
		s.locked = true
		s.encrypted = true
		s.mtime = info.ModTime()
		s.size = info.Size()
		return nil
	}

	path := s.secretsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading vault: %w", err)
	}

	variables, sets, err := parseSecretsData(data)
	if err != nil {
		return err
	}
	s.variables = variables
	s.sets = sets
	s.stampMtimeLocked()
	return nil
}

// parseSecretsData parses on-disk data and returns fresh maps. It accepts
// three formats and migrates legacy shapes in-memory; saves always rewrite
// the file as v2.
//
//   - v2: {"version": 2, "variables": [...], "sets": [...]}
//   - v1: {"secrets": [...], "sets": [...]}                       — pre-rename
//   - v0: [{"key", "value", "set"}]                                — pre-sets
//
// Returning rather than mutating lets callers swap state in atomically — a
// parse error must never wipe the live in-memory variables.
func parseSecretsData(data []byte) (map[string]Variable, map[string]Set, error) {
	variables := make(map[string]Variable)
	sets := make(map[string]Set)

	// Peek at the top-level JSON shape: array → v0, object → v1 or v2.
	trimmed := skipJSONWhitespace(data)
	switch {
	case len(trimmed) == 0:
		// Empty file is a valid empty store.
		return variables, sets, nil
	case trimmed[0] == '[':
		// v0: legacy flat array of secrets, no sets, no metadata.
		var legacy []legacySecret
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, nil, fmt.Errorf("parsing vault (v0): %w", err)
		}
		for _, sec := range legacy {
			variables[sec.Key] = Variable{
				Key:      sec.Key,
				Value:    sec.Value,
				Set:      sec.Set,
				Type:     TypeString,
				IsSecret: true,
			}
		}
		return variables, sets, nil
	case trimmed[0] == '{':
		// v1 or v2: try v2 first by checking for "version" / "variables" keys.
		var probe struct {
			Version   *int              `json:"version"`
			Variables []json.RawMessage `json:"variables"`
			Secrets   []json.RawMessage `json:"secrets"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			return nil, nil, fmt.Errorf("parsing vault: %w", err)
		}

		if probe.Version != nil || probe.Variables != nil {
			var sd storeData
			if err := json.Unmarshal(data, &sd); err != nil {
				return nil, nil, fmt.Errorf("parsing vault (v2): %w", err)
			}
			for _, v := range sd.Variables {
				if v.Type == "" {
					v.Type = TypeString
				}
				variables[v.Key] = v
			}
			for _, set := range sd.Sets {
				sets[set.Name] = set
			}
			return variables, sets, nil
		}

		// v1: object with "secrets" key (and optional "sets").
		var v1 struct {
			Secrets []legacySecret `json:"secrets"`
			Sets    []Set          `json:"sets"`
		}
		if err := json.Unmarshal(data, &v1); err != nil {
			return nil, nil, fmt.Errorf("parsing vault (v1): %w", err)
		}
		for _, sec := range v1.Secrets {
			variables[sec.Key] = Variable{
				Key:      sec.Key,
				Value:    sec.Value,
				Set:      sec.Set,
				Type:     TypeString,
				IsSecret: true,
			}
		}
		for _, set := range v1.Sets {
			sets[set.Name] = set
		}
		return variables, sets, nil
	default:
		return nil, nil, fmt.Errorf("parsing vault: unexpected JSON shape")
	}
}

// legacySecret matches the pre-rename Secret struct shape so v0/v1 files
// (which used "key"/"value"/"set" without type or sensitivity metadata) can
// decode without redefining the wire format.
type legacySecret struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Set   string `json:"set,omitempty"`
}

// skipJSONWhitespace returns the input with leading JSON whitespace (space,
// tab, CR, LF) stripped. Used to inspect the first non-whitespace byte.
func skipJSONWhitespace(b []byte) []byte {
	for i, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b[i:]
		}
	}
	return nil
}

// Get returns a variable's value and whether it exists. Read methods take the
// write lock because reloadIfChanged may mutate state.
func (s *Store) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	v, ok := s.variables[key]
	if !ok {
		return "", false
	}
	return v.Value, true
}

// GetVariable returns the full variable record (including type and sensitivity)
// and whether it exists.
func (s *Store) GetVariable(key string) (Variable, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	v, ok := s.variables[key]
	return v, ok
}

// Set adds or updates a variable's value. For existing keys, the previous
// Set, Type, and IsSecret are preserved (this is the historic Set behaviour
// from the secrets-only era and many callers rely on it). For new keys the
// secure default (IsSecret=true, Type=string) applies (Article XII).
func (s *Store) Set(key, value string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		if existing, ok := s.variables[key]; ok {
			existing.Value = value
			s.variables[key] = existing
		} else {
			s.variables[key] = Variable{
				Key: key, Value: value, Type: TypeString, IsSecret: true,
			}
		}
		return s.saveLocked()
	})
}

// SetWithSet adds or updates a variable and assigns it to a set. Preserves
// existing Type/IsSecret metadata for known keys; new keys default to
// secret/string.
func (s *Store) SetWithSet(key, value, setName string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		if existing, ok := s.variables[key]; ok {
			existing.Value = value
			existing.Set = setName
			s.variables[key] = existing
		} else {
			s.variables[key] = Variable{
				Key: key, Value: value, Set: setName,
				Type: TypeString, IsSecret: true,
			}
		}
		if setName != "" {
			if _, exists := s.sets[setName]; !exists {
				s.sets[setName] = Set{Name: setName}
			}
		}
		return s.saveLocked()
	})
}

// SetVariable adds or updates a variable with full metadata control. All
// fields are taken from v; existing entries are fully replaced. Used by the
// CLI/API when callers want to thread type and sensitivity explicitly.
// Auto-creates referenced sets so callers don't need a separate CreateSet.
func (s *Store) SetVariable(v Variable) error {
	if v.Key == "" {
		return fmt.Errorf("variable key cannot be empty")
	}
	if v.Type == "" {
		v.Type = TypeString
	}
	if !IsValidType(v.Type) {
		return fmt.Errorf("invalid variable type: %q", v.Type)
	}

	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		s.variables[v.Key] = v
		if v.Set != "" {
			if _, exists := s.sets[v.Set]; !exists {
				s.sets[v.Set] = Set{Name: v.Set}
			}
		}
		return s.saveLocked()
	})
}

// Delete removes a variable by key.
func (s *Store) Delete(key string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		if _, ok := s.variables[key]; !ok {
			return fmt.Errorf("variable %q not found", key)
		}

		delete(s.variables, key)
		return s.saveLocked()
	})
}

// List returns all variables sorted by key.
func (s *Store) List() []Variable {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	result := make([]Variable, 0, len(s.variables))
	for _, v := range s.variables {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

// Import bulk-imports variables. Plain-map imports default to secret/string
// (Article XII secure default); callers needing per-key type or visibility
// should use ImportVariables.
func (s *Store) Import(secrets map[string]string) (int, error) {
	vars := make([]Variable, 0, len(secrets))
	for k, v := range secrets {
		vars = append(vars, Variable{Key: k, Value: v, Type: TypeString, IsSecret: true})
	}
	return s.ImportVariables(vars)
}

// ImportVariables bulk-imports variables, preserving per-entry metadata.
// Returns the count of keys imported.
func (s *Store) ImportVariables(vars []Variable) (int, error) {
	var count int
	err := state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		for _, v := range vars {
			if v.Key == "" {
				continue
			}
			if v.Type == "" {
				v.Type = TypeString
			}
			if !IsValidType(v.Type) {
				return fmt.Errorf("invalid variable type %q for key %q", v.Type, v.Key)
			}
			s.variables[v.Key] = v
			if v.Set != "" {
				if _, exists := s.sets[v.Set]; !exists {
					s.sets[v.Set] = Set{Name: v.Set}
				}
			}
			count++
		}
		return s.saveLocked()
	})
	return count, err
}

// Export returns all variables as a key-value map.
func (s *Store) Export() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	result := make(map[string]string, len(s.variables))
	for k, v := range s.variables {
		result[k] = v.Value
	}
	return result
}

// Keys returns sorted key names only.
func (s *Store) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	keys := make([]string, 0, len(s.variables))
	for k := range s.variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Has checks existence without returning the value.
func (s *Store) Has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()
	_, ok := s.variables[key]
	return ok
}

// Values returns variable values flagged as secret (IsSecret=true), used to
// seed log redaction. Plaintext variables are intentionally excluded so
// non-sensitive values like REGION=us-east-1 stay legible in logs.
func (s *Store) Values() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	vals := make([]string, 0, len(s.variables))
	for _, v := range s.variables {
		if v.IsSecret && v.Value != "" {
			vals = append(vals, v.Value)
		}
	}
	return vals
}

// SetSecretSet assigns a variable to a set (or unassigns if setName is empty).
func (s *Store) SetSecretSet(key, setName string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		v, ok := s.variables[key]
		if !ok {
			return fmt.Errorf("variable %q not found", key)
		}

		v.Set = setName
		s.variables[key] = v

		// Auto-create set if it doesn't exist
		if setName != "" {
			if _, exists := s.sets[setName]; !exists {
				s.sets[setName] = Set{Name: setName}
			}
		}

		return s.saveLocked()
	})
}

// ListSets returns all sets with their member counts.
func (s *Store) ListSets() []SetSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	// Count members per set
	counts := make(map[string]int)
	for _, v := range s.variables {
		if v.Set != "" {
			counts[v.Set]++
		}
	}

	result := make([]SetSummary, 0, len(s.sets))
	for _, set := range s.sets {
		result = append(result, SetSummary{
			Name:        set.Name,
			Description: set.Description,
			Count:       counts[set.Name],
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// CreateSet creates an empty variable set.
func (s *Store) CreateSet(name string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		if _, exists := s.sets[name]; exists {
			return fmt.Errorf("set %q already exists", name)
		}

		s.sets[name] = Set{Name: name}
		return s.saveLocked()
	})
}

// DeleteSet removes a set. Variables in the set are unassigned but not deleted.
func (s *Store) DeleteSet(name string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		if _, exists := s.sets[name]; !exists {
			return fmt.Errorf("set %q not found", name)
		}

		// Unassign variables from this set
		for k, v := range s.variables {
			if v.Set == name {
				v.Set = ""
				s.variables[k] = v
			}
		}

		delete(s.sets, name)
		return s.saveLocked()
	})
}

// GetSetSecrets returns all variables belonging to a set, sorted by key.
// Retains its historic name (set-based "secrets" lookup) for backward
// compatibility with callers; the unified store treats plaintext and secret
// variables identically when grouping by set.
func (s *Store) GetSetSecrets(setName string) []Variable {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()

	var result []Variable
	for _, v := range s.variables {
		if v.Set == setName {
			result = append(result, v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

// IsLocked returns true when the vault is encrypted and not yet unlocked.
// Takes the write lock so reloadIfChanged can pick up external lock/unlock
// transitions before reporting state.
func (s *Store) IsLocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()
	return s.locked
}

// IsEncrypted returns true when the vault has an encrypted backing file.
// Takes the write lock so reloadIfChanged can pick up external lock/unlock
// transitions before reporting state.
func (s *Store) IsEncrypted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadIfChanged()
	return s.encrypted
}

// Lock encrypts the vault with a passphrase.
func (s *Store) Lock(passphrase string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		data, err := s.serializeVariables()
		if err != nil {
			return err
		}

		ev, err := LockVault(data, passphrase)
		if err != nil {
			return fmt.Errorf("encrypting vault: %w", err)
		}

		encData, err := marshalEncryptedVault(ev)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(s.baseDir, 0700); err != nil {
			return fmt.Errorf("creating vault directory: %w", err)
		}

		if err := atomicWrite(s.encryptedPath(), encData, 0600); err != nil {
			return err
		}

		// Remove plaintext file
		_ = os.Remove(s.secretsPath())

		s.encrypted = true
		s.locked = false
		s.passphrase = passphrase
		s.stampMtimeLocked()
		return nil
	})
}

// Unlock decrypts an encrypted vault into memory.
func (s *Store) Unlock(passphrase string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if !s.locked {
			return nil
		}

		data, err := os.ReadFile(s.encryptedPath())
		if err != nil {
			return fmt.Errorf("reading encrypted vault: %w", err)
		}

		ev, err := unmarshalEncryptedVault(data)
		if err != nil {
			return err
		}

		plaintext, err := UnlockVault(ev, passphrase)
		if err != nil {
			return err
		}

		variables, sets, err := parseSecretsData(plaintext)
		if err != nil {
			return err
		}
		s.variables = variables
		s.sets = sets
		s.locked = false
		s.passphrase = passphrase
		s.stampMtimeLocked()
		return nil
	})
}

// ChangePassphrase re-encrypts the DEK with a new passphrase.
func (s *Store) ChangePassphrase(oldPass, newPass string) error {
	if !s.encrypted {
		return fmt.Errorf("vault is not encrypted")
	}

	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		data, err := os.ReadFile(s.encryptedPath())
		if err != nil {
			return fmt.Errorf("reading encrypted vault: %w", err)
		}

		ev, err := unmarshalEncryptedVault(data)
		if err != nil {
			return err
		}

		changed, err := ChangePassphrase(ev, oldPass, newPass)
		if err != nil {
			return err
		}

		encData, err := marshalEncryptedVault(changed)
		if err != nil {
			return err
		}

		if err := atomicWrite(s.encryptedPath(), encData, 0600); err != nil {
			return err
		}

		s.passphrase = newPass
		s.stampMtimeLocked()
		return nil
	})
}

// secretsPath returns the path to the variables JSON file. The filename
// stays "secrets.json" for backward compatibility with v0/v1 deployments;
// the rename is at the API/CLI/Go-struct layer, not the on-disk filename.
func (s *Store) secretsPath() string {
	return filepath.Join(s.baseDir, "secrets.json")
}

// encryptedPath returns the path to the encrypted vault file.
func (s *Store) encryptedPath() string {
	return filepath.Join(s.baseDir, "secrets.enc")
}

// loadLocked reads from disk without acquiring the mutex (caller must hold it).
// Supports plaintext, legacy, and encrypted formats. Updates s.mtime/s.size
// on success so subsequent reloadIfChanged calls have a fresh baseline.
func (s *Store) loadLocked() error {
	// If encrypted and unlocked, read from encrypted file
	if s.encrypted && !s.locked && s.passphrase != "" {
		data, err := os.ReadFile(s.encryptedPath())
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("reading encrypted vault: %w", err)
		}

		ev, err := unmarshalEncryptedVault(data)
		if err != nil {
			return err
		}

		plaintext, err := UnlockVault(ev, s.passphrase)
		if err != nil {
			return err
		}

		variables, sets, err := parseSecretsData(plaintext)
		if err != nil {
			return err
		}
		s.variables = variables
		s.sets = sets
		s.stampMtimeLocked()
		return nil
	}

	path := s.secretsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading vault: %w", err)
	}

	variables, sets, err := parseSecretsData(data)
	if err != nil {
		return err
	}
	s.variables = variables
	s.sets = sets
	s.stampMtimeLocked()
	return nil
}

// activePathLocked returns the backing file path that matches the current
// encryption state. Caller must hold the mutex.
func (s *Store) activePathLocked() string {
	if s.encrypted {
		return s.encryptedPath()
	}
	return s.secretsPath()
}

// stampMtimeLocked records the current mtime and size of the active backing
// file. A failed stat (e.g., file not yet written) leaves the baseline cleared
// so the next reloadIfChanged is a no-op rather than a spurious reload.
// Caller must hold the mutex.
func (s *Store) stampMtimeLocked() {
	info, err := os.Stat(s.activePathLocked())
	if err != nil {
		s.mtime = time.Time{}
		s.size = 0
		return
	}
	s.mtime = info.ModTime()
	s.size = info.Size()
}

// reloadIfChanged refreshes in-memory state to match the current shape of
// the vault on disk. It detects three kinds of change in addition to plain
// content edits: plaintext → encrypted (CLI lock while daemon is up),
// encrypted → plaintext (manual restore or future "decrypt" command), and
// same-file rewrites. When neither backing file exists, in-memory state is
// preserved so an in-flight write (window between rename and stat) doesn't
// surface as silent data loss. The caller must hold the write lock.
func (s *Store) reloadIfChanged() error {
	// secrets.enc takes precedence (matches Load()'s file ordering).
	if info, err := os.Stat(s.encryptedPath()); err == nil {
		if !s.encrypted {
			// Plaintext → encrypted transition. The daemon doesn't know the
			// new passphrase, so drop plaintext from memory and report
			// locked. Done as one uninterrupted sequence so a panic between
			// flag flip and map clear can't leave plaintext readable.
			s.encrypted = true
			s.locked = true
			s.passphrase = ""
			s.variables = make(map[string]Variable)
			s.sets = make(map[string]Set)
			s.mtime = info.ModTime()
			s.size = info.Size()
			return nil
		}
		if s.locked {
			return nil
		}
		// Size is a fallback for filesystems with coarse mtime resolution
		// where a same-second rewrite could share an mtime with the prior
		// file.
		if !info.ModTime().After(s.mtime) && info.Size() == s.size {
			return nil
		}
		return s.loadLocked()
	}
	if info, err := os.Stat(s.secretsPath()); err == nil {
		if s.encrypted {
			// Encrypted → plaintext transition. Toggle flags first so
			// loadLocked reads the plaintext path; on parse failure the
			// atomic swap inside parseSecretsData leaves in-memory variables
			// intact (they were already empty if the daemon was locked).
			s.encrypted = false
			s.locked = false
			s.passphrase = ""
			return s.loadLocked()
		}
		if !info.ModTime().After(s.mtime) && info.Size() == s.size {
			return nil
		}
		return s.loadLocked()
	}
	return nil
}

// serializeVariables returns the current variables as JSON in the v2 shape.
func (s *Store) serializeVariables() ([]byte, error) {
	variables := make([]Variable, 0, len(s.variables))
	for _, v := range s.variables {
		if v.Type == "" {
			v.Type = TypeString
		}
		variables = append(variables, v)
	}
	sort.Slice(variables, func(i, j int) bool { return variables[i].Key < variables[j].Key })

	sets := make([]Set, 0, len(s.sets))
	for _, set := range s.sets {
		sets = append(sets, set)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].Name < sets[j].Name })

	sd := storeData{Version: CurrentStoreVersion, Variables: variables, Sets: sets}
	return json.MarshalIndent(sd, "", "  ")
}

// saveLocked writes variables to disk atomically. Creates directory on first
// write. If the vault is encrypted, re-encrypts and writes to secrets.enc.
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(s.baseDir, 0700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	data, err := s.serializeVariables()
	if err != nil {
		return fmt.Errorf("marshaling vault: %w", err)
	}

	// Re-encrypt if vault is in encrypted mode
	if s.encrypted && s.passphrase != "" {
		ev, err := LockVault(data, s.passphrase)
		if err != nil {
			return fmt.Errorf("re-encrypting vault: %w", err)
		}

		encData, err := marshalEncryptedVault(ev)
		if err != nil {
			return err
		}

		if err := atomicWrite(s.encryptedPath(), encData, 0600); err != nil {
			return err
		}
		s.stampMtimeLocked()
		return nil
	}

	if err := atomicWrite(s.secretsPath(), data, 0600); err != nil {
		return err
	}
	s.stampMtimeLocked()
	return nil
}

// atomicWrite writes data to path via temp file + rename.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}
