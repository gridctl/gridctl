package service

import (
	"os"
	"path/filepath"
	"strings"
)

// SanitizeName maps a stack name onto the supervisor unit alphabet
// [A-Za-z0-9._-]. Other runes become "-", runs of "-" collapse, and
// leading and trailing "-" and "." are trimmed.
func SanitizeName(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range name {
		if isNameRune(r) {
			b.WriteRune(r)
			prevDash = r == '-'
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := b.String()
	// Collapse any dash run that substitution left next to an existing dash.
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-.")
}

func isNameRune(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
}

// Label is the launchd label and the stable unit identity.
func Label(name string) string {
	return "dev.gridctl." + SanitizeName(name)
}

// UnitPath returns the supervisor unit path for name.
//
// Linux uses <home>/.config/systemd/user/gridctl-<name>.service, except
// that XDG_CONFIG_HOME is honored when it is set and either the home is
// not overridden or the cleaned value is inside home. An XDG_CONFIG_HOME
// outside an overridden home is ignored so `gridctl --home /tmp/demo
// service install` cannot write into the real ~/.config.
// macOS always uses <home>/Library/LaunchAgents/dev.gridctl.<name>.plist.
func UnitPath(home string, homeOverridden bool, platform Platform, name string) string {
	name = SanitizeName(name)
	switch platform {
	case PlatformLaunchd:
		return filepath.Join(home, "Library", "LaunchAgents", "dev.gridctl."+name+".plist")
	default:
		base := filepath.Join(home, ".config")
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			cleaned := filepath.Clean(xdg)
			if !homeOverridden || pathInside(home, cleaned) {
				base = cleaned
			}
		}
		return filepath.Join(base, "systemd", "user", "gridctl-"+name+".service")
	}
}

// pathInside reports whether path is home or a descendant of home.
func pathInside(home, path string) bool {
	if home == "" || path == "" {
		return false
	}
	home = filepath.Clean(home)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(home, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// UnitFileName is the systemd unit filename (not a path).
func UnitFileName(name string) string {
	return "gridctl-" + SanitizeName(name) + ".service"
}
