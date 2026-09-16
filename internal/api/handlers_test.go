package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darsheee/remgo/internal/db"
)

func setupTestServer(t *testing.T) (*Server, *db.DB) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "api_test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	srv := NewServer(database, nil)
	return srv, database
}

func TestAPIRemWorkflow(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Handler()

	// 1. Create Rem
	createBody := `{"content":"Operating Systems :: Software that manages hardware"}`
	req := httptest.NewRequest("POST", "/api/rems", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var createdRem db.Rem
	if err := json.Unmarshal(w.Body.Bytes(), &createdRem); err != nil {
		t.Fatalf("failed to unmarshal created rem: %v", err)
	}

	// 2. Get Tree
	treeReq := httptest.NewRequest("GET", "/api/tree", nil)
	wTree := httptest.NewRecorder()
	handler.ServeHTTP(wTree, treeReq)

	if wTree.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wTree.Code)
	}
	var tree []*db.RemTreeNode
	json.Unmarshal(wTree.Body.Bytes(), &tree)
	if len(tree) != 1 || tree[0].ID != createdRem.ID {
		t.Fatalf("expected 1 tree node, got %+v", tree)
	}

	// 3. Search
	searchReq := httptest.NewRequest("GET", "/api/search?q=Operating", nil)
	wSearch := httptest.NewRecorder()
	handler.ServeHTTP(wSearch, searchReq)

	if wSearch.Code != http.StatusOK {
		t.Fatalf("search returned %d", wSearch.Code)
	}
	var searchRes []*db.SearchResult
	json.Unmarshal(wSearch.Body.Bytes(), &searchRes)
	if len(searchRes) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(searchRes))
	}

	// 4. Get Due Cards
	cardsReq := httptest.NewRequest("GET", "/api/cards/due", nil)
	wCards := httptest.NewRecorder()
	handler.ServeHTTP(wCards, cardsReq)

	if wCards.Code != http.StatusOK {
		t.Fatalf("due cards returned %d", wCards.Code)
	}
	var cards []*db.CardWithRem
	json.Unmarshal(wCards.Body.Bytes(), &cards)
	if len(cards) != 1 {
		t.Fatalf("expected 1 due card, got %d", len(cards))
	}

	// 5. Review Card
	revBody := `{"rating": 3, "is_cram": false}`
	revReq := httptest.NewRequest("POST", "/api/cards/"+cards[0].ID+"/review", strings.NewReader(revBody))
	revReq.Header.Set("Content-Type", "application/json")
	wRev := httptest.NewRecorder()
	handler.ServeHTTP(wRev, revReq)

	if wRev.Code != http.StatusOK {
		t.Fatalf("review returned %d: %s", wRev.Code, wRev.Body.String())
	}

	// 6. MCP endpoint via HTTP POST /mcp
	mcpReq := `{"jsonrpc":"2.0","id":100,"method":"tools/list"}`
	mReq := httptest.NewRequest("POST", "/mcp", strings.NewReader(mcpReq))
	mReq.Header.Set("Content-Type", "application/json")
	wMCP := httptest.NewRecorder()
	handler.ServeHTTP(wMCP, mReq)

	if wMCP.Code != http.StatusOK {
		t.Fatalf("mcp returned %d", wMCP.Code)
	}
	if !strings.Contains(wMCP.Body.String(), "search_rems") {
		t.Fatalf("expected MCP tools list, got %s", wMCP.Body.String())
	}
}

func TestAPIExportImport(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Handler()

	// Import markdown
	mdContent := "- Root Topic\n  - Subconcept :: Explanation\n"
	impReq := httptest.NewRequest("POST", "/api/import", bytes.NewBufferString(mdContent))
	wImp := httptest.NewRecorder()
	handler.ServeHTTP(wImp, impReq)

	if wImp.Code != http.StatusOK {
		t.Fatalf("import failed with %d: %s", wImp.Code, wImp.Body.String())
	}

	// Export markdown
	expReq := httptest.NewRequest("GET", "/api/export", nil)
	wExp := httptest.NewRecorder()
	handler.ServeHTTP(wExp, expReq)

	if wExp.Code != http.StatusOK {
		t.Fatalf("export failed: %d", wExp.Code)
	}
	if !strings.Contains(wExp.Body.String(), "Root Topic") {
		t.Fatalf("exported markdown missing Root Topic: %s", wExp.Body.String())
	}
}
