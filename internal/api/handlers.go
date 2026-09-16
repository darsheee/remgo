package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/darsheee/remgo/internal/db"
	"github.com/darsheee/remgo/internal/mcp"
	"github.com/darsheee/remgo/internal/srs"
)

// Server handles all REST API and MCP HTTP endpoints.
type Server struct {
	db        *db.DB
	mcpServer *mcp.Server
	mux       *http.ServeMux
}

// NewServer initializes the HTTP API server.
func NewServer(database *db.DB, staticHandler http.Handler) *Server {
	s := &Server{
		db:        database,
		mcpServer: mcp.NewServer(database),
		mux:       http.NewServeMux(),
	}

	s.registerRoutes(staticHandler)
	return s
}

// Handler returns the top-level http.Handler with CORS enabled.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) registerRoutes(staticHandler http.Handler) {
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

// Handlers
func (s *Server) handleListRems(w http.ResponseWriter, r *http.Request) {
	tree, err := s.db.GetTree(nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (s *Server) handleCreateRem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content  string  `json:"content"`
		ParentID *string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		body.Content = "New Rem"
	}

	rem, err := s.db.CreateRem(body.ParentID, body.Content, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rem)
}

func (s *Server) handleGetRem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rem, err := s.db.GetRem(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rem == nil {
		writeError(w, http.StatusNotFound, "rem not found")
		return
	}

	ancestors, _ := s.db.GetAncestors(id)
	backlinks, _ := s.db.GetBacklinks(rem.Content)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rem":       rem,
		"ancestors": ancestors,
		"backlinks": backlinks,
	})
}

func (s *Server) handleUpdateRem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Content   *string `json:"content"`
		Collapsed *bool   `json:"collapsed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	rem, err := s.db.UpdateRem(id, body.Content, body.Collapsed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rem)
}

func (s *Server) handleDeleteRem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.DeleteRem(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleIndentRem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.IndentRem(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"indented": true})
}

func (s *Server) handleOutdentRem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.OutdentRem(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"outdented": true})
}

func (s *Server) handleMoveRem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		TargetParentID *string `json:"target_parent_id"`
		SortOrder      int     `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.db.MoveRem(id, body.TargetParentID, body.SortOrder); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"moved": true})
}

func (s *Server) handleToggleCollapse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	collapsed, err := s.db.ToggleCollapse(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"collapsed": collapsed})
}

func (s *Server) handleGetTree(w http.ResponseWriter, r *http.Request) {
	var rootID *string
	if q := r.URL.Query().Get("root_id"); q != "" {
		rootID = &q
	}
	tree, err := s.db.GetTree(rootID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	results, err := s.db.Search(q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleGetBacklinks(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		writeError(w, http.StatusBadRequest, "target query parameter required")
		return
	}
	backlinks, err := s.db.GetBacklinks(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, backlinks)
}

func (s *Server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	graph, err := s.db.GetGraphData()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) handleGetDueCards(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	cards, err := s.db.GetDueCards(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *Server) handleGetCramCards(w http.ResponseWriter, r *http.Request) {
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
	cards, err := s.db.GetCramCards(remID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *Server) handleReviewCard(w http.ResponseWriter, r *http.Request) {
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

	result, err := s.db.ReviewCard(id, srs.Rating(body.Rating), body.IsCram)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetCardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetCardStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "json" {
		tree, err := s.db.GetTree(nil)
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
	if err := s.db.ExportMarkdown(w); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.ImportMarkdown(r.Body)
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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	respBytes, err := s.mcpServer.HandleMessage(body)
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
