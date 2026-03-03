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
	baseDir string
	mu      sync.RWMutex
	secrets map[string]Secret
}

// NewStore creates a vault store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{
		baseDir: baseDir,
		secrets: make(map[string]Secret),
	}
}

// Load reads secrets.json into memory. If the file doesn't exist, starts empty.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.secrets = make(map[string]Secret)

	path := s.secretsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading vault: %w", err)
	}

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

		// Reload from disk inside lock to avoid clobbering concurrent writes
		if err := s.loadLocked(); err != nil {
			return err
		}

		s.secrets[key] = Secret{Key: key, Value: value}
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

// secretsPath returns the path to the secrets JSON file.
func (s *Store) secretsPath() string {
	return filepath.Join(s.baseDir, "secrets.json")
}

// loadLocked reads from disk without acquiring the mutex (caller must hold it).
func (s *Store) loadLocked() error {
	path := s.secretsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading vault: %w", err)
	}

	var secrets []Secret
	if err := json.Unmarshal(data, &secrets); err != nil {
		return fmt.Errorf("parsing vault: %w", err)
	}

	s.secrets = make(map[string]Secret, len(secrets))
	for _, sec := range secrets {
		s.secrets[sec.Key] = sec
	}
	return nil
}

// saveLocked writes secrets to disk atomically. Creates directory on first write.
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(s.baseDir, 0700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	// Sort for deterministic output
	secrets := make([]Secret, 0, len(s.secrets))
	for _, sec := range s.secrets {
		secrets = append(secrets, sec)
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].Key < secrets[j].Key })

	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling vault: %w", err)
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
