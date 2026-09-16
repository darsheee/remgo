package db

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darsheee/remgo/internal/srs"
)

func setupTestDB(t *testing.T) *DB {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_remgo.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
	})
	return database
}

func TestCreateAndTreeCTE(t *testing.T) {
	d := setupTestDB(t)

	// 1. Create root document
	doc, err := d.CreateRem(nil, "Biology 101", nil)
	if err != nil {
		t.Fatalf("failed to create root rem: %v", err)
	}

	// 2. Create children
	c1, err := d.CreateRem(&doc.ID, "Cell Theory :: All living organisms are composed of cells", nil)
	if err != nil {
		t.Fatalf("failed to create child rem: %v", err)
	}

	c2, err := d.CreateRem(&doc.ID, "Mitochondria ;; Powerhouse of the cell", nil)
	if err != nil {
		t.Fatalf("failed to create second child: %v", err)
	}

	// 3. Create subchild
	c1Sub, err := d.CreateRem(&c1.ID, "Robert Hooke {{1665}} discovered cells", nil)
	if err != nil {
		t.Fatalf("failed to create subchild: %v", err)
	}
	_ = c2
	_ = c1Sub

	// 4. Test Recursive CTE GetTree
	tree, err := d.GetTree(nil)
	if err != nil {
		t.Fatalf("GetTree failed: %v", err)
	}

	if len(tree) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tree))
	}
	root := tree[0]
	if root.Content != "Biology 101" {
		t.Errorf("expected root content 'Biology 101', got '%s'", root.Content)
	}
	if len(root.Children) != 2 {
		t.Fatalf("expected 2 children under root, got %d", len(root.Children))
	}

	firstChild := root.Children[0]
	if len(firstChild.Children) != 1 {
		t.Fatalf("expected 1 subchild under first child, got %d", len(firstChild.Children))
	}
	if firstChild.Children[0].Content != "Robert Hooke {{1665}} discovered cells" {
		t.Errorf("unexpected subchild content: %s", firstChild.Children[0].Content)
	}

	// 5. Test Ancestors
	ancestors, err := d.GetAncestors(firstChild.Children[0].ID)
	if err != nil {
		t.Fatalf("GetAncestors failed: %v", err)
	}
	if len(ancestors) != 3 {
		t.Fatalf("expected 3 ancestors in path, got %d", len(ancestors))
	}
	if ancestors[0].Content != "Biology 101" || ancestors[1].ID != c1.ID {
		t.Errorf("unexpected ancestor chain: %+v", ancestors)
	}
}

func TestIndentAndOutdent(t *testing.T) {
	d := setupTestDB(t)

	doc, _ := d.CreateRem(nil, "Document", nil)
	b1, _ := d.CreateRem(&doc.ID, "Bullet 1", nil)
	b2, _ := d.CreateRem(&doc.ID, "Bullet 2", nil)

	// Indent Bullet 2 under Bullet 1
	err := d.IndentRem(b2.ID)
	if err != nil {
		t.Fatalf("IndentRem failed: %v", err)
	}

	updatedB2, _ := d.GetRem(b2.ID)
	if updatedB2.ParentID == nil || *updatedB2.ParentID != b1.ID {
		t.Fatalf("expected b2 parent to be b1 (%s), got %v", b1.ID, updatedB2.ParentID)
	}

	// Outdent Bullet 2 back to Document level
	err = d.OutdentRem(b2.ID)
	if err != nil {
		t.Fatalf("OutdentRem failed: %v", err)
	}

	outdentedB2, _ := d.GetRem(b2.ID)
	if outdentedB2.ParentID == nil || *outdentedB2.ParentID != doc.ID {
		t.Fatalf("expected b2 parent to be doc (%s), got %v", doc.ID, outdentedB2.ParentID)
	}
}

func TestCardGenerationAndReview(t *testing.T) {
	d := setupTestDB(t)

	// Create a Rem with forward card and cloze
	content := "Golang :: A fast compiled language created in {{2009}}"
	rem, err := d.CreateRem(nil, content, nil)
	if err != nil {
		t.Fatalf("CreateRem failed: %v", err)
	}

	// Check due cards
	dueCards, err := d.GetDueCards(10)
	if err != nil {
		t.Fatalf("GetDueCards failed: %v", err)
	}

	// Should have 2 cards: 1 forward card and 1 cloze card
	if len(dueCards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(dueCards))
	}

	cardToReview := dueCards[0]
	if cardToReview.NextPreviews == nil {
		t.Fatalf("expected NextPreviews to be computed")
	}

	// Review card with Good
	reviewRes, err := d.ReviewCard(cardToReview.ID, srs.RatingGood, false)
	if err != nil {
		t.Fatalf("ReviewCard failed: %v", err)
	}

	if reviewRes.Card.State != srs.StateReview {
		t.Errorf("expected card state Review, got %s", reviewRes.Card.State)
	}

	// Check CardStats
	stats, err := d.GetCardStats()
	if err != nil {
		t.Fatalf("GetCardStats failed: %v", err)
	}
	if stats.TotalCards != 2 {
		t.Errorf("expected total cards 2, got %d", stats.TotalCards)
	}
	if stats.ReviewedToday != 1 {
		t.Errorf("expected reviewed today 1, got %d", stats.ReviewedToday)
	}

	// Test Card update sync: change definition
	newContent := "Golang :: The Go Programming Language"
	_, err = d.UpdateRem(rem.ID, &newContent, nil)
	if err != nil {
		t.Fatalf("UpdateRem failed: %v", err)
	}

	// Now there should be 1 card (cloze was removed, forward card kept its review history!)
	allCards, err := d.GetCramCards(nil, 10)
	if err != nil {
		t.Fatalf("GetCramCards failed: %v", err)
	}
	if len(allCards) != 1 {
		t.Fatalf("expected 1 card after update, got %d", len(allCards))
	}
	if allCards[0].Front != "Golang" {
		t.Errorf("expected front 'Golang', got '%s'", allCards[0].Front)
	}
	if allCards[0].Reps != 1 {
		t.Errorf("expected preserved Reps=1, got %d", allCards[0].Reps)
	}
}

func TestFTS5Search(t *testing.T) {
	d := setupTestDB(t)

	_, _ = d.CreateRem(nil, "Learning Distributed Systems with Raft consensus", nil)
	_, _ = d.CreateRem(nil, "Spaced repetition outliner architecture", nil)

	results, err := d.Search("Distributed", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if !strings.Contains(results[0].Content, "Distributed Systems") {
		t.Errorf("unexpected search content: %s", results[0].Content)
	}
}

func TestReferencesAndGraph(t *testing.T) {
	d := setupTestDB(t)

	doc1, _ := d.CreateRem(nil, "Computer Science", nil)
	doc2, _ := d.CreateRem(nil, "Software Engineering references [[Computer Science]]", nil)
	_ = doc1
	_ = doc2

	backlinks, err := d.GetBacklinks("Computer Science")
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if len(backlinks) != 1 {
		t.Fatalf("expected 1 backlink, got %d", len(backlinks))
	}
	if backlinks[0].ID != doc2.ID {
		t.Errorf("expected backlink from doc2, got %s", backlinks[0].ID)
	}

	graph, err := d.GetGraphData()
	if err != nil {
		t.Fatalf("GetGraphData failed: %v", err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("expected 2 graph nodes, got %d", len(graph.Nodes))
	}
}

func TestMarkdownExportImport(t *testing.T) {
	d := setupTestDB(t)

	root, _ := d.CreateRem(nil, "Root Topic", nil)
	child, _ := d.CreateRem(&root.ID, "Child Concept :: Explanation", nil)
	_, _ = d.CreateRem(&child.ID, "Grandchild detail", nil)

	var buf bytes.Buffer
	err := d.ExportMarkdown(&buf)
	if err != nil {
		t.Fatalf("ExportMarkdown failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "- Root Topic") || !strings.Contains(output, "  - Child Concept") {
		t.Fatalf("unexpected export format: %s", output)
	}

	// Test import into fresh DB
	d2 := setupTestDB(t)
	count, err := d2.ImportMarkdown(strings.NewReader(output))
	if err != nil {
		t.Fatalf("ImportMarkdown failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 rems imported, got %d", count)
	}

	tree2, _ := d2.GetTree(nil)
	if len(tree2) != 1 || len(tree2[0].Children) != 1 {
		t.Fatalf("imported tree structure invalid: %+v", tree2)
	}
}
