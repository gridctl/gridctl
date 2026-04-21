package skills

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// UpdateStatus records the result of a background update check.
type UpdateStatus struct {
	CheckedAt time.Time              `yaml:"checked_at"`
	Updates   map[string]SkillUpdate `yaml:"updates,omitempty"`
	Errors    []string               `yaml:"errors,omitempty"`
}

// SkillUpdate describes an available update for a skill.
type SkillUpdate struct {
	CurrentSHA string `yaml:"current_sha"`
	LatestSHA  string `yaml:"latest_sha"`
	Repo       string `yaml:"repo"`
	Ref        string `yaml:"ref"`
}

// UpdateCachePath returns the path to the cached update status file.
func UpdateCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gridctl", "cache", "skill-updates.yaml")
}

// ReadUpdateCache reads the cached update status.
func ReadUpdateCache() (*UpdateStatus, error) {
	path := UpdateCachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var status UpdateStatus
	if err := yaml.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// WriteUpdateCache writes the update status to cache.
func WriteUpdateCache(status *UpdateStatus) error {
	path := UpdateCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(status)
	if err != nil {
		return err
	}
	return atomicWriteBytes(path, data)
}

// ShouldCheckUpdates returns false if update checks are disabled.
func ShouldCheckUpdates() bool {
	if os.Getenv("GRIDCTL_NO_SKILL_UPDATE_CHECK") == "1" {
		return false
	}
	if os.Getenv("CI") == "true" {
		return false
	}
	return true
}

// CheckUpdatesBackground runs update checks in a background goroutine.
// Results are written to the cache file for display on next CLI command.
func CheckUpdatesBackground(registryDir string, logger *slog.Logger) {
	if !ShouldCheckUpdates() {
		return
	}

	go func() {
		status := checkAllUpdates(registryDir, logger)
		if err := WriteUpdateCache(status); err != nil {
			logger.Warn("failed to write update cache", "error", err)
		}
	}()
}

func checkAllUpdates(registryDir string, logger *slog.Logger) *UpdateStatus {
	status := &UpdateStatus{
		CheckedAt: time.Now().UTC(),
		Updates:   make(map[string]SkillUpdate),
	}

	skillsDir := filepath.Join(registryDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return status
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 3) // Limit concurrent checks

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(skillsDir, entry.Name())
		if !HasOrigin(skillDir) {
			continue
		}

		wg.Add(1)
		go func(dir, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			origin, err := ReadOrigin(dir)
			if err != nil {
				return
			}

			// Background check uses ambient auth only. A stored CredentialRef
			// is recorded on origin, but resolving vault refs from a
			// background goroutine requires a wired resolver — skip that
			// source rather than fail loudly.
			if origin.CredentialRef != "" {
				return
			}
			newSHA, changed, err := FetchAndCompare(origin.Repo, origin.Ref, origin.CommitSHA, AuthConfig{}, logger)
			if err != nil {
				mu.Lock()
				status.Errors = append(status.Errors, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}

			if changed {
				mu.Lock()
				status.Updates[name] = SkillUpdate{
					CurrentSHA: origin.CommitSHA,
					LatestSHA:  newSHA,
					Repo:       origin.Repo,
					Ref:        origin.Ref,
				}
				mu.Unlock()
			}
		}(skillDir, entry.Name())
	}

	wg.Wait()
	return status
}

// FormatUpdateNotice returns a user-friendly message about available updates.
func FormatUpdateNotice() string {
	status, err := ReadUpdateCache()
	if err != nil || status == nil {
		return ""
	}

	if len(status.Updates) == 0 {
		return ""
	}

	msg := fmt.Sprintf("\n%d skill update(s) available. Run 'gridctl skill update' to update.\n", len(status.Updates))
	return msg
}
