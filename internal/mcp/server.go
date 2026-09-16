package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/darsheee/remgo/internal/db"
	"github.com/darsheee/remgo/internal/srs"
)

// Server coordinates Model Context Protocol (MCP) tool execution.
type Server struct {
	db *db.DB
}

// NewServer initializes an MCP server instance.
func NewServer(database *db.DB) *Server {
	return &Server{db: database}
}

// JSON-RPC 2.0 types
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP Protocol structures
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type PropertyDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Tools exposed to AI Agents
var availableTools = []ToolDefinition{
	{
		Name:        "search_rems",
		Description: "Full-text search (FTS5) across all Rem notes, returning matching rems with ancestor breadcrumbs.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"query": {Type: "string", Description: "Search query or keyword"},
				"limit": {Type: "integer", Description: "Maximum number of results to return (default 20)"},
			},
			Required: []string{"query"},
		},
	},
	{
		Name:        "create_rem",
		Description: "Create a new Rem bullet. Automatically parses flashcards (::, :::, ;;, ==>, {{cloze}}) and [[references]].",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"content":   {Type: "string", Description: "The text content of the Rem"},
				"parent_id": {Type: "string", Description: "Optional parent Rem ID (leave empty for root document)"},
			},
			Required: []string{"content"},
		},
	},
	{
		Name:        "get_rem",
		Description: "Get a specific Rem by ID with its parent, content, and ancestors.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"id": {Type: "string", Description: "The Rem ID"},
			},
			Required: []string{"id"},
		},
	},
	{
		Name:        "get_rem_tree",
		Description: "Get the hierarchical outliner tree rooted at root_id or all root documents.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"root_id": {Type: "string", Description: "Optional root Rem ID to fetch subtree"},
				"format":  {Type: "string", Description: "Format: 'json' or 'markdown'", Enum: []string{"json", "markdown"}},
			},
		},
	},
	{
		Name:        "update_rem",
		Description: "Update a Rem's content or collapsed state. Automatically resynchronizes flashcards and backlinks.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"id":        {Type: "string", Description: "The Rem ID to update"},
				"content":   {Type: "string", Description: "Updated text content"},
				"collapsed": {Type: "boolean", Description: "Whether the bullet is collapsed"},
			},
			Required: []string{"id"},
		},
	},
	{
		Name:        "delete_rem",
		Description: "Delete a Rem and all its descendant bullets and flashcards recursively.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"id": {Type: "string", Description: "The Rem ID to delete"},
			},
			Required: []string{"id"},
		},
	},
	{
		Name:        "get_due_flashcards",
		Description: "Get flashcards due for review under FSRS spaced repetition.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"limit": {Type: "integer", Description: "Maximum cards to fetch (default 20)"},
			},
		},
	},
	{
		Name:        "review_flashcard",
		Description: "Submit an FSRS review rating for a flashcard (1=Again, 2=Hard, 3=Good, 4=Easy).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"card_id": {Type: "string", Description: "The card ID being reviewed"},
				"rating":  {Type: "integer", Description: "Rating: 1 (Again), 2 (Hard), 3 (Good), 4 (Easy)"},
				"is_cram": {Type: "boolean", Description: "True for cram practice without changing real SRS intervals"},
			},
			Required: []string{"card_id", "rating"},
		},
	},
	{
		Name:        "get_backlinks",
		Description: "Find all Rems that reference a given Rem title or ID via [[references]].",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"query": {Type: "string", Description: "Target Rem title or ID"},
			},
			Required: []string{"query"},
		},
	},
	{
		Name:        "get_card_stats",
		Description: "Get aggregate spaced repetition stats (total cards, new, due today, reviewed today).",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]PropertyDef{},
		},
	},
}

// HandleMessage parses and executes a single JSON-RPC message.
func (s *Server) HandleMessage(msg []byte) ([]byte, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		return json.Marshal(JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: -32700, Message: "Parse error"},
		})
	}

	// Notifications (no ID)
	if req.ID == nil && strings.HasPrefix(req.Method, "notifications/") {
		return nil, nil
	}

	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "remgo-mcp",
				"version": "1.0.0",
			},
		}

	case "ping":
		resp.Result = map[string]interface{}{}

	case "tools/list":
		resp.Result = map[string]interface{}{
			"tools": availableTools,
		}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid tool params"}
			break
		}

		callRes := s.executeTool(params.Name, params.Arguments)
		resp.Result = callRes

	default:
		resp.Error = &RPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)}
	}

	return json.Marshal(resp)
}

func (s *Server) executeTool(name string, argsRaw json.RawMessage) ToolCallResult {
	switch name {
	case "search_rems":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		_ = json.Unmarshal(argsRaw, &args)
		res, err := s.db.Search(args.Query, args.Limit)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(res)

	case "create_rem":
		var args struct {
			Content  string  `json:"content"`
			ParentID *string `json:"parent_id"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.Content == "" {
			return toolError("content is required")
		}
		rem, err := s.db.CreateRem(args.ParentID, args.Content, nil)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(rem)

	case "get_rem":
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.ID == "" {
			return toolError("id is required")
		}
		rem, err := s.db.GetRem(args.ID)
		if err != nil {
			return toolError(err.Error())
		}
		if rem == nil {
			return toolError("rem not found")
		}
		ancestors, _ := s.db.GetAncestors(args.ID)
		return toolJSON(map[string]interface{}{
			"rem":       rem,
			"ancestors": ancestors,
		})

	case "get_rem_tree":
		var args struct {
			RootID *string `json:"root_id"`
			Format string  `json:"format"`
		}
		_ = json.Unmarshal(argsRaw, &args)

		if args.Format == "markdown" {
			var buf bytes.Buffer
			if err := s.db.ExportMarkdown(&buf); err != nil {
				return toolError(err.Error())
			}
			return ToolCallResult{Content: []ToolContent{{Type: "text", Text: buf.String()}}}
		}

		tree, err := s.db.GetTree(args.RootID)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(tree)

	case "update_rem":
		var args struct {
			ID        string  `json:"id"`
			Content   *string `json:"content"`
			Collapsed *bool   `json:"collapsed"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.ID == "" {
			return toolError("id is required")
		}
		rem, err := s.db.UpdateRem(args.ID, args.Content, args.Collapsed)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(rem)

	case "delete_rem":
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.ID == "" {
			return toolError("id is required")
		}
		if err := s.db.DeleteRem(args.ID); err != nil {
			return toolError(err.Error())
		}
		return toolText(fmt.Sprintf("Rem %s deleted successfully", args.ID))

	case "get_due_flashcards":
		var args struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(argsRaw, &args)
		cards, err := s.db.GetDueCards(args.Limit)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(cards)

	case "review_flashcard":
		var args struct {
			CardID string `json:"card_id"`
			Rating int    `json:"rating"`
			IsCram bool   `json:"is_cram"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.CardID == "" {
			return toolError("card_id and rating are required")
		}
		res, err := s.db.ReviewCard(args.CardID, srs.Rating(args.Rating), args.IsCram)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(res)

	case "get_backlinks":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.Query == "" {
			return toolError("query is required")
		}
		rems, err := s.db.GetBacklinks(args.Query)
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(rems)

	case "get_card_stats":
		stats, err := s.db.GetCardStats()
		if err != nil {
			return toolError(err.Error())
		}
		return toolJSON(stats)

	default:
		return toolError(fmt.Sprintf("Unknown tool: %s", name))
	}
}

func toolJSON(v interface{}) ToolCallResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: string(b)}},
	}
}

func toolText(s string) ToolCallResult {
	return ToolCallResult{
		Content: []ToolContent{{Type: "text", Text: s}},
	}
}

func toolError(msg string) ToolCallResult {
	return ToolCallResult{
		IsError: true,
		Content: []ToolContent{{Type: "text", Text: "Error: " + msg}},
	}
}

// ServeStdio starts the MCP server over standard input and standard output.
func (s *Server) ServeStdio() error {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				resp, err := s.HandleMessage(trimmed)
				if err == nil && resp != nil {
					writer.Write(resp)
					writer.WriteByte('\n')
					writer.Flush()
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
