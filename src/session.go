package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName = "mls_session"
	sessionTTL        = 24 * time.Hour
)

type session struct {
	ID        string
	CreatedAt time.Time
}

var (
	sessions     = make(map[string]*session)
	sessionMutex sync.RWMutex
)

// generateSessionID creates a cryptographically random 32-byte hex session ID.
func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// createSession stores a new session and returns its ID.
func createSession() (string, error) {
	id, err := generateSessionID()
	if err != nil {
		return "", err
	}
	sessionMutex.Lock()
	sessions[id] = &session{ID: id, CreatedAt: time.Now()}
	sessionMutex.Unlock()
	return id, nil
}

// validateSession checks if a session ID exists and is not expired.
func validateSession(id string) bool {
	sessionMutex.RLock()
	s, ok := sessions[id]
	sessionMutex.RUnlock()
	if !ok {
		return false
	}
	if time.Since(s.CreatedAt) > sessionTTL {
		destroySession(id)
		return false
	}
	return true
}

// destroySession removes a session.
func destroySession(id string) {
	sessionMutex.Lock()
	delete(sessions, id)
	sessionMutex.Unlock()
}

// cleanExpiredSessions periodically removes stale sessions.
func cleanExpiredSessions() {
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			sessionMutex.Lock()
			for id, s := range sessions {
				if time.Since(s.CreatedAt) > sessionTTL {
					delete(sessions, id)
				}
			}
			sessionMutex.Unlock()
		}
	}()
}

// getSessionFromRequest extracts and validates the session cookie.
func getSessionFromRequest(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	return validateSession(cookie.Value)
}

// setSessionCookie writes the session cookie on the response.
func setSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// clearSessionCookie removes the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// loginHandler handles POST /api/admin/login with JSON body {"token":"..."}.
// @Summary      Login with token
// @Description  Authenticate with a valid token and create an admin session
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Success      200   {object}  SessionResponse
// @Failure      400   {object}  ErrorResponse
// @Failure      401   {object}  ErrorResponse
// @Security     TokenAuth
// @Router       /api/admin/login [post]
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		log.Printf("Login denied: bad request remote=%s", r.RemoteAddr)
		http.Error(w, `{"error":"token is required"}`, http.StatusBadRequest)
		return
	}

	if !validToken(body.Token) {
		log.Printf("Login denied: invalid token remote=%s", r.RemoteAddr)
		http.Error(w, `{"error":"invalid token"}`, http.StatusForbidden)
		return
	}

	sessionID, err := createSession()
	if err != nil {
		log.Printf("Login error: session creation failed remote=%s err=%v", r.RemoteAddr, err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, sessionID)
	log.Printf("Login successful remote=%s", r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// logoutHandler handles POST /api/admin/logout.
// @Summary      Logout
// @Description  Logout and invalidate the current admin session
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Success      200   {object}  StatusResponse
// @Failure      401   {object}  ErrorResponse
// @Security     SessionAuth
// @Router       /api/admin/logout [post]
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		destroySession(cookie.Value)
	}
	clearSessionCookie(w)
	log.Printf("Logout remote=%s", r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// sessionCheckHandler handles GET /api/admin/session to check if logged in.
// @Summary      Check session status
// @Description  Get information about the current admin session
// @Tags         Admin
// @Produce      json
// @Success      200   {object}  SessionCheckResponse
// @Failure      401   {object}  ErrorResponse
// @Security     SessionAuth
// @Router       /api/admin/session [get]
func sessionCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if getSessionFromRequest(r) {
		json.NewEncoder(w).Encode(map[string]interface{}{"authenticated": true})
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{"authenticated": false})
	}
}
