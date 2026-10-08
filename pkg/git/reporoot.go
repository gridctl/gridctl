package git

import (
	"errors"

	gogit "github.com/go-git/go-git/v5"
)

// IsRepoRoot reports whether path is a git repository root. It opens the
// path with PlainOpen and does not walk parent directories, so a skill
// directory inside a repository is not a root.
func IsRepoRoot(path string) (bool, error) {
	_, err := gogit.PlainOpen(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gogit.ErrRepositoryNotExists) {
		return false, nil
	}
	return false, err
}
