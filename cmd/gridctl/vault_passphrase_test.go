package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/vault"
)

func writePassphraseFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPassphraseStdinFlag_OnApplyAndServe(t *testing.T) {
	for _, c := range []struct {
		name string
		f    func() string
	}{
		{"apply", func() string { return applyCmd.Flags().Lookup("passphrase-stdin").Usage }},
		{"serve", func() string { return serveCmd.Flags().Lookup("passphrase-stdin").Usage }},
	} {
		if got := c.f(); got != "Read the store passphrase from the first line of stdin (for wrappers; never on a terminal)" {
			t.Errorf("%s --passphrase-stdin usage = %q", c.name, got)
		}
	}
	if applyCmd.Flags().Lookup("passphrase-stdin").Hidden || serveCmd.Flags().Lookup("passphrase-stdin").Hidden {
		t.Fatal("--passphrase-stdin is hidden")
	}
}

func TestWithTerminalPromptHint(t *testing.T) {
	locked := fmt.Errorf("failed to load stack: %w", fmt.Errorf("%w; could not resolve: TOKEN", config.ErrVaultLocked))
	other := errors.New("something else")

	got := withTerminalPromptHint(locked, false)
	if !errors.Is(got, config.ErrVaultLocked) {
		t.Fatal("hint broke errors.Is")
	}
	if !strings.HasSuffix(got.Error(), "\n  Or run gridctl apply in a terminal to be prompted") {
		t.Fatalf("error = %q", got.Error())
	}
	if !strings.HasPrefix(got.Error(), locked.Error()) {
		t.Fatalf("loader text changed: %q", got.Error())
	}
	if got := withTerminalPromptHint(locked, true); got != locked {
		t.Fatalf("terminal error changed: %v", got)
	}
	if got := withTerminalPromptHint(other, false); got != other {
		t.Fatalf("unrelated error changed: %v", got)
	}
	if withTerminalPromptHint(nil, false) != nil {
		t.Fatal("nil error changed")
	}
}

func TestEnsureUnlocked_PassphraseFile(t *testing.T) {
	const pass = "passphrase-long-enough"
	dir := lockedVarStore(t, pass)
	nonTTYStdin(t)
	t.Setenv(vault.PassphraseFileEnv, writePassphraseFile(t, pass+"\n"))

	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ensureUnlocked(store); err != nil {
		t.Fatal(err)
	}
	if store.IsLocked() {
		t.Fatal("store still locked")
	}
}

func TestEnsureUnlocked_PassphraseFileRejected(t *testing.T) {
	dir := lockedVarStore(t, "passphrase-long-enough")
	nonTTYStdin(t)

	t.Run("wrong contents", func(t *testing.T) {
		t.Setenv(vault.PassphraseFileEnv, writePassphraseFile(t, "file-secret-value\n"))
		store := vault.NewStore(dir)
		if err := store.Load(); err != nil {
			t.Fatal(err)
		}
		err := ensureUnlocked(store)
		if err == nil || !strings.Contains(err.Error(), vault.PassphraseFileEnv) {
			t.Fatalf("err = %v, want one naming %s", err, vault.PassphraseFileEnv)
		}
		if strings.Contains(err.Error(), "file-secret-value") {
			t.Fatalf("error echoes the file contents: %v", err)
		}
	})
	t.Run("bad mode", func(t *testing.T) {
		path := writePassphraseFile(t, "file-secret-value\n")
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv(vault.PassphraseFileEnv, path)
		store := vault.NewStore(dir)
		if err := store.Load(); err != nil {
			t.Fatal(err)
		}
		err := ensureUnlocked(store)
		if err == nil || !strings.Contains(err.Error(), vault.PassphraseFileEnv) {
			t.Fatalf("err = %v", err)
		}
		if strings.Contains(err.Error(), "file-secret-value") {
			t.Fatalf("error echoes the file contents: %v", err)
		}
	})
}

func TestEnsureUnlocked_EnvironmentBeforeFile(t *testing.T) {
	const pass = "passphrase-long-enough"
	dir := lockedVarStore(t, pass)
	nonTTYStdin(t)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", pass)
	t.Setenv(vault.PassphraseFileEnv, filepath.Join(t.TempDir(), "missing"))
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ensureUnlocked(store); err != nil {
		t.Fatalf("environment did not take precedence: %v", err)
	}
}

func TestPromptPassphrase_PassphraseFile(t *testing.T) {
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	nonTTYStdin(t)
	t.Setenv(vault.PassphraseFileEnv, writePassphraseFile(t, "from-file\n"))
	got, err := promptPassphrase("Vault passphrase: ")
	if err != nil || got != "from-file" {
		t.Fatalf("promptPassphrase = %q, %v", got, err)
	}
	// Confirmation is skipped for a file, as for the environment.
	got, err = promptPassphraseConfirm()
	if err != nil || got != "from-file" {
		t.Fatalf("promptPassphraseConfirm = %q, %v", got, err)
	}

	t.Setenv(vault.PassphraseFileEnv, filepath.Join(t.TempDir(), "missing"))
	if _, err := promptPassphrase("Vault passphrase: "); err == nil || !strings.Contains(err.Error(), vault.PassphraseFileEnv) {
		t.Fatalf("err = %v", err)
	}
}

func TestVarListAndSet_HonorPassphraseFile(t *testing.T) {
	const pass = "passphrase-long-enough"
	lockedVarStore(t, pass)
	nonTTYStdin(t)
	t.Setenv(vault.PassphraseFileEnv, writePassphraseFile(t, pass+"\n"))

	prevValue := varSetValue
	varSetValue = "new-value"
	t.Cleanup(func() { varSetValue = prevValue })

	var err error
	captureStdout(t, func() { err = runVarSet(context.Background(), "ADDED") })
	if err != nil {
		t.Fatalf("var set: %v", err)
	}
	out := captureStdout(t, func() { err = runVarList(context.Background()) })
	if err != nil {
		t.Fatalf("var list: %v", err)
	}
	if !strings.Contains(out, "ADDED") || !strings.Contains(out, "TOKEN") {
		t.Fatalf("var list output = %q", out)
	}

	t.Setenv(vault.PassphraseFileEnv, writePassphraseFile(t, "file-secret-value\n"))
	captureStdout(t, func() { err = runVarList(context.Background()) })
	if err == nil || strings.Contains(err.Error(), "file-secret-value") {
		t.Fatalf("bad file: err = %v", err)
	}
}
