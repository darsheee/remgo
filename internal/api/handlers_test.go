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

func setupTestServer(t *testing.T, authEnabled ...bool) (*Server, *db.DB) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "api_test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	srv := NewServer(database, nil, authEnabled...)
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

func TestAPICreateRemEmptyAndAfter(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Handler()

	// 1. Create root doc
	docReq := httptest.NewRequest("POST", "/api/rems", strings.NewReader(`{"content":"My Doc"}`))
	docReq.Header.Set("Content-Type", "application/json")
	wDoc := httptest.NewRecorder()
	handler.ServeHTTP(wDoc, docReq)
	var doc db.Rem
	json.Unmarshal(wDoc.Body.Bytes(), &doc)

	// 2. Create bullet 1
	b1Req := httptest.NewRequest("POST", "/api/rems", strings.NewReader(`{"content":"Item 1", "parent_id":"`+doc.ID+`"}`))
	b1Req.Header.Set("Content-Type", "application/json")
	wB1 := httptest.NewRecorder()
	handler.ServeHTTP(wB1, b1Req)
	var b1 db.Rem
	json.Unmarshal(wB1.Body.Bytes(), &b1)

	// 3. Create empty bullet after bullet 1
	b2Req := httptest.NewRequest("POST", "/api/rems", strings.NewReader(`{"content":"", "parent_id":"`+doc.ID+`", "after_id":"`+b1.ID+`"}`))
	b2Req.Header.Set("Content-Type", "application/json")
	wB2 := httptest.NewRecorder()
	handler.ServeHTTP(wB2, b2Req)

	if wB2.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", wB2.Code)
	}
	var b2 db.Rem
	json.Unmarshal(wB2.Body.Bytes(), &b2)
	if b2.Content != "" {
		t.Errorf("expected empty content, got '%s'", b2.Content)
	}

	// 4. Verify tree order
	treeReq := httptest.NewRequest("GET", "/api/tree?root_id="+doc.ID, nil)
	wTree := httptest.NewRecorder()
	handler.ServeHTTP(wTree, treeReq)
	var tree []*db.RemTreeNode
	json.Unmarshal(wTree.Body.Bytes(), &tree)

	if len(tree) != 1 || len(tree[0].Children) != 2 {
		t.Fatalf("expected 2 children, got %+v", tree)
	}
	if tree[0].Children[0].ID != b1.ID || tree[0].Children[1].ID != b2.ID {
		t.Errorf("wrong order: [0]=%s, [1]=%s", tree[0].Children[0].ID, tree[0].Children[1].ID)
	}
}

func TestAPIGetRemBacklinks(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Handler()

	// Create Target Rem
	targetReq := httptest.NewRequest("POST", "/api/rems", strings.NewReader(`{"content":"Go Lang :: Modern language"}`))
	targetReq.Header.Set("Content-Type", "application/json")
	wTarget := httptest.NewRecorder()
	handler.ServeHTTP(wTarget, targetReq)
	var target db.Rem
	json.Unmarshal(wTarget.Body.Bytes(), &target)

	// Create Source Rem referencing Target Rem
	srcReq := httptest.NewRequest("POST", "/api/rems", strings.NewReader(`{"content":"We use [[Go Lang]] for backend"}`))
	srcReq.Header.Set("Content-Type", "application/json")
	wSrc := httptest.NewRecorder()
	handler.ServeHTTP(wSrc, srcReq)

	// Fetch Target Rem details
	getReq := httptest.NewRequest("GET", "/api/rems/"+target.ID, nil)
	wGet := httptest.NewRecorder()
	handler.ServeHTTP(wGet, getReq)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wGet.Code)
	}

	var res struct {
		Rem       *db.Rem   `json:"rem"`
		Backlinks []*db.Rem `json:"backlinks"`
	}
	json.Unmarshal(wGet.Body.Bytes(), &res)

	if len(res.Backlinks) != 1 {
		t.Fatalf("expected 1 backlink to Go Lang, got %d", len(res.Backlinks))
	}
}

func TestAuthWorkflowAndEnforcement(t *testing.T) {
	srv, _ := setupTestServer(t, true) // auth enabled
	handler := srv.Handler()

	// 1. Without auth, protected route /api/tree returns 401
	unauthReq := httptest.NewRequest("GET", "/api/tree", nil)
	wUnauth := httptest.NewRecorder()
	handler.ServeHTTP(wUnauth, unauthReq)
	if wUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", wUnauth.Code)
	}

	// 2. Status check shows auth_enabled = true, has_users = false
	statusReq := httptest.NewRequest("GET", "/api/auth/status", nil)
	wStatus := httptest.NewRecorder()
	handler.ServeHTTP(wStatus, statusReq)
	var statusRes struct {
		AuthEnabled bool `json:"auth_enabled"`
		HasUsers    bool `json:"has_users"`
	}
	json.Unmarshal(wStatus.Body.Bytes(), &statusRes)
	if !statusRes.AuthEnabled || statusRes.HasUsers {
		t.Fatalf("unexpected status: %+v", statusRes)
	}

	// 3. Register First Admin User via /api/auth/setup
	setupBody := `{"username":"admin", "email":"admin@remgo.dev", "password":"adminpassword123"}`
	setupReq := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(setupBody))
	setupReq.Header.Set("Content-Type", "application/json")
	wSetup := httptest.NewRecorder()
	handler.ServeHTTP(wSetup, setupReq)

	if wSetup.Code != http.StatusCreated {
		t.Fatalf("setup failed with %d: %s", wSetup.Code, wSetup.Body.String())
	}

	var setupResp struct {
		User  *db.User `json:"user"`
		Token string   `json:"token"`
	}
	json.Unmarshal(wSetup.Body.Bytes(), &setupResp)
	adminToken := setupResp.Token
	if adminToken == "" || setupResp.User.Role != "admin" {
		t.Fatalf("invalid setup response: %+v", setupResp)
	}

	// Verify session cookie was set
	cookies := wSetup.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "remgo_token" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected remgo_token cookie to be set")
	}

	// 4. Access protected route with Bearer token
	treeReq := httptest.NewRequest("GET", "/api/tree", nil)
	treeReq.Header.Set("Authorization", "Bearer "+adminToken)
	wTree := httptest.NewRecorder()
	handler.ServeHTTP(wTree, treeReq)
	if wTree.Code != http.StatusOK {
		t.Fatalf("access with Bearer token returned %d", wTree.Code)
	}

	// 5. Access protected route with session Cookie
	treeCookieReq := httptest.NewRequest("GET", "/api/tree", nil)
	treeCookieReq.AddCookie(sessionCookie)
	wTreeCookie := httptest.NewRecorder()
	handler.ServeHTTP(wTreeCookie, treeCookieReq)
	if wTreeCookie.Code != http.StatusOK {
		t.Fatalf("access with cookie returned %d", wTreeCookie.Code)
	}

	// 6. Create Personal Access Token (PAT)
	patReq := httptest.NewRequest("POST", "/api/auth/keys", strings.NewReader(`{"name":"Cursor MCP"}`))
	patReq.Header.Set("Authorization", "Bearer "+adminToken)
	patReq.Header.Set("Content-Type", "application/json")
	wPAT := httptest.NewRecorder()
	handler.ServeHTTP(wPAT, patReq)

	if wPAT.Code != http.StatusCreated {
		t.Fatalf("create PAT returned %d: %s", wPAT.Code, wPAT.Body.String())
	}
	var patResp struct {
		Key string `json:"key"`
	}
	json.Unmarshal(wPAT.Body.Bytes(), &patResp)
	if !strings.HasPrefix(patResp.Key, "remgo_pat_") {
		t.Fatalf("invalid PAT: %s", patResp.Key)
	}

	// 7. MCP call with PAT via X-API-Key header
	mcpReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_rem","arguments":{"content":"MCP Note via PAT"}}}`
	mReq := httptest.NewRequest("POST", "/mcp", strings.NewReader(mcpReq))
	mReq.Header.Set("X-API-Key", patResp.Key)
	mReq.Header.Set("Content-Type", "application/json")
	wMCP := httptest.NewRecorder()
	handler.ServeHTTP(wMCP, mReq)
	if wMCP.Code != http.StatusOK {
		t.Fatalf("MCP with PAT returned %d: %s", wMCP.Code, wMCP.Body.String())
	}

	// 8. MCP call without auth -> must be rejected with 401
	unauthMCP := httptest.NewRequest("POST", "/mcp", strings.NewReader(mcpReq))
	wUnauthMCP := httptest.NewRecorder()
	handler.ServeHTTP(wUnauthMCP, unauthMCP)
	if wUnauthMCP.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated MCP call, got %d", wUnauthMCP.Code)
	}

	// 9. Multi-User Isolation: Register second user Bob
	regBody := `{"username":"bob", "email":"bob@remgo.dev", "password":"bobpassword123"}`
	regReq := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	wReg := httptest.NewRecorder()
	handler.ServeHTTP(wReg, regReq)
	if wReg.Code != http.StatusCreated {
		t.Fatalf("bob registration failed: %s", wReg.Body.String())
	}
	var regResp struct {
		Token string `json:"token"`
	}
	json.Unmarshal(wReg.Body.Bytes(), &regResp)
	bobToken := regResp.Token

	// Bob creates note
	bobCreateReq := httptest.NewRequest("POST", "/api/rems", strings.NewReader(`{"content":"Bob Private Secret"}`))
	bobCreateReq.Header.Set("Authorization", "Bearer "+bobToken)
	bobCreateReq.Header.Set("Content-Type", "application/json")
	wBobCreate := httptest.NewRecorder()
	handler.ServeHTTP(wBobCreate, bobCreateReq)
	if wBobCreate.Code != http.StatusCreated {
		t.Fatalf("bob create rem failed: %s", wBobCreate.Body.String())
	}

	// Admin queries tree -> should NOT see Bob's note
	adminTreeReq := httptest.NewRequest("GET", "/api/tree", nil)
	adminTreeReq.Header.Set("Authorization", "Bearer "+adminToken)
	wAdminTree := httptest.NewRecorder()
	handler.ServeHTTP(wAdminTree, adminTreeReq)
	if strings.Contains(wAdminTree.Body.String(), "Bob Private Secret") {
		t.Fatalf("Admin saw Bob's private note! Data leakage!")
	}
}
