package pinapprovals

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gridctl/gridctl/pkg/logging"
)

// Sentinel errors for refused approval input. Callers map them with
// errors.Is. The wrapped text names the problem and says how to retry;
// redaction is not used, because silently mutating audit input would hide
// what the operator submitted.
var (
	ErrInvalidReference  = errors.New("invalid evidence reference")
	ErrCredentialShaped  = errors.New("credential-shaped value")
	ErrTooManyReferences = errors.New("too many evidence references")
	ErrReasonTooLong     = errors.New("approval reason too long")
	ErrInvalidApprover   = errors.New("invalid approver")
)

// ValidateReference trims ref and refuses an empty value, a value longer
// than 2048 bytes, a URL whose userinfo is set, and any string that matches
// a default redaction pattern. The error names what matched and says to
// remove it and retry.
func ValidateReference(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("%w: reference is empty", ErrInvalidReference)
	}
	if len(ref) > maxReferenceBytes {
		return fmt.Errorf("%w: reference is longer than %d bytes", ErrInvalidReference, maxReferenceBytes)
	}
	return credentialShape(ref)
}

// ValidateReferences trims each entry, drops exact duplicates (first
// occurrence wins), refuses more than 16 remaining, and applies
// ValidateReference to each. The returned slice is what callers store.
func ValidateReferences(refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(refs))
	cleaned := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		cleaned = append(cleaned, ref)
	}
	if len(cleaned) > maxReferences {
		return nil, fmt.Errorf("%w: at most %d evidence references are allowed; remove extras and retry", ErrTooManyReferences, maxReferences)
	}
	for i, ref := range cleaned {
		if err := ValidateReference(ref); err != nil {
			return nil, fmt.Errorf("evidence reference %d: %w", i+1, err)
		}
	}
	return cleaned, nil
}

// ValidateReason refuses a reason longer than 4096 bytes and a reason that
// is credential-shaped. Empty is allowed.
func ValidateReason(reason string) error {
	if len(reason) > maxReasonBytes {
		return fmt.Errorf("%w: reason is longer than %d bytes; shorten it and retry", ErrReasonTooLong, maxReasonBytes)
	}
	if reason == "" {
		return nil
	}
	return credentialShape(reason)
}

// ValidateApprover trims name and refuses a value longer than 256 bytes or
// one that is credential-shaped. Empty, including whitespace-only, is
// allowed: attribution then falls through to the next source. The check
// does not authenticate the name.
func ValidateApprover(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if len(name) > maxApproverBytes {
		return fmt.Errorf("%w: approver is longer than %d bytes; shorten it and retry", ErrInvalidApprover, maxApproverBytes)
	}
	return credentialShape(name)
}

func credentialShape(s string) error {
	u, err := url.Parse(s)
	if err == nil && u.Scheme != "" && u.User != nil {
		return fmt.Errorf("%w: URL carries userinfo; remove it and retry", ErrCredentialShaped)
	}
	if logging.MatchesRedactPattern(s) {
		return fmt.Errorf("%w: looks like a bearer token or key=value secret; remove it and retry", ErrCredentialShaped)
	}
	return nil
}
