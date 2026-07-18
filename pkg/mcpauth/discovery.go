package mcpauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// ErrDCRUnsupported reports an authorization server that refuses dynamic
// client registration. The message names the fix because it is shown to
// the user verbatim.
var ErrDCRUnsupported = errors.New(
	"authorization server does not support dynamic client registration; " +
		"set auth.client_id (and auth.client_secret if issued) in stack.yaml")

// discovery is the resolved OAuth environment for one resource server.
type discovery struct {
	Resource string // canonical resource URL (RFC 8707 resource indicator)
	Issuer   string
	Meta     *oauthex.AuthServerMeta
	Scopes   []string // scopes to request, in priority order of source
}

// canonicalResource normalizes a server URL into its RFC 8707 canonical
// form: lowercase scheme and host, no fragment, default ports dropped.
func canonicalResource(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing server URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("server URL must be http or https, got %q", u.Scheme)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
	}
	u.Fragment = ""
	return u.String(), nil
}

// newDiscoveryClient builds the HTTP client used for every discovery,
// registration, token, and revocation request. Discovery URLs are
// attacker-controlled by a malicious MCP server, so the dialer blocks
// link-local targets (the cloud-metadata SSRF class, 169.254.169.254
// included) while still allowing loopback and ordinary LAN addresses that
// local-first stacks legitimately use. Redirects are capped.
func newDiscoveryClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if ip.IP.IsLinkLocalUnicast() || ip.IP.IsLinkLocalMulticast() {
				return nil, fmt.Errorf("refusing link-local address %s for %s", ip.IP, host)
			}
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// probeChallenge issues an unauthenticated GET against the resource and
// returns the parsed WWW-Authenticate challenges (which may be empty for
// servers that rely on well-known discovery paths alone).
func probeChallenge(ctx context.Context, client *http.Client, resource string) ([]oauthex.Challenge, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probing %s: %w", resource, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	headers := resp.Header.Values("WWW-Authenticate")
	if len(headers) == 0 {
		return nil, nil
	}
	challenges, err := oauthex.ParseWWWAuthenticate(headers)
	if err != nil {
		return nil, nil // malformed challenge: fall back to well-known paths
	}
	return challenges, nil
}

// prmCandidates returns protected-resource-metadata URLs in the spec's
// priority order: the challenge's resource_metadata parameter, then the
// path-inserted well-known URI, then the root well-known URI.
func prmCandidates(resource string, challenges []oauthex.Challenge) []string {
	var out []string
	for _, ch := range challenges {
		if v := ch.Params["resource_metadata"]; v != "" {
			out = append(out, v)
		}
	}
	u, err := url.Parse(resource)
	if err != nil {
		return out
	}
	origin := u.Scheme + "://" + u.Host
	if p := strings.TrimSuffix(u.Path, "/"); p != "" {
		out = append(out, origin+"/.well-known/oauth-protected-resource"+p)
	}
	out = append(out, origin+"/.well-known/oauth-protected-resource")
	return out
}

// asMetaCandidates returns authorization-server-metadata URLs for an issuer
// in the spec's priority order (RFC 8414 path insertion first, then OIDC
// path insertion, then OIDC path appending).
func asMetaCandidates(issuer string) []string {
	u, err := url.Parse(issuer)
	if err != nil {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.Path, "/")
	if p != "" {
		return []string{
			origin + "/.well-known/oauth-authorization-server" + p,
			origin + "/.well-known/openid-configuration" + p,
			origin + p + "/.well-known/openid-configuration",
		}
	}
	return []string{
		origin + "/.well-known/oauth-authorization-server",
		origin + "/.well-known/openid-configuration",
	}
}

// discover resolves the OAuth environment for a resource server: challenge
// probe, protected resource metadata (with fallback chain), authorization
// server metadata (with fallback chain, PKCE support verified by the SDK).
// configScopes, when non-empty, override every advertised scope source.
func discover(ctx context.Context, client *http.Client, serverURL string, configScopes []string) (*discovery, error) {
	resource, err := canonicalResource(serverURL)
	if err != nil {
		return nil, err
	}

	challenges, err := probeChallenge(ctx, client, resource)
	if err != nil {
		return nil, err
	}

	// Scope precedence: stack.yaml > challenge scope param > PRM scopes.
	var challengeScopes []string
	for _, ch := range challenges {
		if v := ch.Params["scope"]; v != "" {
			challengeScopes = strings.Fields(v)
		}
	}

	var prm *oauthex.ProtectedResourceMetadata
	for _, mdURL := range prmCandidates(resource, challenges) {
		md, mdErr := oauthex.GetProtectedResourceMetadata(ctx, mdURL, resource, client)
		if mdErr == nil && md != nil {
			prm = md
			break
		}
	}

	// Issuer: PRM's first authorization server, else (for servers predating
	// RFC 9728) the resource origin itself.
	var issuer string
	var prmScopes []string
	if prm != nil && len(prm.AuthorizationServers) > 0 {
		issuer = prm.AuthorizationServers[0]
		prmScopes = prm.ScopesSupported
	} else {
		u, _ := url.Parse(resource)
		issuer = u.Scheme + "://" + u.Host
	}

	var meta *oauthex.AuthServerMeta
	var lastErr error
	for _, mdURL := range asMetaCandidates(issuer) {
		m, mErr := oauthex.GetAuthServerMeta(ctx, mdURL, issuer, client)
		if mErr != nil {
			lastErr = mErr
			continue
		}
		if m != nil {
			meta = m
			break
		}
	}
	if meta == nil {
		if lastErr != nil {
			return nil, fmt.Errorf("discovering authorization server for %s: %w", resource, lastErr)
		}
		return nil, fmt.Errorf("no authorization server metadata found for %s (issuer %s)", resource, issuer)
	}

	scopes := configScopes
	if len(scopes) == 0 {
		scopes = challengeScopes
	}
	if len(scopes) == 0 {
		scopes = prmScopes
	}

	return &discovery{Resource: resource, Issuer: meta.Issuer, Meta: meta, Scopes: scopes}, nil
}

// clientRegistrationRequest is the RFC 7591 registration payload. Rolled by
// hand (rather than oauthex.RegisterClient) so application_type can be
// declared: without "native", strict servers default to "web" and reject
// loopback redirect URIs (SEP-837).
type clientRegistrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
	ApplicationType         string   `json:"application_type"`
	Scope                   string   `json:"scope,omitempty"`
}

type clientRegistrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// registerClient performs dynamic client registration against the issuer,
// declaring a native public client bound to redirectURI.
func registerClient(ctx context.Context, client *http.Client, d *discovery, redirectURI string) (*ClientRegistration, error) {
	if d.Meta.RegistrationEndpoint == "" {
		return nil, ErrDCRUnsupported
	}

	reqBody := clientRegistrationRequest{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "gridctl",
		ApplicationType:         "native",
	}
	// Registering scopes matters: some servers reject token requests for
	// scopes the client did not register (go-sdk issue #1102 class).
	if len(d.Scopes) > 0 {
		reqBody.Scope = strings.Join(d.Scopes, " ")
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling registration request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Meta.RegistrationEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registering client: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
	case http.StatusNotFound, http.StatusNotImplemented, http.StatusMethodNotAllowed:
		return nil, ErrDCRUnsupported
	default:
		var regErr clientRegistrationResponse
		if json.Unmarshal(body, &regErr) == nil && regErr.Error != "" {
			return nil, fmt.Errorf("client registration rejected: %s (%s)", regErr.Error, regErr.ErrorDesc)
		}
		return nil, fmt.Errorf("client registration failed: HTTP %d", resp.StatusCode)
	}

	var reg clientRegistrationResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("parsing registration response: %w", err)
	}
	if reg.ClientID == "" {
		return nil, fmt.Errorf("registration response missing client_id")
	}
	return &ClientRegistration{
		Issuer:       d.Issuer,
		ClientID:     reg.ClientID,
		ClientSecret: reg.ClientSecret,
		RedirectURI:  redirectURI,
	}, nil
}

// validateAuthorizeURL enforces the spec's URL-scheme rules before the URL
// is handed to a browser: https always allowed, http only for loopback
// hosts, everything else (javascript:, data:, file:) rejected.
func validateAuthorizeURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid authorization URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("authorization URL uses http on a non-loopback host: %s", host)
	default:
		return fmt.Errorf("authorization URL has forbidden scheme %q", u.Scheme)
	}
}
