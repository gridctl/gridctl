package mcp

import (
	"sync"
	"testing"
	"time"
)

func TestNewSessionManager(t *testing.T) {
	m := NewSessionManager()
	if m == nil {
		t.Fatal("NewSessionManager returned nil")
	}
	if len(m.List()) != 0 {
		t.Errorf("new session manager should have no sessions, got %d", len(m.List()))
	}
}

func TestSessionManager_Create(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "test-client", Version: "1.0"}

	session := m.Create(clientInfo)

	if session == nil {
		t.Fatal("Create returned nil session")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}
	if session.ClientInfo.Name != "test-client" {
		t.Errorf("expected client name 'test-client', got '%s'", session.ClientInfo.Name)
	}
	if !session.Initialized {
		t.Error("session should be initialized")
	}
	if session.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if session.LastSeen.IsZero() {
		t.Error("LastSeen should be set")
	}
}

func TestSessionManager_Create_UniqueIDs(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		session := m.Create(clientInfo)
		if ids[session.ID] {
			t.Fatalf("duplicate session ID generated: %s", session.ID)
		}
		ids[session.ID] = true
	}
}

func TestSessionManager_Get(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "test-client", Version: "1.0"}

	created := m.Create(clientInfo)
	retrieved := m.Get(created.ID)

	if retrieved == nil {
		t.Fatal("Get returned nil for existing session")
	}
	if retrieved.ID != created.ID {
		t.Errorf("expected ID '%s', got '%s'", created.ID, retrieved.ID)
	}
}

func TestSessionManager_Get_NotFound(t *testing.T) {
	m := NewSessionManager()

	session := m.Get("nonexistent-id")
	if session != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestSessionManager_Touch(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "test-client", Version: "1.0"}

	session := m.Create(clientInfo)
	originalLastSeen := session.LastSeen

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	m.Touch(session.ID)

	updated := m.Get(session.ID)
	if !updated.LastSeen.After(originalLastSeen) {
		t.Error("LastSeen should be updated after Touch")
	}
}

func TestSessionManager_Touch_NonExistent(t *testing.T) {
	m := NewSessionManager()

	// Should not panic for nonexistent session
	m.Touch("nonexistent-id")
}

func TestSessionManager_Delete(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "test-client", Version: "1.0"}

	session := m.Create(clientInfo)
	m.Delete(session.ID)

	if m.Get(session.ID) != nil {
		t.Error("session should be nil after Delete")
	}
}

func TestSessionManager_List(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	m.Create(clientInfo)
	m.Create(clientInfo)
	m.Create(clientInfo)

	sessions := m.List()
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}
}

func TestSessionManager_Cleanup(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	// Create a session
	session := m.Create(clientInfo)

	// Manually set LastSeen to the past
	m.mu.Lock()
	m.sessions[session.ID].LastSeen = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()

	// Cleanup sessions older than 1 hour
	removed := m.Cleanup(1 * time.Hour)

	if removed != 1 {
		t.Errorf("expected 1 removed session, got %d", removed)
	}
	if m.Get(session.ID) != nil {
		t.Error("old session should be removed")
	}
}

func TestSessionManager_Cleanup_KeepsRecent(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	// Create a fresh session
	session := m.Create(clientInfo)

	// Cleanup sessions older than 1 hour (this session is recent)
	removed := m.Cleanup(1 * time.Hour)

	if removed != 0 {
		t.Errorf("expected 0 removed sessions, got %d", removed)
	}
	if m.Get(session.ID) == nil {
		t.Error("recent session should not be removed")
	}
}

func TestSessionManager_Count(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	if m.Count() != 0 {
		t.Errorf("expected 0, got %d", m.Count())
	}

	m.Create(clientInfo)
	m.Create(clientInfo)

	if m.Count() != 2 {
		t.Errorf("expected 2, got %d", m.Count())
	}
}

func TestSessionManager_EvictOldest(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	// Fill to capacity
	for i := 0; i < maxSessions; i++ {
		m.Create(clientInfo)
	}

	if m.Count() != maxSessions {
		t.Fatalf("expected %d sessions, got %d", maxSessions, m.Count())
	}

	// Set one session to be the oldest
	m.mu.Lock()
	var oldestID string
	for id := range m.sessions {
		oldestID = id
		break
	}
	m.sessions[oldestID].LastSeen = time.Now().Add(-1 * time.Hour)
	m.mu.Unlock()

	// Create one more - should evict the oldest
	m.Create(clientInfo)

	if m.Count() != maxSessions {
		t.Errorf("expected %d after eviction, got %d", maxSessions, m.Count())
	}
	if m.Get(oldestID) != nil {
		t.Error("oldest session should have been evicted")
	}
}

func TestSessionManager_EvictOldest_EvictsCorrectSession(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	// Fill to capacity
	for i := 0; i < maxSessions; i++ {
		m.Create(clientInfo)
	}

	// Make two sessions old, but one older than the other
	m.mu.Lock()
	ids := make([]string, 0, 2)
	for id := range m.sessions {
		ids = append(ids, id)
		if len(ids) == 2 {
			break
		}
	}
	m.sessions[ids[0]].LastSeen = time.Now().Add(-2 * time.Hour)
	m.sessions[ids[1]].LastSeen = time.Now().Add(-1 * time.Hour)
	m.mu.Unlock()

	// Create one more - should evict the *oldest* (ids[0])
	m.Create(clientInfo)

	if m.Get(ids[0]) != nil {
		t.Error("oldest session (2h old) should have been evicted")
	}
	if m.Get(ids[1]) == nil {
		t.Error("second oldest session (1h old) should still exist")
	}
}

func TestSessionManager_Concurrent(t *testing.T) {
	m := NewSessionManager()
	clientInfo := ClientInfo{Name: "client", Version: "1.0"}

	var wg sync.WaitGroup
	numGoroutines := 20

	// Concurrent creates
	var createdIDs sync.Map
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session := m.Create(clientInfo)
			createdIDs.Store(session.ID, true)
		}()
	}
	wg.Wait()

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.List()
		}()
	}
	wg.Wait()

	// Concurrent touch and get
	createdIDs.Range(func(key, value interface{}) bool {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			m.Touch(id)
			_ = m.Get(id)
		}(key.(string))
		return true
	})
	wg.Wait()

	// If we get here without deadlock or panic, test passes
	sessions := m.List()
	if len(sessions) != numGoroutines {
		t.Errorf("expected %d sessions, got %d", numGoroutines, len(sessions))
	}
}
