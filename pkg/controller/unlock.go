package controller

import (
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/vault"
)

// unlockFromEnvironment unlocks store from GRIDCTL_VAULT_PASSPHRASE, or
// from the file named by GRIDCTL_VAULT_PASSPHRASE_FILE. A locked store with
// neither variable set is not an error. The file is never created here, and
// errors do not include the passphrase.
func unlockFromEnvironment(store *vault.Store) error {
	if store == nil || !store.IsLocked() {
		return nil
	}
	if pass := os.Getenv("GRIDCTL_VAULT_PASSPHRASE"); pass != "" {
		if err := store.Unlock(pass); err != nil {
			return fmt.Errorf("unlocking vault: %w", err)
		}
		return nil
	}
	path := os.Getenv(vault.PassphraseFileEnv)
	if path == "" {
		return nil
	}
	pass, err := vault.ReadPassphraseFile(path)
	if err != nil {
		return err
	}
	if err := store.Unlock(pass); err != nil {
		return fmt.Errorf("unlocking vault from %s: passphrase rejected", vault.PassphraseFileEnv)
	}
	return nil
}
