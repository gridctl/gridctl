package vault

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStore_SetAndGet(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		wantOK    bool
		wantValue string
	}{
		{name: "set and get", key: "API_KEY", value: "secret123", wantOK: true, wantValue: "secret123"},
		{name: "empty value", key: "EMPTY", value: "", wantOK: true, wantValue: ""},
		{name: "special characters", key: "DB_URL", value: "postgres://user:p@ss@host:5432/db", wantOK: true, wantValue: "postgres://user:p@ss@host:5432/db"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore(t.TempDir())
			if err := store.Set(tc.key, tc.value); err != nil {
				t.Fatalf("Set() error: %v", err)
			}

			got, ok := store.Get(tc.key)
			if ok != tc.wantOK {
				t.Errorf("Get() ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.wantValue {
				t.Errorf("Get() = %q, want %q", got, tc.wantValue)
			}
		})
	}
}

func TestStore_Get_NonExistent(t *testing.T) {
	store := NewStore(t.TempDir())
	val, ok := store.Get("MISSING")
	if ok {
		t.Error("Get() returned ok=true for nonexistent key")
	}
	if val != "" {
		t.Errorf("Get() = %q, want empty string", val)
	}
}

func TestStore_Delete(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Set("KEY", "value"); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete("KEY"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	if _, ok := store.Get("KEY"); ok {
		t.Error("Get() returned ok after Delete()")
	}
}

func TestStore_Delete_NonExistent(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.Delete("MISSING")
	if err == nil {
		t.Error("Delete() should return error for nonexistent key")
	}
}

func TestStore_List(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("BETA", "b")
	_ = store.Set("ALPHA", "a")
	_ = store.Set("GAMMA", "g")

	secrets := store.List()
	if len(secrets) != 3 {
		t.Fatalf("List() returned %d secrets, want 3", len(secrets))
	}

	// Verify sorted order
	if secrets[0].Key != "ALPHA" || secrets[1].Key != "BETA" || secrets[2].Key != "GAMMA" {
		t.Errorf("List() not sorted: %v", secrets)
	}
}

func TestStore_Import(t *testing.T) {
	store := NewStore(t.TempDir())

	input := map[string]string{
		"KEY1": "val1",
		"KEY2": "val2",
		"KEY3": "val3",
	}

	count, err := store.Import(input)
	if err != nil {
		t.Fatalf("Import() error: %v", err)
	}
	if count != 3 {
		t.Errorf("Import() count = %d, want 3", count)
	}

	for k, want := range input {
		got, ok := store.Get(k)
		if !ok {
			t.Errorf("key %q not found after import", k)
		}
		if got != want {
			t.Errorf("Get(%q) = %q, want %q", k, got, want)
		}
	}
}

func TestStore_Import_Overwrites(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("KEY", "old")

	_, err := store.Import(map[string]string{"KEY": "new"})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := store.Get("KEY")
	if got != "new" {
		t.Errorf("Import did not overwrite: got %q, want %q", got, "new")
	}
}

func TestStore_Export(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("A", "1")
	_ = store.Set("B", "2")

	exported := store.Export()
	if len(exported) != 2 {
		t.Fatalf("Export() returned %d entries, want 2", len(exported))
	}
	if exported["A"] != "1" || exported["B"] != "2" {
		t.Errorf("Export() = %v", exported)
	}
}

func TestStore_Keys(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("ZETA", "z")
	_ = store.Set("ALPHA", "a")

	keys := store.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys() returned %d, want 2", len(keys))
	}
	if keys[0] != "ALPHA" || keys[1] != "ZETA" {
		t.Errorf("Keys() not sorted: %v", keys)
	}
}

func TestStore_Has(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("EXISTS", "value")

	if !store.Has("EXISTS") {
		t.Error("Has() returned false for existing key")
	}
	if store.Has("MISSING") {
		t.Error("Has() returned true for missing key")
	}
}

func TestStore_Values(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("A", "val1")
	_ = store.Set("B", "val2")
	_ = store.Set("C", "") // empty value should be excluded

	vals := store.Values()
	if len(vals) != 2 {
		t.Fatalf("Values() returned %d, want 2", len(vals))
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Write with first store
	store1 := NewStore(dir)
	_ = store1.Set("KEY", "persisted")

	// Load with second store
	store2 := NewStore(dir)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	got, ok := store2.Get("KEY")
	if !ok || got != "persisted" {
		t.Errorf("Persistence failed: ok=%v, got=%q", ok, got)
	}
}

func TestStore_Load_EmptyDir(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Errorf("Load() should not error on missing file: %v", err)
	}
}

func TestStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEY", "value")

	path := filepath.Join(dir, "secrets.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestStore_DirPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	store := NewStore(dir)
	_ = store.Set("KEY", "value")

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("dir permissions = %o, want 0700", perm)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	store := NewStore(t.TempDir())

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// 10 writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "KEY"
			val := "value"
			if err := store.Set(key, val); err != nil {
				errs <- err
			}
		}(i)
	}

	// 10 readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Get("KEY")
			store.List()
			store.Keys()
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestStore_UpdateExistingKey(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("KEY", "old")
	_ = store.Set("KEY", "new")

	got, _ := store.Get("KEY")
	if got != "new" {
		t.Errorf("Set() did not update: got %q, want %q", got, "new")
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Set multiple keys
	_ = store.Set("A", "1")
	_ = store.Set("B", "2")

	// Verify no temp files remain
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file not cleaned up: %s", e.Name())
		}
	}
}

// --- Variable Set Tests ---

func TestStore_SetWithSet(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.SetWithSet("TOKEN", "abc123", "github"); err != nil {
		t.Fatalf("SetWithSet() error: %v", err)
	}

	// Verify secret stored
	got, ok := store.Get("TOKEN")
	if !ok || got != "abc123" {
		t.Errorf("Get() = %q, ok=%v; want %q, ok=true", got, ok, "abc123")
	}

	// Verify set was auto-created
	sets := store.ListSets()
	if len(sets) != 1 || sets[0].Name != "github" {
		t.Errorf("ListSets() = %v, want [{github 1}]", sets)
	}
	if sets[0].Count != 1 {
		t.Errorf("set count = %d, want 1", sets[0].Count)
	}

	// Verify secret is in the set
	secrets := store.GetSetSecrets("github")
	if len(secrets) != 1 || secrets[0].Key != "TOKEN" {
		t.Errorf("GetSetSecrets(github) = %v, want [{TOKEN}]", secrets)
	}
}

func TestStore_SetPreservesExistingSet(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.SetWithSet("KEY", "val1", "mygroup")

	// Update value without changing set
	_ = store.Set("KEY", "val2")

	secrets := store.GetSetSecrets("mygroup")
	if len(secrets) != 1 || secrets[0].Value != "val2" {
		t.Errorf("Set() should preserve set assignment; got %v", secrets)
	}
}

func TestStore_CreateSet(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.CreateSet("production"); err != nil {
		t.Fatalf("CreateSet() error: %v", err)
	}

	sets := store.ListSets()
	if len(sets) != 1 || sets[0].Name != "production" || sets[0].Count != 0 {
		t.Errorf("ListSets() = %v, want [{production 0}]", sets)
	}
}

func TestStore_CreateSet_Duplicate(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.CreateSet("dup")

	err := store.CreateSet("dup")
	if err == nil {
		t.Error("CreateSet() should error on duplicate")
	}
}

func TestStore_DeleteSet(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.CreateSet("temp")
	_ = store.SetWithSet("KEY", "val", "temp")

	if err := store.DeleteSet("temp"); err != nil {
		t.Fatalf("DeleteSet() error: %v", err)
	}

	// Set should be gone
	sets := store.ListSets()
	if len(sets) != 0 {
		t.Errorf("ListSets() after delete = %v, want empty", sets)
	}

	// Secret should still exist but unassigned
	secrets := store.List()
	if len(secrets) != 1 || secrets[0].Set != "" {
		t.Errorf("Secret should be unassigned after set delete; got %v", secrets)
	}
}

func TestStore_DeleteSet_NotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.DeleteSet("missing")
	if err == nil {
		t.Error("DeleteSet() should error on nonexistent set")
	}
}

func TestStore_SetSecretSet(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.Set("KEY", "value")
	_ = store.CreateSet("group")

	if err := store.SetSecretSet("KEY", "group"); err != nil {
		t.Fatalf("SetSecretSet() error: %v", err)
	}

	secrets := store.GetSetSecrets("group")
	if len(secrets) != 1 || secrets[0].Key != "KEY" {
		t.Errorf("GetSetSecrets() = %v, want [{KEY}]", secrets)
	}
}

func TestStore_SetSecretSet_Unassign(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.SetWithSet("KEY", "val", "group")

	if err := store.SetSecretSet("KEY", ""); err != nil {
		t.Fatalf("SetSecretSet() error: %v", err)
	}

	secrets := store.GetSetSecrets("group")
	if len(secrets) != 0 {
		t.Errorf("GetSetSecrets() = %v, want empty", secrets)
	}
}

func TestStore_SetSecretSet_NotFound(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.SetSecretSet("MISSING", "group")
	if err == nil {
		t.Error("SetSecretSet() should error on nonexistent key")
	}
}

func TestStore_ListSets_Sorted(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.CreateSet("beta")
	_ = store.CreateSet("alpha")
	_ = store.CreateSet("gamma")

	sets := store.ListSets()
	if len(sets) != 3 {
		t.Fatalf("ListSets() returned %d, want 3", len(sets))
	}
	if sets[0].Name != "alpha" || sets[1].Name != "beta" || sets[2].Name != "gamma" {
		t.Errorf("ListSets() not sorted: %v", sets)
	}
}

func TestStore_GetSetSecrets_Sorted(t *testing.T) {
	store := NewStore(t.TempDir())
	_ = store.SetWithSet("CHARLIE", "c", "group")
	_ = store.SetWithSet("ALPHA", "a", "group")
	_ = store.SetWithSet("BRAVO", "b", "group")

	secrets := store.GetSetSecrets("group")
	if len(secrets) != 3 {
		t.Fatalf("GetSetSecrets() returned %d, want 3", len(secrets))
	}
	if secrets[0].Key != "ALPHA" || secrets[1].Key != "BRAVO" || secrets[2].Key != "CHARLIE" {
		t.Errorf("GetSetSecrets() not sorted: %v", secrets)
	}
}

func TestStore_LegacyFormatBackwardCompat(t *testing.T) {
	dir := t.TempDir()

	// Write legacy format (flat array)
	legacy := `[{"key":"LEGACY","value":"old-format"}]`
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	got, ok := store.Get("LEGACY")
	if !ok || got != "old-format" {
		t.Errorf("Legacy format not loaded: ok=%v, got=%q", ok, got)
	}
}

func TestStore_SetsPersistence(t *testing.T) {
	dir := t.TempDir()

	// Write with first store
	store1 := NewStore(dir)
	_ = store1.CreateSet("persist-set")
	_ = store1.SetWithSet("KEY", "val", "persist-set")

	// Load with second store
	store2 := NewStore(dir)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	sets := store2.ListSets()
	if len(sets) != 1 || sets[0].Name != "persist-set" || sets[0].Count != 1 {
		t.Errorf("Sets not persisted: %v", sets)
	}
}

// --- Encryption Tests ---

func TestStore_Lock(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("API_KEY", "secret123")

	if err := store.Lock("mypassphrase"); err != nil {
		t.Fatalf("Lock() error: %v", err)
	}

	// secrets.enc should exist
	if _, err := os.Stat(filepath.Join(dir, "secrets.enc")); err != nil {
		t.Error("secrets.enc should exist after lock")
	}

	// secrets.json should be removed
	if _, err := os.Stat(filepath.Join(dir, "secrets.json")); !os.IsNotExist(err) {
		t.Error("secrets.json should be removed after lock")
	}

	// Store should not be locked (we just locked it, but data is in memory)
	if store.IsLocked() {
		t.Error("store should not be locked after Lock() (data in memory)")
	}

	// Data should still be accessible
	val, ok := store.Get("API_KEY")
	if !ok || val != "secret123" {
		t.Errorf("Get after Lock: ok=%v, val=%q", ok, val)
	}
}

func TestStore_LockUnlock_NewStore(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("DB_PASS", "s3cret")
	_ = store.SetWithSet("TOKEN", "tok123", "github")

	if err := store.Lock("pass"); err != nil {
		t.Fatalf("Lock() error: %v", err)
	}

	// Load with new store — should be locked
	store2 := NewStore(dir)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !store2.IsLocked() {
		t.Error("new store should be locked")
	}

	// Can't access secrets while locked
	if _, ok := store2.Get("DB_PASS"); ok {
		t.Error("should not be able to Get while locked")
	}

	// Unlock
	if err := store2.Unlock("pass"); err != nil {
		t.Fatalf("Unlock() error: %v", err)
	}

	if store2.IsLocked() {
		t.Error("should not be locked after Unlock")
	}

	// Verify data
	val, ok := store2.Get("DB_PASS")
	if !ok || val != "s3cret" {
		t.Errorf("Get after Unlock: ok=%v, val=%q", ok, val)
	}

	// Verify sets survived
	sets := store2.ListSets()
	if len(sets) != 1 || sets[0].Name != "github" {
		t.Errorf("sets after unlock: %v", sets)
	}
}

func TestStore_Unlock_WrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEY", "val")
	_ = store.Lock("correct")

	store2 := NewStore(dir)
	_ = store2.Load()

	if err := store2.Unlock("wrong"); err == nil {
		t.Error("expected error with wrong passphrase")
	}

	if !store2.IsLocked() {
		t.Error("should still be locked after failed unlock")
	}
}

func TestStore_ChangePassphrase(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("SECRET", "data")
	_ = store.Lock("oldpass")

	if err := store.ChangePassphrase("oldpass", "newpass"); err != nil {
		t.Fatalf("ChangePassphrase() error: %v", err)
	}

	// New store should unlock with new passphrase
	store2 := NewStore(dir)
	_ = store2.Load()

	if err := store2.Unlock("newpass"); err != nil {
		t.Fatalf("Unlock with new passphrase error: %v", err)
	}

	val, ok := store2.Get("SECRET")
	if !ok || val != "data" {
		t.Errorf("Get after passphrase change: ok=%v, val=%q", ok, val)
	}

	// Old passphrase should fail
	store3 := NewStore(dir)
	_ = store3.Load()

	if err := store3.Unlock("oldpass"); err == nil {
		t.Error("expected error with old passphrase")
	}
}

func TestStore_ChangePassphrase_WrongOld(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEY", "val")
	_ = store.Lock("correct")

	if err := store.ChangePassphrase("wrong", "newpass"); err == nil {
		t.Error("expected error with wrong old passphrase")
	}
}

func TestStore_ModifyAfterUnlock(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEY1", "val1")
	_ = store.Lock("pass")

	// Unlock and modify
	store2 := NewStore(dir)
	_ = store2.Load()
	_ = store2.Unlock("pass")
	_ = store2.Set("KEY2", "val2")

	// Verify re-encrypted file is valid
	store3 := NewStore(dir)
	_ = store3.Load()

	if !store3.IsLocked() {
		t.Error("should be locked on fresh load")
	}

	_ = store3.Unlock("pass")

	val1, ok1 := store3.Get("KEY1")
	val2, ok2 := store3.Get("KEY2")
	if !ok1 || val1 != "val1" {
		t.Errorf("KEY1: ok=%v, val=%q", ok1, val1)
	}
	if !ok2 || val2 != "val2" {
		t.Errorf("KEY2: ok=%v, val=%q", ok2, val2)
	}
}

func TestStore_EncryptedFilePermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEY", "val")
	_ = store.Lock("pass")

	info, err := os.Stat(filepath.Join(dir, "secrets.enc"))
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestStore_IsEncrypted(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEY", "val")

	if store.IsEncrypted() {
		t.Error("should not be encrypted initially")
	}

	_ = store.Lock("pass")

	if !store.IsEncrypted() {
		t.Error("should be encrypted after lock")
	}
}

func TestStore_LockAlreadyLocked(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEY", "val")
	_ = store.Lock("pass")

	// Locking again should re-encrypt (not error)
	if err := store.Lock("newpass"); err != nil {
		t.Fatalf("second Lock() error: %v", err)
	}

	// Should unlock with new passphrase
	store2 := NewStore(dir)
	_ = store2.Load()
	if err := store2.Unlock("newpass"); err != nil {
		t.Fatalf("Unlock with new pass error: %v", err)
	}
}

func TestStore_DeleteWhileEncrypted(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Set("KEEP", "val1")
	_ = store.Set("DEL", "val2")
	_ = store.Lock("pass")

	// Delete while encrypted
	_ = store.Delete("DEL")

	// Reload and verify
	store2 := NewStore(dir)
	_ = store2.Load()
	_ = store2.Unlock("pass")

	if _, ok := store2.Get("DEL"); ok {
		t.Error("deleted key should not exist")
	}
	if val, ok := store2.Get("KEEP"); !ok || val != "val1" {
		t.Error("kept key should exist")
	}
}
