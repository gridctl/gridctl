package mcpauth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const machineKeyLen = 32

// loadOrCreateMachineKey resolves the 32-byte key that seals the token
// store: a 0600 keyfile inside the 0700 oauth state dir, generated on
// first use. The key is random and machine-local; no passphrase is
// involved, so the daemon reads tokens across restarts without an unlock
// step.
//
// Encryption with an adjacent keyfile protects the tokens file when it
// leaks on its own (backups, copy-paste, log bundles), not against an
// attacker who can read the whole directory. darwin Keychain integration
// was evaluated and deferred: the security CLI can hang a background
// daemon on keychain authorization, and daemon startup must never block
// on a GUI prompt.
func loadOrCreateMachineKey(dir string) ([]byte, error) {
	return fileMachineKey(filepath.Join(dir, "key"))
}

// fileMachineKey loads the key from a 0600 keyfile, creating it on first use.
func fileMachineKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		key, decErr := hex.DecodeString(strings.TrimSpace(string(raw)))
		if decErr == nil && len(key) == machineKeyLen {
			return key, nil
		}
		return nil, fmt.Errorf("machine keyfile %s is malformed", path)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading machine keyfile: %w", err)
	}

	key := make([]byte, machineKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating machine key: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("writing machine keyfile: %w", err)
	}
	return key, nil
}
