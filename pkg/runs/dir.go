package runs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gridctl/gridctl/pkg/state"
)

const (
	activeFileName = "runs.jsonl"
	dirPerm        = 0o700
	filePerm       = 0o600
)

// Dir returns the stack-level runs directory (~/.gridctl/runs/<stack>).
func Dir(stackName string) (string, error) {
	base, err := state.BaseDir()
	if err != nil {
		return "", err
	}
	if stackName == "" {
		return "", fmt.Errorf("stack name is required")
	}
	if err := validateStackName(stackName); err != nil {
		return "", err
	}
	return filepath.Join(base, "runs", stackName), nil
}

// ActivePath returns the active JSONL path for a stack.
func ActivePath(stackName string) (string, error) {
	dir, err := Dir(stackName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, activeFileName), nil
}

func validateStackName(name string) error {
	if name == "." || name == ".." {
		return fmt.Errorf("invalid stack name")
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid stack name")
	}
	for _, r := range name {
		if r == 0 || r == '/' || r == '\\' {
			return fmt.Errorf("invalid stack name")
		}
	}
	return nil
}

func ensureDir(path string) error {
	if err := refuseSymlink(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, dirPerm); err != nil {
		return err
	}
	return os.Chmod(path, dirPerm)
}

func refuseSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink path %s", path)
	}
	return nil
}

func refuseSymlinkParents(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for {
		if err := refuseSymlink(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return nil
		}
		abs = parent
	}
}
