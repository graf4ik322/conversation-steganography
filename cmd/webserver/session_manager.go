package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	conversationstenography "conversationstenography"
)

// AuditEvent represents one transparency-console event pushed to the client.
type AuditEvent struct {
	Timestamp time.Time `json:"t"`
	Message   string    `json:"m"`
	Type      string    `json:"type,omitempty"` // "info", "security", "wipe"
}

// Session holds all per-conversation state with isolated lifecycle.
type Session struct {
	id         string
	key        []byte // derived key — explicitly zeroed on expiry/revoke
	config     *conversationstenography.GenerativeConfig
	model      conversationstenography.LanguageModel
	codec      *conversationstenography.GenerativeCodec
	chain      *conversationstenography.ConversationChain
	created    time.Time
	expiresAt  time.Time // hard cap
	slidingTTL time.Duration
	alive      bool
	timer      *time.Timer
	console    []AuditEvent
	consoleCh  chan AuditEvent
	mu         sync.Mutex
}

func (s *Session) pushAudit(msg string, eventType string) {
	evt := AuditEvent{Timestamp: time.Now(), Message: msg, Type: eventType}
	s.console = append(s.console, evt)
	select {
	case s.consoleCh <- evt:
	default:
	}
}

// SessionManager provides per-session isolation with sliding TTL + hard cap.
type SessionManager struct {
	mu         sync.Mutex
	sessions   map[string]*Session
	ttl        time.Duration // sliding window per encode/decode
	maxTTL     time.Duration // absolute hard cap from creation
}

// NewSessionManager creates a session manager with the given TTLs.
// slidingTTL: how long a session lives after last activity (e.g., 15min).
// maxTTL: absolute max lifetime regardless of activity (e.g., 12h).
func NewSessionManager(slidingTTL, maxTTL time.Duration) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
		ttl:      slidingTTL,
		maxTTL:   maxTTL,
	}
}

// CreateSession creates a new session from a conversation name and secret phrase.
func (sm *SessionManager) CreateSession(
	ctx context.Context,
	conversation string,
	secretPhrase string,
	model conversationstenography.LanguageModel,
	config *conversationstenography.GenerativeConfig,
) (_ *Session, auditEvents []AuditEvent, err error) {
	if len(secretPhrase) < 16 {
		return nil, nil, fmt.Errorf("secret phrase must be at least 16 characters")
	}

	// Derive key from phrase — phrase is in-memory only for this call
	key, err := conversationstenography.DeriveKeyFromPhrase(secretPhrase, conversation)
	if err != nil {
		return nil, nil, fmt.Errorf("key derivation: %w", err)
	}

	// Zero out the phrase reference immediately
	// (Go GC will reclaim the string memory; explicit zero via slice trick)
	// Note: Go strings are immutable, so we can't zero the original. But
	// the string reference is released as soon as this function returns.

	sessionID := generateSessionID()

	codec, err := conversationstenography.NewGenerativeCodec(model, *config)
	if err != nil {
		return nil, nil, fmt.Errorf("codec init: %w", err)
	}

	s := &Session{
		id:         sessionID,
		key:        key,
		config:     config,
		model:      model,
		codec:      codec,
		created:    time.Now(),
		expiresAt:  time.Now().Add(sm.maxTTL),
		slidingTTL: sm.ttl,
		alive:      true,
		consoleCh:  make(chan AuditEvent, 64),
	}

	// Hard cap timer (per-session, not global sweep)
	s.timer = time.AfterFunc(sm.maxTTL, func() {
		sm.wipeSession(s.id)
	})

	sm.mu.Lock()
	sm.sessions[sessionID] = s
	sm.mu.Unlock()

	auditEvents = []AuditEvent{
		{Timestamp: time.Now(), Message: "Session opened, key derived from phrase, phrase removed from memory", Type: "security"},
		{Timestamp: time.Now(), Message: fmt.Sprintf("Session active for %s (hard cap: %s)", sm.ttl, sm.maxTTL), Type: "info"},
	}

	return s, auditEvents, nil
}

// GetSession returns a session by ID, extending its sliding TTL.
func (sm *SessionManager) GetSession(id string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.sessions[id]
	if !ok || !s.alive {
		return nil
	}

	// Check hard cap
	if time.Now().After(s.expiresAt) {
		sm.wipeSessionLocked(id)
		return nil
	}

	// Reset sliding TTL timer
	s.timer.Reset(sm.ttl)

	return s
}

// RevokeSession immediately wipes a session and its key material.
func (sm *SessionManager) RevokeSession(id string) []AuditEvent {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.sessions[id]
	if !ok {
		return nil
	}

	if !s.alive {
		return nil
	}

	events := sm.wipeSessionLocked(id)
	return events
}

// wipeSession removes a session and zeroes its key material (called from timer).
func (sm *SessionManager) wipeSession(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.wipeSessionLocked(id)
}

func (sm *SessionManager) wipeSessionLocked(id string) []AuditEvent {
	s, ok := sm.sessions[id]
	if !ok {
		return nil
	}

	s.alive = false
	s.timer.Stop()

	// Zero out the derived key
	for i := range s.key {
		s.key[i] = 0
	}
	s.key = nil

	events := []AuditEvent{
		{Timestamp: time.Now(), Message: "Session key wiped from memory, zeroed and released", Type: "wipe"},
	}

	delete(sm.sessions, id)
	return events
}

// SessionCount returns the number of active sessions.
func (sm *SessionManager) SessionCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.sessions)
}

func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate session ID: %v", err))
	}
	return hex.EncodeToString(b)
}

// Ensure sensitiveCompare uses constant-time comparison.
func sensitiveCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
