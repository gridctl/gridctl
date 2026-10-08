package skills

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSourceName_Branches(t *testing.T) {
	const tracked = "/tmp/a/skills"
	lf := &LockFile{Sources: map[string]LockedSource{
		"skills": {Kind: SourceKindLocal, Repo: tracked},
	}}

	reused, err := ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: tracked})
	require.NoError(t, err)
	assert.Equal(t, "skills", reused)

	_, err = ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: tracked, Explicit: "other"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already tracks")

	named, err := ResolveSourceName(&LockFile{}, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/fresh", Explicit: "chosen"})
	require.NoError(t, err)
	assert.Equal(t, "chosen", named)

	known, err := ResolveSourceName(&LockFile{}, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/fresh", KnownName: "claude-code"})
	require.NoError(t, err)
	assert.Equal(t, "claude-code", known)

	base, err := ResolveSourceName(&LockFile{}, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/fresh/widget"})
	require.NoError(t, err)
	assert.Equal(t, "widget", base)

	_, err = ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/c/skills"})
	require.ErrorIs(t, err, ErrSourceConflict)

	suffixed, err := ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/b/skills", ClientImport: true})
	require.NoError(t, err)
	assert.Equal(t, "skills-"+rootSuffix("/tmp/b/skills"), suffixed)

	taken := &LockFile{Sources: map[string]LockedSource{
		"skills":                                {Kind: SourceKindLocal, Repo: tracked},
		"skills-" + rootSuffix("/tmp/b/skills"): {Kind: SourceKindLocal, Repo: "/elsewhere"},
	}}
	_, err = ResolveSourceName(taken, SourceNameInput{Kind: SourceKindLocal, Root: "/tmp/b/skills", ClientImport: true})
	require.ErrorIs(t, err, ErrSourceConflict)

	gitNamed, err := ResolveSourceName(nil, SourceNameInput{Root: "https://github.com/acme/widgets.git", Explicit: "custom"})
	require.NoError(t, err)
	assert.Equal(t, "custom", gitNamed)
	gitDerived, err := ResolveSourceName(nil, SourceNameInput{Root: "https://github.com/acme/widgets.git"})
	require.NoError(t, err)
	assert.Equal(t, "widgets", gitDerived)
}

func TestResolveSourceName_SuffixStableOnReimport(t *testing.T) {
	root := "/tmp/b/skills"
	lf := &LockFile{Sources: map[string]LockedSource{
		"skills": {Kind: SourceKindLocal, Repo: "/tmp/a/skills"},
	}}
	first, err := ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: root, ClientImport: true})
	require.NoError(t, err)
	lf.Sources[first] = LockedSource{Kind: SourceKindLocal, Repo: root}
	second, err := ResolveSourceName(lf, SourceNameInput{Kind: SourceKindLocal, Root: root, ClientImport: true})
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestAllowOverwrite(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"local": {Kind: SourceKindLocal, Skills: map[string]LockedSkill{"alpha": {}}, Agents: map[string]LockedAgent{"reviewer": {}}},
		"git":   {Skills: map[string]LockedSkill{"beta": {}}, Agents: map[string]LockedAgent{"planner": {}}},
	}}
	cases := []struct {
		name     string
		kind     string
		resource string
		source   string
		selected bool
		force    bool
		agent    bool
		want     bool
	}{
		{"force always", SourceKindLocal, "alpha", "other", false, true, false, true},
		{"local unowned", SourceKindLocal, "fresh", "local", false, false, false, true},
		{"local same owner", SourceKindLocal, "alpha", "local", false, false, false, true},
		{"local other local owner", SourceKindLocal, "alpha", "other", true, false, false, false},
		{"local git owner", SourceKindLocal, "beta", "other", true, false, false, false},
		{"git selected", "", "beta", "git", true, false, false, true},
		{"git unselected", "", "beta", "git", false, false, false, false},
		{"git selected local owner", "", "alpha", "git", true, false, false, false},
		{"git agent selected unowned", "", "fresh", "git", true, false, true, true},
		{"git agent selected same", "", "planner", "git", true, false, true, true},
		{"git agent other local", "", "reviewer", "git", true, false, true, false},
		{"git agent unselected", "", "planner", "git", false, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AllowOverwrite(lf, tc.kind, tc.resource, tc.source, tc.selected, tc.force, tc.agent)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGuardPackOwnership(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"local": {Kind: SourceKindLocal, Skills: map[string]LockedSkill{"alpha": {}}, Agents: map[string]LockedAgent{"reviewer": {}}},
	}}
	disc := &CloneResult{
		Skills: []DiscoveredSkill{{Name: "alpha"}, {Name: "beta"}},
		Agents: []DiscoveredAgent{{Name: "reviewer"}},
	}
	require.NoError(t, GuardPackOwnership(lf, "pack", nil, ImportOptions{}))
	err := GuardPackOwnership(lf, "pack", disc, ImportOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alpha")

	require.NoError(t, GuardPackOwnership(lf, "local", disc, ImportOptions{Selected: []string{"beta"}}))
	err = GuardPackOwnership(lf, "pack", disc, ImportOptions{ResourceKind: ResourceKindAgent})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reviewer")
	require.NoError(t, GuardPackOwnership(lf, "pack", disc, ImportOptions{Selected: []string{"beta"}}))
}

func TestReleaseInstalledNames_KeepsUnrelatedAndPack(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"mine": {
			Kind: SourceKindLocal,
			Repo: "/tmp/mine",
			Skills: map[string]LockedSkill{
				"one":  {Path: "one", TreeHash: "sha256:one"},
				"keep": {Path: "keep", TreeHash: "sha256:keep"},
			},
			Agents: map[string]LockedAgent{"side": {Path: "agents/side.md", ContentHash: "aa"}},
			Pack:   &LockedPack{Name: "kept"},
		},
		"git": {Skills: map[string]LockedSkill{"one": {Path: "."}}},
	}}
	ReleaseInstalledNames(lf, "upstream", false, map[string]struct{}{"one": {}}, nil)
	mine := lf.Sources["mine"]
	assert.NotContains(t, mine.Skills, "one")
	assert.Contains(t, mine.Skills, "keep")
	assert.Contains(t, mine.Agents, "side")
	require.NotNil(t, mine.Pack)
	assert.Equal(t, "kept", mine.Pack.Name)
	assert.Equal(t, CombineTrackedSourceHash(mine.Skills, mine.Agents), mine.ContentHash)
	assert.Contains(t, lf.Sources["git"].Skills, "one")

	lf.Sources["other"] = LockedSource{
		Kind:   SourceKindLocal,
		Agents: map[string]LockedAgent{"side": {Path: "agents/side.md", ContentHash: "aa"}},
	}
	ReleaseInstalledNames(lf, "mine", true, nil, map[string]struct{}{"side": {}})
	assert.Contains(t, lf.Sources["mine"].Agents, "side")
	_, otherLeft := lf.Sources["other"]
	assert.False(t, otherLeft)
}

func TestReleaseInstalledNames_DeletesEmptiedSource(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"mine": {Kind: SourceKindLocal, Skills: map[string]LockedSkill{"one": {}}},
	}}
	ReleaseInstalledNames(lf, "upstream", false, map[string]struct{}{"one": {}}, nil)
	_, ok := lf.Sources["mine"]
	assert.False(t, ok)
}

func TestRecordLocalSource_RenameKeepsInstalledName(t *testing.T) {
	lf := &LockFile{}
	installed := map[string]LockedSkill{
		"renamed-widget": {Path: ".", ContentHash: "abc", TreeHash: "sha256:abc"},
	}
	err := RecordLocalSource(lf, "widget", "/tmp/widget", time.Time{}, installed, nil, &CloneResult{
		Skills: []DiscoveredSkill{{Name: "widget", Path: "."}},
	}, true)
	require.NoError(t, err)
	src := lf.Sources["widget"]
	assert.Contains(t, src.Skills, "renamed-widget")
	assert.NotContains(t, src.Skills, "widget")
	assert.Equal(t, CombineTrackedSourceHash(src.Skills, nil), src.ContentHash)
	assert.NotEqual(t, CombineTrackedSourceHash(nil, nil), src.ContentHash)
}

func TestRecordLocalSource_DropsAbsentOnlyWhenAllowed(t *testing.T) {
	prev := map[string]LockedSkill{
		"alpha": {Path: "alpha", TreeHash: "sha256:a"},
		"beta":  {Path: "beta", TreeHash: "sha256:b"},
	}
	lf := &LockFile{Sources: map[string]LockedSource{
		"src": {Kind: SourceKindLocal, Repo: "/tmp/src", Skills: prev},
	}}
	installed := map[string]LockedSkill{"alpha": {Path: "alpha", TreeHash: "sha256:a2"}}
	discovered := &CloneResult{Skills: []DiscoveredSkill{{Name: "alpha", Path: "alpha"}}}
	require.NoError(t, RecordLocalSource(lf, "src", "/tmp/src", lf.Sources["src"].FetchedAt, installed, nil, discovered, false))
	assert.Contains(t, lf.Sources["src"].Skills, "beta")

	require.NoError(t, RecordLocalSource(lf, "src", "/tmp/src", lf.Sources["src"].FetchedAt, installed, nil, discovered, true))
	assert.NotContains(t, lf.Sources["src"].Skills, "beta")
	assert.Contains(t, lf.Sources["src"].Skills, "alpha")
}

func TestGitLockSourceName(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"repo-skills": {Repo: "https://github.com/acme/skills.git", Skills: map[string]LockedSkill{"one": {}}},
		"other":       {Repo: "https://github.com/acme/other.git", Skills: map[string]LockedSkill{"two": {}}},
		"local":       {Kind: SourceKindLocal, Repo: "/tmp/skills", Skills: map[string]LockedSkill{"three": {}}},
	}}
	assert.Equal(t, "repo-skills", gitLockSourceName(lf, &Origin{Repo: "https://github.com/acme/skills.git"}, "one", true))
	assert.Equal(t, "", gitLockSourceName(lf, &Origin{Repo: "https://github.com/acme/skills.git"}, "missing", true))
	assert.Equal(t, "", gitLockSourceName(lf, &Origin{Repo: "/tmp/skills", Kind: SourceKindLocal}, "three", true))
	assert.Equal(t, "", gitLockSourceName(lf, &Origin{Repo: "https://github.com/acme/other.git"}, "one", true))
}

func TestRemoveSkill_RecomputesLocalHash(t *testing.T) {
	lf := &LockFile{Sources: map[string]LockedSource{
		"src": {
			Kind:        SourceKindLocal,
			Repo:        "/tmp/src",
			ContentHash: "stale",
			Skills: map[string]LockedSkill{
				"alpha": {Path: "alpha", TreeHash: "sha256:aaa"},
				"beta":  {Path: "beta", TreeHash: "sha256:bbb"},
			},
			Agents: map[string]LockedAgent{"reviewer": {Path: "agents/reviewer.md", ContentHash: "ccc"}},
		},
		"git": {
			Repo:        "https://example.com/r.git",
			ContentHash: "abc",
			Skills:      map[string]LockedSkill{"one": {}, "two": {}},
		},
	}}
	lf.RemoveSkill("beta")
	src := lf.Sources["src"]
	assert.NotContains(t, src.Skills, "beta")
	assert.Equal(t, CombineTrackedSourceHash(src.Skills, src.Agents), src.ContentHash)
	assert.NotEqual(t, "stale", src.ContentHash)

	lf.RemoveAgent("reviewer")
	src = lf.Sources["src"]
	assert.NotContains(t, src.Agents, "reviewer")
	assert.Equal(t, CombineTrackedSourceHash(src.Skills, src.Agents), src.ContentHash)

	lf.RemoveSkill("one")
	assert.Equal(t, "abc", lf.Sources["git"].ContentHash)
	assert.NotContains(t, lf.Sources["git"].Skills, "one")
}

func TestCheckedLocationsAndProjectionHint(t *testing.T) {
	home := t.TempDir()
	locs := CheckedLocations("opencode", home)
	assert.Contains(t, locs, filepath.Join(home, ".config", "opencode", "skills"))
	assert.Contains(t, locs, filepath.Join(home, ".claude", "skills"))
	assert.NotContains(t, CheckedLocations("missing", home), home)
	assert.Contains(t, ProjectionHint("claude-code"), "gridctl skill project sync --client claude-code")
}
