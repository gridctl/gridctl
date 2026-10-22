package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// maxSessions is the maximum number of concurrent sessions before eviction.
const maxSessions = 1000

// Session represents an MCP client session.
//
// Sessions are a handshake-era (2025-11-25 and earlier) concept: the
// 2026-07-28 stateless generation removed them, deriving identity per
// request from _meta instead (see handleStateless). This struct keeps
// serving handshake peers for as long as that generation is supported
// (two-generation policy, stable plus next); it is deprecated in the
// sense that no new fields or semantics should be added for
// stateless-era needs, not that it is going away soon.
type Session struct {
	ID         string
	ClientInfo ClientInfo
	// ClientID is the normalized form of ClientInfo.Name (see NormalizeClientID).
	// It is the stable attribution dimension threaded through tool-call observers
	// so cost and token aggregates can be grouped per originating client.
	ClientID string
	// AccessID is the stable identifier used to scope this client's tool access
	// (the key under stack.yaml `clients:`). It is the explicit link-time
	// identifier when the client declares one (the `client` query parameter or
	// X-Gridctl-Client-Id header), otherwise it falls back to ClientID. Both are
	// normalized so configuration, the wire, and the UI reconcile on one form.
	// In OAuth mode AccessID is the raw principal and is not normalized.
	AccessID string
	// Principal is the verified OAuth identity claim that created this session.
	// Empty outside OAuth mode. It is the source of truth for AccessID and is
	// compared on every later request. Raw subjects are case-sensitive; map
	// them with clients.profiles aliases.
	Principal string
	// Group is the tool group this session is bound to, set when the client
	// connected through a /groups/{name}/mcp endpoint. Empty for the default
	// full-surface /mcp endpoint. Frozen at initialize, like AccessID.
	Group string
	// Discovery is the per-session discovery override ("deferred" or "full").
	// Empty means the stack default. Frozen at initialize; later requests on
	// this session ignore the query parameter.
	Discovery string
	// ProtocolVersion is the MCP protocol version negotiated at initialize
	// (echo of the client's requested version when supported, otherwise the
	// latest supported version).
	ProtocolVersion string
	Initialized     bool
	SkillsExtension bool
	CreatedAt       time.Time
	LastSeen        time.Time

	// toolFingerprint is the last recorded hash of this session's visible
	// tool list. Empty means none has been recorded. Handshake delivery
	// state only; the stateless generation has no sessions.
	toolFingerprint string
}

// SessionManager manages client sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func (m *SessionManager) setSkillsExtension(id string, declared bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[id]; session != nil {
		session.SkillsExtension = declared
	}
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session. If the session count exceeds maxSessions,
// the oldest session (by LastSeen) is evicted.
//
// accessID is the explicit, link-time-assigned client identifier resolved from
// the connection (query parameter or header); pass "" when the client declared
// none, in which case the normalized clientInfo.name becomes the access id.
// Both forms are normalized so enforcement, config, and UI reconcile.
//
// group is the tool group of the endpoint the client connected through;
// pass "" for the default /mcp endpoint.
//
// protocolVersion is the MCP protocol version negotiated for this session.
func (m *SessionManager) Create(clientInfo ClientInfo, accessID, group, protocolVersion, discovery string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.sessions) >= maxSessions {
		m.evictOldest()
	}

	session := m.create(clientInfo, accessID, "", group, protocolVersion, discovery)
	m.sessions[session.ID] = session
	return session
}

// create builds a session. principal, when set, is stored verbatim as AccessID
// and is not passed through NormalizeClientID. The declared accessID is then
// only the display label.
func (m *SessionManager) create(clientInfo ClientInfo, accessID, principal, group, protocolVersion, discovery string) *Session {
	normalized := NormalizeClientID(clientInfo.Name)
	resolvedAccess := NormalizeClientID(accessID)
	clientID := normalized
	if principal != "" {
		resolvedAccess = principal
		if declared := NormalizeClientID(accessID); declared != "" {
			clientID = declared
		}
	} else if resolvedAccess == "" {
		resolvedAccess = normalized
	}

	id := generateSessionID()
	return &Session{
		ID:              id,
		ClientInfo:      clientInfo,
		ClientID:        clientID,
		AccessID:        resolvedAccess,
		Principal:       principal,
		Group:           group,
		Discovery:       strings.TrimSpace(discovery),
		ProtocolVersion: protocolVersion,
		Initialized:     true,
		CreatedAt:       time.Now(),
		LastSeen:        time.Now(),
	}
}

// CreatePrincipal is Create for a verified OAuth principal. AccessID is the
// raw principal. declared is the display label (query, header, or empty).
func (m *SessionManager) CreatePrincipal(clientInfo ClientInfo, declared, principal, group, protocolVersion, discovery string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) >= maxSessions {
		m.evictOldest()
	}
	session := m.create(clientInfo, declared, principal, group, protocolVersion, discovery)
	m.sessions[session.ID] = session
	return session
}

// evictOldest removes the session with the oldest LastSeen time.
// Must be called with m.mu held.
func (m *SessionManager) evictOldest() {
	var oldestID string
	var oldestTime time.Time
	for id, s := range m.sessions {
		if oldestID == "" || s.LastSeen.Before(oldestTime) {
			oldestID = id
			oldestTime = s.LastSeen
		}
	}
	if oldestID != "" {
		delete(m.sessions, oldestID)
	}
}

// Get retrieves a session by ID.
func (m *SessionManager) Get(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// Touch updates the last seen time for a session.
func (m *SessionManager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.LastSeen = time.Now()
	}
}

// SetToolFingerprint records the visible-list hash for a session.
// A missing session is ignored.
func (m *SessionManager) SetToolFingerprint(id, fp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.toolFingerprint = fp
	}
}

// SwapToolFingerprint stores fp and returns the previous value.
// ok is false when the session has been deleted or evicted.
func (m *SessionManager) SwapToolFingerprint(id, fp string) (previous string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, found := m.sessions[id]
	if !found {
		return "", false
	}
	previous = s.toolFingerprint
	s.toolFingerprint = fp
	return previous, true
}

// Delete removes a session.
func (m *SessionManager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// List returns all sessions.
func (m *SessionManager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// Count returns the number of active sessions.
func (m *SessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Cleanup removes stale sessions older than the given duration.
func (m *SessionManager) Cleanup(maxAge time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for id, s := range m.sessions {
		if s.LastSeen.Before(cutoff) {
			delete(m.sessions, id)
			removed++
		}
	}
	return removed
}

func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read always returns nil error on supported platforms
	return hex.EncodeToString(b)
}
