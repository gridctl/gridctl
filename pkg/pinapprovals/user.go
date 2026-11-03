package pinapprovals

import (
	"errors"
	"os"
	"os/user"
)

// currentUsername is the OS lookup LocalUser uses. Tests replace the
// function passed to localUser; this variable stays the production path.
func currentUsername() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	if u == nil || u.Username == "" {
		return "", errors.New("empty username")
	}
	return u.Username, nil
}

// LocalUser returns the local OS account and the source os_user. On lookup
// failure it falls back to $USER, then $USERNAME. It returns empty strings
// when none is set. It never returns an error and never panics.
func LocalUser() (name, source string) {
	return localUser(currentUsername, os.Getenv)
}

func localUser(current func() (string, error), getenv func(string) string) (name, source string) {
	if current != nil {
		if got, err := current(); err == nil && got != "" {
			return got, SourceOSUser
		}
	}
	if getenv == nil {
		return "", ""
	}
	if got := getenv("USER"); got != "" {
		return got, SourceOSUser
	}
	if got := getenv("USERNAME"); got != "" {
		return got, SourceOSUser
	}
	return "", ""
}

// ResolveApprover applies the approval attribution order: a non-empty
// verified principal (oauth), else a non-empty declared name, else the
// local OS user. This is attribution, not authentication.
func ResolveApprover(principal, declared string) (name, source string) {
	if principal != "" {
		return principal, SourceOAuth
	}
	if declared != "" {
		return declared, SourceDeclared
	}
	return LocalUser()
}
