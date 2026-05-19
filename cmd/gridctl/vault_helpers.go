package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// loadVault opens the gridctl variable store. The name is retained from the
// secrets-only era because callers across the CLI surface still refer to
// "the vault" — the on-disk file is still secrets.json / secrets.enc and the
// store struct is still vault.Store. The "vault → var" rename is at the user
// surface (commands, YAML syntax, web routes), not the internal naming.
func loadVault() (*vault.Store, error) {
	store := vault.NewStore(state.VaultDir())
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("loading variable store: %w", err)
	}
	return store, nil
}

// ensureUnlocked prompts for the passphrase when the store is locked.
func ensureUnlocked(store *vault.Store) error {
	if !store.IsLocked() {
		return nil
	}

	pass := os.Getenv("GRIDCTL_VAULT_PASSPHRASE")
	if pass == "" {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("vault is locked. Set GRIDCTL_VAULT_PASSPHRASE or run 'gridctl var unlock'")
		}
		fmt.Print("Vault passphrase: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("reading passphrase: %w", err)
		}
		pass = string(raw)
	}

	return store.Unlock(pass)
}

// promptPassphrase reads a hidden passphrase from the terminal.
func promptPassphrase(prompt string) (string, error) {
	pass := os.Getenv("GRIDCTL_VAULT_PASSPHRASE")
	if pass != "" {
		return pass, nil
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return "", fmt.Errorf("interactive input required. Set GRIDCTL_VAULT_PASSPHRASE for non-interactive use")
	}

	fmt.Print(prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return string(raw), nil
}

// promptPassphraseConfirm reads a passphrase twice and confirms they match.
func promptPassphraseConfirm() (string, error) {
	pass1, err := promptPassphrase("New passphrase: ")
	if err != nil {
		return "", err
	}
	if pass1 == "" {
		return "", fmt.Errorf("passphrase cannot be empty")
	}

	// Skip confirmation if using env var
	if os.Getenv("GRIDCTL_VAULT_PASSPHRASE") != "" {
		return pass1, nil
	}

	pass2, err := promptPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}

	if pass1 != pass2 {
		return "", fmt.Errorf("passphrases do not match")
	}
	return pass1, nil
}

// maskValue returns a masked version of a secret value.
func maskValue(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return v[:2] + strings.Repeat("*", len(v)-4) + v[len(v)-2:]
}
