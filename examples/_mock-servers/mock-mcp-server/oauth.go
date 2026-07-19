// OAuth 2.1 mode for the mock MCP server. Enabled with -oauth, it turns
// the server into a self-contained protected resource + authorization
// server implementing exactly what gridctl's broker drives: RFC 9728
// protected resource metadata, RFC 8414 AS metadata, RFC 7591 dynamic
// client registration (refusable with -oauth-no-dcr), an auto-approving
// authorization endpoint (no human in the loop, so integration tests can
// play the browser with a plain HTTP client), a token endpoint with PKCE
// S256 verification, RFC 8707 resource checks, rotating refresh tokens,
// and RFC 7009 revocation.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var (
	oauthMode      bool
	oauthNoDCR     bool
	oauthAccessTTL int
	oauthBaseURL   string
)

func init() {
	flag.BoolVar(&oauthMode, "oauth", false, "Require OAuth 2.1 authorization")
	flag.BoolVar(&oauthNoDCR, "oauth-no-dcr", false, "Refuse dynamic client registration (501)")
	flag.IntVar(&oauthAccessTTL, "oauth-access-ttl", 3600, "Access token lifetime in seconds")
	flag.StringVar(&oauthBaseURL, "base-url", "", "Externally visible base URL (default http://127.0.0.1:<port>)")
}

// authState holds the AS's in-memory state.
type authState struct {
	mu             sync.Mutex
	codes          map[string]codeGrant // authorization code -> pending grant
	accessTokens   map[string]time.Time // access token -> expiry
	refreshTokens  map[string]bool      // live refresh tokens (rotated on use)
	revoked        []string
	tokenCounter   int
	refreshCounter int
}

type codeGrant struct {
	challenge   string
	redirectURI string
	clientID    string
}

var oauthSrv = &authState{
	codes:         map[string]codeGrant{},
	accessTokens:  map[string]time.Time{},
	refreshTokens: map[string]bool{},
}

func randomToken(prefix string) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(buf)
}

// requireBearer wraps a handler with the 401 + WWW-Authenticate challenge.
func requireBearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !oauthMode {
			next(w, r)
			return
		}
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		valid := false
		if len(header) > len(prefix) && header[:len(prefix)] == prefix {
			token := header[len(prefix):]
			oauthSrv.mu.Lock()
			expiry, ok := oauthSrv.accessTokens[token]
			oauthSrv.mu.Unlock()
			valid = ok && time.Now().Before(expiry)
		}
		if !valid {
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf("Bearer resource_metadata=%q", oauthBaseURL+"/.well-known/oauth-protected-resource"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// registerOAuthRoutes mounts the discovery, authorization, token,
// registration, and revocation endpoints.
func registerOAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSONBody(w, map[string]any{
			"resource":              oauthBaseURL + "/mcp",
			"authorization_servers": []string{oauthBaseURL},
			"scopes_supported":      []string{"mock.read"},
		})
	})

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		meta := map[string]any{
			"issuer":                           oauthBaseURL,
			"authorization_endpoint":           oauthBaseURL + "/authorize",
			"token_endpoint":                   oauthBaseURL + "/token",
			"revocation_endpoint":              oauthBaseURL + "/revoke",
			"jwks_uri":                         oauthBaseURL + "/jwks",
			"response_types_supported":         []string{"code"},
			"grant_types_supported":            []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported": []string{"S256"},
		}
		if !oauthNoDCR {
			meta["registration_endpoint"] = oauthBaseURL + "/register"
		}
		writeJSONBody(w, meta)
	})

	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if oauthNoDCR {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		var req struct {
			RedirectURIs    []string `json:"redirect_uris"`
			ApplicationType string   `json:"application_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.RedirectURIs) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONBody(w, map[string]string{"client_id": "mock-dyn-client"})
	})

	// Auto-approving authorization endpoint: validates PKCE + resource
	// params and 302s straight back with a fresh code. The integration
	// test's HTTP client plays the browser by following this redirect.
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		challenge := q.Get("code_challenge")
		redirectURI := q.Get("redirect_uri")
		state := q.Get("state")
		if q.Get("code_challenge_method") != "S256" || challenge == "" || redirectURI == "" || state == "" {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		if q.Get("resource") != oauthBaseURL+"/mcp" {
			http.Error(w, "missing or wrong resource indicator", http.StatusBadRequest)
			return
		}

		code := randomToken("code")
		oauthSrv.mu.Lock()
		oauthSrv.codes[code] = codeGrant{challenge: challenge, redirectURI: redirectURI, clientID: q.Get("client_id")}
		oauthSrv.mu.Unlock()

		loc, _ := url.Parse(redirectURI)
		params := loc.Query()
		params.Set("code", code)
		params.Set("state", state)
		params.Set("iss", oauthBaseURL)
		loc.RawQuery = params.Encode()
		http.Redirect(w, r, loc.String(), http.StatusFound)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("resource") != oauthBaseURL+"/mcp" {
			writeOAuthErrorBody(w, "invalid_target")
			return
		}

		oauthSrv.mu.Lock()
		defer oauthSrv.mu.Unlock()

		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			code := r.PostForm.Get("code")
			grant, ok := oauthSrv.codes[code]
			if !ok {
				writeOAuthErrorBody(w, "invalid_grant")
				return
			}
			delete(oauthSrv.codes, code) // single use
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != grant.challenge {
				writeOAuthErrorBody(w, "invalid_grant")
				return
			}
			oauthSrv.issueLocked(w)
		case "refresh_token":
			token := r.PostForm.Get("refresh_token")
			if !oauthSrv.refreshTokens[token] {
				writeOAuthErrorBody(w, "invalid_grant")
				return
			}
			delete(oauthSrv.refreshTokens, token) // single-use rotation
			oauthSrv.refreshCounter++
			oauthSrv.issueLocked(w)
		default:
			writeOAuthErrorBody(w, "unsupported_grant_type")
		}
	})

	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		oauthSrv.mu.Lock()
		oauthSrv.revoked = append(oauthSrv.revoked, r.PostForm.Get("token"))
		oauthSrv.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	// Test introspection: how many refreshes have happened. Unauthenticated
	// on purpose; this is a mock.
	mux.HandleFunc("/debug/oauth", func(w http.ResponseWriter, r *http.Request) {
		oauthSrv.mu.Lock()
		defer oauthSrv.mu.Unlock()
		writeJSONBody(w, map[string]any{
			"tokens_issued": oauthSrv.tokenCounter,
			"refreshes":     oauthSrv.refreshCounter,
			"revoked":       oauthSrv.revoked,
		})
	})
}

// issueLocked mints a fresh access + refresh token pair. Caller holds mu.
func (s *authState) issueLocked(w http.ResponseWriter) {
	s.tokenCounter++
	access := fmt.Sprintf("mock-access-%d", s.tokenCounter)
	refresh := randomToken("refresh")
	s.accessTokens[access] = time.Now().Add(time.Duration(oauthAccessTTL) * time.Second)
	s.refreshTokens[refresh] = true
	writeJSONBody(w, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    oauthAccessTTL,
		"refresh_token": refresh,
	})
}

func writeJSONBody(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthErrorBody(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func maybeLogOAuthMode() {
	if !oauthMode {
		return
	}
	log.Printf("OAuth mode: base=%s dcr=%v access_ttl=%ds", oauthBaseURL, !oauthNoDCR, oauthAccessTTL)
}
