package controller

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/vault"
)

// lockedStoreHome writes a store holding key under a temp GRIDCTL_HOME,
// locks it with pass, clears both passphrase variables, and returns the
// store directory.
func lockedStoreHome(t *testing.T, key, pass string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv(vault.PassphraseFileEnv, "")
	vaultDir := filepath.Join(home, ".gridctl", "vault")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := vault.NewStore(vaultDir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(key, "stored-value"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock(pass); err != nil {
		t.Fatal(err)
	}
	return vaultDir
}

func loadLockedStore(t *testing.T, dir string) *vault.Store {
	t.Helper()
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if !store.IsLocked() {
		t.Fatal("expected a locked store")
	}
	return store
}

func TestDeploy_LockedStoreWithoutPassphrase(t *testing.T) {
	lockedStoreHome(t, "DEMO_TOKEN", "correct")
	t.Setenv("DEMO_TOKEN", "")
	if err := os.Unsetenv("DEMO_TOKEN"); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	body := `version: "1"
name: locked-deploy
network:
  name: locked-net
mcp-servers:
  - name: remote
    url: https://mcp.example.com/mcp
    headers:
      Authorization: "Bearer ${var:DEMO_TOKEN}"
`
	if err := os.WriteFile(stackPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	err := New(Config{StackPath: stackPath, Foreground: true, Quiet: true}).Deploy(t.Context())
	if err == nil {
		t.Fatal("expected a locked-store error")
	}
	if !errors.Is(err, config.ErrVaultLocked) {
		t.Fatalf("errors.Is(err, ErrVaultLocked) = false: %v", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "missing variable(s)") || strings.Contains(msg, "stored-value") {
		t.Fatalf("error = %q", msg)
	}
	if !strings.Contains(msg, "DEMO_TOKEN") {
		t.Fatalf("error does not name the key: %q", msg)
	}
}

func TestVaultSetAdapter_IsLockedForwards(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", "correct")
	store := loadLockedStore(t, dir)
	adapter := newVaultSetAdapter(store)
	if !adapter.IsLocked() {
		t.Fatal("adapter reports unlocked for a locked store")
	}
	if err := store.Unlock("correct"); err != nil {
		t.Fatal(err)
	}
	if adapter.IsLocked() {
		t.Fatal("adapter reports locked after Unlock")
	}
}

func TestLoadStack_LockedStoreSecretSetsWarnThroughAdapter(t *testing.T) {
	dir := lockedStoreHome(t, "DEMO_TOKEN", "correct")
	store := loadLockedStore(t, dir)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	body := `version: "1"
name: locked-sets
network:
  name: locked-net
secrets:
  sets:
    - dev
mcp-servers:
  - name: remote
    url: https://mcp.example.com/mcp
`
	if err := os.WriteFile(stackPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := []config.LoadOption{
		config.WithVault(store),
		config.WithVaultSets(newVaultSetAdapter(store)),
	}
	opts = append(opts, overrideLoadOptions(Config{StackPath: stackPath})...)
	if _, err := config.LoadStack(stackPath, opts...); err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Count(logs.String(), "secrets.sets injected nothing because the variable store is locked") != 1 {
		t.Fatalf("logs = %s", logs.String())
	}
}
