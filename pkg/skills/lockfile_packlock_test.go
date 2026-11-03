package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockFile_PackLockStampOnlyWhenNeeded(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.lock.yaml")
	require.NoError(t, WriteLockFile(plain, &LockFile{Sources: map[string]LockedSource{
		"s": {Repo: "https://example.com/p", Pack: &LockedPack{Name: "p", Stack: &LockedStack{Path: "stack.yaml", Name: "p"}}},
	}}))
	raw, err := os.ReadFile(plain)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 5")
	assert.NotContains(t, string(raw), "version: 8")

	locked := filepath.Join(dir, "locked.lock.yaml")
	require.NoError(t, WriteLockFile(locked, &LockFile{Sources: map[string]LockedSource{
		"s": {Repo: "https://example.com/p", Pack: &LockedPack{
			Name: "p",
			Lock: &LockedPackLock{State: "honored", Digest: "sha256:abc", Path: "gridctl-pack.lock.yaml"},
		}},
	}}))
	raw, err = os.ReadFile(locked)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 8")

	member := filepath.Join(dir, "member.lock.yaml")
	require.NoError(t, WriteLockFile(member, &LockFile{Sources: map[string]LockedSource{
		"p/netops": {Repo: "https://example.com/n", PackMember: "p", LockedBy: "p"},
	}}))
	raw, err = os.ReadFile(member)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 8")
	assert.Contains(t, string(raw), "locked_by: p")
}

func TestLockFile_FragmentRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fragment.lock.yaml")
	in := &LockFile{Sources: map[string]LockedSource{
		"booster": {Repo: "https://example.com/b", Pack: &LockedPack{
			Name: "booster",
			Fragment: &LockedFragment{
				Base: "infra", Path: "servers.yaml", Name: "infra",
				ContentHash: "sha256:abc", CheckoutDir: "/tmp/co",
			},
		}},
	}}
	require.NoError(t, WriteLockFile(path, in))
	got, err := ReadLockFile(path)
	require.NoError(t, err)
	frag := got.Sources["booster"].Pack.Fragment
	require.NotNil(t, frag)
	assert.Equal(t, "infra", frag.Base)
	assert.Equal(t, "servers.yaml", frag.Path)
	assert.Equal(t, "infra", frag.Name)
	assert.Equal(t, "sha256:abc", frag.ContentHash)
	assert.Equal(t, "/tmp/co", frag.CheckoutDir)
	// ReadLockFile reports the reader's maximum. The on-disk stamp stays at
	// the fragment version when the record has no hooks.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 8")
	assert.Equal(t, ImportLockVersion, got.Version)
}

func TestLockFile_HooksStampVersion9AndRetainSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.lock.yaml")
	in := &LockFile{Sources: map[string]LockedSource{
		"pack": {
			Repo: "https://example.com/pack",
			Skills: map[string]LockedSkill{
				"alpha": {Path: "skills/alpha", ContentHash: "h"},
			},
			Pack: &LockedPack{Name: "team", Hooks: []string{"block-force-push"}, Rules: []string{"style"}},
		},
	}}
	require.NoError(t, WriteLockFile(path, in))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "version: 9")

	without := &LockFile{Sources: map[string]LockedSource{
		"plain": {Repo: "https://example.com/skills", Pack: &LockedPack{Name: "plain", Skills: []string{"alpha"}}},
	}}
	plainPath := filepath.Join(dir, "plain.lock.yaml")
	require.NoError(t, WriteLockFile(plainPath, without))
	plain, err := os.ReadFile(plainPath)
	require.NoError(t, err)
	assert.Contains(t, string(plain), "version: 2")
	assert.NotContains(t, string(plain), "version: 9")

	lf, err := ReadLockFile(path)
	require.NoError(t, err)
	lf.RemoveSkill("alpha")
	src, ok := lf.Sources["pack"]
	require.True(t, ok, "source must survive while hooks or rules remain")
	assert.Equal(t, []string{"block-force-push"}, src.Pack.Hooks)
	lf.RemoveHook("block-force-push")
	src, ok = lf.Sources["pack"]
	require.True(t, ok, "source must survive while rules remain")
	assert.Empty(t, src.Pack.Hooks)
	// Drop the rules and confirm the source goes once nothing remains.
	src.Pack.Rules = nil
	lf.Sources["pack"] = src
	lf.RemoveAgent("missing")
	// RemoveAgent on a missing name is a no-op. Clear by removing the last
	// retained selection through the skill path after re-adding a skill.
	src.Skills = map[string]LockedSkill{"beta": {Path: "b"}}
	lf.Sources["pack"] = src
	lf.RemoveSkill("beta")
	_, ok = lf.Sources["pack"]
	assert.False(t, ok)
}

func TestLockFile_PackLockNewerRefused(t *testing.T) {
	if lockVersionPackLock != lockVersionPackSources+1 {
		t.Fatalf("pack lock stamp = %d, want %d", lockVersionPackLock, lockVersionPackSources+1)
	}
	dir := t.TempDir()
	newer := filepath.Join(dir, "newer.lock.yaml")
	require.NoError(t, os.WriteFile(newer, []byte("version: 10\nsources: {}\n"), 0o644))
	_, err := ReadLockFile(newer)
	require.ErrorIs(t, err, ErrNewerImportLockVersion)
}
