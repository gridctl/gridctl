package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// Seams so tests can prove the Homebrew-symlink rule without the host layout.
var (
	lookPathGridctl = defaultLookPath
	executablePath  = os.Executable
	evalSymlinks    = filepath.EvalSymlinks
)

func defaultLookPath() (string, error) {
	return lookPath("gridctl")
}

// lookPath is split from exec.LookPath so the default stays one line and
// tests can replace lookPathGridctl without importing exec here twice.
var lookPath = func(name string) (string, error) {
	return execLookPath(name)
}

// ResolveBinary chooses the path the unit will execute.
//
// An absolute executable --binary wins. Otherwise, if LookPath("gridctl")
// and os.Executable resolve to the same file, the LookPath result is
// written (the stable Homebrew symlink). If they differ, os.Executable is
// written so a development binary is not replaced by a release on PATH.
// The unit never relies on PATH to find gridctl.
func ResolveBinary(override string) (string, error) {
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("--binary must be an absolute path, got %q", override)
		}
		info, err := os.Stat(override)
		if err != nil {
			return "", fmt.Errorf("--binary %s: %w", override, err)
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("--binary %s is not executable", override)
		}
		return override, nil
	}
	exe, exeErr := executablePath()
	looked, lookErr := lookPathGridctl()
	if lookErr == nil && exeErr == nil {
		lookedResolved, lerr := evalSymlinks(looked)
		exeResolved, eerr := evalSymlinks(exe)
		if lerr == nil && eerr == nil && lookedResolved == exeResolved {
			return looked, nil
		}
	}
	if exeErr != nil {
		if lookErr == nil {
			return looked, nil
		}
		return "", fmt.Errorf("resolving gridctl binary: %w", exeErr)
	}
	return exe, nil
}
