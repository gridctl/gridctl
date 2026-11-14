package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const sourceTestPass = "source-test-passphrase" // #nosec G101 -- test fixture

func lockedStore(t *testing.T) (string, *Store) {
	t.Helper()
	dir := t.TempDir()
	w := NewStore(dir)
	if err := w.Set("API_KEY", "abc123"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := w.Lock(sourceTestPass); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.IsLocked() {
		t.Fatal("store not locked after Load")
	}
	return dir, s
}

func TestStore_UnlockSource_SetByEachUnlockPath(t *testing.T) {
	for _, src := range []string{
		UnlockSourceEnvironment,
		UnlockSourcePassphraseFile,
		UnlockSourceStdin,
		UnlockSourceAPI,
		UnlockSourceCLI,
	} {
		t.Run(src, func(t *testing.T) {
			_, s := lockedStore(t)
			if got := s.UnlockSource(); got != "" {
				t.Fatalf("UnlockSource() on locked store = %q, want empty", got)
			}
			if err := s.Unlock(sourceTestPass); err != nil {
				t.Fatalf("Unlock: %v", err)
			}
			if got := s.UnlockSource(); got != "" {
				t.Fatalf("UnlockSource() before SetUnlockSource = %q, want empty", got)
			}
			s.SetUnlockSource(src)
			if got := s.UnlockSource(); got != src {
				t.Fatalf("UnlockSource() = %q, want %q", got, src)
			}
		})
	}
}

func TestStore_UnlockSource_IgnoredWhileLocked(t *testing.T) {
	_, s := lockedStore(t)
	s.SetUnlockSource(UnlockSourceAPI)
	if s.unlockSource != "" {
		t.Fatalf("unlockSource on locked store = %q, want empty", s.unlockSource)
	}
	if err := s.Unlock(sourceTestPass); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got := s.UnlockSource(); got != "" {
		t.Fatalf("UnlockSource() = %q, want empty (set while locked must not stick)", got)
	}
}

func TestStore_UnlockSource_EmptyOnPlaintext(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Set("K", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	s.SetUnlockSource(UnlockSourceAPI)
	if got := s.UnlockSource(); got != "" {
		t.Fatalf("UnlockSource() on plaintext store = %q, want empty", got)
	}
	if s.unlockSource != "" {
		t.Fatalf("unlockSource on plaintext store = %q, want empty", s.unlockSource)
	}
}

func TestStore_UnlockSource_LockLeavesItSettable(t *testing.T) {
	// Lock on a plaintext store ends encrypted and unlocked; the caller
	// (handleVaultLock) records the source afterward.
	s := NewStore(t.TempDir())
	if err := s.Set("K", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Lock(sourceTestPass); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if s.IsLocked() || !s.IsEncrypted() {
		t.Fatal("store after Lock should be encrypted and unlocked")
	}
	if got := s.UnlockSource(); got != "" {
		t.Fatalf("UnlockSource() right after Lock = %q, want empty", got)
	}
	s.SetUnlockSource(UnlockSourceAPI)
	if got := s.UnlockSource(); got != UnlockSourceAPI {
		t.Fatalf("UnlockSource() = %q, want %q", got, UnlockSourceAPI)
	}
}

func TestStore_Relock(t *testing.T) {
	dir, s := lockedStore(t)
	if err := s.Unlock(sourceTestPass); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	s.SetUnlockSource(UnlockSourceStdin)
	before, err := os.ReadFile(filepath.Join(dir, "secrets.enc"))
	if err != nil {
		t.Fatalf("read secrets.enc: %v", err)
	}

	relocked, err := s.Relock()
	if err != nil || !relocked {
		t.Fatalf("Relock() = %v, %v; want true, nil", relocked, err)
	}
	if !s.IsLocked() {
		t.Fatal("IsLocked() after Relock = false")
	}
	if !s.IsEncrypted() {
		t.Fatal("IsEncrypted() after Relock = false")
	}
	if s.passphrase != "" || s.unlockSource != "" {
		t.Fatal("Relock left the passphrase or unlock source in memory")
	}
	if len(s.variables) != 0 || len(s.sets) != 0 {
		t.Fatal("Relock left variables or sets in memory")
	}
	if _, ok := s.Get("API_KEY"); ok {
		t.Fatal("Get after Relock returned a value")
	}
	// A later reload (several reads) must keep it locked.
	for i := 0; i < 3; i++ {
		if !s.IsLocked() {
			t.Fatalf("IsLocked() read %d after Relock = false", i)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "secrets.enc"))
	if err != nil {
		t.Fatalf("read secrets.enc: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Relock modified secrets.enc")
	}

	again, err := s.Relock()
	if err != nil || again {
		t.Fatalf("second Relock() = %v, %v; want false, nil", again, err)
	}

	// The session can be started again.
	if err := s.Unlock(sourceTestPass); err != nil {
		t.Fatalf("Unlock after Relock: %v", err)
	}
	if v, ok := s.Get("API_KEY"); !ok || v != "abc123" {
		t.Fatalf("Get after re-unlock = %q, %v", v, ok)
	}
}

func TestStore_Relock_Plaintext(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Set("K", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	relocked, err := s.Relock()
	if !errors.Is(err, ErrNotEncrypted) || relocked {
		t.Fatalf("Relock() on plaintext = %v, %v; want false, ErrNotEncrypted", relocked, err)
	}
	if v, ok := s.Get("K"); !ok || v != "v" {
		t.Fatal("Relock on plaintext dropped variables")
	}
}

func TestStore_UnlockSource_ClearedByPlaintextToEncryptedReload(t *testing.T) {
	dir := t.TempDir()
	daemon := NewStore(dir)
	if err := daemon.Set("K", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// A stale record must not survive the transition even if one existed.
	daemon.mu.Lock()
	daemon.unlockSource = UnlockSourceAPI
	daemon.mu.Unlock()

	cli := NewStore(dir)
	if err := cli.Load(); err != nil {
		t.Fatalf("cli Load: %v", err)
	}
	if err := cli.Lock(sourceTestPass); err != nil {
		t.Fatalf("cli Lock: %v", err)
	}
	if !daemon.IsLocked() {
		t.Fatal("daemon not locked after external lock")
	}
	if daemon.unlockSource != "" {
		t.Fatalf("unlockSource after plaintext-to-encrypted reload = %q, want empty", daemon.unlockSource)
	}
}

func TestStore_UnlockSource_ClearedByEncryptedToPlaintextReload(t *testing.T) {
	dir, s := lockedStore(t)
	if err := s.Unlock(sourceTestPass); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	s.SetUnlockSource(UnlockSourceEnvironment)

	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(`{"version":2,"variables":[]}`), 0600); err != nil {
		t.Fatalf("write secrets.json: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "secrets.enc")); err != nil {
		t.Fatalf("remove secrets.enc: %v", err)
	}
	if s.IsEncrypted() {
		t.Fatal("store still encrypted after external decrypt")
	}
	if s.unlockSource != "" {
		t.Fatalf("unlockSource after encrypted-to-plaintext reload = %q, want empty", s.unlockSource)
	}
	if got := s.UnlockSource(); got != "" {
		t.Fatalf("UnlockSource() = %q, want empty", got)
	}
}
