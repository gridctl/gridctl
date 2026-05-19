package vault

import (
	"encoding/json"
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

func TestStore_LoadV0_FlatArray(t *testing.T) {
	dir := t.TempDir()

	// v0: legacy flat array, no sets, no type, no is_secret.
	v0 := `[{"key":"LEGACY","value":"old-format","set":"legacy-set"}]`
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(v0), 0600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	v, ok := store.GetVariable("LEGACY")
	if !ok {
		t.Fatal("LEGACY key not found")
	}
	if v.Value != "old-format" {
		t.Errorf("Value = %q, want %q", v.Value, "old-format")
	}
	if v.Type != TypeString {
		t.Errorf("Type = %q, want %q (v0 default)", v.Type, TypeString)
	}
	if !v.IsSecret {
		t.Error("IsSecret = false, want true (v0 default, Article XII)")
	}
	if v.Set != "legacy-set" {
		t.Errorf("Set = %q, want %q", v.Set, "legacy-set")
	}
}

func TestStore_LoadV1_SecretsObject(t *testing.T) {
	dir := t.TempDir()

	// v1: object with "secrets" key (pre-rename), no version, no type, no is_secret.
	v1 := `{"secrets":[{"key":"OLD_KEY","value":"v1-val"}],"sets":[{"name":"db"}]}`
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(v1), 0600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	v, ok := store.GetVariable("OLD_KEY")
	if !ok {
		t.Fatal("OLD_KEY not found")
	}
	if v.Value != "v1-val" {
		t.Errorf("Value = %q, want %q", v.Value, "v1-val")
	}
	if v.Type != TypeString {
		t.Errorf("Type = %q, want %q (v1 default)", v.Type, TypeString)
	}
	if !v.IsSecret {
		t.Error("IsSecret = false, want true (v1 default, Article XII)")
	}

	sets := store.ListSets()
	if len(sets) != 1 || sets[0].Name != "db" {
		t.Errorf("ListSets() = %v, want [{db 0}]", sets)
	}
}

func TestStore_LoadV2_Roundtrip(t *testing.T) {
	dir := t.TempDir()

	// v2: explicit version, variables array with full metadata.
	v2 := `{
  "version": 2,
  "variables": [
    {"key": "REGION", "value": "us-east-1", "type": "string", "is_secret": false},
    {"key": "DB_PASS", "value": "p4ss", "type": "string", "is_secret": true, "set": "db"},
    {"key": "PORTS", "value": "[\"80\",\"443\"]", "type": "list", "is_secret": false}
  ],
  "sets": [{"name": "db", "description": "Database vars"}]
}`
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(v2), 0600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Plaintext variable should round-trip with IsSecret=false.
	region, ok := store.GetVariable("REGION")
	if !ok {
		t.Fatal("REGION not found")
	}
	if region.IsSecret {
		t.Error("REGION.IsSecret = true, want false")
	}
	if region.Type != TypeString {
		t.Errorf("REGION.Type = %q, want %q", region.Type, TypeString)
	}

	// Secret variable should preserve type and set assignment.
	pass, _ := store.GetVariable("DB_PASS")
	if !pass.IsSecret {
		t.Error("DB_PASS.IsSecret = false, want true")
	}
	if pass.Set != "db" {
		t.Errorf("DB_PASS.Set = %q, want %q", pass.Set, "db")
	}

	// Typed list should preserve Type=list.
	ports, _ := store.GetVariable("PORTS")
	if ports.Type != TypeList {
		t.Errorf("PORTS.Type = %q, want %q", ports.Type, TypeList)
	}
}

func TestStore_SaveAlwaysWritesV2(t *testing.T) {
	dir := t.TempDir()

	// Seed a v1 file.
	v1 := `{"secrets":[{"key":"A","value":"v1"}]}`
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(v1), 0600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	// Any mutation forces a save.
	if err := store.Set("B", "new"); err != nil {
		t.Fatal(err)
	}

	// Re-read raw file: it must now be v2.
	data, err := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}

	var disk map[string]any
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("file not valid JSON: %v", err)
	}
	if v, ok := disk["version"].(float64); !ok || int(v) != CurrentStoreVersion {
		t.Errorf("on-disk version = %v, want %d", disk["version"], CurrentStoreVersion)
	}
	if _, ok := disk["variables"]; !ok {
		t.Error("on-disk file missing \"variables\" key after save")
	}
	if _, ok := disk["secrets"]; ok {
		t.Error("on-disk file still has \"secrets\" key after save (should be \"variables\")")
	}
}

func TestStore_Values_FiltersByIsSecret(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.SetVariable(Variable{Key: "SECRET_TOK", Value: "tok123", Type: TypeString, IsSecret: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVariable(Variable{Key: "REGION", Value: "us-east-1", Type: TypeString, IsSecret: false}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVariable(Variable{Key: "EMPTY_SECRET", Value: "", Type: TypeString, IsSecret: true}); err != nil {
		t.Fatal(err)
	}

	vals := store.Values()
	if len(vals) != 1 {
		t.Fatalf("Values() returned %d values, want 1 (only non-empty secret)", len(vals))
	}
	if vals[0] != "tok123" {
		t.Errorf("Values()[0] = %q, want %q", vals[0], "tok123")
	}
}

func TestStore_SetVariable_DefaultsToSecret(t *testing.T) {
	store := NewStore(t.TempDir())

	// Plain Set() must default to IsSecret=true (Article XII).
	if err := store.Set("KEY", "val"); err != nil {
		t.Fatal(err)
	}
	v, _ := store.GetVariable("KEY")
	if !v.IsSecret {
		t.Error("Set() default IsSecret = false, want true (Article XII)")
	}
	if v.Type != TypeString {
		t.Errorf("Set() default Type = %q, want %q", v.Type, TypeString)
	}
}

func TestStore_SetVariable_Plaintext(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.SetVariable(Variable{
		Key: "REGION", Value: "us-east-1", Type: TypeString, IsSecret: false,
	}); err != nil {
		t.Fatal(err)
	}

	v, _ := store.GetVariable("REGION")
	if v.IsSecret {
		t.Error("plaintext SetVariable produced IsSecret=true")
	}

	// Values() must NOT include plaintext values.
	vals := store.Values()
	for _, val := range vals {
		if val == "us-east-1" {
			t.Error("Values() leaked plaintext value into redaction set")
		}
	}
}

func TestStore_SetVariable_RejectsInvalidType(t *testing.T) {
	store := NewStore(t.TempDir())

	err := store.SetVariable(Variable{Key: "K", Value: "v", Type: "weird"})
	if err == nil {
		t.Error("SetVariable accepted invalid type")
	}
}

func TestStore_ImportVariables(t *testing.T) {
	store := NewStore(t.TempDir())

	vars := []Variable{
		{Key: "A", Value: "1", Type: TypeString, IsSecret: true},
		{Key: "B", Value: "us-east-1", Type: TypeString, IsSecret: false},
		{Key: "C", Value: `["x","y"]`, Type: TypeList, IsSecret: false},
	}

	count, err := store.ImportVariables(vars)
	if err != nil {
		t.Fatalf("ImportVariables() error: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}

	b, _ := store.GetVariable("B")
	if b.IsSecret {
		t.Error("B should be plaintext after import")
	}
	c, _ := store.GetVariable("C")
	if c.Type != TypeList {
		t.Errorf("C.Type = %q, want %q", c.Type, TypeList)
	}
}

func TestStore_EncryptedV1Migrates_LocksAsV2(t *testing.T) {
	// A vault that was encrypted with a v1 inner shape, when unlocked and
	// re-saved, must round-trip as v2 with IsSecret=true on every entry.
	dir := t.TempDir()

	// Manually encrypt a v1-shaped plaintext.
	v1 := []byte(`{"secrets":[{"key":"DB_PASS","value":"v1pass"}]}`)
	ev, err := LockVault(v1, "pass")
	if err != nil {
		t.Fatalf("LockVault() error: %v", err)
	}
	encData, err := marshalEncryptedVault(ev)
	if err != nil {
		t.Fatalf("marshalEncryptedVault() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.enc"), encData, 0600); err != nil {
		t.Fatal(err)
	}

	// Load, unlock, and force a save.
	store := NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Unlock("pass"); err != nil {
		t.Fatalf("Unlock() error: %v", err)
	}

	// In-memory: every v1 entry must default to IsSecret=true.
	v, ok := store.GetVariable("DB_PASS")
	if !ok {
		t.Fatal("DB_PASS not found after unlock")
	}
	if !v.IsSecret {
		t.Error("DB_PASS.IsSecret = false after v1 migration, want true")
	}
	if v.Type != TypeString {
		t.Errorf("DB_PASS.Type = %q, want %q", v.Type, TypeString)
	}

	// Set a new variable to trigger a save. The re-encrypted blob must now
	// contain a v2 inner shape; verify by decrypting and inspecting.
	if err := store.SetVariable(Variable{
		Key: "REGION", Value: "us-east-1", Type: TypeString, IsSecret: false,
	}); err != nil {
		t.Fatalf("SetVariable() error: %v", err)
	}

	encNew, err := os.ReadFile(filepath.Join(dir, "secrets.enc"))
	if err != nil {
		t.Fatal(err)
	}
	evNew, err := unmarshalEncryptedVault(encNew)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := UnlockVault(evNew, "pass")
	if err != nil {
		t.Fatal(err)
	}

	var inner map[string]any
	if err := json.Unmarshal(plain, &inner); err != nil {
		t.Fatalf("decrypted inner JSON parse: %v", err)
	}
	if ver, _ := inner["version"].(float64); int(ver) != CurrentStoreVersion {
		t.Errorf("re-encrypted inner version = %v, want %d", inner["version"], CurrentStoreVersion)
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

// --- Cross-process reload tests ---
//
// These simulate the daemon + CLI scenario: two Store instances pointed at
// the same baseDir, one writing and the other reading.

func TestStore_ReloadsOnExternalWrite_Plaintext(t *testing.T) {
	dir := t.TempDir()

	// "server" loads an empty vault.
	server := NewStore(dir)
	if err := server.Load(); err != nil {
		t.Fatalf("server Load(): %v", err)
	}
	if got := len(server.List()); got != 0 {
		t.Fatalf("server List() before external write = %d, want 0", got)
	}

	// "cli" writes via a separate Store instance.
	cli := NewStore(dir)
	if err := cli.Load(); err != nil {
		t.Fatalf("cli Load(): %v", err)
	}
	if err := cli.Set("API_KEY", "abc123"); err != nil {
		t.Fatalf("cli Set(): %v", err)
	}
	if _, err := cli.Import(map[string]string{"DB_URL": "postgres://x", "TOKEN": "t1"}); err != nil {
		t.Fatalf("cli Import(): %v", err)
	}

	// "server" reads should now reflect the cli writes.
	secrets := server.List()
	if len(secrets) != 3 {
		t.Fatalf("server List() after external write = %d, want 3", len(secrets))
	}

	val, ok := server.Get("API_KEY")
	if !ok || val != "abc123" {
		t.Errorf("server Get(API_KEY) = %q, %v; want %q, true", val, ok, "abc123")
	}
	if !server.Has("DB_URL") {
		t.Error("server Has(DB_URL) = false; want true")
	}
	if got := len(server.Keys()); got != 3 {
		t.Errorf("server Keys() len = %d, want 3", got)
	}

	// Subsequent CLI delete should also propagate.
	if err := cli.Delete("TOKEN"); err != nil {
		t.Fatalf("cli Delete(): %v", err)
	}
	if server.Has("TOKEN") {
		t.Error("server Has(TOKEN) after external delete = true; want false")
	}
}

func TestStore_ReloadsOnExternalWrite_Encrypted(t *testing.T) {
	dir := t.TempDir()
	const pass = "test-passphrase"

	// "server" creates an encrypted vault and holds the passphrase.
	server := NewStore(dir)
	if err := server.Load(); err != nil {
		t.Fatalf("server Load(): %v", err)
	}
	if err := server.Set("INITIAL", "v0"); err != nil {
		t.Fatalf("server Set(): %v", err)
	}
	if err := server.Lock(pass); err != nil {
		t.Fatalf("server Lock(): %v", err)
	}

	// "cli" loads, unlocks with the same passphrase, writes.
	cli := NewStore(dir)
	if err := cli.Load(); err != nil {
		t.Fatalf("cli Load(): %v", err)
	}
	if err := cli.Unlock(pass); err != nil {
		t.Fatalf("cli Unlock(): %v", err)
	}
	if err := cli.Set("API_KEY", "abc123"); err != nil {
		t.Fatalf("cli Set(): %v", err)
	}

	// Server reads should pick up the encrypted external write.
	secrets := server.List()
	if len(secrets) != 2 {
		t.Fatalf("server List() after external encrypted write = %d, want 2", len(secrets))
	}

	val, ok := server.Get("API_KEY")
	if !ok || val != "abc123" {
		t.Errorf("server Get(API_KEY) = %q, %v; want %q, true", val, ok, "abc123")
	}
}

func TestStore_LockedEncryptedReadIsNoop(t *testing.T) {
	dir := t.TempDir()

	// Set up an encrypted vault on disk.
	writer := NewStore(dir)
	if err := writer.Set("KEY", "val"); err != nil {
		t.Fatalf("writer Set(): %v", err)
	}
	if err := writer.Lock("pass"); err != nil {
		t.Fatalf("writer Lock(): %v", err)
	}

	// Fresh server starts with the vault locked (no passphrase known).
	server := NewStore(dir)
	if err := server.Load(); err != nil {
		t.Fatalf("server Load(): %v", err)
	}
	if !server.IsLocked() {
		t.Fatal("server should be locked after Load on encrypted vault")
	}

	// Reads should not error, should not unlock the vault, and should not
	// surface secret values from disk.
	for i := 0; i < 3; i++ {
		_ = server.List()
		_, _ = server.Get("KEY")
		_ = server.Keys()
		_ = server.Values()
	}
	if !server.IsLocked() {
		t.Error("server became unlocked after reads; reload-on-read must be no-op while locked")
	}
}

func TestStore_ReloadIgnoresCorruptFile(t *testing.T) {
	// A corrupt external write (e.g. a half-flushed file or manual edit
	// gone wrong) must not wipe the daemon's in-memory secrets. The parse
	// error propagates inside loadLocked but the existing maps are
	// preserved.
	dir := t.TempDir()

	store := NewStore(dir)
	if err := store.Set("KEY", "val"); err != nil {
		t.Fatalf("Set(): %v", err)
	}
	if got := len(store.List()); got != 1 {
		t.Fatalf("List() before corruption = %d, want 1", got)
	}

	// Overwrite the backing file with garbage. Use a distinct mtime to
	// guarantee the gate fires.
	path := filepath.Join(dir, "secrets.json")
	if err := os.WriteFile(path, []byte("not valid json {{{"), 0600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	val, ok := store.Get("KEY")
	if !ok || val != "val" {
		t.Errorf("Get(KEY) after corrupt overwrite = %q, %v; want %q, true", val, ok, "val")
	}
}

func TestStore_ReloadDetectsPlaintextToEncryptedTransition(t *testing.T) {
	// Cross-process: while a "daemon" Store has the plaintext vault loaded,
	// a "cli" Store calls Lock(). The daemon's next read must drop the
	// in-memory plaintext, report locked, and not leak the previous values.
	dir := t.TempDir()
	const pass = "test-passphrase"

	daemon := NewStore(dir)
	if err := daemon.Load(); err != nil {
		t.Fatalf("daemon Load(): %v", err)
	}
	if err := daemon.Set("API_KEY", "abc123"); err != nil {
		t.Fatalf("daemon Set(): %v", err)
	}
	if daemon.IsLocked() {
		t.Fatal("daemon IsLocked() before lock = true; want false")
	}

	cli := NewStore(dir)
	if err := cli.Load(); err != nil {
		t.Fatalf("cli Load(): %v", err)
	}
	if err := cli.Lock(pass); err != nil {
		t.Fatalf("cli Lock(): %v", err)
	}

	if !daemon.IsLocked() {
		t.Fatal("daemon IsLocked() after external lock = false; want true (plaintext leak)")
	}
	if got := daemon.List(); len(got) != 0 {
		t.Errorf("daemon List() after external lock = %d secrets; want 0", len(got))
	}
	if val, ok := daemon.Get("API_KEY"); ok {
		t.Errorf("daemon Get(API_KEY) after external lock = %q, true; want \"\", false", val)
	}
}

func TestStore_ReloadDetectsEncryptedToPlaintextTransition(t *testing.T) {
	// No CLI command performs encrypted → plaintext on disk today; the
	// symmetric branch is exercised here by direct file manipulation, which
	// also covers a manual restore-from-backup or future decrypt command.
	dir := t.TempDir()
	const pass = "test-passphrase"

	writer := NewStore(dir)
	if err := writer.Set("OLD_KEY", "old"); err != nil {
		t.Fatalf("writer Set(): %v", err)
	}
	if err := writer.Lock(pass); err != nil {
		t.Fatalf("writer Lock(): %v", err)
	}

	daemon := NewStore(dir)
	if err := daemon.Load(); err != nil {
		t.Fatalf("daemon Load(): %v", err)
	}
	if !daemon.IsLocked() {
		t.Fatal("daemon IsLocked() on encrypted vault = false; want true")
	}

	plaintext := []byte(`{"secrets":[{"key":"NEW_KEY","value":"new"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), plaintext, 0600); err != nil {
		t.Fatalf("write secrets.json: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "secrets.enc")); err != nil {
		t.Fatalf("remove secrets.enc: %v", err)
	}

	if daemon.IsLocked() {
		t.Error("daemon IsLocked() after external decrypt = true; want false")
	}
	if daemon.IsEncrypted() {
		t.Error("daemon IsEncrypted() after external decrypt = true; want false")
	}
	val, ok := daemon.Get("NEW_KEY")
	if !ok || val != "new" {
		t.Errorf("daemon Get(NEW_KEY) = %q, %v; want %q, true", val, ok, "new")
	}
	if _, ok := daemon.Get("OLD_KEY"); ok {
		t.Error("daemon Get(OLD_KEY) returned ok=true after external decrypt swap")
	}
}

func TestStore_IsLockedReflectsExternalLock(t *testing.T) {
	// IsLocked() and IsEncrypted() must observe an external lock without
	// a prior read call — handleVaultStatus reaches for them directly.
	dir := t.TempDir()
	const pass = "test-passphrase"

	daemon := NewStore(dir)
	if err := daemon.Load(); err != nil {
		t.Fatalf("daemon Load(): %v", err)
	}
	if err := daemon.Set("API_KEY", "abc123"); err != nil {
		t.Fatalf("daemon Set(): %v", err)
	}

	cli := NewStore(dir)
	if err := cli.Load(); err != nil {
		t.Fatalf("cli Load(): %v", err)
	}
	if err := cli.Lock(pass); err != nil {
		t.Fatalf("cli Lock(): %v", err)
	}

	if !daemon.IsLocked() {
		t.Fatal("daemon IsLocked() after external lock without prior read = false; want true")
	}
	if !daemon.IsEncrypted() {
		t.Fatal("daemon IsEncrypted() after external lock without prior read = false; want true")
	}
}

func TestStore_ReloadIgnoresMissingFile(t *testing.T) {
	// reloadIfChanged must treat a missing backing file as a no-op rather
	// than wiping in-memory state — otherwise a transient stat error would
	// surface as silent data loss to readers.
	dir := t.TempDir()

	store := NewStore(dir)
	if err := store.Set("KEY", "val"); err != nil {
		t.Fatalf("Set(): %v", err)
	}

	// Prime the cache with a read.
	if got := len(store.List()); got != 1 {
		t.Fatalf("List() before remove = %d, want 1", got)
	}

	// Remove the backing file. A correct reloadIfChanged treats a missing
	// file as no-op (preserve in-memory state).
	if err := os.Remove(filepath.Join(dir, "secrets.json")); err != nil {
		t.Fatalf("remove secrets.json: %v", err)
	}

	val, ok := store.Get("KEY")
	if !ok || val != "val" {
		t.Errorf("Get(KEY) after file removal = %q, %v; want %q, true", val, ok, "val")
	}
}
