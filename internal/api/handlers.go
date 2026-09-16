package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/darsheee/remgo/internal/db"
	"github.com/darsheee/remgo/internal/mcp"
	"github.com/darsheee/remgo/internal/srs"
)

// Server handles all REST API, MCP HTTP, and authentication endpoints.
type Server struct {
	db            *db.DB
	mcpServer     *mcp.Server
	mux           *http.ServeMux
	authEnabled   bool
	defaultUserID string
	jwtSecret     []byte
}

// NewServer initializes the HTTP API server.
func NewServer(database *db.DB, staticHandler http.Handler, authEnabled ...bool) *Server {
	enabled := false
	if len(authEnabled) > 0 {
		enabled = authEnabled[0]
	}

	secret := []byte(os.Getenv("REMGO_JWT_SECRET"))
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}

	s := &Server{
		db:            database,
		mcpServer:     mcp.NewServer(database),
		mux:           http.NewServeMux(),
		authEnabled:   enabled,
		defaultUserID: db.DefaultUserID,
		jwtSecret:     secret,
	}

	s.registerRoutes(staticHandler)
	return s
}

// SetDefaultUserID configures the user ID to use for single-user (unauthenticated) requests.
func (s *Server) SetDefaultUserID(userID string) {
	if userID != "" {
		s.defaultUserID = userID
	}
}

// GetJWTSecret returns the active JWT secret.
func (s *Server) GetJWTSecret() []byte {
	return s.jwtSecret
}

// IsAuthEnabled returns true if authentication is required.
func (s *Server) IsAuthEnabled() bool {
	return s.authEnabled
}

// Handler returns the top-level http.Handler with CORS and auth enforcement.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Auth Gatekeeper for protected API routes when auth is enabled
		if s.authEnabled {
			path := r.URL.Path
			isAuthEndpoint := strings.HasPrefix(path, "/api/auth/status") ||
				strings.HasPrefix(path, "/api/auth/login") ||
				strings.HasPrefix(path, "/api/auth/register") ||
				strings.HasPrefix(path, "/api/auth/setup")

			isProtectedAPI := (strings.HasPrefix(path, "/api/") && !isAuthEndpoint) || strings.HasPrefix(path, "/mcp")

			if isProtectedAPI {
				user := s.getUser(r)
				if user == nil || user.ID == db.DefaultUserID {
					writeError(w, http.StatusUnauthorized, "unauthorized: authentication required")
					return
				}
			}
		}

		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) registerRoutes(staticHandler http.Handler) {
	// Authentication endpoints
	s.mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleAuthSetup)
	s.mux.HandleFunc("POST /api/auth/register", s.handleAuthRegister)
	s.mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	s.mux.HandleFunc("GET /api/auth/me", s.handleAuthMe)
	s.mux.HandleFunc("GET /api/auth/keys", s.handleListAPIKeys)
	s.mux.HandleFunc("POST /api/auth/keys", s.handleCreateAPIKey)
	s.mux.HandleFunc("DELETE /api/auth/keys/{id}", s.handleDeleteAPIKey)

	// Rems endpoints
	s.mux.HandleFunc("GET /api/rems", s.handleListRems)
	s.mux.HandleFunc("POST /api/rems", s.handleCreateRem)
	s.mux.HandleFunc("GET /api/rems/{id}", s.handleGetRem)
	s.mux.HandleFunc("PUT /api/rems/{id}", s.handleUpdateRem)
	s.mux.HandleFunc("DELETE /api/rems/{id}", s.handleDeleteRem)
	s.mux.HandleFunc("POST /api/rems/{id}/indent", s.handleIndentRem)
	s.mux.HandleFunc("POST /api/rems/{id}/outdent", s.handleOutdentRem)
	s.mux.HandleFunc("POST /api/rems/{id}/move", s.handleMoveRem)
	s.mux.HandleFunc("POST /api/rems/{id}/toggle-collapse", s.handleToggleCollapse)

	// Tree and search
	s.mux.HandleFunc("GET /api/tree", s.handleGetTree)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/backlinks", s.handleGetBacklinks)
	s.mux.HandleFunc("GET /api/graph", s.handleGetGraph)

	// Spaced repetition & Flashcards
	s.mux.HandleFunc("GET /api/cards/due", s.handleGetDueCards)
	s.mux.HandleFunc("GET /api/cards/cram", s.handleGetCramCards)
	s.mux.HandleFunc("POST /api/cards/{id}/review", s.handleReviewCard)
	s.mux.HandleFunc("GET /api/cards/stats", s.handleGetCardStats)

	// Import & Export
	s.mux.HandleFunc("GET /api/export", s.handleExport)
	s.mux.HandleFunc("POST /api/import", s.handleImport)

	// PDF documents & highlights
	s.mux.HandleFunc("POST /api/pdfs", s.handleUploadPDF)
	s.mux.HandleFunc("GET /api/pdfs", s.handleListPDFs)
	s.mux.HandleFunc("GET /api/pdfs/{id}", s.handleGetPDF)
	s.mux.HandleFunc("GET /api/pdfs/{id}/content", s.handleStreamPDFContent)
	s.mux.HandleFunc("DELETE /api/pdfs/{id}", s.handleDeletePDF)
	s.mux.HandleFunc("GET /api/pdfs/{id}/highlights", s.handleListPDFHighlights)
	s.mux.HandleFunc("POST /api/pdfs/{id}/highlights", s.handleCreatePDFHighlight)
	s.mux.HandleFunc("DELETE /api/highlights/{id}", s.handleDeletePDFHighlight)
	s.mux.HandleFunc("DELETE /api/pdfs/{id}/highlights/{hl_id}", s.handleDeletePDFHighlight)

	// MCP JSON-RPC over HTTP
	s.mux.HandleFunc("POST /mcp", s.handleMCP)
	s.mux.HandleFunc("POST /mcp/rpc", s.handleMCP)
	s.mux.HandleFunc("GET /mcp/sse", s.handleMCPSSE)

	// Web UI
	if staticHandler != nil {
		s.mux.Handle("/", staticHandler)
	}
}

// Helpers
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) extractRawToken(r *http.Request) string {
	// 1. Authorization: Bearer <token>
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	// 2. X-API-Key: <key>
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return strings.TrimSpace(apiKey)
	}

	// 3. Cookie: remgo_token
	if cookie, err := r.Cookie("remgo_token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// 4. URL query param (for SSE and exports): ?api_key=... or ?token=...
	if qKey := r.URL.Query().Get("api_key"); qKey != "" {
		return qKey
	}
	if qToken := r.URL.Query().Get("token"); qToken != "" {
		return qToken
	}

	return ""
}

func (s *Server) getUser(r *http.Request) *db.User {
	token := s.extractRawToken(r)
	if token != "" {
		user, err := s.db.AuthenticateToken(token, s.jwtSecret)
		if err == nil && user != nil {
			return user
		}
	}

	// If auth is not enabled, default to configured default account
	if !s.authEnabled {
		if s.defaultUserID != "" && s.defaultUserID != db.DefaultUserID {
			user, err := s.db.GetUserByID(s.defaultUserID)
			if err == nil && user != nil {
				return user
			}
		}
		return &db.User{
			ID:       db.DefaultUserID,
			Username: db.DefaultUsername,
			Role:     "admin",
		}
	}

	return nil
}

func (s *Server) getUserID(r *http.Request) string {
	u := s.getUser(r)
	if u != nil {
		return u.ID
	}
	return db.DefaultUserID
}

// Handlers
func (s *Server) handleListRems(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	tree, err := s.db.GetTree(userID, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (s *Server) handleCreateRem(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	var body struct {
		Content   *string `json:"content"`
		ParentID  *string `json:"parent_id"`
		AfterID   *string `json:"after_id"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	content := ""
	if body.Content != nil {
		content = *body.Content
	}

	var rem *db.Rem
	var err error

	if body.AfterID != nil && *body.AfterID != "" {
		rem, err = s.db.CreateRemAfter(userID, *body.AfterID, content)
	} else {
		rem, err = s.db.CreateRem(userID, body.ParentID, content, body.SortOrder)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rem)
}

func (s *Server) handleGetRem(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	rem, err := s.db.GetRem(userID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rem == nil {
		writeError(w, http.StatusNotFound, "rem not found")
		return
	}

	ancestors, _ := s.db.GetAncestors(userID, id)
	backlinks, _ := s.db.GetBacklinks(userID, id)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rem":       rem,
		"ancestors": ancestors,
		"backlinks": backlinks,
	})
}

func (s *Server) handleUpdateRem(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	var body struct {
		Content   *string `json:"content"`
		Collapsed *bool   `json:"collapsed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	rem, err := s.db.UpdateRem(userID, id, body.Content, body.Collapsed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rem)
}

func (s *Server) handleDeleteRem(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	if err := s.db.DeleteRem(userID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleIndentRem(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	if err := s.db.IndentRem(userID, id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"indented": true})
}

func (s *Server) handleOutdentRem(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	if err := s.db.OutdentRem(userID, id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"outdented": true})
}

func (s *Server) handleMoveRem(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	var body struct {
		TargetParentID *string `json:"target_parent_id"`
		SortOrder      int     `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.db.MoveRem(userID, id, body.TargetParentID, body.SortOrder); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"moved": true})
}

func (s *Server) handleToggleCollapse(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	collapsed, err := s.db.ToggleCollapse(userID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"collapsed": collapsed})
}

func (s *Server) handleGetTree(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	var rootID *string
	if q := r.URL.Query().Get("root_id"); q != "" {
		rootID = &q
	}
	tree, err := s.db.GetTree(userID, rootID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	q := r.URL.Query().Get("q")
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	results, err := s.db.Search(userID, q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleGetBacklinks(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	target := r.URL.Query().Get("target")
	if target == "" {
		writeError(w, http.StatusBadRequest, "target query parameter required")
		return
	}
	backlinks, err := s.db.GetBacklinks(userID, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, backlinks)
}

func (s *Server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	graph, err := s.db.GetGraphData(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) handleGetDueCards(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	cards, err := s.db.GetDueCards(userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *Server) handleGetCramCards(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	var remID *string
	if q := r.URL.Query().Get("rem_id"); q != "" {
		remID = &q
	}
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	cards, err := s.db.GetCramCards(userID, remID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *Server) handleReviewCard(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	var body struct {
		Rating int  `json:"rating"`
		IsCram bool `json:"is_cram"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Rating < 1 || body.Rating > 4 {
		writeError(w, http.StatusBadRequest, "rating must be between 1 and 4")
		return
	}

	result, err := s.db.ReviewCard(userID, id, srs.Rating(body.Rating), body.IsCram)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetCardStats(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	stats, err := s.db.GetCardStats(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	format := r.URL.Query().Get("format")
	if format == "json" {
		tree, err := s.db.GetTree(userID, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename=remgo_export.json")
		writeJSON(w, http.StatusOK, tree)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=remgo_export.md")
	if err := s.db.ExportMarkdown(userID, w); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	count, err := s.db.ImportMarkdown(userID, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("import failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"imported": count,
		"message":  fmt.Sprintf("Successfully imported %d Rems", count),
	})
}

// MCP JSON-RPC over HTTP
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	respBytes, err := s.mcpServer.HandleMessageForUser(body, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if respBytes == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
}

func (s *Server) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
	flusher.Flush()

	<-r.Context().Done()
}
