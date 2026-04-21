package git

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// Sentinel errors classify git-operation failures so callers can render
// actionable UI messages without string-matching raw go-git output.
var (
	// ErrAuthRequired: the remote rejected the request because no
	// credentials were presented.
	ErrAuthRequired = errors.New("authentication required")

	// ErrAuthFailed: credentials were presented but rejected.
	ErrAuthFailed = errors.New("authentication failed")

	// ErrNotFound: the remote returned a "not found" response. On hosts
	// like GitHub this may also mean a private repository accessed without
	// sufficient credentials.
	ErrNotFound = errors.New("repository not found")

	// ErrHostKeyMismatch: the SSH host key did not match the local
	// known_hosts — a potential man-in-the-middle indicator.
	ErrHostKeyMismatch = errors.New("ssh host key mismatch")

	// ErrSSHAgentMissing: ssh-agent was unavailable (socket missing or
	// SSH_AUTH_SOCK unset) when SSH-agent auth was requested.
	ErrSSHAgentMissing = errors.New("ssh agent not available")

	// ErrEmptyToken: a token-based Auther was constructed with an empty
	// token. Reported eagerly rather than falling through to an
	// unauthenticated clone that then fails with a cryptic error.
	ErrEmptyToken = errors.New("token is empty")

	// ErrProtocolMismatch: an Auther was asked to produce credentials for
	// a URL whose protocol does not match the auth mechanism (e.g., an
	// HTTPS token against an SSH URL).
	ErrProtocolMismatch = errors.New("protocol mismatch")
)

// ClassifyError maps raw go-git (or network) errors to sentinels in this
// package, wrapping the original so errors.Is / errors.As still find it.
// Unknown errors are returned unchanged — the caller is free to wrap them
// with its own context.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, transport.ErrAuthenticationRequired):
		return fmt.Errorf("%w: %w", ErrAuthRequired, err)
	case errors.Is(err, transport.ErrAuthorizationFailed):
		return fmt.Errorf("%w: %w", ErrAuthFailed, err)
	case errors.Is(err, transport.ErrRepositoryNotFound):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}

	// Fall back to substring matching for errors that upstream does not
	// expose as typed sentinels. Each pattern is documented near its case.
	msg := err.Error()
	switch {
	// go-git's SSH host-key check surfaces as a knownhosts package error
	// whose text contains "knownhosts:" and "mismatch". See
	// github.com/go-git/go-git/v5/plumbing/transport/ssh/knownhosts.go.
	case strings.Contains(msg, "knownhosts:") && strings.Contains(msg, "mismatch"):
		return fmt.Errorf("%w: %w", ErrHostKeyMismatch, err)
	// xanzy/ssh-agent surfaces "SSH_AUTH_SOCK" or "SSH agent" text when the
	// socket can't be dialed.
	case strings.Contains(msg, "SSH_AUTH_SOCK") || strings.Contains(msg, "SSH agent"):
		return fmt.Errorf("%w: %w", ErrSSHAgentMissing, err)
	}

	return err
}
