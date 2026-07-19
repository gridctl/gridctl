package mcpauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// fakeAS is an httptest-backed resource server + authorization server
// implementing the discovery, registration, token, and revocation
// endpoints the broker drives.
type fakeAS struct {
	t   *testing.T
	srv *httptest.Server

	mu             sync.Mutex
	dcrStatus      int    // 0 = registration succeeds
	issuedCode     string // authorization code the token endpoint accepts
	codeChallenge  string // S256 challenge bound to issuedCode (set by test)
	refreshError   string // non-empty = refresh returns this OAuth error
	accessCount    int    // counts issued access tokens
	lastTokenForm  url.Values
	revokedTokens  []string
	registerCalls  int
	rotatedRefresh string // refresh token returned by the last refresh
}

func newFakeAS(t *testing.T) *fakeAS {
	f := &fakeAS{t: t, issuedCode: "test-code"}
	mux := http.NewServeMux()

	// The MCP resource endpoint: always 401 with a challenge.
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate",
			fmt.Sprintf("Bearer resource_metadata=%q", f.srv.URL+"/.well-known/oauth-protected-resource"))
		w.WriteHeader(http.StatusUnauthorized)
	})

	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"resource":              f.srv.URL + "/mcp",
			"authorization_servers": []string{f.srv.URL},
			"scopes_supported":      []string{"mcp.read", "mcp.write"},
		})
	})

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                           f.srv.URL,
			"authorization_endpoint":           f.srv.URL + "/authorize",
			"token_endpoint":                   f.srv.URL + "/token",
			"registration_endpoint":            f.srv.URL + "/register",
			"revocation_endpoint":              f.srv.URL + "/revoke",
			"jwks_uri":                         f.srv.URL + "/jwks",
			"response_types_supported":         []string{"code"},
			"grant_types_supported":            []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported": []string{"S256"},
		})
	})

	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.registerCalls++
		status := f.dcrStatus
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		var req clientRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.ApplicationType != "native" {
			f.t.Errorf("registration missing application_type=native, got %q", req.ApplicationType)
		}
		if len(req.RedirectURIs) == 0 {
			f.t.Error("registration missing redirect_uris")
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]any{"client_id": "dyn-client"})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.lastTokenForm = r.PostForm

		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != f.issuedCode {
				writeOAuthError(w, "invalid_grant")
				return
			}
			verifier := r.PostForm.Get("code_verifier")
			if verifier == "" {
				writeOAuthError(w, "invalid_request")
				return
			}
			if f.codeChallenge != "" {
				sum := sha256.Sum256([]byte(verifier))
				if base64.RawURLEncoding.EncodeToString(sum[:]) != f.codeChallenge {
					writeOAuthError(w, "invalid_grant")
					return
				}
			}
			if r.PostForm.Get("resource") == "" {
				writeOAuthError(w, "invalid_target")
				return
			}
			f.accessCount++
			writeJSON(w, map[string]any{
				"access_token":  fmt.Sprintf("access-%d", f.accessCount),
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "refresh-1",
			})
		case "refresh_token":
			if f.refreshError != "" {
				writeOAuthError(w, f.refreshError)
				return
			}
			if r.PostForm.Get("resource") == "" {
				writeOAuthError(w, "invalid_target")
				return
			}
			f.accessCount++
			f.rotatedRefresh = fmt.Sprintf("refresh-%d", f.accessCount)
			writeJSON(w, map[string]any{
				"access_token":  fmt.Sprintf("access-%d", f.accessCount),
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": f.rotatedRefresh,
			})
		default:
			writeOAuthError(w, "unsupported_grant_type")
		}
	})

	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		_ = r.ParseForm()
		f.mu.Lock()
		f.revokedTokens = append(f.revokedTokens, r.PostForm.Get("token"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAS) resource() string { return f.srv.URL + "/mcp" }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// recordingSink captures state transitions for assertions.
type recordingSink struct {
	mu     sync.Mutex
	states map[string][]mcp.ServerAuthState
}

func newRecordingSink() *recordingSink {
	return &recordingSink{states: map[string][]mcp.ServerAuthState{}}
}

func (s *recordingSink) SetServerAuthState(name string, st mcp.ServerAuthState) {
	s.mu.Lock()
	s.states[name] = append(s.states[name], st)
	s.mu.Unlock()
}

func (s *recordingSink) last(name string) (mcp.ServerAuthState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hist := s.states[name]
	if len(hist) == 0 {
		return mcp.ServerAuthState{}, false
	}
	return hist[len(hist)-1], true
}

func newTestBroker(t *testing.T) (*Broker, *TokenStore, *recordingSink) {
	t.Helper()
	store, err := NewTokenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := newRecordingSink()
	b := NewBroker(store, "http://localhost:8180"+CallbackPath, nil)
	b.SetStateSink(sink)
	return b, store, sink
}

// runAuthFlow drives Begin + Complete against the fake AS and returns the
// state token used.
func runAuthFlow(t *testing.T, b *Broker, f *fakeAS, server string) {
	t.Helper()
	authURL, stateToken, err := b.BeginAuthorization(context.Background(), server, time.Minute)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing authorize URL: %v", err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("authorize URL missing code_challenge")
	}
	if q.Get("resource") != f.resource() {
		t.Errorf("authorize resource = %q, want %q", q.Get("resource"), f.resource())
	}
	if q.Get("state") != stateToken {
		t.Errorf("authorize state mismatch")
	}

	f.mu.Lock()
	f.codeChallenge = q.Get("code_challenge")
	f.mu.Unlock()

	if err := b.CompleteAuthorization(context.Background(), stateToken, f.issuedCode, f.srv.URL); err != nil {
		t.Fatalf("CompleteAuthorization: %v", err)
	}
}

func TestBrokerFullFlow(t *testing.T) {
	f := newFakeAS(t)
	b, store, sink := newTestBroker(t)

	if err := b.Configure("notion", f.resource(), &mcp.ServerAuthConfig{Type: "oauth"}); err != nil {
		t.Fatal(err)
	}
	if st, ok := sink.last("notion"); !ok || st.Status != mcp.AuthStatusNeedsAuth {
		t.Fatalf("expected needs_auth after configure, got %+v", st)
	}

	runAuthFlow(t, b, f, "notion")

	if st, ok := sink.last("notion"); !ok || st.Status != mcp.AuthStatusAuthorized {
		t.Fatalf("expected authorized after flow, got %+v", st)
	}

	name, value, err := b.HeaderSource("notion").AuthHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthHeader: %v", err)
	}
	if name != "Authorization" || !strings.HasPrefix(value, "Bearer access-") {
		t.Errorf("unexpected header %q: %q", name, value)
	}

	// Persistence: a fresh broker over the same store dir sees the grant.
	store2, err := NewTokenStore(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	b2 := NewBroker(store2, b.redirectURL, nil)
	if err := b2.Configure("notion", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	info, err := b2.ServerStatus("notion")
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != mcp.AuthStatusAuthorized {
		t.Errorf("restart lost authorization: %+v", info)
	}
	if info.Issuer != f.srv.URL {
		t.Errorf("issuer = %q, want %q", info.Issuer, f.srv.URL)
	}
}

func TestBrokerStateSingleUse(t *testing.T) {
	f := newFakeAS(t)
	b, _, _ := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	runAuthFlow(t, b, f, "s")

	// Second redemption with any state that was already used must fail.
	err := b.CompleteAuthorization(context.Background(), "bogus", f.issuedCode, "")
	if err == nil {
		t.Fatal("expected unknown state to be rejected")
	}
}

func TestBrokerIssMismatchAborts(t *testing.T) {
	f := newFakeAS(t)
	b, store, _ := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	_, stateToken, err := b.BeginAuthorization(context.Background(), "s", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = b.CompleteAuthorization(context.Background(), stateToken, f.issuedCode, "https://evil.example.com")
	if err == nil || !strings.Contains(err.Error(), "issuer mismatch") {
		t.Fatalf("expected issuer mismatch abort, got %v", err)
	}
	if _, ok, _ := store.Grant(f.resource()); ok {
		t.Fatal("no grant may be stored after an iss mismatch")
	}
}

func TestBrokerDCRRefusedNamesFallback(t *testing.T) {
	f := newFakeAS(t)
	f.dcrStatus = http.StatusNotImplemented
	b, _, _ := newTestBroker(t)
	if err := b.Configure("slack", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := b.BeginAuthorization(context.Background(), "slack", time.Minute)
	if !errors.Is(err, ErrDCRUnsupported) {
		t.Fatalf("expected ErrDCRUnsupported, got %v", err)
	}
	if !strings.Contains(err.Error(), "auth.client_id") {
		t.Errorf("error must name the static-client fallback: %v", err)
	}
}

func TestBrokerStaticClientSkipsDCR(t *testing.T) {
	f := newFakeAS(t)
	f.dcrStatus = http.StatusNotImplemented
	b, _, _ := newTestBroker(t)
	if err := b.Configure("slack", f.resource(), &mcp.ServerAuthConfig{
		Type: "oauth", ClientID: "static-id", ClientSecret: "static-secret",
	}); err != nil {
		t.Fatal(err)
	}

	runAuthFlow(t, b, f, "slack")

	f.mu.Lock()
	registerCalls := f.registerCalls
	f.mu.Unlock()
	if registerCalls != 0 {
		t.Errorf("static client must not hit the registration endpoint, got %d calls", registerCalls)
	}
}

func TestGrantSourceRefreshRotation(t *testing.T) {
	f := newFakeAS(t)
	b, store, _ := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	resource, _ := canonicalResource(f.resource())

	if err := store.PutRegistration(ClientRegistration{
		Issuer: f.srv.URL, ClientID: "dyn-client", RedirectURI: b.redirectURL,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutGrant(Grant{
		Resource: resource,
		Issuer:   f.srv.URL,
		Token: &oauth2.Token{
			AccessToken:  "stale",
			RefreshToken: "refresh-0",
			Expiry:       time.Now().Add(-time.Hour),
		},
		TokenEndpoint: f.srv.URL + "/token",
	}); err != nil {
		t.Fatal(err)
	}

	_, value, err := b.HeaderSource("s").AuthHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthHeader: %v", err)
	}
	if value == "Bearer stale" {
		t.Fatal("expired token was not refreshed")
	}

	// The rotated refresh token must be persisted immediately.
	grant, ok, err := store.Grant(resource)
	if err != nil || !ok {
		t.Fatalf("grant missing after refresh: %v", err)
	}
	f.mu.Lock()
	rotated := f.rotatedRefresh
	f.mu.Unlock()
	if grant.Token.RefreshToken != rotated {
		t.Errorf("rotated refresh token not persisted: got %q, want %q", grant.Token.RefreshToken, rotated)
	}
	if got := f.lastTokenForm.Get("resource"); got != resource {
		t.Errorf("refresh request missing resource param: %q", got)
	}
}

func TestGrantSourceInvalidGrantSelfHeals(t *testing.T) {
	f := newFakeAS(t)
	f.refreshError = "invalid_grant"
	b, store, sink := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	resource, _ := canonicalResource(f.resource())

	if err := store.PutRegistration(ClientRegistration{
		Issuer: f.srv.URL, ClientID: "dyn-client", RedirectURI: b.redirectURL,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutGrant(Grant{
		Resource: resource,
		Issuer:   f.srv.URL,
		Token: &oauth2.Token{
			AccessToken:  "stale",
			RefreshToken: "dead-refresh",
			Expiry:       time.Now().Add(-time.Hour),
		},
		TokenEndpoint: f.srv.URL + "/token",
	}); err != nil {
		t.Fatal(err)
	}

	src := b.HeaderSource("s")
	_, _, err := src.AuthHeader(context.Background())
	var needsAuth *mcp.NeedsAuthError
	if !errors.As(err, &needsAuth) {
		t.Fatalf("expected NeedsAuthError, got %v", err)
	}
	if !strings.Contains(err.Error(), "gridctl auth login s") {
		t.Errorf("error must name the login command: %v", err)
	}

	// Self-heal: the dead grant is discarded exactly once.
	if _, ok, _ := store.Grant(resource); ok {
		t.Fatal("dead grant must be deleted after invalid_grant")
	}
	if st, ok := sink.last("s"); !ok || st.Status != mcp.AuthStatusNeedsAuth {
		t.Fatalf("expected needs_auth after refresh rejection, got %+v", st)
	}

	// A second call must not hit the token endpoint again (grant gone).
	f.mu.Lock()
	f.lastTokenForm = nil
	f.mu.Unlock()
	_, _, err = src.AuthHeader(context.Background())
	if !errors.As(err, &needsAuth) {
		t.Fatalf("expected NeedsAuthError on second call, got %v", err)
	}
	f.mu.Lock()
	retried := f.lastTokenForm != nil
	f.mu.Unlock()
	if retried {
		t.Error("second call after self-heal must not retry the refresh")
	}
}

func TestBrokerLogoutRevokesAndDeletes(t *testing.T) {
	f := newFakeAS(t)
	b, store, sink := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	runAuthFlow(t, b, f, "s")

	if err := b.Logout(context.Background(), "s"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	resource, _ := canonicalResource(f.resource())
	if _, ok, _ := store.Grant(resource); ok {
		t.Fatal("grant must be deleted on logout")
	}
	f.mu.Lock()
	revoked := len(f.revokedTokens)
	f.mu.Unlock()
	if revoked == 0 {
		t.Error("logout must attempt revocation")
	}
	if st, ok := sink.last("s"); !ok || st.Status != mcp.AuthStatusNeedsAuth {
		t.Fatalf("expected needs_auth after logout, got %+v", st)
	}
}

func TestBrokerResetClearsRegistration(t *testing.T) {
	f := newFakeAS(t)
	b, store, _ := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	runAuthFlow(t, b, f, "s")

	if _, ok, _ := store.Registration(f.srv.URL); !ok {
		t.Fatal("expected a cached registration after DCR")
	}
	if err := b.Reset(context.Background(), "s"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok, _ := store.Registration(f.srv.URL); ok {
		t.Error("reset must delete the cached registration")
	}
	resource, _ := canonicalResource(f.resource())
	if _, ok, _ := store.Grant(resource); ok {
		t.Error("reset must delete the grant")
	}
}

func TestBrokerWait(t *testing.T) {
	f := newFakeAS(t)
	b, _, _ := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	authURL, stateToken, err := b.BeginAuthorization(context.Background(), "s", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	f.mu.Lock()
	f.codeChallenge = u.Query().Get("code_challenge")
	f.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- b.Wait(context.Background(), stateToken) }()

	time.Sleep(50 * time.Millisecond)
	if err := b.CompleteAuthorization(context.Background(), stateToken, f.issuedCode, ""); err != nil {
		t.Fatalf("CompleteAuthorization: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not observe completion")
	}
}

func TestCallbackHandler(t *testing.T) {
	f := newFakeAS(t)
	b, _, sink := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	authURL, stateToken, err := b.BeginAuthorization(context.Background(), "s", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	f.mu.Lock()
	f.codeChallenge = u.Query().Get("code_challenge")
	f.mu.Unlock()

	cb := httptest.NewServer(b.CallbackHandler())
	defer cb.Close()

	resp, err := http.Get(cb.URL + "?code=" + f.issuedCode + "&state=" + url.QueryEscape(stateToken))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback returned %d", resp.StatusCode)
	}
	if st, ok := sink.last("s"); !ok || st.Status != mcp.AuthStatusAuthorized {
		t.Fatalf("expected authorized after callback, got %+v", st)
	}
}

func TestCanonicalResource(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://MCP.Example.com/mcp", "https://mcp.example.com/mcp", false},
		{"https://mcp.example.com:443/mcp", "https://mcp.example.com/mcp", false},
		{"http://localhost:8080/mcp", "http://localhost:8080/mcp", false},
		{"https://mcp.example.com/mcp#frag", "https://mcp.example.com/mcp", false},
		{"ftp://example.com", "", true},
	}
	for _, tt := range tests {
		got, err := canonicalResource(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("canonicalResource(%q): expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("canonicalResource(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("canonicalResource(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTokenStoreCorruptFileDegrades(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.enc"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Grant("https://x.example.com/mcp"); err == nil {
		t.Fatal("corrupt store must surface an error, not panic")
	}

	// The broker degrades a store error to needs-auth, never a crash.
	b := NewBroker(store, "http://localhost:8180"+CallbackPath, nil)
	if err := b.Configure("s", "https://x.example.com/mcp", nil); err != nil {
		t.Fatal(err)
	}
	_, _, err = b.HeaderSource("s").AuthHeader(context.Background())
	var needsAuth *mcp.NeedsAuthError
	if !errors.As(err, &needsAuth) {
		t.Fatalf("expected NeedsAuthError from corrupt store, got %v", err)
	}
}

func TestTokenStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := Grant{
		Resource:      "https://mcp.example.com/mcp",
		Issuer:        "https://as.example.com",
		Scopes:        []string{"read"},
		Token:         &oauth2.Token{AccessToken: "a", RefreshToken: "r"},
		TokenEndpoint: "https://as.example.com/token",
	}
	if err := store.PutGrant(g); err != nil {
		t.Fatal(err)
	}

	// The file on disk must not contain the plaintext tokens.
	raw, err := os.ReadFile(filepath.Join(dir, "tokens.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "access") || strings.Contains(string(raw), `"a"`) {
		t.Error("token store file leaks plaintext token material")
	}

	// A second store instance with the same dir reads it back.
	store2, err := NewTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := store2.Grant(g.Resource)
	if err != nil || !ok {
		t.Fatalf("grant not found: %v", err)
	}
	if got.Token.AccessToken != "a" || got.Token.RefreshToken != "r" {
		t.Errorf("round trip mangled token: %+v", got.Token)
	}
}

func TestCallbackDenialFailsWaiterImmediately(t *testing.T) {
	f := newFakeAS(t)
	b, _, _ := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	_, stateToken, err := b.BeginAuthorization(context.Background(), "s", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- b.Wait(context.Background(), stateToken) }()

	// The user clicks Deny: the AS redirects back with error=access_denied.
	cb := httptest.NewServer(b.CallbackHandler())
	defer cb.Close()
	resp, err := http.Get(cb.URL + "?error=access_denied&state=" + url.QueryEscape(stateToken))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case waitErr := <-done:
		if waitErr == nil || !strings.Contains(waitErr.Error(), "access_denied") {
			t.Fatalf("expected denial to reach the waiter, got %v", waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("denial did not wake the waiter; it would have run out the flow timeout")
	}
}

func TestWaitAfterCompletionAndLaterBegin(t *testing.T) {
	f := newFakeAS(t)
	b, _, _ := newTestBroker(t)
	if err := b.Configure("a", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Configure("b", f.resource(), nil); err != nil {
		t.Fatal(err)
	}

	authURL, stateA, err := b.BeginAuthorization(context.Background(), "a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	f.mu.Lock()
	f.codeChallenge = u.Query().Get("code_challenge")
	f.mu.Unlock()
	if err := b.CompleteAuthorization(context.Background(), stateA, f.issuedCode, ""); err != nil {
		t.Fatal(err)
	}

	// A second Begin (for another server) sweeps pending state; the
	// completed flow must survive so a late or re-issued wait still
	// observes the outcome.
	if _, _, err := b.BeginAuthorization(context.Background(), "b", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := b.Wait(context.Background(), stateA); err != nil {
		t.Fatalf("late Wait after completion must succeed, got %v", err)
	}
}

func TestResetWithoutGrantStillClearsRegistration(t *testing.T) {
	f := newFakeAS(t)
	b, store, _ := newTestBroker(t)
	if err := b.Configure("s", f.resource(), nil); err != nil {
		t.Fatal(err)
	}
	// A cached registration with no grant: the state reset exists for.
	if err := store.PutRegistration(ClientRegistration{
		Issuer: f.srv.URL, ClientID: "stale-client", RedirectURI: b.redirectURL,
	}); err != nil {
		t.Fatal(err)
	}

	if err := b.Reset(context.Background(), "s"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok, _ := store.Registration(f.srv.URL); ok {
		t.Fatal("reset must delete the cached registration even with no grant")
	}
}
