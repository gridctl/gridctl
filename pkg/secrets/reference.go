// Package secrets resolves ${secret:<provider>/<path>} references by running
// host providers. Load and validation never call this package.
package secrets

import (
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
)

const stackFailureKey = "stack"

// Reference is one secret:<provider>/<path> token.
type Reference struct {
	Provider string
	Path     string
}

// String returns the index key secret:<provider>/<path>.
func (r Reference) String() string {
	return config.SecretRefKey(r.Provider, r.Path)
}

// ParseReference parses a secret:<provider>/<path> key. The path may contain
// slashes. A leading ${} wrapper is not accepted.
func ParseReference(key string) (Reference, error) {
	provider, path, ok := config.ParseSecretRefKey(strings.TrimSpace(key))
	if !ok {
		return Reference{}, fmt.Errorf("invalid secret reference %q", key)
	}
	return Reference{Provider: provider, Path: path}, nil
}

// Error is a value-free provider failure. Error() never includes stdout,
// stderr, or the resolved value.
type Error struct {
	Ref      Reference
	Category string
	Err      error
}

func (e *Error) Error() string {
	if e == nil {
		return "secret: unknown"
	}
	return fmt.Sprintf("secret %s/%s: %s", e.Ref.Provider, e.Ref.Path, e.Category)
}

// Unwrap returns the underlying error so errors.Is can match context errors.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func secretError(ref Reference, category string, err error) *Error {
	return &Error{Ref: ref, Category: category, Err: err}
}
