package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"gopkg.in/yaml.v3"
)

// LockFileName is the committed pack lock, written next to gridctl-pack.yaml
// only by `gridctl pack lock`.
const LockFileName = "gridctl-pack.lock.yaml"

// LockVersion is the gridctl-pack.lock.yaml schema this gridctl writes.
const LockVersion = 1

// ErrNewerPackLockVersion signals a pack lock written by a newer gridctl.
// Callers must not honor or rewrite it.
var ErrNewerPackLockVersion = errors.New("pack lock was written by a newer gridctl version")

// commitPattern is a full git object id. Short SHAs are not lock inputs.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// LockFile is gridctl-pack.lock.yaml. The digest covers sources and
// stack.extends only. Comments, formatting, auth blocks, and every other
// manifest field do not stale the lock and never appear in it.
type LockFile struct {
	Version        int                      `yaml:"version"`
	Pack           string                   `yaml:"pack"`
	ManifestDigest string                   `yaml:"manifestDigest"`
	Sources        map[string]LockedSource  `yaml:"sources,omitempty"`
	Extends        map[string]LockedExtends `yaml:"extends,omitempty"`
}

// LockedSource is one resolved sources: entry. Repo carries no credential
// userinfo. An ssh:// username with no password is a login name and is kept.
type LockedSource struct {
	Repo   string `yaml:"repo"`
	Ref    string `yaml:"ref,omitempty"`
	Commit string `yaml:"commit"`
	Path   string `yaml:"path,omitempty"`
}

// LockedExtends is the base pack a booster extends, pinned by repo and commit.
type LockedExtends struct {
	Repo   string `yaml:"repo"`
	Commit string `yaml:"commit"`
}

// ParseLockFile decodes a pack lock. version greater than LockVersion is
// refused with ErrNewerPackLockVersion. A missing or non-positive version,
// a commit that is not a 40-hex SHA, and a repo carrying URL userinfo are
// refused. An ssh:// URL may carry a username with no password; that is a
// login name, not a credential. An https userinfo component and any SSH
// password are refused. The newer-version message names LockFileName, which
// is the path authors and the CLI print.
func ParseLockFile(data []byte) (*LockFile, error) {
	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing pack lock: %w", err)
	}
	if lf.Version > LockVersion {
		return nil, fmt.Errorf("%w (%s is version %d, this gridctl supports %d; upgrade gridctl)",
			ErrNewerPackLockVersion, LockFileName, lf.Version, LockVersion)
	}
	if lf.Version < 1 {
		return nil, fmt.Errorf("pack lock version must be a positive integer")
	}
	if err := validateLockRepos(&lf); err != nil {
		return nil, err
	}
	return &lf, nil
}

func validateLockRepos(lf *LockFile) error {
	for name, src := range lf.Sources {
		if err := rejectUserinfo(src.Repo); err != nil {
			return fmt.Errorf("source %q repo: %w", name, err)
		}
		if !commitPattern.MatchString(src.Commit) {
			return fmt.Errorf("source %q commit %q must be a 40-character hexadecimal SHA", name, src.Commit)
		}
	}
	for name, ext := range lf.Extends {
		if err := rejectUserinfo(ext.Repo); err != nil {
			return fmt.Errorf("extends %q repo: %w", name, err)
		}
		if !commitPattern.MatchString(ext.Commit) {
			return fmt.Errorf("extends %q commit %q must be a 40-character hexadecimal SHA", name, ext.Commit)
		}
	}
	return nil
}

func rejectUserinfo(repo string) error {
	if repo == "" {
		return fmt.Errorf("repo is required")
	}
	if sshUsernameOnly(repo) {
		return nil
	}
	if hasURLUserinfo(repo) {
		return fmt.Errorf("repo %q must not carry URL userinfo", gitpkg.RedactURL(repo))
	}
	return nil
}

// sshUsernameOnly reports an ssh:// URL whose userinfo is a username and
// not a password. The scp form git@host:path has no URL userinfo.
func sshUsernameOnly(repo string) bool {
	if gitpkg.DetectProtocol(repo) != gitpkg.ProtocolSSH {
		return false
	}
	u, err := url.Parse(repo)
	if err != nil || u.User == nil {
		return false
	}
	if _, set := u.User.Password(); set {
		return false
	}
	return u.User.Username() != ""
}

func hasURLUserinfo(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.User == nil {
		return false
	}
	return true
}

// WriteLockFile marshals lf with sorted map keys, a trailing newline, and
// mode 0644, via a temp file in the same directory. It refuses a lock that
// would not parse.
func WriteLockFile(path string, lf *LockFile) error {
	if lf == nil {
		return fmt.Errorf("pack lock is nil")
	}
	if err := validateLockRepos(lf); err != nil {
		return err
	}
	if lf.Version < 1 || lf.Version > LockVersion {
		return fmt.Errorf("pack lock version %d is not writable by this gridctl", lf.Version)
	}
	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling pack lock: %w", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating pack lock directory: %w", err)
	}
	return atomicWrite(path, data)
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// digestSource is one sources entry with auth stripped. Auth is not a lock
// input: a credential reference must never influence the digest and must
// never appear in the lock.
type digestSource struct {
	Repo string `json:"repo"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

type digestBody struct {
	Sources map[string]digestSource `json:"sources"`
	Extends string                  `json:"extends"`
}

// ManifestDigest returns sha256:<hex> over the canonical JSON encoding of
// sources and stack.extends, with auth blocks stripped. Comments, formatting,
// and every non-source manifest field do not stale the lock. encoding/json
// sorts map keys, so source order does not matter.
func ManifestDigest(m *Manifest) string {
	body := digestBody{Sources: map[string]digestSource{}, Extends: ""}
	if m != nil {
		body.Extends = m.Stack.Extends
		for name, src := range m.Sources {
			body.Sources[name] = digestSource{Repo: src.Repo, Ref: src.Ref, Path: src.Path}
		}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
