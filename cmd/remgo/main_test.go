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
	// Single-user mode (auth disabled)
	apiServer := api.NewServer(database, staticHandler, false)
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

func TestEndToEndServerWithAuth(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "e2e_auth_remgo.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	staticHandler := web.Handler()
	// Multi-user mode with auth forced
	apiServer := api.NewServer(database, staticHandler, true)
	ts := httptest.NewServer(apiServer.Handler())
	defer ts.Close()

	// 1. Root and static assets are always accessible
	res, err := http.Get(ts.URL + "/")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET /: %v", err)
	}

	// 2. Protected API returns 401
	pRes, err := http.Get(ts.URL + "/api/tree")
	if err != nil || pRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /api/tree, got %d", pRes.StatusCode)
	}

	// 3. Status returns auth_enabled: true, has_users: false
	sRes, err := http.Get(ts.URL + "/api/auth/status")
	if err != nil || sRes.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET /api/auth/status: %v", err)
	}
	var status struct {
		AuthEnabled bool `json:"auth_enabled"`
		HasUsers    bool `json:"has_users"`
	}
	json.NewDecoder(sRes.Body).Decode(&status)
	if !status.AuthEnabled || status.HasUsers {
		t.Fatalf("unexpected status: %+v", status)
	}

	// 4. Setup first admin account
	setupReq := `{"username":"admin", "email":"admin@example.com", "password":"adminpassword123"}`
	setRes, err := http.Post(ts.URL+"/api/auth/setup", "application/json", strings.NewReader(setupReq))
	if err != nil || setRes.StatusCode != http.StatusCreated {
		t.Fatalf("failed to setup admin: %v", err)
	}

	var setupResult struct {
		Token string `json:"token"`
	}
	json.NewDecoder(setRes.Body).Decode(&setupResult)
	if setupResult.Token == "" {
		t.Fatalf("empty token returned from setup")
	}

	// 5. Access protected API using Bearer token
	req, _ := http.NewRequest("GET", ts.URL+"/api/tree", nil)
	req.Header.Set("Authorization", "Bearer "+setupResult.Token)
	client := &http.Client{}
	tRes, err := client.Do(req)
	if err != nil || tRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with Bearer token, got %d", tRes.StatusCode)
	}
}

func TestSeedInitialNotesIdempotency(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "seed_idempotent.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// 1. First run on empty database: notes should be seeded
	seedInitialNotesIfEmpty(database)
	treeDef, err := database.GetTree(db.DefaultUserID, nil)
	if err != nil || len(treeDef) == 0 {
		t.Fatalf("expected notes to be seeded for default user")
	}

	// 2. Admin registers: notes are adopted by admin
	admin, err := database.CreateUser("admin", "admin@remgo.dev", "adminpass123", "admin")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	treeAdmin, _ := database.GetTree(admin.ID, nil)
	if len(treeAdmin) == 0 {
		t.Fatalf("expected starter notes to be migrated to admin")
	}

	// Verify DefaultUserID has 0 notes now
	treeDefAfter, _ := database.GetTree(db.DefaultUserID, nil)
	if len(treeDefAfter) != 0 {
		t.Fatalf("expected default user to have 0 notes after admin adoption, got %d", len(treeDefAfter))
	}

	// 3. Server restarts: seedInitialNotesIfEmpty is called again
	seedInitialNotesIfEmpty(database)

	// Verify DefaultUserID STILL has 0 notes (did not re-seed!)
	treeDefAfterRestart, _ := database.GetTree(db.DefaultUserID, nil)
	if len(treeDefAfterRestart) != 0 {
		t.Fatalf("seedInitialNotesIfEmpty incorrectly re-seeded notes after admin setup! got %d", len(treeDefAfterRestart))
	}
}

func TestSingleUserModeAdoptsSingleUser(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "single_user_adopt.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create 1 human user with private notes
	alice, err := database.CreateUser("alice", "alice@remgo.dev", "alicepass123", "admin")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	_, err = database.CreateRem(alice.ID, nil, "Alice Personal Diary", nil)
	if err != nil {
		t.Fatalf("CreateRem failed: %v", err)
	}

	// Run server in single-user mode (auth disabled)
	users, _ := database.ListUsers()
	defaultUserID := db.DefaultUserID
	if len(users) == 1 {
		defaultUserID = users[0].ID
	}

	staticHandler := web.Handler()
	apiServer := api.NewServer(database, staticHandler, false)
	apiServer.SetDefaultUserID(defaultUserID)
	ts := httptest.NewServer(apiServer.Handler())
	defer ts.Close()

	// Status endpoint should show authenticated = true and user = alice
	sRes, err := http.Get(ts.URL + "/api/auth/status")
	if err != nil || sRes.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET /api/auth/status: %v", err)
	}
	var status struct {
		AuthEnabled   bool     `json:"auth_enabled"`
		Authenticated bool     `json:"authenticated"`
		User          *db.User `json:"user"`
	}
	json.NewDecoder(sRes.Body).Decode(&status)
	if status.AuthEnabled {
		t.Errorf("expected auth_enabled = false")
	}
	if !status.Authenticated || status.User == nil || status.User.ID != alice.ID {
		t.Errorf("expected single user mode to adopt alice: %+v", status)
	}

	// Tree endpoint should return Alice's notes without any authentication headers
	tRes, err := http.Get(ts.URL + "/api/tree")
	if err != nil || tRes.StatusCode != http.StatusOK {
		t.Fatalf("failed to GET /api/tree: %v", err)
	}
	var tree []*db.RemTreeNode
	json.NewDecoder(tRes.Body).Decode(&tree)
	if len(tree) != 1 || tree[0].Content != "Alice Personal Diary" {
		t.Fatalf("single user mode failed to return alice's notes: %+v", tree)
	}
}
