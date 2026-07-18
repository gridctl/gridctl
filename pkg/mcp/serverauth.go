package mcp

import (
	"context"
	"fmt"
	"time"
)

// Downstream authorization states surfaced per server.
const (
	AuthStatusAuthorized = "authorized"
	AuthStatusNeedsAuth  = "needs_auth"
)

// AuthRequiredError reports a downstream HTTP 401 (or 403 carrying an OAuth
// challenge). Transport code returns it so callers can distinguish "needs
// authorization" from "broken" and start discovery from the challenge.
type AuthRequiredError struct {
	Status    int
	Challenge string // raw WWW-Authenticate header value (may be empty)
	Body      string
}

// Error keeps the transport's established "HTTP <code>: <body>" shape so
// existing callers that match on the message are unaffected.
func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// NeedsAuthError is returned on requests to a server whose authorization is
// missing or expired. The message is user-facing: it is what an upstream
// LLM client displays when a tool call fails, so it names the exact fix.
type NeedsAuthError struct {
	Server string
}

func (e *NeedsAuthError) Error() string {
	return fmt.Sprintf("%s requires authorization: run 'gridctl auth login %s' or open the gridctl UI",
		e.Server, e.Server)
}

// TokenInvalidator is implemented by header sources that cache credentials.
// InvalidateToken drops the cached credential and reports whether a retry
// is worthwhile (i.e. a refresh path exists). The transport calls it once
// on a 401 so an expired-but-refreshable token heals silently.
type TokenInvalidator interface {
	InvalidateToken() bool
}

// ServerAuthState is the downstream authorization state recorded for a
// server. Empty Status means no OAuth state is tracked for the server.
type ServerAuthState struct {
	Status string     `json:"status"`           // AuthStatusAuthorized or AuthStatusNeedsAuth
	Issuer string     `json:"issuer,omitempty"` // authorization server issuer, when known
	Expiry *time.Time `json:"expiry,omitempty"` // access token expiry, when known
	Error  string     `json:"error,omitempty"`  // short reason, e.g. "authorization expired"
}

// ServerAuthConfig mirrors config.ServerAuth for downstream client wiring.
// All credential fields arrive already expanded (variables resolved).
type ServerAuthConfig struct {
	Type         string   // "bearer", "header", or "oauth"
	Token        string   // resolved bearer token (type: bearer)
	Header       string   // header name (type: header)
	Value        string   // resolved header value (type: header)
	Scopes       []string // requested OAuth scopes (type: oauth)
	ClientID     string   // pre-registered OAuth client ID (type: oauth)
	ClientSecret string   // pre-registered OAuth client secret (type: oauth)
}

// HeaderSource supplies the authentication header attached to every
// downstream request. Implementations may fetch or refresh credentials;
// an error aborts the request and surfaces to the caller unchanged, so
// typed errors (e.g. authorization-required) pass through the transport.
type HeaderSource interface {
	AuthHeader(ctx context.Context) (name, value string, err error)
}

// staticHeaderSource returns a fixed header on every call.
type staticHeaderSource struct {
	name  string
	value string
}

func (s staticHeaderSource) AuthHeader(context.Context) (string, string, error) {
	return s.name, s.value, nil
}

// NewStaticHeaderSource returns a HeaderSource that always yields the given
// header. Used for auth types "bearer" and "header".
func NewStaticHeaderSource(name, value string) HeaderSource {
	return staticHeaderSource{name: name, value: value}
}

// StaticHeaderSourceFor builds the HeaderSource for a static auth config.
// Returns nil for nil configs and for types that need a live source
// (type: oauth is wired by the broker, not here).
func StaticHeaderSourceFor(auth *ServerAuthConfig) HeaderSource {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case "bearer":
		if auth.Token == "" {
			return nil
		}
		return NewStaticHeaderSource("Authorization", "Bearer "+auth.Token)
	case "header":
		if auth.Header == "" {
			return nil
		}
		return NewStaticHeaderSource(auth.Header, auth.Value)
	default:
		return nil
	}
}
