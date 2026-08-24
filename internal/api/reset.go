package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/modelsync"
	"github.com/gridctl/gridctl/pkg/resetops"
	"github.com/gridctl/gridctl/pkg/state"
)

// resetTokenTTL bounds how long a preview-issued confirm token stays
// valid. Long enough to read the preview, short enough that a leaked
// token is stale by the time it travels anywhere.
const resetTokenTTL = 5 * time.Minute

// resetTokens is the in-memory single-use confirm-token store. Tokens
// force the preview-then-execute shape at the API layer: a blind
// scripted POST /api/reset is a no-op, and a cross-origin page cannot
// read the preview response to obtain one.
type resetTokens struct {
	mu     sync.Mutex
	tokens map[string]resetToken
}

type resetToken struct {
	purge   bool
	force   bool
	expires time.Time
}

func (r *resetTokens) issue(purge, force bool) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tokens == nil {
		r.tokens = map[string]resetToken{}
	}
	// Opportunistic expiry sweep keeps the map from growing.
	now := time.Now()
	for k, v := range r.tokens {
		if now.After(v.expires) {
			delete(r.tokens, k)
		}
	}
	r.tokens[tok] = resetToken{purge: purge, force: force, expires: now.Add(resetTokenTTL)}
	return tok, nil
}

// consume validates and burns a token (single use). The token binds
// BOTH tier flags: a preview that showed hand-edits as kept must not
// authorize a force execution that deletes them.
func (r *resetTokens) consume(tok string, purge, force bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tokens[tok]
	if !ok {
		return false
	}
	delete(r.tokens, tok)
	return time.Now().Before(t.expires) && t.purge == purge && t.force == force
}

// resetMgr lazily builds the reset engine from the same lazily built
// kind managers the other handlers use. Tests inject via SetResetManagers.
func (s *Server) resetMgr() (*resetops.Managers, error) {
	s.resetOnce.Do(func() {
		if s.resetManagers != nil {
			return
		}
		home, err := state.Home()
		if err != nil {
			s.resetErr = err
			return
		}
		m := &resetops.Managers{Home: home}
		if sm, err := s.skillsyncManager(); err == nil {
			m.Skills = sm
		} else {
			m.Missing = append(m.Missing, "skill")
		}
		if am, err := s.agentsMgr(); err == nil {
			m.Agents = am
		} else {
			m.Missing = append(m.Missing, "agent")
		}
		if cm, err := s.contextsMgr(); err == nil {
			m.Contexts = cm
		} else {
			m.Missing = append(m.Missing, "context")
		}
		if mm, err := modelsync.NewManager(); err == nil {
			m.Models = mm
		} else {
			m.Missing = append(m.Missing, "models")
		}
		if wm, err := s.wiringMgr(); err == nil {
			m.Wiring = wm
		} else {
			m.Missing = append(m.Missing, "wiring")
		}
		if s.resetRuntime != nil {
			m.Runtime = s.resetRuntime
		}
		s.resetManagers = m
	})
	return s.resetManagers, s.resetErr
}

// SetResetManagers injects the reset engine. Tests use it to keep the
// reset handlers away from the real home directory.
func (s *Server) SetResetManagers(m *resetops.Managers) {
	s.resetOnce.Do(func() {})
	s.resetManagers = m
}

// SetResetRuntime injects the container-teardown runtime for reset.
func (s *Server) SetResetRuntime(rt resetops.Runtime) {
	s.resetRuntime = rt
}

// guardResetRequest enforces the transport-level guards shared by both
// reset endpoints. Loopback-only for BOTH tiers: the default tier
// deletes every gridctl-created file in the user's client directories,
// which is not meaningfully milder over the network, and the
// --insecure-allow-unauthenticated escape hatch makes an exposed
// unauthenticated API a reachable configuration. The Origin check
// rejects cross-site fetches (the default CORS policy echoes the
// request origin); the embedded UI is same-origin and sends none or a
// matching one. Content-Type is strictly enforced so the request can
// never be a CORS simple request.
func (s *Server) guardResetRequest(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		writeJSONError(w, "reset is only accepted from loopback connections", http.StatusForbidden)
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			writeJSONError(w, "reset rejects cross-origin requests", http.StatusForbidden)
			return false
		}
	}
	ct := r.Header.Get("Content-Type")
	if ct != "application/json" && !strings.HasPrefix(ct, "application/json;") {
		writeJSONError(w, "reset requires Content-Type: application/json", http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

type resetPreviewRequest struct {
	Purge bool `json:"purge"`
	Force bool `json:"force"`
}

type resetExecuteRequest struct {
	Purge         bool   `json:"purge"`
	Force         bool   `json:"force"`
	ConfirmToken  string `json:"confirm_token"`
	ConfirmPhrase string `json:"confirm_phrase"`
}

// handleResetPreview computes the reset inventory without writing
// anything and issues the single-use confirm token the execute call
// must present.
// POST /api/reset/preview
func (s *Server) handleResetPreview(w http.ResponseWriter, r *http.Request) {
	if !s.guardResetRequest(w, r) {
		return
	}
	var req resetPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	mgr, err := s.resetMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	doc, err := mgr.Preview(r.Context(), resetops.Options{Purge: req.Purge, Force: req.Force})
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	token, err := s.resetTokenStore.issue(req.Purge, req.Force)
	if err != nil {
		writeJSONError(w, "issuing confirm token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"confirm_token":  token,
		"confirm_phrase": mgr.GridctlDir(), // what purge must type, resolved
		"doc":            doc,
	})
}

// handleResetExecute runs the reset. Requires a live preview token;
// purge additionally requires the resolved-path confirm phrase, so the
// UI gate is enforced rather than decorative.
//
// Self-termination (FR12a): this handler runs inside a process the
// reset kills. Execute defers our own daemon's stop, our state file,
// and the purge RemoveAll behind doc.Finalize; the full result document
// is written and flushed first, then a goroutine finalizes and exits.
// POST /api/reset
func (s *Server) handleResetExecute(w http.ResponseWriter, r *http.Request) {
	if !s.guardResetRequest(w, r) {
		return
	}
	var req resetExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	mgr, err := s.resetMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	// Cheap request checks run BEFORE the token is consumed: the phrase
	// is printed by the preview (it is a deliberate-attention gate, not
	// a secret), so burning the single-use token on a typo or on a 409
	// only forces a pointless re-preview.
	if req.Purge && req.ConfirmPhrase != mgr.GridctlDir() {
		writeJSONError(w, "purge requires confirm_phrase to equal "+mgr.GridctlDir(), http.StatusUnprocessableEntity)
		return
	}

	if !s.resetRunning.CompareAndSwap(false, true) {
		writeJSONError(w, "a reset is already running", http.StatusConflict)
		return
	}
	// Released on every path EXCEPT when a finalize goroutine is about
	// to exit the process: releasing there would let a second reset
	// start and be killed mid-cascade by the pending exit.
	release := true
	defer func() {
		if release {
			s.resetRunning.Store(false)
		}
	}()

	if !s.resetTokenStore.consume(req.ConfirmToken, req.Purge, req.Force) {
		writeJSONError(w, "missing, expired, or already-used confirm token; call POST /api/reset/preview first", http.StatusUnprocessableEntity)
		return
	}

	doc, err := mgr.Execute(r.Context(), resetops.Options{
		Purge:   req.Purge,
		Force:   req.Force,
		SelfPID: os.Getpid(),
	}, nil)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	finalize := doc.Finalize
	doc.Finalize = nil
	writeJSON(w, doc)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	if finalize != nil {
		release = false // held until the process exits
		exit := s.resetExit
		if exit == nil {
			exit = os.Exit
		}
		go func() {
			// Give the response bytes a moment to leave the socket, then
			// complete our own teardown and exit: the daemon reset just
			// dismantled has nothing left to serve.
			time.Sleep(500 * time.Millisecond)
			if err := finalize(); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Error("reset finalize failed", "error", err)
				exit(1)
			}
			slog.Info("reset complete; daemon exiting")
			exit(0)
		}()
	}
}

// SetResetExit overrides the process-exit function the purge finalizer
// calls. Tests use it to observe self-termination without dying.
func (s *Server) SetResetExit(exit func(int)) {
	s.resetExit = exit
}
