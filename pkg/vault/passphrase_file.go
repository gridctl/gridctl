package vault

import (
	"fmt"
	"os"
	"strings"
)

// PassphraseFileEnv is the environment variable naming a user-owned file
// whose contents unlock an encrypted store. Gridctl never creates the file.
const PassphraseFileEnv = "GRIDCTL_VAULT_PASSPHRASE_FILE" // #nosec G101 -- environment variable name, not a secret value

// maxPassphraseFileBytes is the read cap. A larger file is refused before
// its contents are used, and the error does not echo them.
const maxPassphraseFileBytes = 4 << 10

// passphraseFileOwner reports the numeric owner of info. Tests replace it
// to simulate another user without root.
var passphraseFileOwner = fileOwnerUID

// ReadPassphraseFile reads path and returns the passphrase with one
// trailing newline trimmed. The file must be a regular file (not a
// symlink), mode 0600 or stricter, and owned by the current user. Errors
// name the problem and never include the file contents.
func ReadPassphraseFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("passphrase file path is empty")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("passphrase file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("passphrase file %s: not a regular file", path)
	}
	perm := info.Mode().Perm()
	if perm&^os.FileMode(0o600) != 0 {
		return "", fmt.Errorf("passphrase file %s: mode is %04o, want 0600 or stricter", path, perm)
	}
	owner, ok := passphraseFileOwner(info)
	if !ok {
		return "", fmt.Errorf("passphrase file %s: cannot verify owner", path)
	}
	if owner != currentUID() {
		return "", fmt.Errorf("passphrase file %s: owner is %d, want the current user (%d)", path, owner, currentUID())
	}
	if info.Size() > maxPassphraseFileBytes {
		return "", fmt.Errorf("passphrase file %s: larger than 4 KiB", path)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- user-typed passphrase file path, size-capped above
	if err != nil {
		return "", fmt.Errorf("passphrase file %s: %w", path, err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

func currentUID() uint32 {
	return passphraseCurrentUID()
}
