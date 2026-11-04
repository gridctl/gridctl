package hookpins

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// hookNamePattern matches skills.ValidateHookName. This package cannot import
// pkg/skills: that package imports hookpins to pin a hook on import.
var hookNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validHookName(name string) error {
	if name == "" {
		return fmt.Errorf("hook name is required")
	}
	if !hookNamePattern.MatchString(name) {
		return fmt.Errorf("hook name %q must be lowercase letters, digits, and hyphens", name)
	}
	return nil
}

// registryHookDir is the hook directory under registryDir. The name is
// checked before the join so a path segment cannot leave the registry.
func registryHookDir(registryDir, name string) (string, error) {
	if err := validHookName(name); err != nil {
		return "", fmt.Errorf("hookpins: %w", err)
	}
	root := filepath.Join(registryDir, "hooks")
	dir := filepath.Join(root, name)
	rel, err := filepath.Rel(root, dir)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("hookpins: hook name %q must be lowercase letters, digits, and hyphens", name)
	}
	return dir, nil
}
