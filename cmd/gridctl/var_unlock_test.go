package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/vault"
)

func lockedVarStore(t *testing.T, pass string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv(vault.PassphraseFileEnv, "")
	dir := filepath.Join(home, ".gridctl", "vault")
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("TOKEN", "stored"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock(pass); err != nil {
		t.Fatal(err)
	}
	return dir
}

// nonTTYStdin points os.Stdin at a regular file for the test.
func nonTTYStdin(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = prev
		_ = f.Close()
	})
}

func TestRunVarUnlock_DoesNotPersist(t *testing.T) {
	const pass = "passphrase-long-enough"
	dir := lockedVarStore(t, pass)
	encPath := filepath.Join(dir, "secrets.enc")
	before, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", pass)

	var runErr error
	out := captureStdout(t, func() { runErr = runVarUnlock() })
	if runErr != nil {
		t.Fatal(runErr)
	}
	for _, want := range []string{"verified", "this process only", "nothing persisted", "variables"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
	after, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatalf("secrets.enc after unlock: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("secrets.enc changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.json")); !os.IsNotExist(err) {
		t.Fatalf("secrets.json exists after unlock: %v", err)
	}
}

func TestVarUnlockHelp_StatesNonPersistent(t *testing.T) {
	if !strings.Contains(varUnlockCmd.Short, "does not persist") {
		t.Fatalf("Short = %q", varUnlockCmd.Short)
	}
	for _, want := range []string{"this process only", "No file is written", "GRIDCTL_VAULT_PASSPHRASE", "GRIDCTL_VAULT_PASSPHRASE_FILE"} {
		if !strings.Contains(varUnlockCmd.Long, want) {
			t.Errorf("Long missing %q", want)
		}
	}
}

func TestEnsureUnlocked_NonTTYMessageNamesEnvironment(t *testing.T) {
	dir := lockedVarStore(t, "passphrase-long-enough")
	nonTTYStdin(t)
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	err := ensureUnlocked(store)
	if err == nil {
		t.Fatal("expected a locked error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "GRIDCTL_VAULT_PASSPHRASE") || !strings.Contains(msg, "terminal") {
		t.Fatalf("error = %q", msg)
	}
	if strings.Contains(msg, "var unlock") || strings.Contains(msg, "GRIDCTL_VAULT_PASSPHRASE_FILE") {
		t.Fatalf("error = %q", msg)
	}
}
