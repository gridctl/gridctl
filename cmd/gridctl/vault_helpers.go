package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/mattn/go-isatty"
)

// loadVault opens the gridctl variable store. The name is retained from the
// secrets-only era because callers across the CLI surface still refer to
// "the vault" — the on-disk file is still secrets.json / secrets.enc and the
// store struct is still vault.Store. The "vault → var" rename is at the user
// surface (commands, YAML syntax, web routes), not the internal naming.
func loadVault() (*vault.Store, error) {
	vaultDir, err := state.VaultDir()
	if err != nil {
		return nil, fmt.Errorf("loading variable store: %w", err)
	}
	store := vault.NewStore(vaultDir)
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("loading variable store: %w", err)
	}
	return store, nil
}

// ensureUnlocked unlocks a locked store from GRIDCTL_VAULT_PASSPHRASE, then
// GRIDCTL_VAULT_PASSPHRASE_FILE, then a terminal prompt. It serves commands
// outside the var family, which never use a running gateway; the var
// subcommands resolve their store through openVarBackend.
func ensureUnlocked(store *vault.Store) error {
	if !store.IsLocked() {
		return nil
	}
	if handled, err := unlockStoreFromEnvironment(store); handled || err != nil {
		return err
	}
	if !stdinIsTerminal() {
		return fmt.Errorf("vault is locked. Set GRIDCTL_VAULT_PASSPHRASE, or run this command in a terminal to be prompted")
	}
	pass, err := readHiddenPassphrase("Vault passphrase: ")
	if err != nil {
		return err
	}
	return store.Unlock(pass)
}

// passphraseFromFile reads the file named by GRIDCTL_VAULT_PASSPHRASE_FILE.
// ok is false when the variable is unset. Errors name the variable and the
// problem, never the contents.
func passphraseFromFile() (pass string, ok bool, err error) {
	path := os.Getenv(vault.PassphraseFileEnv)
	if path == "" {
		return "", false, nil
	}
	pass, err = vault.ReadPassphraseFile(path)
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", vault.PassphraseFileEnv, err)
	}
	return pass, true, nil
}

// passphraseFromEnvironment returns GRIDCTL_VAULT_PASSPHRASE, else the
// contents of GRIDCTL_VAULT_PASSPHRASE_FILE. ok is false when neither is set.
func passphraseFromEnvironment() (pass string, ok bool, err error) {
	if pass := os.Getenv("GRIDCTL_VAULT_PASSPHRASE"); pass != "" {
		return pass, true, nil
	}
	return passphraseFromFile()
}

// stdinIsTerminal is a seam for tests.
var stdinIsTerminal = func() bool { return isatty.IsTerminal(os.Stdin.Fd()) }

// readHiddenPassphrase prints prompt and reads a hidden line from the
// terminal. ctrl-C cancels the read, restores the terminal, and returns
// "passphrase entry cancelled" instead of killing the process with echo off.
func readHiddenPassphrase(prompt string) (string, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Print(prompt)
	raw, err := controller.ReadHidden(ctx, os.Stdin, os.Stdout)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// promptPassphrase returns GRIDCTL_VAULT_PASSPHRASE, the contents of
// GRIDCTL_VAULT_PASSPHRASE_FILE, or a hidden passphrase read from the
// terminal, in that order.
func promptPassphrase(prompt string) (string, error) {
	pass, ok, err := passphraseFromEnvironment()
	if err != nil {
		return "", err
	}
	if ok {
		return pass, nil
	}

	if !stdinIsTerminal() {
		return "", fmt.Errorf("interactive input required. Set GRIDCTL_VAULT_PASSPHRASE for non-interactive use")
	}
	return readHiddenPassphrase(prompt)
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

	// Skip confirmation when the passphrase came from the environment.
	if _, fromEnv, _ := passphraseFromEnvironment(); fromEnv {
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
