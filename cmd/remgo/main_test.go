package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darsheee/remgo/internal/api"
	"github.com/darsheee/remgo/internal/db"
	"github.com/darsheee/remgo/internal/web"
)

func TestEndToEndServer(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "e2e_remgo.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Seed initial notes like main() does
	seedInitialNotesIfEmpty(database)

	staticHandler := web.Handler()
	apiServer := api.NewServer(database, staticHandler)
	ts := httptest.NewServer(apiServer.Handler())
	defer ts.Close()

	// 1. Verify Root HTML is served by embedded FS
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("failed to GET /: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for root, got %d", res.StatusCode)
	}
	bodyBuf := make([]byte, 512)
	n, _ := res.Body.Read(bodyBuf)
	res.Body.Close()
	if !strings.Contains(string(bodyBuf[:n]), "RemGo") {
		t.Fatalf("root page missing 'RemGo', got %s", string(bodyBuf[:n]))
	}

	// 2. Verify static assets (app.js, style.css, favicon.svg)
	for _, asset := range []string{"/app.js", "/style.css", "/favicon.svg"} {
		aRes, err := http.Get(ts.URL + asset)
		if err != nil || aRes.StatusCode != http.StatusOK {
			t.Fatalf("failed to load asset %s: status %d", asset, aRes.StatusCode)
		}
		aRes.Body.Close()
	}

	// 3. Verify Tree contains seeded RemNote starter bullets
	treeRes, err := http.Get(ts.URL + "/api/tree")
	if err != nil || treeRes.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET /api/tree: %v", err)
	}
	var tree []*db.RemTreeNode
	json.NewDecoder(treeRes.Body).Decode(&tree)
	treeRes.Body.Close()

	if len(tree) != 1 {
		t.Fatalf("expected 1 seeded root document, got %d", len(tree))
	}
	if tree[0].Content != "Getting Started with RemGo" {
		t.Errorf("unexpected root title: %s", tree[0].Content)
	}
	if len(tree[0].Children) < 4 {
		t.Errorf("expected at least 4 starter bullets, got %d", len(tree[0].Children))
	}

	// 4. Verify Due Cards generated from seeded syntax
	cardsRes, err := http.Get(ts.URL + "/api/cards/due")
	if err != nil || cardsRes.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET /api/cards/due: %v", err)
	}
	var cards []*db.CardWithRem
	json.NewDecoder(cardsRes.Body).Decode(&cards)
	cardsRes.Body.Close()

	if len(cards) < 3 {
		t.Fatalf("expected at least 3 generated flashcards from seeded data, got %d", len(cards))
	}

	// 5. Verify MCP Endpoint on the same server
	mcpReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_rems","arguments":{"query":"Mitochondria"}}}`
	mRes, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(mcpReq))
	if err != nil || mRes.StatusCode != http.StatusOK {
		t.Fatalf("failed to POST /mcp: %v", err)
	}
	var mcpResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	json.NewDecoder(mRes.Body).Decode(&mcpResp)
	mRes.Body.Close()

	if len(mcpResp.Result.Content) == 0 || !strings.Contains(mcpResp.Result.Content[0].Text, "Powerhouse of the cell") {
		t.Fatalf("MCP search result missing Mitochondria, got %+v", mcpResp)
	}
}
