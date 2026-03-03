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

// Store manages secrets in a JSON file with mutex-protected access.
type Store struct {
	baseDir    string
	mu         sync.RWMutex
	secrets    map[string]Secret
	sets       map[string]Set
	locked     bool   // true when secrets.enc exists and vault is not unlocked
	encrypted  bool   // true when vault was loaded from secrets.enc (re-encrypt on save)
	passphrase string // held in memory for re-encryption after modifications
}

// NewStore creates a vault store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir: baseDir,
		secrets: make(map[string]Secret),
		sets:    make(map[string]Set),
	}
}

// Load reads secrets into memory. Checks for secrets.enc first (encrypted),
// then falls back to secrets.json (plaintext). If the file doesn't exist, starts empty.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.secrets = make(map[string]Secret)
	s.sets = make(map[string]Set)
	s.locked = false
	s.encrypted = false

	// Check for encrypted vault first
	encPath := s.encryptedPath()
	if _, err := os.Stat(encPath); err == nil {
		s.locked = true
		s.encrypted = true
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

	return s.parseSecretsData(data)
}

// parseSecretsData parses secrets JSON into the in-memory maps.
func (s *Store) parseSecretsData(data []byte) error {
	// Try new format first (object with secrets and sets)
	var sd storeData
	if err := json.Unmarshal(data, &sd); err == nil && (sd.Secrets != nil || sd.Sets != nil) {
		for _, sec := range sd.Secrets {
			s.secrets[sec.Key] = sec
		}
		for _, set := range sd.Sets {
			s.sets[set.Name] = set
		}
		return nil
	}

	// Fall back to legacy flat array format
	var secrets []Secret
	if err := json.Unmarshal(data, &secrets); err != nil {
		return fmt.Errorf("parsing vault: %w", err)
	}

	for _, sec := range secrets {
		s.secrets[sec.Key] = sec
	}
	return nil
}

// Get returns a secret value and whether it exists.
func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sec, ok := s.secrets[key]
	if !ok {
		return "", false
	}
	return sec.Value, true
}

// Set adds or updates a secret. Creates the vault directory on first write.
func (s *Store) Set(key, value string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		existing, ok := s.secrets[key]
		if ok {
			existing.Value = value
			s.secrets[key] = existing
		} else {
			s.secrets[key] = Secret{Key: key, Value: value}
		}
		return s.saveLocked()
	})
}

// SetWithSet adds or updates a secret and assigns it to a set.
func (s *Store) SetWithSet(key, value, setName string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		s.secrets[key] = Secret{Key: key, Value: value, Set: setName}
		// Auto-create set if it doesn't exist
		if setName != "" {
			if _, exists := s.sets[setName]; !exists {
				s.sets[setName] = Set{Name: setName}
			}
		}
		return s.saveLocked()
	})
}

// Delete removes a secret by key.
func (s *Store) Delete(key string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		if _, ok := s.secrets[key]; !ok {
			return fmt.Errorf("secret %q not found", key)
		}

		delete(s.secrets, key)
		return s.saveLocked()
	})
}

// List returns all secrets sorted by key.
func (s *Store) List() []Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Secret, 0, len(s.secrets))
	for _, sec := range s.secrets {
		result = append(result, sec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

// Import bulk-imports secrets. Returns the count of keys imported.
func (s *Store) Import(secrets map[string]string) (int, error) {
	var count int
	err := state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		for k, v := range secrets {
			s.secrets[k] = Secret{Key: k, Value: v}
			count++
		}
		return s.saveLocked()
	})
	return count, err
}

// Export returns all secrets as a key-value map.
func (s *Store) Export() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string, len(s.secrets))
	for k, sec := range s.secrets {
		result[k] = sec.Value
	}
	return result
}

// Keys returns sorted key names only.
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Has checks existence without returning the value.
func (s *Store) Has(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.secrets[key]
	return ok
}

// Values returns all secret values (for redaction registration).
func (s *Store) Values() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vals := make([]string, 0, len(s.secrets))
	for _, sec := range s.secrets {
		if sec.Value != "" {
			vals = append(vals, sec.Value)
		}
	}
	return vals
}

// SetSecretSet assigns a secret to a set (or unassigns if setName is empty).
func (s *Store) SetSecretSet(key, setName string) error {
	return state.WithLock("vault", 5*time.Second, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		if err := s.loadLocked(); err != nil {
			return err
		}

		sec, ok := s.secrets[key]
		if !ok {
			return fmt.Errorf("secret %q not found", key)
		}

		sec.Set = setName
		s.secrets[key] = sec

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
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Count members per set
	counts := make(map[string]int)
	for _, sec := range s.secrets {
		if sec.Set != "" {
			counts[sec.Set]++
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

// DeleteSet removes a set. Secrets in the set are unassigned but not deleted.
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

		// Unassign secrets from this set
		for k, sec := range s.secrets {
			if sec.Set == name {
				sec.Set = ""
				s.secrets[k] = sec
			}
		}

		delete(s.sets, name)
		return s.saveLocked()
	})
}

// GetSetSecrets returns all secrets belonging to a set, sorted by key.
func (s *Store) GetSetSecrets(setName string) []Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Secret
	for _, sec := range s.secrets {
		if sec.Set == setName {
			result = append(result, sec)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

// IsLocked returns true when the vault is encrypted and not yet unlocked.
func (s *Store) IsLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.locked
}

// IsEncrypted returns true when the vault has an encrypted backing file.
func (s *Store) IsEncrypted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

		data, err := s.serializeSecrets()
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

		s.secrets = make(map[string]Secret)
		s.sets = make(map[string]Set)
		if err := s.parseSecretsData(plaintext); err != nil {
			return err
		}

		s.locked = false
		s.passphrase = passphrase
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
		return nil
	})
}

// secretsPath returns the path to the secrets JSON file.
func (s *Store) secretsPath() string {
	return filepath.Join(s.baseDir, "secrets.json")
}

// encryptedPath returns the path to the encrypted vault file.
func (s *Store) encryptedPath() string {
	return filepath.Join(s.baseDir, "secrets.enc")
}

// loadLocked reads from disk without acquiring the mutex (caller must hold it).
// Supports plaintext, legacy, and encrypted formats.
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

		s.secrets = make(map[string]Secret)
		s.sets = make(map[string]Set)
		return s.parseSecretsData(plaintext)
	}

	path := s.secretsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading vault: %w", err)
	}

	s.secrets = make(map[string]Secret)
	s.sets = make(map[string]Set)
	return s.parseSecretsData(data)
}

// serializeSecrets returns the current secrets as JSON.
func (s *Store) serializeSecrets() ([]byte, error) {
	secrets := make([]Secret, 0, len(s.secrets))
	for _, sec := range s.secrets {
		secrets = append(secrets, sec)
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].Key < secrets[j].Key })

	sets := make([]Set, 0, len(s.sets))
	for _, set := range s.sets {
		sets = append(sets, set)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].Name < sets[j].Name })

	sd := storeData{Secrets: secrets, Sets: sets}
	return json.MarshalIndent(sd, "", "  ")
}

// saveLocked writes secrets to disk atomically. Creates directory on first write.
// If the vault is encrypted, re-encrypts and writes to secrets.enc.
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(s.baseDir, 0700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	data, err := s.serializeSecrets()
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

		return atomicWrite(s.encryptedPath(), encData, 0600)
	}

	return atomicWrite(s.secretsPath(), data, 0600)
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
