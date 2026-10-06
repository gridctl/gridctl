package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockFile_StackStamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.lock.yaml")
	lf := &LockFile{Sources: map[string]LockedSource{
		"packsrc": {
			Repo: "https://example.com/p",
			Pack: &LockedPack{
				Name: "team-pack",
				Stack: &LockedStack{
					Path: "stack.yaml", Name: "team", ContentHash: "abc", CheckoutDir: "/tmp/co",
				},
			},
		},
	}}
	require.NoError(t, WriteLockFile(path, lf))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 5")
	assert.Contains(t, string(raw), "checkout_dir: /tmp/co")
	assert.NotContains(t, string(raw), "version: 4")

	got, err := ReadLockFile(path)
	require.NoError(t, err)
	require.NotNil(t, got.Sources["packsrc"].Pack.Stack)
	assert.Equal(t, "stack.yaml", got.Sources["packsrc"].Pack.Stack.Path)
	assert.Equal(t, "abc", got.Sources["packsrc"].Pack.Stack.ContentHash)

	// SSH fields without a stack stay at version 4. Variables without a
	// stack stay at version 3. A pack with neither stays at version 2.
	sshPath := filepath.Join(dir, "ssh.lock.yaml")
	require.NoError(t, WriteLockFile(sshPath, &LockFile{Sources: map[string]LockedSource{
		"s": {Repo: "ssh://git@127.0.0.1/x.git", SSHKeyPath: "/abs/key", Pack: &LockedPack{Name: "p"}},
	}}))
	sshRaw, err := os.ReadFile(sshPath)
	require.NoError(t, err)
	assert.Contains(t, string(sshRaw), "version: 4")

	required := true
	varPath := filepath.Join(dir, "vars.lock.yaml")
	require.NoError(t, WriteLockFile(varPath, &LockFile{Sources: map[string]LockedSource{
		"s": {Repo: "https://example.com/p", Pack: &LockedPack{
			Name:      "p",
			Variables: map[string]LockedVariableDeclaration{"TOKEN": {Required: &required}},
		}},
	}}))
	varRaw, err := os.ReadFile(varPath)
	require.NoError(t, err)
	assert.Contains(t, string(varRaw), "version: 3")
	assert.NotContains(t, string(varRaw), "version: 5")

	packPath := filepath.Join(dir, "pack.lock.yaml")
	require.NoError(t, WriteLockFile(packPath, &LockFile{Sources: map[string]LockedSource{
		"s": {Repo: "https://example.com/p", Pack: &LockedPack{Name: "p"}},
	}}))
	packRaw, err := os.ReadFile(packPath)
	require.NoError(t, err)
	assert.Contains(t, string(packRaw), "version: 2")
}

func TestLockFile_StackStampRefusedByPriorReader(t *testing.T) {
	if lockVersionStack != lockVersionSSHAuth+1 {
		t.Fatalf("stack stamp = %d, want %d", lockVersionStack, lockVersionSSHAuth+1)
	}
	// ReadLockFile refuses a file newer than the reader's maximum. A
	// reader that stops at the SSH stamp therefore refuses a stack file.
	if lockVersionStack <= lockVersionSSHAuth {
		t.Fatal("a version-4 reader would accept a stack-carrying file")
	}
	dir := t.TempDir()
	newer := filepath.Join(dir, "newer.lock.yaml")
	require.NoError(t, os.WriteFile(newer, []byte(fmt.Sprintf("version: %d\nsources: {}\n", ImportLockVersion+1)), 0o644))
	_, err := ReadLockFile(newer)
	require.ErrorIs(t, err, ErrNewerImportLockVersion)

	current := filepath.Join(dir, "stack.lock.yaml")
	require.NoError(t, os.WriteFile(current, []byte("version: 5\nsources:\n  pack:\n    repo: https://example.com/p\n    pack:\n      name: team\n      stack:\n        path: stack.yaml\n        name: team\n        content_hash: abc\n        checkout_dir: /tmp/co\n"), 0o644))
	got, err := ReadLockFile(current)
	require.NoError(t, err)
	require.NotNil(t, got.Sources["pack"].Pack.Stack)
	assert.Equal(t, "team", got.Sources["pack"].Pack.Stack.Name)
}
