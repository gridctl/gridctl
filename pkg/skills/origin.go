package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Origin tracks the remote source of an imported skill.
// Stored as .origin.json alongside the SKILL.md file.
type Origin struct {
	Repo        string       `json:"repo"`
	Ref         string       `json:"ref"`
	Path        string       `json:"path,omitempty"`
	CommitSHA   string       `json:"commitSha"`
	ImportedAt  time.Time    `json:"importedAt"`
	ContentHash string       `json:"contentHash"`
	Fingerprint *Fingerprint `json:"fingerprint,omitempty"`
	// CredentialRef is an opaque reference like "${vault:GIT_TOKEN}" used to
	// re-resolve credentials on skill update. Raw token values are never
	// persisted — only the reference string.
	CredentialRef string `json:"credentialRef,omitempty"`
}

const originFileName = ".origin.json"

// ReadOrigin reads the .origin.json file from a skill directory.
func ReadOrigin(skillDir string) (*Origin, error) {
	path := filepath.Join(skillDir, originFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading origin file: %w", err)
	}

	var origin Origin
	if err := json.Unmarshal(data, &origin); err != nil {
		return nil, fmt.Errorf("parsing origin file: %w", err)
	}

	return &origin, nil
}

// WriteOrigin writes the .origin.json file to a skill directory.
func WriteOrigin(skillDir string, origin *Origin) error {
	data, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling origin: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(skillDir, originFileName)
	return atomicWriteBytes(path, data)
}

// DeleteOrigin removes the .origin.json file from a skill directory.
func DeleteOrigin(skillDir string) error {
	path := filepath.Join(skillDir, originFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting origin file: %w", err)
	}
	return nil
}

// HasOrigin checks if a skill directory has an .origin.json file.
func HasOrigin(skillDir string) bool {
	path := filepath.Join(skillDir, originFileName)
	_, err := os.Stat(path)
	return err == nil
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
