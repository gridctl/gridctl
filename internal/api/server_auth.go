package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// maxAuthTimeout caps the client-requested authorization wait.
const maxAuthTimeout = 15 * time.Minute

// handleAuthServers returns per-server downstream authorization state for
// every OAuth-configured server. GET /api/auth/servers.
func (s *Server) handleAuthServers(w http.ResponseWriter, r *http.Request) {
	if s.oauthBroker == nil {
		writeJSONError(w, "OAuth brokering is not enabled", http.StatusNotImplemented)
		return
	}
	writeJSON(w, s.oauthBroker.Status())
}

// handleAuthLogin starts the authorization-code flow for a server and
// returns the URL the client must open plus the flow's state token.
// POST /api/servers/{name}/auth/login.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.oauthBroker == nil {
		writeJSONError(w, "OAuth brokering is not enabled", http.StatusNotImplemented)
		return
	}
	name := r.PathValue("name")

	var body struct {
		TimeoutSeconds int `json:"timeoutSeconds"`
	}
	// An empty or malformed body keeps the broker's default timeout.
	_ = json.NewDecoder(r.Body).Decode(&body)
	timeout := time.Duration(body.TimeoutSeconds) * time.Second
	if timeout > maxAuthTimeout {
		timeout = maxAuthTimeout
	}

	authorizeURL, state, err := s.oauthBroker.BeginAuthorization(r.Context(), name, timeout)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{
		"authorize_url": authorizeURL,
		"state":         state,
	})
}

// handleAuthWait blocks until the flow keyed by state completes, fails, or
// times out. GET /api/servers/{name}/auth/wait?state=...
func (s *Server) handleAuthWait(w http.ResponseWriter, r *http.Request) {
	if s.oauthBroker == nil {
		writeJSONError(w, "OAuth brokering is not enabled", http.StatusNotImplemented)
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		writeJSONError(w, "missing state parameter", http.StatusBadRequest)
		return
	}
	if err := s.oauthBroker.Wait(r.Context(), state); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "authorized"})
}

// handleAuthManual accepts a pasted redirect URL (the --manual path for
// sessions where the browser cannot reach the daemon's callback).
// POST /api/servers/{name}/auth/manual.
func (s *Server) handleAuthManual(w http.ResponseWriter, r *http.Request) {
	if s.oauthBroker == nil {
		writeJSONError(w, "OAuth brokering is not enabled", http.StatusNotImplemented)
		return
	}
	var body struct {
		RedirectURL string `json:"redirectUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RedirectURL == "" {
		writeJSONError(w, "body must include redirectUrl", http.StatusBadRequest)
		return
	}
	if err := s.oauthBroker.CompleteManual(r.Context(), body.RedirectURL); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "authorized"})
}

// handleAuthLogout revokes (best effort) and deletes the grant backing a
// server. POST /api/servers/{name}/auth/logout.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if s.oauthBroker == nil {
		writeJSONError(w, "OAuth brokering is not enabled", http.StatusNotImplemented)
		return
	}
	name := r.PathValue("name")
	if err := s.oauthBroker.Logout(r.Context(), name); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "logged_out"})
}

// handleAuthReset deletes the grant and the cached client registration for
// a server. POST /api/servers/{name}/auth/reset.
func (s *Server) handleAuthReset(w http.ResponseWriter, r *http.Request) {
	if s.oauthBroker == nil {
		writeJSONError(w, "OAuth brokering is not enabled", http.StatusNotImplemented)
		return
	}
	name := r.PathValue("name")
	if err := s.oauthBroker.Reset(r.Context(), name); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "reset"})
}
