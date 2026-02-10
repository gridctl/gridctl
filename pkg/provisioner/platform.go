package provisioner

import (
	"os"
	"path/filepath"
	"runtime"
)

// homeDir returns the current user's home directory.
func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// expandPath resolves platform-specific config paths.
// Handles ~ expansion and %APPDATA%/%USERPROFILE% on Windows.
func expandPath(template string) string {
	home := homeDir()

	switch runtime.GOOS {
	case "windows":
		// Expand Windows environment variables
		template = os.ExpandEnv(template)
	}

	// Expand ~ to home directory
	if len(template) > 0 && template[0] == '~' {
		template = filepath.Join(home, template[1:])
	}

	return template
}

// configPathForPlatform returns the appropriate config path for the current OS.
// Takes paths keyed by "darwin", "windows", "linux".
func configPathForPlatform(paths map[string]string) string {
	p, ok := paths[runtime.GOOS]
	if !ok {
		return ""
	}
	return expandPath(p)
}

// fileExists returns true if the file at path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
