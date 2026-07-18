package mcpauth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	keychainService = "gridctl-oauth-store"
	keychainAccount = "gridctl"
	machineKeyLen   = 32
)

// loadOrCreateMachineKey resolves the 32-byte key that seals the token
// store. On darwin the login Keychain is preferred (via the security CLI);
// anywhere else, or when the Keychain is unavailable, a 0600 keyfile inside
// the 0700 oauth state dir is used. The key is random and machine-local:
// no passphrase is involved, so the daemon reads tokens across restarts
// without an unlock step.
func loadOrCreateMachineKey(dir string, useKeychain bool) ([]byte, error) {
	if useKeychain && runtime.GOOS == "darwin" {
		if key, err := keychainMachineKey(); err == nil {
			return key, nil
		}
		// Keychain unavailable (headless session, locked keychain, missing
		// binary): fall through to the keyfile rather than failing.
	}
	return fileMachineKey(filepath.Join(dir, "key"))
}

// keychainMachineKey loads the key from the darwin login Keychain,
// generating and storing a fresh one on first use.
func keychainMachineKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", keychainAccount, "-w").Output()
	if err == nil {
		key, decErr := hex.DecodeString(strings.TrimSpace(string(out)))
		if decErr == nil && len(key) == machineKeyLen {
			return key, nil
		}
		return nil, fmt.Errorf("keychain entry is malformed")
	}

	key := make([]byte, machineKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating machine key: %w", err)
	}
	// -U updates an existing item so a malformed entry does not wedge us.
	if err := exec.Command("security", "add-generic-password",
		"-s", keychainService, "-a", keychainAccount,
		"-w", hex.EncodeToString(key), "-U").Run(); err != nil {
		return nil, fmt.Errorf("storing machine key in keychain: %w", err)
	}
	return key, nil
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
