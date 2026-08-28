package vault

import (
	"fmt"
	"slices"
	"strings"
)

var internalCredentialKeys = [...]string{
	"GRIDCTL_SSH_KEY_PASSPHRASE",
	"GRIDCTL_VAULT_PASSPHRASE",
	"OP_CONNECT_TOKEN",
	"OP_SERVICE_ACCOUNT_TOKEN",
}

// InternalCredentialKeys returns a sorted defensive copy of the exact internal
// credential denylist. IsInternalCredential also reserves the GRIDCTL_ prefix.
func InternalCredentialKeys() []string {
	return slices.Clone(internalCredentialKeys[:])
}

// IsInternalCredential reports whether key is reserved for gridctl bootstrap
// or control-plane use and must not be delivered to downstream workloads.
func IsInternalCredential(key string) bool {
	return strings.HasPrefix(key, "GRIDCTL_") || slices.Contains(internalCredentialKeys[:], key)
}

// InternalCredentialError reports an attempt to persist or resolve a reserved
// control-plane credential.
type InternalCredentialError struct {
	Key string
}

func (e *InternalCredentialError) Error() string {
	return fmt.Sprintf("variable %q is reserved for gridctl internal credentials", e.Key)
}

// Is lets callers match errors for the same denied key with errors.Is.
func (e *InternalCredentialError) Is(target error) bool {
	other, ok := target.(*InternalCredentialError)
	return ok && e.Key == other.Key
}

// NewInternalCredentialError returns the typed error used at credential
// persistence and delivery boundaries.
func NewInternalCredentialError(key string) error {
	return &InternalCredentialError{Key: key}
}
