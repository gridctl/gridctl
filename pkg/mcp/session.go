package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// maxSessions is the maximum number of concurrent sessions before eviction.
const maxSessions = 1000

// Session represents an MCP client session.
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
	AccessID string
	// Group is the tool group this session is bound to, set when the client
	// connected through a /groups/{name}/mcp endpoint. Empty for the default
	// full-surface /mcp endpoint. Frozen at initialize, like AccessID.
	Group string
	// ProtocolVersion is the MCP protocol version negotiated at initialize
	// (echo of the client's requested version when supported, otherwise the
	// latest supported version).
	ProtocolVersion string
	Initialized     bool
	CreatedAt       time.Time
	LastSeen        time.Time
}

// SessionManager manages client sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
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
func (m *SessionManager) Create(clientInfo ClientInfo, accessID, group, protocolVersion string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.sessions) >= maxSessions {
		m.evictOldest()
	}

	normalized := NormalizeClientID(clientInfo.Name)
	resolvedAccess := NormalizeClientID(accessID)
	if resolvedAccess == "" {
		resolvedAccess = normalized
	}

	id := generateSessionID()
	session := &Session{
		ID:              id,
		ClientInfo:      clientInfo,
		ClientID:        normalized,
		AccessID:        resolvedAccess,
		Group:           group,
		ProtocolVersion: protocolVersion,
		Initialized:     true,
		CreatedAt:       time.Now(),
		LastSeen:        time.Now(),
	}
	m.sessions[id] = session
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
