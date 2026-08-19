package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SessionData stores information about an authenticated user session.
type SessionData struct {
	Token     string    `json:"token"`
	UserID    uint      `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IsExpired returns true if the session has passed its expiration time.
func (s *SessionData) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// SessionManager provides in-memory session storage with configurable TTL.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*SessionData
	ttl      time.Duration
}

// NewSessionManager creates a SessionManager with the given TTL.
// If ttl is zero, it defaults to 24 hours.
func NewSessionManager(ttl time.Duration) *SessionManager {
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	sm := &SessionManager{
		sessions: make(map[string]*SessionData),
		ttl:      ttl,
	}
	// Start background cleanup goroutine
	go sm.cleanup()
	return sm
}

// CreateSession creates a new session for the given user and returns the session data.
func (sm *SessionManager) CreateSession(userID uint, email, name string, roles []string) (*SessionData, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	session := &SessionData{
		Token:     token,
		UserID:    userID,
		Email:     email,
		Name:      name,
		Roles:     roles,
		CreatedAt: now,
		ExpiresAt: now.Add(sm.ttl),
	}

	sm.mu.Lock()
	sm.sessions[token] = session
	sm.mu.Unlock()

	return session, nil
}

// GetSession retrieves a session by token. Returns nil if not found or expired.
func (sm *SessionManager) GetSession(token string) *SessionData {
	sm.mu.RLock()
	session, ok := sm.sessions[token]
	sm.mu.RUnlock()

	if !ok {
		return nil
	}
	if session.IsExpired() {
		// Lazy cleanup of expired session
		sm.DeleteSession(token)
		return nil
	}
	return session
}

// DeleteSession removes a session by token.
func (sm *SessionManager) DeleteSession(token string) {
	sm.mu.Lock()
	delete(sm.sessions, token)
	sm.mu.Unlock()
}

// InvalidateUserSessions removes all sessions for a given user ID.
// This supports the 60-second invalidation requirement when Authentik revokes a user.
func (sm *SessionManager) InvalidateUserSessions(userID uint) {
	sm.mu.Lock()
	for token, session := range sm.sessions {
		if session.UserID == userID {
			delete(sm.sessions, token)
		}
	}
	sm.mu.Unlock()
}

// ActiveSessionCount returns the number of non-expired sessions. Useful for testing.
func (sm *SessionManager) ActiveSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	count := 0
	now := time.Now()
	for _, session := range sm.sessions {
		if now.Before(session.ExpiresAt) {
			count++
		}
	}
	return count
}

// cleanup runs periodically to remove expired sessions.
func (sm *SessionManager) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		sm.mu.Lock()
		now := time.Now()
		for token, session := range sm.sessions {
			if now.After(session.ExpiresAt) {
				delete(sm.sessions, token)
			}
		}
		sm.mu.Unlock()
	}
}

// generateToken creates a cryptographically random session token.
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
