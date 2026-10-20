package resourceauth

import "errors"

// Failure categories logged instead of token material or issuer URLs.
const (
	CategoryMalformed            = "malformed"
	CategorySignature            = "signature"
	CategoryIssuer               = "issuer"
	CategoryAudience             = "audience"
	CategoryExpired              = "expired"
	CategoryNotYetValid          = "not_yet_valid"
	CategoryUnknownKID           = "unknown_kid"
	CategoryAlgorithm            = "algorithm"
	CategoryTyp                  = "typ"
	CategoryScope                = "scope"
	CategoryIdentity             = "identity"
	CategoryDiscoveryUnavailable = "discovery_unavailable"
)

// ErrInvalidToken is returned for a token the resource server rejects.
var ErrInvalidToken = errors.New("invalid_token")

// ErrInsufficientScope is returned when required scopes are missing.
var ErrInsufficientScope = errors.New("insufficient_scope")

// ErrUnavailable is returned when the authorization server cannot be used.
var ErrUnavailable = errors.New("authorization server unavailable")

// TokenError is an invalid token with a log category. It never carries the
// raw token.
type TokenError struct {
	Category string
	Err      error
}

func (e *TokenError) Error() string {
	if e == nil || e.Category == "" {
		return "invalid_token"
	}
	return "invalid_token: " + e.Category
}

func (e *TokenError) Unwrap() error {
	if e == nil || e.Err == nil {
		return ErrInvalidToken
	}
	return e.Err
}

func (e *TokenError) Is(target error) bool {
	return target == ErrInvalidToken
}

func invalidToken(category string) error {
	return &TokenError{Category: category, Err: ErrInvalidToken}
}

func invalidTokenCause(category string, cause error) error {
	if cause == nil {
		cause = ErrInvalidToken
	}
	return &TokenError{Category: category, Err: cause}
}

// ScopeError is an insufficient-scope rejection. Required is the configured
// scope list, used in the WWW-Authenticate scope parameter.
type ScopeError struct {
	Missing  []string
	Required []string
}

func (e *ScopeError) Error() string { return "insufficient_scope" }

func (e *ScopeError) Is(target error) bool { return target == ErrInsufficientScope }

func (e *ScopeError) Unwrap() error { return ErrInsufficientScope }

// DiscoveryError is a discovery or key-set failure. Error text does not
// include the issuer URL or any token.
type DiscoveryError struct {
	Category string
	cause    error
}

func (e *DiscoveryError) Error() string { return "authorization server unavailable" }

func (e *DiscoveryError) Unwrap() error {
	if e == nil || e.cause == nil {
		return ErrUnavailable
	}
	return e.cause
}

func (e *DiscoveryError) Is(target error) bool { return target == ErrUnavailable }

func discoveryUnavailable(cause error) error {
	return &DiscoveryError{Category: CategoryDiscoveryUnavailable, cause: cause}
}
