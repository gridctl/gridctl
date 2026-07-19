package mcpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// refreshTimeout bounds one refresh round trip. Refresh runs under its own
// deadline, detached from the caller's (a health ping's 5s budget must not
// cancel a legitimate refresh midway).
const refreshTimeout = 30 * time.Second

// expirySkew refreshes slightly before nominal expiry so a token never
// dies mid-request.
const expirySkew = 30 * time.Second

// grantSource is the live mcp.HeaderSource for one server. It caches the
// grant's token in memory, refreshes it (persisting rotations) when it
// nears expiry, and reports mcp.NeedsAuthError when no usable grant
// exists. It implements mcp.TokenInvalidator so the transport's single
// 401 retry can force a refresh.
type grantSource struct {
	broker *Broker
	server string

	// resourceOverride binds the source to a raw resource instead of a
	// configured server name (the probe path).
	resourceOverride string

	mu    sync.Mutex
	token *oauth2.Token // cached; nil forces a store read
}

// AuthHeader implements mcp.HeaderSource.
func (s *grantSource) AuthHeader(ctx context.Context) (string, string, error) {
	tok, err := s.currentToken(ctx)
	if err != nil {
		return "", "", err
	}
	return "Authorization", "Bearer " + tok.AccessToken, nil
}

// InvalidateToken implements mcp.TokenInvalidator: it expires the cached
// token so the next AuthHeader call refreshes. Returns true only when a
// refresh path exists, so the transport does not retry hopeless requests.
func (s *grantSource) InvalidateToken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == nil || s.token.RefreshToken == "" {
		s.token = nil
		return false
	}
	s.token.Expiry = time.Now().Add(-time.Minute)
	return true
}

// clearCache drops the cached token so the next AuthHeader call re-reads
// the store. Called by the broker after logins and logouts (never while
// the broker holds its own mutex; see Broker.dropSourceCaches).
func (s *grantSource) clearCache() {
	s.mu.Lock()
	s.token = nil
	s.mu.Unlock()
}

// currentToken returns a valid access token, reading the store and
// refreshing as needed.
func (s *grantSource) currentToken(ctx context.Context) (*oauth2.Token, error) {
	cfg, ok := s.broker.configFor(s)
	if !ok {
		return nil, &mcp.NeedsAuthError{Server: s.server}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != nil && tokenUsable(s.token) {
		return s.token, nil
	}

	// Cache miss or stale: re-read the store (a CLI login in another
	// process may have written a fresh grant).
	grant, found, err := s.broker.store.Grant(cfg.Resource)
	if err != nil || !found || grant.Token == nil {
		return nil, &mcp.NeedsAuthError{Server: s.server}
	}
	s.broker.redactToken(grant.Token)
	if tokenUsable(grant.Token) {
		s.token = grant.Token
		return s.token, nil
	}
	if grant.Token.RefreshToken == "" {
		s.broker.markNeedsAuth(s.server, "access token expired and no refresh token was granted")
		return nil, &mcp.NeedsAuthError{Server: s.server}
	}

	tok, err := s.refresh(ctx, cfg, grant)
	if err != nil {
		return nil, err
	}
	s.token = tok
	return tok, nil
}

// tokenUsable reports whether a token can be sent now: non-empty and not
// within the skew window of expiry (zero expiry means non-expiring).
func tokenUsable(t *oauth2.Token) bool {
	if t == nil || t.AccessToken == "" {
		return false
	}
	if t.Expiry.IsZero() {
		return true
	}
	return time.Until(t.Expiry) > expirySkew
}

// tokenResponse is the RFC 6749 token endpoint response shape.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// refresh performs a refresh-token grant by hand. x/oauth2's TokenSource
// cannot attach per-request parameters, and RFC 8707 requires the resource
// indicator on token requests too. Rotated refresh tokens are persisted
// before the new access token is first used; a refresh rejection
// (invalid_grant / invalid_client) discards the grant once and lands the
// server in needs-auth with no retry loop.
func (s *grantSource) refresh(ctx context.Context, cfg serverConfig, grant Grant) (*oauth2.Token, error) {
	// Refresh gets its own deadline, detached from the caller's.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()

	creds := s.broker.clientCredsForGrant(grant, s.server)
	if creds.ClientID == "" {
		s.broker.markNeedsAuth(s.server, "no client identity for refresh; run login again")
		return nil, &mcp.NeedsAuthError{Server: s.server}
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {grant.Token.RefreshToken},
		"resource":      {grant.Resource},
	}
	if creds.ClientSecret == "" {
		form.Set("client_id", creds.ClientID)
	}

	req, err := http.NewRequestWithContext(rctx, http.MethodPost, grant.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if creds.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(creds.ClientID), url.QueryEscape(creds.ClientSecret))
	}

	resp, err := s.broker.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refreshing token for %s: %w", s.server, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("parsing token response for %s: HTTP %d", s.server, resp.StatusCode)
	}

	if tr.Error != "" || resp.StatusCode >= 400 {
		if tr.Error == "invalid_grant" || tr.Error == "invalid_client" {
			// Self-heal: the stored grant is dead. Discard it once so the
			// next attempt starts clean instead of retry-looping.
			_ = s.broker.store.DeleteGrant(grant.Resource)
			s.broker.markNeedsAuth(s.server, "authorization expired (refresh rejected)")
			return nil, &mcp.NeedsAuthError{Server: s.server}
		}
		return nil, fmt.Errorf("token refresh failed for %s: HTTP %d %s", s.server, resp.StatusCode, tr.Error)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token refresh for %s returned no access token", s.server)
	}

	tok := &oauth2.Token{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		RefreshToken: tr.RefreshToken,
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = grant.Token.RefreshToken // server did not rotate
	}
	if tr.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}

	s.broker.redactToken(tok)

	// Persist the rotation before returning: single-use rotating refresh
	// tokens die permanently if the process exits between use and persist.
	grant.Token = tok
	if err := s.broker.store.PutGrant(grant); err != nil {
		return nil, fmt.Errorf("persisting refreshed token: %w", err)
	}
	return tok, nil
}

// clientCredsForGrant resolves the client credentials used for refresh and
// revocation: the server's static config when set, else the issuer's
// stored registration.
func (b *Broker) clientCredsForGrant(grant Grant, server string) ClientRegistration {
	b.mu.Lock()
	cfg, ok := b.servers[server]
	b.mu.Unlock()
	if ok && cfg.ClientID != "" {
		return ClientRegistration{Issuer: grant.Issuer, ClientID: cfg.ClientID, ClientSecret: cfg.Secret}
	}
	if reg, found, err := b.store.Registration(grant.Issuer); err == nil && found {
		return reg
	}
	return ClientRegistration{}
}
