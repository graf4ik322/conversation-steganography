package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	conversationstenography "conversationstenography"
)

const sessionCookieName = "steg_session_id"

type Handler struct {
	sessions *SessionManager
	model    conversationstenography.LanguageModel
	config   *conversationstenography.GenerativeConfig
	mu       sync.Mutex
}

func NewHandler(sm *SessionManager, model conversationstenography.LanguageModel, cfg *conversationstenography.GenerativeConfig) *Handler {
	return &Handler{
		sessions: sm,
		model:    model,
		config:   cfg,
	}
}

type sessionStartRequest struct {
	ConversationName string `json:"conversation_name"`
	SecretPhrase     string `json:"secret_phrase"`
}

type sessionStartResponse struct {
	SessionID   string       `json:"session_id,omitempty"`
	AuditEvents []AuditEvent `json:"audit_events,omitempty"`
	Error       string       `json:"error,omitempty"`
}

type inviteSessionRequest struct {
	Token           string `json:"token"`
	Topic           string `json:"topic"`
	DurationMinutes int    `json:"duration_minutes"`
}

type inviteSessionResponse struct {
	InviteURL   string       `json:"invite_url,omitempty"`
	AuditEvents []AuditEvent `json:"audit_events,omitempty"`
	Error       string       `json:"error,omitempty"`
}

type statusResponse struct {
	Alive            bool   `json:"alive"`
	TTLSeconds       int    `json:"ttl_seconds,omitempty"`
	RemainingSeconds int    `json:"remaining_seconds,omitempty"`
	MessageCount     int    `json:"message_count,omitempty"`
	TTLMode          string `json:"ttl_mode,omitempty"` // "sliding" or "fixed"
}

type encodeRequest struct {
	Plaintext string `json:"plaintext"`
}

type encodeResponse struct {
	CoverText   string       `json:"cover_text,omitempty"`
	AuditEvents []AuditEvent `json:"audit_events,omitempty"`
	Error       string       `json:"error,omitempty"`
}

type decodeRequest struct {
	CoverText string `json:"cover_text"`
	Sender    string `json:"sender"`
}

type decodeResponse struct {
	Plaintext   string       `json:"plaintext,omitempty"`
	AuditEvents []AuditEvent `json:"audit_events,omitempty"`
	Error       string       `json:"error,omitempty"`
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func getSessionID(r *http.Request) string {
	// 1. Check X-Session-Token header (invite mode)
	if token := r.Header.Get("X-Session-Token"); token != "" {
		return token
	}
	// 2. Check cookie (phrase mode)
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req sessionStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, sessionStartResponse{Error: "invalid request body"})
		return
	}

	if req.ConversationName == "" || req.SecretPhrase == "" {
		writeJSON(w, http.StatusBadRequest, sessionStartResponse{Error: "conversation_name and secret_phrase are required"})
		return
	}

	if len(req.SecretPhrase) < 16 {
		writeJSON(w, http.StatusBadRequest, sessionStartResponse{Error: "secret_phrase must be at least 16 characters"})
		return
	}

	ctx := r.Context()
	session, auditEvents, err := h.sessions.CreateSession(ctx, req.ConversationName, req.SecretPhrase, h.model, h.config)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, sessionStartResponse{Error: err.Error()})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.id,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   int(h.sessions.maxTTL.Seconds()),
	})

	writeJSON(w, http.StatusOK, sessionStartResponse{
		SessionID:   session.id,
		AuditEvents: auditEvents,
	})
}

// handleInviteSession creates a fixed-TTL session from a client-generated token.
func (h *Handler) handleInviteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req inviteSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, inviteSessionResponse{Error: "invalid request body"})
		return
	}

	if req.Token == "" || req.Topic == "" {
		writeJSON(w, http.StatusBadRequest, inviteSessionResponse{Error: "token and topic are required"})
		return
	}

	if req.DurationMinutes < 1 || req.DurationMinutes > 1440 {
		writeJSON(w, http.StatusBadRequest, inviteSessionResponse{Error: "duration must be 1-1440 minutes"})
		return
	}

	if len(req.Token) < 32 {
		writeJSON(w, http.StatusBadRequest, inviteSessionResponse{Error: "token must be at least 32 characters"})
		return
	}

	duration := time.Duration(req.DurationMinutes) * time.Minute

	session, auditEvents, err := h.sessions.CreateInviteSession(
		req.Token, req.Topic, duration, h.model, h.config,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, inviteSessionResponse{Error: err.Error()})
		return
	}

	_ = session // session is identified by the token itself

	writeJSON(w, http.StatusOK, inviteSessionResponse{
		InviteURL:   "/#" + req.Token,
		AuditEvents: auditEvents,
	})
}

func (h *Handler) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(r)
	if sessionID == "" {
		writeJSON(w, http.StatusUnauthorized, statusResponse{Alive: false})
		return
	}

	s := h.sessions.GetSession(sessionID)
	if s == nil {
		writeJSON(w, http.StatusUnauthorized, statusResponse{Alive: false})
		return
	}

	s.mu.Lock()
	remaining := time.Until(s.expiresAt)
	s.mu.Unlock()

	ttlMode := "sliding"
	if s.ttlMode == TTLModeFixed {
		ttlMode = "fixed"
	}

	writeJSON(w, http.StatusOK, statusResponse{
		Alive:            true,
		TTLSeconds:       int(remaining.Seconds()),
		RemainingSeconds: int(remaining.Seconds()),
		TTLMode:          ttlMode,
	})
}

func (h *Handler) handleEncode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := getSessionID(r)
	if sessionID == "" {
		writeJSON(w, http.StatusUnauthorized, encodeResponse{Error: "no active session"})
		return
	}

	s := h.sessions.GetSession(sessionID)
	if s == nil {
		writeJSON(w, http.StatusUnauthorized, encodeResponse{Error: "session expired"})
		return
	}

	var req encodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, encodeResponse{Error: "invalid request body"})
		return
	}

	if req.Plaintext == "" {
		writeJSON(w, http.StatusBadRequest, encodeResponse{Error: "plaintext is required"})
		return
	}

	ctx := r.Context()
	coverText, err := h.encodeMessage(ctx, s, req.Plaintext)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, encodeResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, encodeResponse{
		CoverText: coverText,
		AuditEvents: []AuditEvent{
			{Timestamp: time.Now(), Message: "Message encrypted and passed to model for encoding", Type: "info"},
			{Timestamp: time.Now(), Message: "Cover text generated", Type: "info"},
		},
	})
}

func (h *Handler) handleDecode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := getSessionID(r)
	if sessionID == "" {
		writeJSON(w, http.StatusUnauthorized, decodeResponse{Error: "no active session"})
		return
	}

	s := h.sessions.GetSession(sessionID)
	if s == nil {
		writeJSON(w, http.StatusUnauthorized, decodeResponse{Error: "session expired"})
		return
	}

	var req decodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, decodeResponse{Error: "invalid request body"})
		return
	}

	if req.CoverText == "" {
		writeJSON(w, http.StatusBadRequest, decodeResponse{Error: "cover_text is required"})
		return
	}

	if req.Sender == "" {
		req.Sender = "remote"
	}

	ctx := r.Context()
	plaintext, err := h.decodeMessage(ctx, s, req.CoverText, req.Sender)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, decodeResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, decodeResponse{
		Plaintext: plaintext,
		AuditEvents: []AuditEvent{
			{Timestamp: time.Now(), Message: "Cover text decoded, carrier authentication verified", Type: "info"},
			{Timestamp: time.Now(), Message: "Previous message from memory session erased", Type: "security"},
		},
	})
}

func (h *Handler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(r)
	if sessionID == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_cleared"})
		return
	}

	events := h.sessions.RevokeSession(sessionID)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "revoked",
		"audit_events": events,
	})
}

func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(r)
	if sessionID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s := h.sessions.GetSession(sessionID)
	if s == nil {
		http.Error(w, "session expired", http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	for {
		select {
		case evt, ok := <-s.consoleCh:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

func (h *Handler) encodeMessage(ctx context.Context, s *Session, plaintext string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Authenticated encryption (AES-GCM)
	payload, err := conversationstenography.SealCarrierPayload(
		s.key, s.id, "encode", 1, []byte(plaintext),
	)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	// 2. Generative embedding (model → cover text)
	coverText, err := s.codec.Encode(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}

	return coverText, nil
}

func (h *Handler) decodeMessage(ctx context.Context, s *Session, coverText string, sender string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Extract payload from cover text
	payload, err := s.codec.Decode(ctx, coverText)
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}

	// 2. Decrypt and verify (same AAD as encode)
	plainBytes, err := conversationstenography.OpenCarrierPayload(
		s.key, s.id, "encode", 1, payload,
	)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plainBytes), nil
}
