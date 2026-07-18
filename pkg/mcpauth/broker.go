package mcpauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// DefaultAuthTimeout bounds how long a pending authorization waits for the
// browser callback before it expires with no partial state.
const DefaultAuthTimeout = 5 * time.Minute

// StateSink receives authorization state transitions; *mcp.Gateway
// satisfies it. A nil sink is valid (probe-only broker use).
type StateSink interface {
	SetServerAuthState(name string, st mcp.ServerAuthState)
}

// serverConfig is the per-server OAuth configuration registered by the
// controller when the stack is applied.
type serverConfig struct {
	ServerURL string
	Resource  string // canonical form of ServerURL
	Scopes    []string
	ClientID  string // pre-registered client (DCR fallback)
	Secret    string
}

// pendingAuth is one in-flight authorization-code flow, keyed by state.
type pendingAuth struct {
	server    string
	disc      *discovery
	client    ClientRegistration // resolved client identity (static or DCR)
	verifier  string             // PKCE code verifier
	expiresAt time.Time
	done      chan struct{}
	err       error // valid after done is closed
	completed bool
}

// ServerAuthInfo is the per-server view returned to the CLI and API.
type ServerAuthInfo struct {
	Server   string     `json:"server"`
	Resource string     `json:"resource"`
	Status   string     `json:"status"` // mcp.AuthStatusAuthorized | mcp.AuthStatusNeedsAuth
	Issuer   string     `json:"issuer,omitempty"`
	Scopes   []string   `json:"scopes,omitempty"`
	Expiry   *time.Time `json:"expiry,omitempty"`
}

// Broker owns downstream OAuth state: discovery, client identity, the
// authorization-code + PKCE flow, token persistence, and refresh rotation.
type Broker struct {
	store      *TokenStore
	httpClient *http.Client
	logger     *slog.Logger

	// redirectURL is the daemon-hosted callback, e.g.
	// http://localhost:8180/oauth/callback.
	redirectURL string

	sink         StateSink             // may be nil
	onAuthorized func(server string)   // may be nil; called after a successful login
	redactor     func(values []string) // may be nil; registers token values for log redaction

	mu      sync.Mutex
	servers map[string]serverConfig // server name -> oauth config
	pending map[string]*pendingAuth // state -> in-flight flow
	sources map[string]*grantSource // server name -> live header source
}

// NewBroker constructs a broker persisting into store, with the given
// daemon callback URL.
func NewBroker(store *TokenStore, redirectURL string, logger *slog.Logger) *Broker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Broker{
		store:       store,
		httpClient:  newDiscoveryClient(30 * time.Second),
		logger:      logger,
		redirectURL: redirectURL,
		servers:     make(map[string]serverConfig),
		pending:     make(map[string]*pendingAuth),
		sources:     make(map[string]*grantSource),
	}
}

// SetStateSink installs the gateway hook that receives state transitions.
func (b *Broker) SetStateSink(sink StateSink) { b.sink = sink }

// SetOnAuthorized installs a callback fired after a successful
// authorization; the controller uses it to re-register the server.
func (b *Broker) SetOnAuthorized(fn func(server string)) { b.onAuthorized = fn }

// SetRedactor installs the log-redaction hook. Every access and refresh
// token is registered the moment it enters memory, before any code path
// can log a request or response containing it.
func (b *Broker) SetRedactor(fn func(values []string)) { b.redactor = fn }

// redactToken registers a token's material with the redactor.
func (b *Broker) redactToken(tok *oauth2.Token) {
	if b.redactor == nil || tok == nil {
		return
	}
	var vals []string
	if tok.AccessToken != "" {
		vals = append(vals, tok.AccessToken)
	}
	if tok.RefreshToken != "" {
		vals = append(vals, tok.RefreshToken)
	}
	if len(vals) > 0 {
		b.redactor(vals)
	}
}

// Configure registers (or updates) the OAuth config for a server and
// reflects its current state into the sink: authorized when a usable grant
// already exists for the resource, needs-auth otherwise.
func (b *Broker) Configure(server, serverURL string, auth *mcp.ServerAuthConfig) error {
	resource, err := canonicalResource(serverURL)
	if err != nil {
		return err
	}
	cfg := serverConfig{ServerURL: serverURL, Resource: resource}
	if auth != nil {
		cfg.Scopes = auth.Scopes
		cfg.ClientID = auth.ClientID
		cfg.Secret = auth.ClientSecret
	}
	b.mu.Lock()
	b.servers[server] = cfg
	b.mu.Unlock()

	b.publishState(server)
	return nil
}

// Deconfigure removes a server from the broker without touching stored
// grants (other stacks may share them; RemoveServerGrant handles cleanup).
func (b *Broker) Deconfigure(server string) {
	b.mu.Lock()
	delete(b.servers, server)
	delete(b.sources, server)
	b.mu.Unlock()
}

// HeaderSource returns the live token source for a configured server. The
// same instance is reused so its cached token survives across requests.
func (b *Broker) HeaderSource(server string) mcp.HeaderSource {
	b.mu.Lock()
	defer b.mu.Unlock()
	if src, ok := b.sources[server]; ok {
		return src
	}
	src := &grantSource{broker: b, server: server}
	b.sources[server] = src
	return src
}

// HeaderSourceForResource returns a header source bound to a raw resource
// URL rather than a configured server name. The probe uses it so a
// pre-apply token acquired in the wizard carries over to the applied
// server (both key by canonical resource URL).
func (b *Broker) HeaderSourceForResource(serverURL string) mcp.HeaderSource {
	resource, err := canonicalResource(serverURL)
	if err != nil {
		return nil
	}
	return &grantSource{broker: b, server: resource, resourceOverride: resource}
}

// configFor resolves the effective config for a grantSource: a registered
// server by name, or an ad-hoc resource override from the probe path.
func (b *Broker) configFor(src *grantSource) (serverConfig, bool) {
	if src.resourceOverride != "" {
		return serverConfig{ServerURL: src.resourceOverride, Resource: src.resourceOverride}, true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	cfg, ok := b.servers[src.server]
	return cfg, ok
}

// BeginAuthorization starts the authorization-code flow for a configured
// server: discovery, client identity resolution (static config, cached
// registration, then DCR), PKCE, and the composed authorization URL. It
// returns the URL for the caller to open (the broker never opens a
// browser) and the single-use state that keys the pending flow.
func (b *Broker) BeginAuthorization(ctx context.Context, server string, timeout time.Duration) (authorizeURL, authState string, err error) {
	b.mu.Lock()
	cfg, ok := b.servers[server]
	b.mu.Unlock()
	if !ok {
		return "", "", fmt.Errorf("no OAuth configuration for server %q", server)
	}
	if timeout <= 0 {
		timeout = DefaultAuthTimeout
	}

	disc, err := discover(ctx, b.httpClient, cfg.ServerURL, cfg.Scopes)
	if err != nil {
		return "", "", err
	}

	client, err := b.resolveClient(ctx, disc, cfg)
	if err != nil {
		return "", "", err
	}

	verifier := oauth2.GenerateVerifier()
	stateToken, err := randomState()
	if err != nil {
		return "", "", err
	}

	oc := b.oauthConfig(disc, client)
	authorizeURL = oc.AuthCodeURL(stateToken,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", disc.Resource),
	)
	if err := validateAuthorizeURL(authorizeURL); err != nil {
		return "", "", err
	}

	b.mu.Lock()
	b.sweepPendingLocked()
	b.pending[stateToken] = &pendingAuth{
		server:    server,
		disc:      disc,
		client:    client,
		verifier:  verifier,
		expiresAt: time.Now().Add(timeout),
		done:      make(chan struct{}),
	}
	b.mu.Unlock()

	return authorizeURL, stateToken, nil
}

// CompleteAuthorization redeems an authorization code delivered to the
// callback (or pasted manually). state is single-use; iss, when the AS
// returned one, must match the recorded issuer (RFC 9207).
func (b *Broker) CompleteAuthorization(ctx context.Context, stateToken, code, iss string) error {
	b.mu.Lock()
	p, ok := b.pending[stateToken]
	if ok && (p.completed || time.Now().After(p.expiresAt)) {
		ok = false
	}
	if ok {
		p.completed = true
	}
	b.mu.Unlock()
	if !ok {
		return errors.New("unknown, expired, or already-used authorization state")
	}

	err := b.completePending(ctx, p, code, iss)
	p.err = err
	close(p.done)
	if err != nil {
		b.logger.Warn("authorization failed", "server", p.server, "error", err)
		return err
	}

	b.logger.Info("authorization complete", "server", p.server, "issuer", p.disc.Issuer)
	b.publishState(p.server)
	if b.onAuthorized != nil {
		b.onAuthorized(p.server)
	}
	return nil
}

// CompleteManual accepts a full pasted redirect URL (the --manual path for
// SSH sessions where the loopback callback cannot reach the daemon).
func (b *Broker) CompleteManual(ctx context.Context, redirectURL string) error {
	u, err := url.Parse(strings.TrimSpace(redirectURL))
	if err != nil {
		return fmt.Errorf("invalid redirect URL: %w", err)
	}
	q := u.Query()
	if e := q.Get("error"); e != "" {
		return fmt.Errorf("authorization server returned error: %s (%s)", e, q.Get("error_description"))
	}
	code, stateToken := q.Get("code"), q.Get("state")
	if code == "" || stateToken == "" {
		return errors.New("redirect URL is missing code or state")
	}
	return b.CompleteAuthorization(ctx, stateToken, code, q.Get("iss"))
}

// Wait blocks until the flow keyed by state completes, fails, or expires.
func (b *Broker) Wait(ctx context.Context, stateToken string) error {
	b.mu.Lock()
	p, ok := b.pending[stateToken]
	b.mu.Unlock()
	if !ok {
		return errors.New("unknown authorization state")
	}
	deadline := time.NewTimer(time.Until(p.expiresAt))
	defer deadline.Stop()
	select {
	case <-p.done:
		return p.err
	case <-deadline.C:
		return errors.New("authorization timed out; run login again")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// completePending performs the code exchange for a pending flow.
func (b *Broker) completePending(ctx context.Context, p *pendingAuth, code, iss string) error {
	// RFC 9207: when the AS identifies itself in the redirect, a mismatch
	// with the recorded issuer aborts redemption (mix-up attack).
	if iss != "" && iss != p.disc.Issuer {
		return fmt.Errorf("issuer mismatch in authorization response: got %q, expected %q", iss, p.disc.Issuer)
	}

	oc := b.oauthConfig(p.disc, p.client)
	ctx = context.WithValue(ctx, oauth2.HTTPClient, b.httpClient)
	tok, err := oc.Exchange(ctx, code,
		oauth2.VerifierOption(p.verifier),
		oauth2.SetAuthURLParam("resource", p.disc.Resource),
	)
	if err != nil {
		return fmt.Errorf("exchanging authorization code: %w", err)
	}
	b.redactToken(tok)

	grant := Grant{
		Resource:           p.disc.Resource,
		Issuer:             p.disc.Issuer,
		Scopes:             p.disc.Scopes,
		Token:              tok,
		TokenEndpoint:      p.disc.Meta.TokenEndpoint,
		RevocationEndpoint: p.disc.Meta.RevocationEndpoint,
	}
	if err := b.store.PutGrant(grant); err != nil {
		return fmt.Errorf("persisting grant: %w", err)
	}

	// Refresh the live source cache so in-flight clients pick up the new
	// token without waiting for a store re-read.
	b.mu.Lock()
	for _, src := range b.sources {
		src.dropCacheIfResource(p.disc.Resource)
	}
	b.mu.Unlock()
	return nil
}

// resolveClient picks the client identity in spec order: pre-registered
// static credentials, cached registration for the issuer, then DCR.
func (b *Broker) resolveClient(ctx context.Context, disc *discovery, cfg serverConfig) (ClientRegistration, error) {
	if cfg.ClientID != "" {
		return ClientRegistration{
			Issuer:       disc.Issuer,
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.Secret,
			RedirectURI:  b.redirectURL,
		}, nil
	}

	if reg, ok, err := b.store.Registration(disc.Issuer); err == nil && ok && reg.RedirectURI == b.redirectURL {
		return reg, nil
	}

	reg, err := registerClient(ctx, b.httpClient, disc, b.redirectURL)
	if err != nil {
		return ClientRegistration{}, err
	}
	if err := b.store.PutRegistration(*reg); err != nil {
		return ClientRegistration{}, fmt.Errorf("persisting client registration: %w", err)
	}
	return *reg, nil
}

// oauthConfig builds the x/oauth2 config for a discovered environment and
// resolved client identity.
func (b *Broker) oauthConfig(disc *discovery, client ClientRegistration) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		RedirectURL:  b.redirectURL,
		Scopes:       disc.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  disc.Meta.AuthorizationEndpoint,
			TokenURL: disc.Meta.TokenEndpoint,
		},
	}
}

// Logout revokes (best effort, RFC 7009) and deletes the grant backing a
// configured server.
func (b *Broker) Logout(ctx context.Context, server string) error {
	b.mu.Lock()
	cfg, ok := b.servers[server]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("no OAuth configuration for server %q", server)
	}
	return b.removeGrant(ctx, cfg.Resource, server)
}

// RemoveServerGrant is the stack-removal cleanup path: it revokes and
// deletes the grant only when no other configured server shares the
// resource.
func (b *Broker) RemoveServerGrant(ctx context.Context, server string) error {
	b.mu.Lock()
	cfg, ok := b.servers[server]
	var shared bool
	if ok {
		for name, other := range b.servers {
			if name != server && other.Resource == cfg.Resource {
				shared = true
				break
			}
		}
	}
	b.mu.Unlock()
	if !ok || shared {
		b.Deconfigure(server)
		return nil
	}
	err := b.removeGrant(ctx, cfg.Resource, server)
	b.Deconfigure(server)
	return err
}

func (b *Broker) removeGrant(ctx context.Context, resource, server string) error {
	grant, ok, err := b.store.Grant(resource)
	if err == nil && ok {
		b.revokeToken(ctx, grant, server)
	}
	if err := b.store.DeleteGrant(resource); err != nil {
		return err
	}
	b.mu.Lock()
	for _, src := range b.sources {
		src.dropCacheIfResource(resource)
	}
	b.mu.Unlock()
	b.publishState(server)
	return nil
}

// revokeToken is best-effort RFC 7009 revocation of the refresh token (or
// access token when no refresh token exists). Failures are logged, never
// fatal: local deletion is the real cleanup.
func (b *Broker) revokeToken(ctx context.Context, grant Grant, server string) {
	if grant.RevocationEndpoint == "" || grant.Token == nil {
		return
	}
	token := grant.Token.RefreshToken
	hint := "refresh_token"
	if token == "" {
		token = grant.Token.AccessToken
		hint = "access_token"
	}
	if token == "" {
		return
	}

	form := url.Values{"token": {token}, "token_type_hint": {hint}}
	creds := b.clientCredsForGrant(grant, server)
	if creds.ClientSecret == "" && creds.ClientID != "" {
		form.Set("client_id", creds.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, grant.RevocationEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if creds.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(creds.ClientID), url.QueryEscape(creds.ClientSecret))
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.logger.Debug("token revocation failed", "server", server, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b.logger.Debug("token revocation rejected", "server", server, "status", resp.StatusCode)
	}
}

// Reset deletes the grant and the issuer's cached client registration for
// a server: the first-class version of mcp-remote's rm -rf escape hatch.
func (b *Broker) Reset(ctx context.Context, server string) error {
	b.mu.Lock()
	cfg, ok := b.servers[server]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("no OAuth configuration for server %q", server)
	}
	grant, found, err := b.store.Grant(cfg.Resource)
	if err == nil && found {
		if regErr := b.store.DeleteRegistration(grant.Issuer); regErr != nil {
			return regErr
		}
	}
	return b.removeGrant(ctx, cfg.Resource, server)
}

// Status returns per-server authorization info for every configured server.
func (b *Broker) Status() []ServerAuthInfo {
	b.mu.Lock()
	servers := make(map[string]serverConfig, len(b.servers))
	for name, cfg := range b.servers {
		servers[name] = cfg
	}
	b.mu.Unlock()

	out := make([]ServerAuthInfo, 0, len(servers))
	for name, cfg := range servers {
		out = append(out, b.infoFor(name, cfg))
	}
	return out
}

// ServerStatus returns authorization info for one configured server.
func (b *Broker) ServerStatus(server string) (ServerAuthInfo, error) {
	b.mu.Lock()
	cfg, ok := b.servers[server]
	b.mu.Unlock()
	if !ok {
		return ServerAuthInfo{}, fmt.Errorf("no OAuth configuration for server %q", server)
	}
	return b.infoFor(server, cfg), nil
}

func (b *Broker) infoFor(server string, cfg serverConfig) ServerAuthInfo {
	info := ServerAuthInfo{Server: server, Resource: cfg.Resource, Status: mcp.AuthStatusNeedsAuth}
	grant, ok, err := b.store.Grant(cfg.Resource)
	if err != nil || !ok || grant.Token == nil {
		return info
	}
	// A grant with a refresh token is authorized even when the access token
	// is stale: the next request refreshes it. Without a refresh token the
	// access token's own validity decides.
	if grant.Token.RefreshToken != "" || grant.Token.Valid() {
		info.Status = mcp.AuthStatusAuthorized
		info.Issuer = grant.Issuer
		info.Scopes = grant.Scopes
		if !grant.Token.Expiry.IsZero() {
			e := grant.Token.Expiry
			info.Expiry = &e
		}
	}
	return info
}

// publishState pushes a server's current state into the sink.
func (b *Broker) publishState(server string) {
	if b.sink == nil {
		return
	}
	b.mu.Lock()
	cfg, ok := b.servers[server]
	b.mu.Unlock()
	if !ok {
		return
	}
	info := b.infoFor(server, cfg)
	st := mcp.ServerAuthState{Status: info.Status, Issuer: info.Issuer, Expiry: info.Expiry}
	if info.Status == mcp.AuthStatusNeedsAuth {
		st.Error = "authorization required"
	}
	b.sink.SetServerAuthState(server, st)
}

// markNeedsAuth transitions a server to needs-auth after a refresh
// rejection, with the reason surfaced in status.
func (b *Broker) markNeedsAuth(server, reason string) {
	if b.sink == nil {
		return
	}
	b.sink.SetServerAuthState(server, mcp.ServerAuthState{
		Status: mcp.AuthStatusNeedsAuth,
		Error:  reason,
	})
}

// sweepPendingLocked drops expired or completed pending flows. Caller
// holds b.mu.
func (b *Broker) sweepPendingLocked() {
	now := time.Now()
	for stateToken, p := range b.pending {
		if p.completed || now.After(p.expiresAt.Add(time.Minute)) {
			delete(b.pending, stateToken)
		}
	}
}

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
