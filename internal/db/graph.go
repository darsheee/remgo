package db

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/darsheee/remgo/internal/parser"
)

// GraphNode represents a node in the knowledge network.
type GraphNode struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	CardCount int    `json:"card_count"`
	IsRoot    bool   `json:"is_root"`
}

// GraphEdge represents a connection between two nodes.
type GraphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"` // "reference" or "hierarchy"
}

// GraphData holds nodes and edges for network visualization.
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GetGraphData builds the knowledge graph connecting Rems via hierarchy and references for a user.
func (d *DB) GetGraphData(userID string) (*GraphData, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	nodes := make([]GraphNode, 0)
	edges := make([]GraphEdge, 0)
	nodeMap := make(map[string]bool)

	// 1. Fetch Rems
	rows, err := d.sqlDB.Query(`
		SELECT r.id, r.parent_id, r.content,
		       (SELECT COUNT(*) FROM cards c WHERE c.rem_id = r.id AND c.user_id = ?) as card_count
		FROM rems r
		WHERE r.user_id = ?;
	`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query rems for graph: %w", err)
	}
	defer rows.Close()

	type remInfo struct {
		id        string
		parentID  *string
		content   string
		cardCount int
	}
	var remsList []remInfo

	for rows.Next() {
		var ri remInfo
		var pID *string
		if err := rows.Scan(&ri.id, &pID, &ri.content, &ri.cardCount); err != nil {
			return nil, err
		}
		ri.parentID = pID
		remsList = append(remsList, ri)

		label := parser.CleanDelimiters(ri.content)
		if len(label) > 40 {
			label = label[:37] + "..."
		}
		if label == "" {
			label = "Untitled"
		}

		nodes = append(nodes, GraphNode{
			ID:        ri.id,
			Label:     label,
			CardCount: ri.cardCount,
			IsRoot:    ri.parentID == nil,
		})
		nodeMap[ri.id] = true
	}

	// 2. Add Hierarchy Edges
	for _, ri := range remsList {
		if ri.parentID != nil && nodeMap[*ri.parentID] {
			edges = append(edges, GraphEdge{
				ID:     fmt.Sprintf("h_%s_%s", *ri.parentID, ri.id),
				Source: *ri.parentID,
				Target: ri.id,
				Type:   "hierarchy",
			})
		}
	}

	// 3. Add Reference Edges
	refRows, err := d.sqlDB.Query(`
		SELECT source_rem_id, target_rem_id, target_title
		FROM references_map
		WHERE user_id = ?
	`, userID)
	if err == nil {
		defer refRows.Close()
		for refRows.Next() {
			var src string
			var targetID *string
			var title string
			if err := refRows.Scan(&src, &targetID, &title); err == nil {
				if targetID != nil && nodeMap[*targetID] {
					edges = append(edges, GraphEdge{
						ID:     fmt.Sprintf("r_%s_%s", src, *targetID),
						Source: src,
						Target: *targetID,
						Type:   "reference",
					})
				}
			}
		}
	}

	return &GraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// ExportMarkdown exports all documents and trees for a user as a clean markdown file.
func (d *DB) ExportMarkdown(userID string, w io.Writer) error {
	if userID == "" {
		userID = DefaultUserID
	}
	tree, err := d.GetTree(userID, nil)
	if err != nil {
		return err
	}

	var writeNode func(node *RemTreeNode, depth int) error
	writeNode = func(node *RemTreeNode, depth int) error {
		indent := strings.Repeat("  ", depth)
		line := fmt.Sprintf("%s- %s\n", indent, node.Content)
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		for _, child := range node.Children {
			if err := writeNode(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	for _, root := range tree {
		if err := writeNode(root, 0); err != nil {
			return err
		}
	}
	return nil
}

// ImportMarkdown imports a Markdown outline where indentation represents hierarchy for a user.
func (d *DB) ImportMarkdown(userID string, r io.Reader) (int, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	scanner := bufio.NewScanner(r)
	type stackItem struct {
		id    string
		level int
	}

	var stack []stackItem
	count := 0

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Count leading spaces or tabs
		leadingSpaces := 0
		for _, ch := range line {
			if ch == ' ' {
				leadingSpaces++
			} else if ch == '\t' {
				leadingSpaces += 2
			} else {
				break
			}
		}
		level := leadingSpaces / 2

		content := strings.TrimPrefix(trimmed, "- ")
		content = strings.TrimPrefix(content, "* ")
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		// Pop stack until parent level is strictly less than current level
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}

		var parentID *string
		if len(stack) > 0 {
			p := stack[len(stack)-1].id
			parentID = &p
		}

		rem, err := d.CreateRem(userID, parentID, content, nil)
		if err != nil {
			return count, err
		}
		count++

		stack = append(stack, stackItem{id: rem.ID, level: level})
	}

	return count, scanner.Err()
}
