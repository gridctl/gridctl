package project

// LockfileState describes which projection lockfiles exist on disk,
// for surfaces like `gridctl doctor` that report migration state
// without loading entries.
type LockfileState struct {
	// UnifiedPath is where the unified lockfile lives (or will live).
	UnifiedPath string
	// Unified reports whether the unified lockfile exists.
	Unified bool
	// LegacySkill and LegacyContext report live (version 1) legacy
	// lockfiles awaiting migration.
	LegacySkill   bool
	LegacyContext bool
	// Tombstones lists legacy paths already tombstoned by a migration.
	Tombstones []string
	// BackupRoot is where migration backups are kept.
	BackupRoot string
}

// InspectLockfiles reports the on-disk projection lockfile state under
// home. Read-only; unreadable or malformed legacy files are reported as
// live so the caller surfaces them rather than ignoring them.
func InspectLockfiles(home string) LockfileState {
	s := NewStore(home)
	st := LockfileState{
		UnifiedPath: s.Path(),
		Unified:     fileExists(s.Path()),
		BackupRoot:  s.migrationBackupRoot(),
	}
	for _, probe := range []struct {
		path string
		live *bool
	}{
		{s.legacySkillPath(), &st.LegacySkill},
		{s.legacyContextPath(), &st.LegacyContext},
	} {
		// An unreadable or malformed legacy file counts as live: the
		// caller should surface it, not ignore it.
		var head struct{}
		exists, tombstone, _ := readLegacyFile(probe.path, &head)
		switch {
		case !exists:
		case tombstone:
			st.Tombstones = append(st.Tombstones, probe.path)
		default:
			*probe.live = true
		}
	}
	return st
}
