package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSkillsConfig(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(t *testing.T, cfg *SkillsConfig)
	}{
		{
			name: "basic config",
			yaml: `
defaults:
  auto_update: true
  update_interval: "12h"
sources:
  - name: my-skills
    repo: https://github.com/org/skills
    ref: main
`,
			check: func(t *testing.T, cfg *SkillsConfig) {
				assert.True(t, cfg.Defaults.AutoUpdate)
				assert.Equal(t, "12h", cfg.Defaults.UpdateInterval)
				assert.Len(t, cfg.Sources, 1)
				assert.Equal(t, "my-skills", cfg.Sources[0].Name)
				assert.Equal(t, "https://github.com/org/skills", cfg.Sources[0].Repo)
				assert.Equal(t, "main", cfg.Sources[0].Ref)
			},
		},
		{
			name: "auto-generates name from repo",
			yaml: `
sources:
  - repo: https://github.com/org/cool-skills.git
`,
			check: func(t *testing.T, cfg *SkillsConfig) {
				assert.Equal(t, "cool-skills", cfg.Sources[0].Name)
			},
		},
		{
			name: "missing repo",
			yaml: `
sources:
  - name: broken
`,
			wantErr: true,
		},
		{
			name: "per-source auto_update override",
			yaml: `
defaults:
  auto_update: true
sources:
  - repo: https://github.com/org/skills
    auto_update: false
`,
			check: func(t *testing.T, cfg *SkillsConfig) {
				assert.False(t, cfg.EffectiveAutoUpdate(&cfg.Sources[0]))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "skills.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0644))

			cfg, err := LoadSkillsConfig(path)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestEffectiveUpdateInterval(t *testing.T) {
	cfg := DefaultSkillsConfig()

	// Default interval
	src := &SkillSource{}
	assert.Equal(t, 24*time.Hour, cfg.EffectiveUpdateInterval(src))

	// Per-source override
	src.UpdateInterval = "6h"
	assert.Equal(t, 6*time.Hour, cfg.EffectiveUpdateInterval(src))

	// Invalid interval falls back to 24h
	src.UpdateInterval = "invalid"
	assert.Equal(t, 24*time.Hour, cfg.EffectiveUpdateInterval(src))
}

func TestIsSemVerConstraint(t *testing.T) {
	tests := []struct {
		ref    string
		expect bool
	}{
		{"^1.2.0", true},
		{"~2.0", true},
		{">=1.0.0", true},
		{"<3.0.0", true},
		{"main", false},
		{"v1.2.3", false},
		{"abc123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			assert.Equal(t, tt.expect, IsSemVerConstraint(tt.ref))
		})
	}
}

func TestResolveSemVerConstraint(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		tags       []string
		wantTag    string
		wantErr    bool
	}{
		{
			name:       "caret constraint",
			constraint: "^1.2.0",
			tags:       []string{"v1.1.0", "v1.2.0", "v1.2.5", "v1.3.0", "v2.0.0"},
			wantTag:    "v1.3.0",
		},
		{
			name:       "tilde constraint",
			constraint: "~1.2.0",
			tags:       []string{"v1.1.0", "v1.2.0", "v1.2.5", "v1.3.0"},
			wantTag:    "v1.2.5",
		},
		{
			name:       "no match",
			constraint: "^5.0.0",
			tags:       []string{"v1.0.0", "v2.0.0"},
			wantErr:    true,
		},
		{
			name:       "tags without v prefix",
			constraint: "^1.0.0",
			tags:       []string{"1.0.0", "1.1.0", "1.2.0"},
			wantTag:    "1.2.0",
		},
		{
			name:       "mixed valid and invalid tags",
			constraint: "^1.0.0",
			tags:       []string{"v1.0.0", "not-a-version", "v1.1.0", "latest"},
			wantTag:    "v1.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, err := ResolveSemVerConstraint(tt.constraint, tt.tags)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTag, tag)
		})
	}
}

func TestRepoToName(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"https://github.com/org/repo.git", "repo"},
		{"https://github.com/org/my-skills", "my-skills"},
		{"git@github.com:org/repo.git", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			assert.Equal(t, tt.want, repoToName(tt.repo))
		})
	}
}
