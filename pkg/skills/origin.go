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
	Repo        string    `json:"repo"`
	Ref         string    `json:"ref"`
	Path        string    `json:"path,omitempty"`
	CommitSHA   string    `json:"commitSha"`
	ImportedAt  time.Time `json:"importedAt"`
	ContentHash string    `json:"contentHash"`
	// InstalledHash is the SHA-256 of the SKILL.md as written to disk
	// immediately after the last import. DetectDrift compares the current
	// on-disk hash against this to surface local user edits. Distinct from
	// ContentHash, which records the upstream file as fetched (and which
	// diverges from the installed file because of frontmatter render
	// normalization and state injection).
	InstalledHash string       `json:"installedHash,omitempty"`
	Fingerprint   *Fingerprint `json:"fingerprint,omitempty"`
	// SupportingFilesInstalled records that the importer evaluated and
	// installed the complete allowlisted package tree, even when it was empty.
	// Its absence identifies imports created before supporting-file installs.
	SupportingFilesInstalled bool `json:"supportingFilesInstalled,omitempty"`
	// CredentialRef is an opaque reference like "${vault:GIT_TOKEN}" used to
	// re-resolve credentials on skill update. Raw token values are never
	// persisted, only the reference string.
	CredentialRef string `json:"credentialRef,omitempty"`
	// AuthMethod, SSHUser, and SSHKeyPath persist ssh-key authentication so a
	// later update can rebuild it. Only the path is stored, never key material
	// or a passphrase.
	AuthMethod string `json:"authMethod,omitempty"`
	SSHUser    string `json:"sshUser,omitempty"`
	SSHKeyPath string `json:"sshKeyPath,omitempty"`
}

// StoredAuth is the non-secret authentication recorded for a source.
// Token values and passphrases are never part of it.
type StoredAuth struct {
	Method        string
	SSHUser       string
	SSHKeyPath    string
	CredentialRef string
}

// StoredAuth returns the authentication this origin recorded at import.
func (o *Origin) StoredAuth() StoredAuth {
	if o == nil {
		return StoredAuth{}
	}
	return StoredAuth{
		Method:        o.AuthMethod,
		SSHUser:       o.SSHUser,
		SSHKeyPath:    o.SSHKeyPath,
		CredentialRef: o.CredentialRef,
	}
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
