package state

import (
	"fmt"
	"os"
	"path/filepath"
)

// HomeEnv is the environment variable that replaces the home directory
// for every gridctl-derived path: the ~/.gridctl state tree and the
// client projection targets (~/.claude, ~/.gemini, ...) alike. It is a
// $HOME replacement, not a data-dir override: pointing it at /tmp/demo
// gives a fully isolated instance whose projections land under
// /tmp/demo/.claude and never touch the real client directories.
//
// The --home global flag is sugar that sets this variable in-process
// before any path resolves; the daemon child inherits it through
// os.Environ(), which is why the env var is the primary mechanism.
const HomeEnv = "GRIDCTL_HOME"

// Home resolves the directory gridctl treats as the user's home:
// GRIDCTL_HOME when set (must be absolute), otherwise the OS home.
// It never falls back to a relative path: a home that cannot be
// resolved is an error, so downstream destructive operations
// (gridctl reset --purge does an os.RemoveAll under this path) can
// never target the working directory by accident.
func Home() (string, error) {
	if h := os.Getenv(HomeEnv); h != "" {
		if !filepath.IsAbs(h) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", HomeEnv, h)
		}
		return filepath.Clean(h), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w (set %s to override)", err, HomeEnv)
	}
	return h, nil
}

// HomeOverridden reports whether the active home comes from GRIDCTL_HOME
// rather than the OS home. Commands that mutate state print a one-line
// stderr disclosure when this is true.
func HomeOverridden() bool {
	return os.Getenv(HomeEnv) != ""
}
