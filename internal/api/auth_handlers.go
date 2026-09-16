package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/darsheee/remgo/internal/auth"
	"github.com/darsheee/remgo/internal/db"
)

// handleAuthStatus returns whether auth is enabled and the current session state.
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	hasUsers, _ := s.db.HasUsers()
	user := s.getUser(r)
	authenticated := user != nil && user.ID != db.DefaultUserID

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"auth_enabled":  s.authEnabled,
		"has_users":     hasUsers,
		"authenticated": authenticated,
		"user":          user,
	})
}

// handleAuthSetup registers the first admin user when no users exist.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	hasUsers, _ := s.db.HasUsers()
	if hasUsers {
		writeError(w, http.StatusForbidden, "setup is already completed; please log in")
		return
	}

	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.db.CreateUser(body.Username, body.Email, body.Password, "admin")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Auto-login on setup
	rawToken, _, err := s.db.CreateSession(user.ID, r.UserAgent(), r.RemoteAddr, 30*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	jwtToken, _ := auth.CreateJWT(s.jwtSecret, auth.JWTClaims{
		UserID:    user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      user.Role,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix(),
	})

	s.setSessionCookie(w, rawToken, 30*86400)

	tokenToReturn := rawToken
	if jwtToken != "" {
		tokenToReturn = jwtToken
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user":    user,
		"token":   tokenToReturn,
		"message": "Admin user created successfully",
	})
}

// handleAuthRegister registers a new user.
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.db.CreateUser(body.Username, body.Email, body.Password, "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Auto-login on registration
	rawToken, _, err := s.db.CreateSession(user.ID, r.UserAgent(), r.RemoteAddr, 30*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	jwtToken, _ := auth.CreateJWT(s.jwtSecret, auth.JWTClaims{
		UserID:    user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      user.Role,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix(),
	})

	s.setSessionCookie(w, rawToken, 30*86400)

	tokenToReturn := rawToken
	if jwtToken != "" {
		tokenToReturn = jwtToken
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user":    user,
		"token":   tokenToReturn,
		"message": "Account created successfully",
	})
}

// handleAuthLogin authenticates a user and returns a token + sets cookie.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.db.AuthenticateUser(body.Username, body.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username/email or password")
		return
	}

	rawToken, _, err := s.db.CreateSession(user.ID, r.UserAgent(), r.RemoteAddr, 30*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	jwtToken, _ := auth.CreateJWT(s.jwtSecret, auth.JWTClaims{
		UserID:    user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      user.Role,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Unix(),
	})

	s.setSessionCookie(w, rawToken, 30*86400)

	tokenToReturn := rawToken
	if jwtToken != "" {
		tokenToReturn = jwtToken
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":    user,
		"token":   tokenToReturn,
		"message": "Login successful",
	})
}

// handleAuthLogout terminates the active session and clears the cookie.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	token := s.extractRawToken(r)
	if token != "" {
		_ = s.db.DeleteSession(token)
	}

	s.setSessionCookie(w, "", -1)

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}

// handleAuthMe returns the current authenticated user profile.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user": user,
	})
}

// handleListAPIKeys returns Personal Access Tokens for the current user.
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keys, err := s.db.ListAPIKeys(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// handleCreateAPIKey creates a new Personal Access Token (remgo_pat_...).
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	rawKey, apiKey, err := s.db.CreateAPIKey(user.ID, body.Name, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"key":     rawKey,
		"api_key": apiKey,
	})
}

// handleDeleteAPIKey revokes a Personal Access Token.
func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keyID := r.PathValue("id")
	if err := s.db.DeleteAPIKey(user.ID, keyID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     "remgo_token",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}
