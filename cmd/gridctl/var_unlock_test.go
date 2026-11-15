package main

import (
	"bytes"
	"context"
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

	var stdout, stderr bytes.Buffer
	if code := runVarUnlockSession(context.Background(), &stdout, &stderr, "text"); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != noGatewayUnlockNotice {
		t.Errorf("output = %q, want the no-gateway notice", got)
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

func TestVarUnlockHelp_DescribesGatewaySession(t *testing.T) {
	if varUnlockCmd.Short != "Unlock the variable store in every running gateway" {
		t.Fatalf("Short = %q", varUnlockCmd.Short)
	}
	for _, want := range []string{"verify the passphrase", "each running gateway for this home", "only in its own\nmemory", "nothing stays unlocked", "gridctl apply\nprompts", "GRIDCTL_VAULT_PASSPHRASE_FILE"} {
		if !strings.Contains(strings.ToLower(varUnlockCmd.Long), strings.ToLower(want)) {
			t.Errorf("Long missing %q", want)
		}
	}
	if varLockCmd.Short != "Encrypt the store, or lock it again in every running gateway" {
		t.Fatalf("lock Short = %q", varLockCmd.Short)
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
