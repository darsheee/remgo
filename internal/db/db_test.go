package db

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darsheee/remgo/internal/auth"
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
	u := DefaultUserID

	// 1. Create root document
	doc, err := d.CreateRem(u, nil, "Biology 101", nil)
	if err != nil {
		t.Fatalf("failed to create root rem: %v", err)
	}

	// 2. Create children
	c1, err := d.CreateRem(u, &doc.ID, "Cell Theory :: All living organisms are composed of cells", nil)
	if err != nil {
		t.Fatalf("failed to create child rem: %v", err)
	}

	c2, err := d.CreateRem(u, &doc.ID, "Mitochondria ;; Powerhouse of the cell", nil)
	if err != nil {
		t.Fatalf("failed to create second child: %v", err)
	}

	// 3. Create subchild
	c1Sub, err := d.CreateRem(u, &c1.ID, "Robert Hooke {{1665}} discovered cells", nil)
	if err != nil {
		t.Fatalf("failed to create subchild: %v", err)
	}
	_ = c2
	_ = c1Sub

	// 4. Test Recursive CTE GetTree
	tree, err := d.GetTree(u, nil)
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
	ancestors, err := d.GetAncestors(u, firstChild.Children[0].ID)
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
	u := DefaultUserID

	doc, _ := d.CreateRem(u, nil, "Document", nil)
	b1, _ := d.CreateRem(u, &doc.ID, "Bullet 1", nil)
	b2, _ := d.CreateRem(u, &doc.ID, "Bullet 2", nil)

	// Indent Bullet 2 under Bullet 1
	err := d.IndentRem(u, b2.ID)
	if err != nil {
		t.Fatalf("IndentRem failed: %v", err)
	}

	updatedB2, _ := d.GetRem(u, b2.ID)
	if updatedB2.ParentID == nil || *updatedB2.ParentID != b1.ID {
		t.Fatalf("expected b2 parent to be b1 (%s), got %v", b1.ID, updatedB2.ParentID)
	}

	// Outdent Bullet 2 back to Document level
	err = d.OutdentRem(u, b2.ID)
	if err != nil {
		t.Fatalf("OutdentRem failed: %v", err)
	}

	outdentedB2, _ := d.GetRem(u, b2.ID)
	if outdentedB2.ParentID == nil || *outdentedB2.ParentID != doc.ID {
		t.Fatalf("expected b2 parent to be doc (%s), got %v", doc.ID, outdentedB2.ParentID)
	}
}

func TestCardGenerationAndReview(t *testing.T) {
	d := setupTestDB(t)
	u := DefaultUserID

	// Create a Rem with forward card and cloze
	content := "Golang :: A fast compiled language created in {{2009}}"
	rem, err := d.CreateRem(u, nil, content, nil)
	if err != nil {
		t.Fatalf("CreateRem failed: %v", err)
	}

	// Check due cards
	dueCards, err := d.GetDueCards(u, 10)
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
	reviewRes, err := d.ReviewCard(u, cardToReview.ID, srs.RatingGood, false)
	if err != nil {
		t.Fatalf("ReviewCard failed: %v", err)
	}

	if reviewRes.Card.State != srs.StateReview {
		t.Errorf("expected card state Review, got %s", reviewRes.Card.State)
	}

	// Check CardStats
	stats, err := d.GetCardStats(u)
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
	_, err = d.UpdateRem(u, rem.ID, &newContent, nil)
	if err != nil {
		t.Fatalf("UpdateRem failed: %v", err)
	}

	// Now there should be 1 card (cloze was removed, forward card kept its review history!)
	allCards, err := d.GetCramCards(u, nil, 10)
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
	u := DefaultUserID

	_, _ = d.CreateRem(u, nil, "Learning Distributed Systems with Raft consensus", nil)
	_, _ = d.CreateRem(u, nil, "Spaced repetition outliner architecture", nil)

	results, err := d.Search(u, "Distributed", 10)
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
	u := DefaultUserID

	doc1, _ := d.CreateRem(u, nil, "Computer Science", nil)
	doc2, _ := d.CreateRem(u, nil, "Software Engineering references [[Computer Science]]", nil)
	_ = doc1
	_ = doc2

	backlinks, err := d.GetBacklinks(u, "Computer Science")
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if len(backlinks) != 1 {
		t.Fatalf("expected 1 backlink, got %d", len(backlinks))
	}
	if backlinks[0].ID != doc2.ID {
		t.Errorf("expected backlink from doc2, got %s", backlinks[0].ID)
	}

	graph, err := d.GetGraphData(u)
	if err != nil {
		t.Fatalf("GetGraphData failed: %v", err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("expected 2 graph nodes, got %d", len(graph.Nodes))
	}
}

func TestMarkdownExportImport(t *testing.T) {
	d := setupTestDB(t)
	u := DefaultUserID

	root, _ := d.CreateRem(u, nil, "Root Topic", nil)
	child, _ := d.CreateRem(u, &root.ID, "Child Concept :: Explanation", nil)
	_, _ = d.CreateRem(u, &child.ID, "Grandchild detail", nil)

	var buf bytes.Buffer
	err := d.ExportMarkdown(u, &buf)
	if err != nil {
		t.Fatalf("ExportMarkdown failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "- Root Topic") || !strings.Contains(output, "  - Child Concept") {
		t.Fatalf("unexpected export format: %s", output)
	}

	// Test import into fresh DB
	d2 := setupTestDB(t)
	count, err := d2.ImportMarkdown(u, strings.NewReader(output))
	if err != nil {
		t.Fatalf("ImportMarkdown failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 rems imported, got %d", count)
	}

	tree2, _ := d2.GetTree(u, nil)
	if len(tree2) != 1 || len(tree2[0].Children) != 1 {
		t.Fatalf("imported tree structure invalid: %+v", tree2)
	}
}

func TestMultiLineListCardHierarchy(t *testing.T) {
	d := setupTestDB(t)
	u := DefaultUserID

	// Create list card header
	listHeader, err := d.CreateRem(u, nil, "Causes of World War I ==>", nil)
	if err != nil {
		t.Fatalf("failed to create list header: %v", err)
	}

	// Initially no items, so no cards yet
	cards, _ := d.GetCramCards(u, &listHeader.ID, 10)
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards before children are added, got %d", len(cards))
	}

	// Add child bullets
	c1, _ := d.CreateRem(u, &listHeader.ID, "Militarism", nil)
	c2, _ := d.CreateRem(u, &listHeader.ID, "Alliances", nil)
	c3, _ := d.CreateRem(u, &listHeader.ID, "Imperialism", nil)
	c4, _ := d.CreateRem(u, &listHeader.ID, "Nationalism", nil)
	_ = c1
	_ = c2
	_ = c3

	// Now 1 list card should exist with all 4 items in the Back
	cards, err = d.GetCramCards(u, &listHeader.ID, 10)
	if err != nil {
		t.Fatalf("GetCramCards failed: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 list card generated from children, got %d", len(cards))
	}
	if cards[0].Front != "Causes of World War I ==>" {
		t.Errorf("unexpected front: %s", cards[0].Front)
	}
	expectedBack := "1. Militarism\n2. Alliances\n3. Imperialism\n4. Nationalism"
	if cards[0].Back != expectedBack {
		t.Errorf("expected back:\n%s\ngot:\n%s", expectedBack, cards[0].Back)
	}

	// Delete one child bullet (Nationalism)
	err = d.DeleteRem(u, c4.ID)
	if err != nil {
		t.Fatalf("failed to delete child: %v", err)
	}

	// List card should automatically update to have 3 items
	cards, _ = d.GetCramCards(u, &listHeader.ID, 10)
	if len(cards) != 1 {
		t.Fatalf("expected 1 list card after child deletion, got %d", len(cards))
	}
	expectedBack3 := "1. Militarism\n2. Alliances\n3. Imperialism"
	if cards[0].Back != expectedBack3 {
		t.Errorf("expected updated back:\n%s\ngot:\n%s", expectedBack3, cards[0].Back)
	}
}

func TestCardPreservationOnFrontEdit(t *testing.T) {
	d := setupTestDB(t)
	u := DefaultUserID

	// Create a card with typo in front
	rem, err := d.CreateRem(u, nil, "Goolang :: Fast language", nil)
	if err != nil {
		t.Fatalf("failed to create rem: %v", err)
	}

	due, _ := d.GetDueCards(u, 10)
	if len(due) != 1 {
		t.Fatalf("expected 1 card, got %d", len(due))
	}
	originalCardID := due[0].ID

	// Review the card with Good
	res, err := d.ReviewCard(u, originalCardID, srs.RatingGood, false)
	if err != nil {
		t.Fatalf("ReviewCard failed: %v", err)
	}
	if res.Card.Reps != 1 || res.Card.State != srs.StateReview {
		t.Fatalf("unexpected state after review: reps=%d, state=%s", res.Card.Reps, res.Card.State)
	}

	// Fix the typo in the Rem front
	fixedContent := "Golang :: Fast language"
	_, err = d.UpdateRem(u, rem.ID, &fixedContent, nil)
	if err != nil {
		t.Fatalf("UpdateRem failed: %v", err)
	}

	// Check that the card ID, Reps, and SRS State were preserved!
	allCards, _ := d.GetCramCards(u, &rem.ID, 10)
	if len(allCards) != 1 {
		t.Fatalf("expected 1 card after edit, got %d", len(allCards))
	}
	c := allCards[0]
	if c.ID != originalCardID {
		t.Errorf("expected card ID to be preserved! original=%s, now=%s", originalCardID, c.ID)
	}
	if c.Front != "Golang" {
		t.Errorf("expected front to be updated to 'Golang', got '%s'", c.Front)
	}
	if c.Reps != 1 {
		t.Errorf("expected Reps=1 to be preserved, got %d", c.Reps)
	}
	if c.State != srs.StateReview {
		t.Errorf("expected State=Review to be preserved, got %s", c.State)
	}
}

func TestCreateRemAfter(t *testing.T) {
	d := setupTestDB(t)
	u := DefaultUserID

	doc, _ := d.CreateRem(u, nil, "Document", nil)
	b1, _ := d.CreateRem(u, &doc.ID, "Bullet 1", nil)
	b2, _ := d.CreateRem(u, &doc.ID, "Bullet 2", nil)

	// Insert new bullet after Bullet 1
	inserted, err := d.CreateRemAfter(u, b1.ID, "Bullet 1.5")
	if err != nil {
		t.Fatalf("CreateRemAfter failed: %v", err)
	}

	tree, _ := d.GetTree(u, &doc.ID)
	if len(tree) != 1 || len(tree[0].Children) != 3 {
		t.Fatalf("expected 3 children under doc, got %+v", tree)
	}

	c := tree[0].Children
	if c[0].ID != b1.ID || c[1].ID != inserted.ID || c[2].ID != b2.ID {
		t.Errorf("bullets in wrong order: [0]=%s, [1]=%s, [2]=%s", c[0].Content, c[1].Content, c[2].Content)
	}
}

func TestLateReferenceResolution(t *testing.T) {
	d := setupTestDB(t)
	u := DefaultUserID

	// Create Note 1 that references [[Artificial Intelligence]] before AI note exists
	note1, err := d.CreateRem(u, nil, "Machine Learning is a subset of [[Artificial Intelligence]]", nil)
	if err != nil {
		t.Fatalf("failed to create note1: %v", err)
	}
	_ = note1

	// Now create the Artificial Intelligence note
	aiNote, err := d.CreateRem(u, nil, "Artificial Intelligence :: Intelligence demonstrated by machines", nil)
	if err != nil {
		t.Fatalf("failed to create aiNote: %v", err)
	}

	// Backlinks for aiNote by ID should resolve note1!
	backlinks, err := d.GetBacklinks(u, aiNote.ID)
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if len(backlinks) != 1 {
		t.Fatalf("expected 1 backlink to aiNote, got %d", len(backlinks))
	}
	if backlinks[0].ID != note1.ID {
		t.Errorf("expected backlink from note1, got %s", backlinks[0].ID)
	}
}

func TestCascadeDeleteFTS5Clean(t *testing.T) {
	d := setupTestDB(t)
	u := DefaultUserID

	parent, _ := d.CreateRem(u, nil, "Top Parent Topic", nil)
	child, _ := d.CreateRem(u, &parent.ID, "Deep Child Quantum Teleportation", nil)
	_ = child

	// Search finds the child
	res, err := d.Search(u, "Quantum", 10)
	if err != nil || len(res) != 1 {
		t.Fatalf("expected 1 search result, got %d (err: %v)", len(res), err)
	}

	// Delete the parent
	err = d.DeleteRem(u, parent.ID)
	if err != nil {
		t.Fatalf("DeleteRem failed: %v", err)
	}

	// Search for child keyword must now return 0 results (no ghost entries in FTS)
	resAfter, err := d.Search(u, "Quantum", 10)
	if err != nil {
		t.Fatalf("Search after delete failed: %v", err)
	}
	if len(resAfter) != 0 {
		t.Fatalf("expected 0 search results after parent cascade delete, got %d ghost entries!", len(resAfter))
	}
}

func TestUserManagementAndAuth(t *testing.T) {
	d := setupTestDB(t)

	has, err := d.HasUsers()
	if err != nil {
		t.Fatalf("HasUsers failed: %v", err)
	}
	if has {
		t.Fatalf("expected no human users initially")
	}

	// 1. Create first user (should become admin)
	admin, err := d.CreateUser("admin", "admin@remgo.dev", "adminpass123", "")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if admin.Role != "admin" {
		t.Errorf("expected first user to be admin, got %s", admin.Role)
	}

	// 2. Duplicate checks
	_, err = d.CreateUser("admin", "other@remgo.dev", "pass123", "")
	if err != ErrUsernameTaken {
		t.Errorf("expected ErrUsernameTaken, got %v", err)
	}
	_, err = d.CreateUser("other", "admin@remgo.dev", "pass123", "")
	if err != ErrEmailTaken {
		t.Errorf("expected ErrEmailTaken, got %v", err)
	}

	// 3. Create second user (should become regular user)
	bob, err := d.CreateUser("bob", "bob@remgo.dev", "bobpass123", "")
	if err != nil {
		t.Fatalf("failed to create bob: %v", err)
	}
	if bob.Role != "user" {
		t.Errorf("expected second user to be 'user', got %s", bob.Role)
	}

	count, _ := d.CountUsers()
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}

	// 4. Authenticate
	authed, err := d.AuthenticateUser("admin", "adminpass123")
	if err != nil || authed.ID != admin.ID {
		t.Fatalf("Authenticate with username failed: %v", err)
	}
	authedByEmail, err := d.AuthenticateUser("admin@remgo.dev", "adminpass123")
	if err != nil || authedByEmail.ID != admin.ID {
		t.Fatalf("Authenticate with email failed: %v", err)
	}
	_, err = d.AuthenticateUser("admin", "wrongpass")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestSessionManagement(t *testing.T) {
	d := setupTestDB(t)

	user, _ := d.CreateUser("sessionuser", "session@remgo.dev", "mypassword123", "")

	// Create session
	token, session, err := d.CreateSession(user.ID, "TestAgent/1.0", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if token == "" || session.ID == "" {
		t.Fatalf("empty token or session ID")
	}

	// Validate session
	valUser, valSess, err := d.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if valUser.ID != user.ID || valSess.ID != session.ID {
		t.Errorf("user or session mismatch: got %s, %s", valUser.ID, valSess.ID)
	}

	// Delete session
	err = d.DeleteSession(token)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// Validate again (should fail)
	_, _, err = d.ValidateSession(token)
	if err == nil {
		t.Errorf("expected session validation to fail after deletion")
	}
}

func TestAPIKeyManagement(t *testing.T) {
	d := setupTestDB(t)

	user, _ := d.CreateUser("patuser", "pat@remgo.dev", "mypassword123", "")

	// Create API Key
	rawKey, keyRecord, err := d.CreateAPIKey(user.ID, "Cursor IDE", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	if !strings.HasPrefix(rawKey, "remgo_pat_") {
		t.Errorf("expected remgo_pat_ prefix, got %s", rawKey)
	}
	if keyRecord.Name != "Cursor IDE" {
		t.Errorf("expected name 'Cursor IDE', got %s", keyRecord.Name)
	}

	// Validate API Key
	valUser, valKey, err := d.ValidateAPIKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateAPIKey failed: %v", err)
	}
	if valUser.ID != user.ID || valKey.ID != keyRecord.ID {
		t.Errorf("validated user or key ID mismatch")
	}
	if valKey.LastUsedAt == nil {
		t.Errorf("expected LastUsedAt to be populated after validation")
	}

	// List API keys
	keys, err := d.ListAPIKeys(user.ID)
	if err != nil || len(keys) != 1 {
		t.Fatalf("expected 1 key in list, got %d (err: %v)", len(keys), err)
	}

	// Delete API Key
	err = d.DeleteAPIKey(user.ID, keyRecord.ID)
	if err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}

	// Validate again should fail
	_, _, err = d.ValidateAPIKey(rawKey)
	if err == nil {
		t.Errorf("expected API key validation to fail after deletion")
	}
}

func TestMultiUserIsolation(t *testing.T) {
	d := setupTestDB(t)

	userA, _ := d.CreateUser("alice", "alice@test.dev", "alicepass123", "")
	userB, _ := d.CreateUser("bob", "bob@test.dev", "bobpass123", "")

	// 1. User A creates notes and flashcards
	docA, err := d.CreateRem(userA.ID, nil, "Alice Private Diary", nil)
	if err != nil {
		t.Fatalf("failed to create doc for Alice: %v", err)
	}
	_, _ = d.CreateRem(userA.ID, &docA.ID, "Secret formula :: E=mc^2", nil)

	// 2. User B creates notes and flashcards
	docB, err := d.CreateRem(userB.ID, nil, "Bob Knowledge Base", nil)
	if err != nil {
		t.Fatalf("failed to create doc for Bob: %v", err)
	}
	_, _ = d.CreateRem(userB.ID, &docB.ID, "Quantum Physics :: Wave particle duality", nil)

	// 3. User A queries tree -> only sees Alice's notes
	treeA, err := d.GetTree(userA.ID, nil)
	if err != nil {
		t.Fatalf("GetTree Alice failed: %v", err)
	}
	if len(treeA) != 1 || treeA[0].Content != "Alice Private Diary" {
		t.Errorf("Alice saw wrong tree: %+v", treeA)
	}

	// 4. User B queries tree -> only sees Bob's notes
	treeB, err := d.GetTree(userB.ID, nil)
	if err != nil {
		t.Fatalf("GetTree Bob failed: %v", err)
	}
	if len(treeB) != 1 || treeB[0].Content != "Bob Knowledge Base" {
		t.Errorf("Bob saw wrong tree: %+v", treeB)
	}

	// 5. User A tries to GetRem or DeleteRem belonging to Bob -> must not find it
	remCheck, err := d.GetRem(userA.ID, docB.ID)
	if err != nil {
		t.Fatalf("unexpected error querying Bob's rem as Alice: %v", err)
	}
	if remCheck != nil {
		t.Errorf("Alice should not be able to get Bob's rem: %+v", remCheck)
	}

	err = d.DeleteRem(userA.ID, docB.ID)
	if err == nil {
		t.Errorf("Alice should not be able to delete Bob's rem!")
	}

	// 6. User A searches for "Quantum" -> 0 hits; User B searches for "Quantum" -> 1 hit
	searchA, _ := d.Search(userA.ID, "Quantum", 10)
	if len(searchA) != 0 {
		t.Errorf("Alice saw Bob's search hit: %+v", searchA)
	}
	searchB, _ := d.Search(userB.ID, "Quantum", 10)
	if len(searchB) != 1 {
		t.Errorf("Bob failed to find his own note: %+v", searchB)
	}

	// 7. Flashcards isolation
	dueA, _ := d.GetDueCards(userA.ID, 10)
	if len(dueA) != 1 || dueA[0].Front != "Secret formula" {
		t.Errorf("Alice due cards mismatch: %+v", dueA)
	}
	dueB, _ := d.GetDueCards(userB.ID, 10)
	if len(dueB) != 1 || dueB[0].Front != "Quantum Physics" {
		t.Errorf("Bob due cards mismatch: %+v", dueB)
	}

	// 8. Cross-tenant parent protection on CreateRem and MoveRem
	_, err = d.CreateRem(userA.ID, &docB.ID, "Intrusion attempt", nil)
	if err == nil {
		t.Errorf("Alice should not be allowed to create a child under Bob's doc!")
	}

	aliceNote, err := d.CreateRem(userA.ID, nil, "Alice Standalone Note", nil)
	if err != nil {
		t.Fatalf("failed to create alice note: %v", err)
	}
	err = d.MoveRem(userA.ID, aliceNote.ID, &docB.ID, 0)
	if err == nil {
		t.Errorf("Alice should not be allowed to move her note under Bob's doc!")
	}
}

func TestAuthenticateTokenUnified(t *testing.T) {
	d := setupTestDB(t)

	user, err := d.CreateUser("authuser", "authuser@remgo.dev", "password123", "")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// 1. Validate via Personal Access Token
	pat, _, err := d.CreateAPIKey(user.ID, "Test PAT", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	uPAT, err := d.AuthenticateToken(pat)
	if err != nil || uPAT.ID != user.ID {
		t.Fatalf("AuthenticateToken with PAT failed: %v", err)
	}

	// 2. Validate via Session Token
	sessToken, _, err := d.CreateSession(user.ID, "agent", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	uSess, err := d.AuthenticateToken(sessToken)
	if err != nil || uSess.ID != user.ID {
		t.Fatalf("AuthenticateToken with Session failed: %v", err)
	}

	// 3. Validate via HS256 JWT
	jwtSecret := []byte("super-secret-jwt-key-for-test-32b")
	jwt, err := auth.CreateJWT(jwtSecret, auth.JWTClaims{
		UserID:    user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      user.Role,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("CreateJWT failed: %v", err)
	}
	uJWT, err := d.AuthenticateToken(jwt, jwtSecret)
	if err != nil || uJWT.ID != user.ID {
		t.Fatalf("AuthenticateToken with JWT failed: %v", err)
	}

	// 4. Invalid token returns error
	_, err = d.AuthenticateToken("invalid_garbage_token")
	if err == nil {
		t.Errorf("expected error for invalid token")
	}

	// 5. Expired session returns error
	expSessToken, _, err := d.CreateSession(user.ID, "agent", "127.0.0.1", -time.Hour)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	_, err = d.AuthenticateToken(expSessToken)
	if err == nil {
		t.Errorf("expected error for expired session token")
	}
}

func TestDBIsEmptyAndDeleteUser(t *testing.T) {
	d := setupTestDB(t)

	// In clean DB without rems, IsEmpty should be true
	if !d.IsEmpty() {
		t.Errorf("expected IsEmpty to be true on clean DB")
	}

	// Create note -> IsEmpty should be false
	rem, err := d.CreateRem(DefaultUserID, nil, "Sample Note", nil)
	if err != nil {
		t.Fatalf("CreateRem failed: %v", err)
	}
	if d.IsEmpty() {
		t.Errorf("expected IsEmpty to be false after creating note")
	}

	// Clean up rem
	_ = d.DeleteRem(DefaultUserID, rem.ID)
	if !d.IsEmpty() {
		t.Errorf("expected IsEmpty to be true after deleting note")
	}

	// Cannot delete DefaultUserID
	err = d.DeleteUser(DefaultUserID)
	if err == nil {
		t.Errorf("expected error when deleting DefaultUserID")
	}

	// Can delete custom user
	customUser, _ := d.CreateUser("deleteme", "del@remgo.dev", "delpass123", "")
	err = d.DeleteUser(customUser.ID)
	if err != nil {
		t.Errorf("failed to delete custom user: %v", err)
	}
}
