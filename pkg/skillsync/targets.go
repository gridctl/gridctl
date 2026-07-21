package skillsync

import (
	"os"
	"path/filepath"
	"strings"
)

// Channel is how one skill reaches one client: a symlink into the
// registry (edits propagate instantly, no drift class) or a full copy
// (needed where the client does not follow symlinked skill dirs).
type Channel string

const (
	ChannelSymlink Channel = "symlink"
	ChannelCopy    Channel = "copy"
)

// Target describes one client's native skills directory. SkillsPath is a
// ~-template expanded against the Manager's home directory.
type Target struct {
	Slug string
	Name string
	// SkillsPath is the directory skills are projected into; each skill
	// becomes SkillsPath/<name> (a symlink or a copied directory).
	SkillsPath string
	// DetectDirs mark the client as initialized on this machine when any
	// of them exists. Sync refuses to create client config trees
	// wholesale, so these gate every write unless AlwaysAvailable is set.
	DetectDirs []string
	// AlwaysAvailable targets skip detection: the vendor-neutral
	// ~/.agents/skills interop dir is created on first projection because
	// gating on its existence would silently skip clients (Grok Build)
	// that read it without ever creating it.
	AlwaysAvailable bool
	// DefaultChannel is used when the user does not pass --copy.
	DefaultChannel Channel
	// ForcedChannel, when set, overrides both the default and --copy.
	// Antigravity is copy-forced until symlink discovery is verified on
	// its exact path (symlinks went undiscovered under a related Gemini
	// skills path; vercel-labs/skills#633).
	ForcedChannel Channel
	// Experimental marks targets whose documented path rests on
	// unofficial sourcing; surfaced in status output.
	Experimental bool
}

// Targets returns the supported projection targets in display order.
// Slugs match pkg/contexts and pkg/provisioner so every gridctl surface
// speaks one client-identifier language ("agents" names the shared
// interop dir, which is multi-client by design).
func Targets() []Target {
	return []Target{
		{
			Slug:            "agents",
			Name:            "Agents interop dir",
			SkillsPath:      "~/.agents/skills",
			AlwaysAvailable: true,
			DefaultChannel:  ChannelSymlink,
		},
		{
			Slug:           "claude-code",
			Name:           "Claude Code",
			SkillsPath:     "~/.claude/skills",
			DetectDirs:     []string{"~/.claude"},
			DefaultChannel: ChannelSymlink,
		},
		{
			Slug:       "antigravity",
			Name:       "Antigravity",
			SkillsPath: "~/.gemini/config/skills",
			// Antigravity-specific dirs, not plain ~/.gemini, so a
			// Gemini-CLI-only install doesn't read as Antigravity
			// (mirrors the pkg/contexts target).
			DetectDirs:     []string{"~/.gemini/config", "~/.gemini/antigravity"},
			DefaultChannel: ChannelCopy,
			ForcedChannel:  ChannelCopy,
			Experimental:   true,
		},
	}
}

// FindTarget returns the projection target for slug.
func FindTarget(slug string) (Target, bool) {
	for _, t := range Targets() {
		if t.Slug == slug {
			return t, true
		}
	}
	return Target{}, false
}

// SupportedSlugs lists the target slugs, derived from the table so error
// messages never go stale.
func SupportedSlugs() []string {
	targets := Targets()
	slugs := make([]string, len(targets))
	for i, t := range targets {
		slugs[i] = t.Slug
	}
	return slugs
}

// expandHome resolves a ~-template against home (mirrors pkg/contexts).
func expandHome(home, template string) string {
	if strings.HasPrefix(template, "~") {
		return filepath.Join(home, strings.TrimPrefix(template, "~"))
	}
	return template
}

// skillsDir resolves the target's skills directory against home.
func (t Target) skillsDir(home string) string {
	return expandHome(home, t.SkillsPath)
}

// available reports whether skills may be projected to this target:
// always for AlwaysAvailable targets, otherwise when any detect dir
// exists (the client is initialized on this machine).
func (t Target) available(home string) bool {
	if t.AlwaysAvailable {
		return true
	}
	for _, d := range t.DetectDirs {
		if info, err := os.Stat(expandHome(home, d)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// channel resolves the effective channel for one sync invocation:
// forced beats everything, then --copy, then the target default.
func (t Target) channel(copyRequested bool) Channel {
	if t.ForcedChannel != "" {
		return t.ForcedChannel
	}
	if copyRequested {
		return ChannelCopy
	}
	return t.DefaultChannel
}
