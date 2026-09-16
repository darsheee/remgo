package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darsheee/remgo/internal/db"
)

func setupTestMCP(t *testing.T) (*Server, *db.DB) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mcp_test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return NewServer(database), database
}

func TestMCPInitializeAndToolsList(t *testing.T) {
	srv, _ := setupTestMCP(t)

	// 1. Test initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	initRespRaw, err := srv.HandleMessage([]byte(initReq))
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	var initResp JSONRPCResponse
	if err := json.Unmarshal(initRespRaw, &initResp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("unexpected error: %+v", initResp.Error)
	}

	// 2. Test tools/list
	toolsReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	toolsRespRaw, err := srv.HandleMessage([]byte(toolsReq))
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	var toolsResp struct {
		Result struct {
			Tools []ToolDefinition `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(toolsRespRaw, &toolsResp); err != nil {
		t.Fatalf("unmarshal tools failed: %v", err)
	}

	if len(toolsResp.Result.Tools) < 5 {
		t.Fatalf("expected at least 5 tools, got %d", len(toolsResp.Result.Tools))
	}
}

func TestMCPToolExecutionWorkflow(t *testing.T) {
	srv, _ := setupTestMCP(t)

	// 1. Create Rem via MCP tool call
	createReq := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_rem","arguments":{"content":"FSRS :: Free Spaced Repetition Scheduler"}}}`
	respRaw, err := srv.HandleMessage([]byte(createReq))
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	var callResp struct {
		Result ToolCallResult `json:"result"`
	}
	if err := json.Unmarshal(respRaw, &callResp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if callResp.Result.IsError {
		t.Fatalf("tool call returned error: %s", callResp.Result.Content[0].Text)
	}

	// 2. Search Rems via MCP tool call
	searchReq := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_rems","arguments":{"query":"FSRS"}}}`
	searchRespRaw, _ := srv.HandleMessage([]byte(searchReq))
	var searchResp struct {
		Result ToolCallResult `json:"result"`
	}
	_ = json.Unmarshal(searchRespRaw, &searchResp)
	if !strings.Contains(searchResp.Result.Content[0].Text, "Free Spaced Repetition") {
		t.Fatalf("expected search result to contain card content, got %s", searchResp.Result.Content[0].Text)
	}

	// 3. Get Due Flashcards via MCP
	dueReq := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_due_flashcards","arguments":{"limit":10}}}`
	dueRespRaw, _ := srv.HandleMessage([]byte(dueReq))
	var dueResp struct {
		Result ToolCallResult `json:"result"`
	}
	_ = json.Unmarshal(dueRespRaw, &dueResp)

	var cards []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(dueResp.Result.Content[0].Text), &cards)
	if len(cards) != 1 {
		t.Fatalf("expected 1 due card, got %d", len(cards))
	}

	// 4. Review Flashcard via MCP
	reviewReq := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"review_flashcard","arguments":{"card_id":"` + cards[0].ID + `","rating":3}}}`
	reviewRespRaw, _ := srv.HandleMessage([]byte(reviewReq))
	var reviewResp struct {
		Result ToolCallResult `json:"result"`
	}
	_ = json.Unmarshal(reviewRespRaw, &reviewResp)
	if reviewResp.Result.IsError {
		t.Fatalf("review tool call returned error: %s", reviewResp.Result.Content[0].Text)
	}

	// 5. Get Tree Markdown via MCP
	treeReq := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_rem_tree","arguments":{"format":"markdown"}}}`
	treeRespRaw, _ := srv.HandleMessage([]byte(treeReq))
	var treeResp struct {
		Result ToolCallResult `json:"result"`
	}
	_ = json.Unmarshal(treeRespRaw, &treeResp)
	if !strings.Contains(treeResp.Result.Content[0].Text, "- FSRS ::") {
		t.Fatalf("expected markdown tree to contain bullet, got: %s", treeResp.Result.Content[0].Text)
	}
}

func TestMCPNotificationHandling(t *testing.T) {
	srv, _ := setupTestMCP(t)

	// JSON-RPC 2.0 notification (no ID) must return nil
	notifReq := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	resp, err := srv.HandleMessage([]byte(notifReq))
	if err != nil {
		t.Fatalf("unexpected error on notification: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response for JSON-RPC notification, got: %s", string(resp))
	}

	// Standard initialized notification without notifications/ prefix
	notifReq2 := `{"jsonrpc":"2.0","method":"initialized"}`
	resp2, err := srv.HandleMessage([]byte(notifReq2))
	if err != nil {
		t.Fatalf("unexpected error on notification: %v", err)
	}
	if resp2 != nil {
		t.Fatalf("expected nil response for JSON-RPC notification without id, got: %s", string(resp2))
	}
}

func TestMCPMultiUserIsolation(t *testing.T) {
	srv, database := setupTestMCP(t)

	userAlice, _ := database.CreateUser("alice_mcp", "alice@mcp.dev", "alicepass123", "")
	userBob, _ := database.CreateUser("bob_mcp", "bob@mcp.dev", "bobpass123", "")

	// 1. Alice creates a rem via MCP
	aliceReq := `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"create_rem","arguments":{"content":"Alice Secret Strategy :: Plan A"}}}`
	respRaw, err := srv.HandleMessageForUser([]byte(aliceReq), userAlice.ID)
	if err != nil {
		t.Fatalf("Alice MCP create failed: %v", err)
	}
	if strings.Contains(string(respRaw), `"isError":true`) {
		t.Fatalf("Alice MCP error: %s", string(respRaw))
	}

	// 2. Bob searches for "Strategy" via MCP -> must return 0 results
	bobReq := `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"search_rems","arguments":{"query":"Strategy"}}}`
	bobRespRaw, err := srv.HandleMessageForUser([]byte(bobReq), userBob.ID)
	if err != nil {
		t.Fatalf("Bob MCP search failed: %v", err)
	}
	var bobSearch struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	json.Unmarshal(bobRespRaw, &bobSearch)
	if len(bobSearch.Result.Content) == 0 || bobSearch.Result.Content[0].Text != "null" && bobSearch.Result.Content[0].Text != "[]" {
		t.Fatalf("Bob should not see Alice's notes: %s", bobSearch.Result.Content[0].Text)
	}

	// 3. Alice searches for "Strategy" via MCP -> must find it
	aliceSearchReq := `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"search_rems","arguments":{"query":"Strategy"}}}`
	aliceSearchRespRaw, _ := srv.HandleMessageForUser([]byte(aliceSearchReq), userAlice.ID)
	if !strings.Contains(string(aliceSearchRespRaw), "Alice Secret Strategy") {
		t.Fatalf("Alice should see her own notes: %s", string(aliceSearchRespRaw))
	}
}
