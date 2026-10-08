package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/stretchr/testify/require"
)

func TestExportSkillsData_OmitsLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	require.NoError(t, skills.WriteLockFile(skills.LockFilePath(), &skills.LockFile{Sources: map[string]skills.LockedSource{
		"upstream": {Repo: "https://github.com/acme/skills", Ref: "main", Skills: map[string]skills.LockedSkill{"one": {Path: "one"}}},
		"local":    {Kind: skills.SourceKindLocal, Repo: filepath.Join(home, "skills"), Skills: map[string]skills.LockedSkill{"two": {Path: "."}}},
	}}))
	data, omitted, err := exportSkillsData(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, omitted)
	require.NotContains(t, string(data), "local paths")
	require.Contains(t, string(data), "https://github.com/acme/skills")
	require.NotContains(t, string(data), filepath.Join(home, "skills"))
}
