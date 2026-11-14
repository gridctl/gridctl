package vault

import "errors"

// Unlock sources recorded by Store.SetUnlockSource and reported by
// Store.UnlockSource.
const (
	UnlockSourceEnvironment    = "environment"
	UnlockSourcePassphraseFile = "passphrase_file"
	UnlockSourceStdin          = "stdin"
	UnlockSourceAPI            = "api"
	UnlockSourceCLI            = "cli"
)

// ErrNotEncrypted is returned by operations that need an encrypted store.
var ErrNotEncrypted = errors.New("store is not encrypted")
