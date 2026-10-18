package controller

import (
	"bytes"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestUnlockFromEnvironment_PassphraseFile(t *testing.T) {
	dir := t.TempDir()
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("TOKEN", "value"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock("correct"); err != nil {
		t.Fatal(err)
	}
	locked := vault.NewStore(dir)
	if err := locked.Load(); err != nil {
		t.Fatal(err)
	}
	if !locked.IsLocked() {
		t.Fatal("expected locked store")
	}
	path := filepath.Join(dir, "pass")
	if err := os.WriteFile(path, []byte("correct\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv(vault.PassphraseFileEnv, path)
	if err := unlockFromEnvironment(locked); err != nil {
		t.Fatal(err)
	}
	if locked.IsLocked() {
		t.Fatal("store stayed locked")
	}
}

func TestUnlockFromEnvironment_BadFileDoesNotEcho(t *testing.T) {
	dir := t.TempDir()
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("TOKEN", "value"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock("correct"); err != nil {
		t.Fatal(err)
	}
	locked := vault.NewStore(dir)
	if err := locked.Load(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pass")
	if err := os.WriteFile(path, []byte("secret-contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv(vault.PassphraseFileEnv, path)
	err := unlockFromEnvironment(locked)
	if err == nil || strings.Contains(err.Error(), "secret-contents") {
		t.Fatalf("error = %v", err)
	}
	if !locked.IsLocked() {
		t.Fatal("bad file unlocked the store")
	}
}

func TestSaveForegroundState_WritesAndCleanupDeletes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("HOME", home)
	sc := New(Config{Port: 8180, Foreground: true})
	stack := &config.Stack{Name: "demo", Gateway: &config.GatewayConfig{Auth: &config.AuthConfig{Token: "tok", Header: "Authorization", Type: "bearer"}}}
	cleanup, err := sc.saveForegroundState("demo", "/tmp/stack.yaml", stack)
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.PID != os.Getpid() || st.Port != 8180 || st.AuthToken != "tok" || st.StackFile != "/tmp/stack.yaml" {
		t.Fatalf("state = %+v", st)
	}
	cleanup()
	if _, err := state.Load("demo"); !os.IsNotExist(err) {
		t.Fatalf("state file remains: %v", err)
	}
}

func TestServe_Foreground_ClearsStateOnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("HOME", home)
	port, closeListener := occupiedPort(t)
	defer closeListener()
	ctrl := New(Config{Port: port, Foreground: true, Quiet: true})
	ctrl.SetWebFS(func() (fs.FS, error) { return nil, nil })
	err := ctrl.Serve(t.Context())
	if err == nil {
		t.Fatal("expected occupied-port error")
	}
	if _, statErr := state.Load("gridctl"); !os.IsNotExist(statErr) {
		t.Fatalf("foreground state file remains after error: %v", statErr)
	}
}

func TestServe_BadPassphraseFileContinues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("HOME", home)
	vaultDir := filepath.Join(home, ".gridctl", "vault")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := vault.NewStore(vaultDir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("TOKEN", "value"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock("correct"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "pass")
	if err := os.WriteFile(path, []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv(vault.PassphraseFileEnv, path)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	port, closeListener := occupiedPort(t)
	defer closeListener()
	ctrl := New(Config{Port: port, Foreground: true, Quiet: true})
	ctrl.SetWebFS(func() (fs.FS, error) { return nil, nil })
	err := ctrl.Serve(t.Context())
	if err == nil {
		t.Fatal("expected occupied-port error, not a passphrase failure")
	}
	if strings.Contains(err.Error(), "passphrase") || strings.Contains(err.Error(), "nope") {
		t.Fatalf("serve failed on the passphrase file: %v", err)
	}
	if !strings.Contains(logs.String(), "store stays locked") {
		t.Fatalf("logs = %s", logs.String())
	}
}
