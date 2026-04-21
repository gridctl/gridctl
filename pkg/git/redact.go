package git

import (
	"net/url"
	"regexp"
)

// tokenPatterns is a short defense-in-depth list of credential-shaped
// substrings that must never survive into logs or error text. This is NOT
// a substitute for careful log construction — it only catches accidental
// leaks from error messages constructed by third-party libraries.
var tokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),         // GitHub classic PAT
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{60,}`), // GitHub fine-grained PAT
	regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`),     // GitLab PAT
}

// embeddedURLUserinfo matches the userinfo component of an HTTP(S) URL so
// we can strip it. The capture group retains scheme://, then we drop
// everything up to and including the '@'.
var embeddedURLUserinfo = regexp.MustCompile(`(https?://)[^@\s/]+@`)

// RedactURL returns a version of raw with any userinfo stripped.
//
//	https://TOKEN@host/path → https://host/path
//	git@host:path           → git@host:path       (SCP-style, unchanged)
//	non-URL text            → unchanged
func RedactURL(raw string) string {
	if raw == "" {
		return raw
	}
	// SCP-style user@host:path has no userinfo component; leave intact.
	if scpSyntax.MatchString(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return raw
	}
	if u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

// RedactString scrubs known token patterns and URL userinfo from arbitrary
// text. Use it on strings whose construction you do not fully control,
// e.g. error messages returned from go-git.
func RedactString(s string) string {
	for _, re := range tokenPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	s = embeddedURLUserinfo.ReplaceAllString(s, "$1")
	return s
}

// RedactError returns an error whose message has been scrubbed of
// credential-shaped content. The original error is preserved via Unwrap so
// errors.Is / errors.As continue to work as expected. Returns nil when err
// is nil, and returns err unchanged when the message does not contain any
// redactable content.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	redacted := RedactString(err.Error())
	if redacted == err.Error() {
		return err
	}
	return &redactedError{msg: redacted, wrapped: err}
}

type redactedError struct {
	msg     string
	wrapped error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.wrapped }
