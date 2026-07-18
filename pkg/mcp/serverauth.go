package mcp

import (
	"context"
)

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
