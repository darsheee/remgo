package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/darsheee/remgo/internal/parser"
	"github.com/google/uuid"
)

// CreateRem inserts a new Rem and synchronizes its flashcards and references.
func (d *DB) CreateRem(parentID *string, content string, sortOrder *int) (*Rem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().UTC()
	id := "rem_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var order int
	if sortOrder != nil {
		order = *sortOrder
		if parentID == nil {
			_, _ = tx.Exec("UPDATE rems SET sort_order = sort_order + 1 WHERE parent_id IS NULL AND sort_order >= ?", order)
		} else {
			_, _ = tx.Exec("UPDATE rems SET sort_order = sort_order + 1 WHERE parent_id = ? AND sort_order >= ?", *parentID, order)
		}
	} else {
		// Calculate next sort order at current level
		var maxOrder sql.NullInt64
		var query string
		var args []interface{}
		if parentID == nil {
			query = "SELECT MAX(sort_order) FROM rems WHERE parent_id IS NULL"
		} else {
			query = "SELECT MAX(sort_order) FROM rems WHERE parent_id = ?"
			args = append(args, *parentID)
		}
		_ = tx.QueryRow(query, args...).Scan(&maxOrder)
		if maxOrder.Valid {
			order = int(maxOrder.Int64) + 1
		} else {
			order = 0
		}
	}

	_, err = tx.Exec(
		"INSERT INTO rems(id, parent_id, content, collapsed, sort_order, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?, ?)",
		id, parentID, content, order, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert rem: %w", err)
	}

	rem := &Rem{
		ID:        id,
		ParentID:  parentID,
		Content:   content,
		Collapsed: false,
		SortOrder: order,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := d.syncCardsAndRefsTx(tx, rem); err != nil {
		return nil, fmt.Errorf("failed to sync cards: %w", err)
	}

	// If parent has ==> list cards, re-sync parent to include new child
	if parentID != nil {
		var parentContent string
		if err := tx.QueryRow("SELECT content FROM rems WHERE id = ?", *parentID).Scan(&parentContent); err == nil {
			if strings.Contains(parentContent, "==>") {
				pRem := &Rem{ID: *parentID, Content: parentContent}
				_ = d.syncCardsAndRefsTx(tx, pRem)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return rem, nil
}

// CreateRemAfter inserts a new Rem immediately after the specified sibling Rem.
func (d *DB) CreateRemAfter(afterRemID string, content string) (*Rem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	prevRem, err := d.getRemInternal(afterRemID)
	if err != nil || prevRem == nil {
		return nil, fmt.Errorf("rem not found: %s", afterRemID)
	}

	now := time.Now().UTC()
	id := "rem_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	order := prevRem.SortOrder + 1

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if prevRem.ParentID == nil {
		_, _ = tx.Exec("UPDATE rems SET sort_order = sort_order + 1 WHERE parent_id IS NULL AND sort_order >= ?", order)
	} else {
		_, _ = tx.Exec("UPDATE rems SET sort_order = sort_order + 1 WHERE parent_id = ? AND sort_order >= ?", *prevRem.ParentID, order)
	}

	_, err = tx.Exec(
		"INSERT INTO rems(id, parent_id, content, collapsed, sort_order, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?, ?)",
		id, prevRem.ParentID, content, order, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert rem: %w", err)
	}

	rem := &Rem{
		ID:        id,
		ParentID:  prevRem.ParentID,
		Content:   content,
		Collapsed: false,
		SortOrder: order,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := d.syncCardsAndRefsTx(tx, rem); err != nil {
		return nil, fmt.Errorf("failed to sync cards: %w", err)
	}

	// If parent has ==> list cards, re-sync parent to include new child
	if prevRem.ParentID != nil {
		var parentContent string
		if err := tx.QueryRow("SELECT content FROM rems WHERE id = ?", *prevRem.ParentID).Scan(&parentContent); err == nil {
			if strings.Contains(parentContent, "==>") {
				pRem := &Rem{ID: *prevRem.ParentID, Content: parentContent}
				_ = d.syncCardsAndRefsTx(tx, pRem)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return rem, nil
}

// GetRem retrieves a single Rem by ID.
func (d *DB) GetRem(id string) (*Rem, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var r Rem
	var parentID sql.NullString
	var collapsedInt int

	err := d.sqlDB.QueryRow(
		"SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at FROM rems WHERE id = ?",
		id,
	).Scan(&r.ID, &parentID, &r.Content, &collapsedInt, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get rem: %w", err)
	}

	if parentID.Valid {
		p := parentID.String
		r.ParentID = &p
	}
	r.Collapsed = collapsedInt == 1
	return &r, nil
}

// UpdateRem updates content and/or collapsed state of a Rem.
func (d *DB) UpdateRem(id string, content *string, collapsed *bool) (*Rem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rem, err := d.getRemInternal(id)
	if err != nil {
		return nil, err
	}
	if rem == nil {
		return nil, fmt.Errorf("rem not found: %s", id)
	}

	now := time.Now().UTC()
	contentChanged := false
	if content != nil && *content != rem.Content {
		rem.Content = *content
		contentChanged = true
	}
	if collapsed != nil {
		rem.Collapsed = *collapsed
	}
	rem.UpdatedAt = now

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	colInt := 0
	if rem.Collapsed {
		colInt = 1
	}

	_, err = tx.Exec(
		"UPDATE rems SET content = ?, collapsed = ?, updated_at = ? WHERE id = ?",
		rem.Content, colInt, rem.UpdatedAt, rem.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update rem: %w", err)
	}

	if contentChanged {
		if err := d.syncCardsAndRefsTx(tx, rem); err != nil {
			return nil, fmt.Errorf("failed to sync cards: %w", err)
		}
		// If this rem is a child of a list card (==>), re-sync the parent
		if rem.ParentID != nil {
			var parentContent string
			if err := tx.QueryRow("SELECT content FROM rems WHERE id = ?", *rem.ParentID).Scan(&parentContent); err == nil {
				if strings.Contains(parentContent, "==>") {
					pRem := &Rem{ID: *rem.ParentID, Content: parentContent}
					_ = d.syncCardsAndRefsTx(tx, pRem)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit update: %w", err)
	}

	return rem, nil
}

// DeleteRem removes a Rem and all child bullets recursively.
func (d *DB) DeleteRem(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var parentID sql.NullString
	_ = d.sqlDB.QueryRow("SELECT parent_id FROM rems WHERE id = ?", id).Scan(&parentID)

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin delete tx: %w", err)
	}
	defer tx.Rollback()

	query := `
	WITH RECURSIVE rem_sub AS (
		SELECT id FROM rems WHERE id = ?
		UNION ALL
		SELECT r.id FROM rems r JOIN rem_sub s ON r.parent_id = s.id
	)
	DELETE FROM rems WHERE id IN (SELECT id FROM rem_sub);`

	res, err := tx.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete rem: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("rem not found: %s", id)
	}

	if parentID.Valid {
		var pContent string
		if err := tx.QueryRow("SELECT content FROM rems WHERE id = ?", parentID.String).Scan(&pContent); err == nil {
			if strings.Contains(pContent, "==>") {
				pRem := &Rem{ID: parentID.String, Content: pContent}
				_ = d.syncCardsAndRefsTx(tx, pRem)
			}
		}
	}

	return tx.Commit()
}

// IndentRem moves a Rem to be the child of its immediately preceding sibling.
func (d *DB) IndentRem(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	rem, err := d.getRemInternal(id)
	if err != nil || rem == nil {
		return fmt.Errorf("rem not found: %s", id)
	}

	// Find immediately preceding sibling
	var prevID string
	var query string
	var args []interface{}
	if rem.ParentID == nil {
		query = "SELECT id FROM rems WHERE parent_id IS NULL AND sort_order < ? ORDER BY sort_order DESC LIMIT 1"
		args = append(args, rem.SortOrder)
	} else {
		query = "SELECT id FROM rems WHERE parent_id = ? AND sort_order < ? ORDER BY sort_order DESC LIMIT 1"
		args = append(args, *rem.ParentID, rem.SortOrder)
	}

	err = d.sqlDB.QueryRow(query, args...).Scan(&prevID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("cannot indent: no preceding sibling exists")
		}
		return fmt.Errorf("failed to query preceding sibling: %w", err)
	}

	// Find max sort_order in preceding sibling's children
	var maxChildOrder sql.NullInt64
	_ = d.sqlDB.QueryRow("SELECT MAX(sort_order) FROM rems WHERE parent_id = ?", prevID).Scan(&maxChildOrder)
	newOrder := 0
	if maxChildOrder.Valid {
		newOrder = int(maxChildOrder.Int64) + 1
	}

	now := time.Now().UTC()
	tx, err := d.sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin indent tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"UPDATE rems SET parent_id = ?, sort_order = ?, updated_at = ? WHERE id = ?",
		prevID, newOrder, now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update parent during indent: %w", err)
	}

	// Re-sync previous parent if it had ==> list cards
	if rem.ParentID != nil {
		var oldPContent string
		if err := tx.QueryRow("SELECT content FROM rems WHERE id = ?", *rem.ParentID).Scan(&oldPContent); err == nil && strings.Contains(oldPContent, "==>") {
			_ = d.syncCardsAndRefsTx(tx, &Rem{ID: *rem.ParentID, Content: oldPContent})
		}
	}

	// Re-sync new parent if it has ==> list cards
	var newPContent string
	if err := tx.QueryRow("SELECT content FROM rems WHERE id = ?", prevID).Scan(&newPContent); err == nil && strings.Contains(newPContent, "==>") {
		_ = d.syncCardsAndRefsTx(tx, &Rem{ID: prevID, Content: newPContent})
	}

	return tx.Commit()
}

// OutdentRem moves a Rem to become the next sibling of its current parent.
func (d *DB) OutdentRem(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	rem, err := d.getRemInternal(id)
	if err != nil || rem == nil {
		return fmt.Errorf("rem not found: %s", id)
	}

	if rem.ParentID == nil {
		return fmt.Errorf("cannot outdent: rem is already at root level")
	}

	parent, err := d.getRemInternal(*rem.ParentID)
	if err != nil || parent == nil {
		return fmt.Errorf("parent rem not found")
	}

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	// Shift siblings following the parent
	if parent.ParentID == nil {
		_, err = tx.Exec("UPDATE rems SET sort_order = sort_order + 1 WHERE parent_id IS NULL AND sort_order > ?", parent.SortOrder)
	} else {
		_, err = tx.Exec("UPDATE rems SET sort_order = sort_order + 1 WHERE parent_id = ? AND sort_order > ?", *parent.ParentID, parent.SortOrder)
	}
	if err != nil {
		return fmt.Errorf("failed to shift siblings: %w", err)
	}

	now := time.Now().UTC()
	newOrder := parent.SortOrder + 1
	_, err = tx.Exec(
		"UPDATE rems SET parent_id = ?, sort_order = ?, updated_at = ? WHERE id = ?",
		parent.ParentID, newOrder, now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to outdent rem: %w", err)
	}

	// Re-sync parent if it has ==> list cards
	if strings.Contains(parent.Content, "==>") {
		_ = d.syncCardsAndRefsTx(tx, parent)
	}

	return tx.Commit()
}

// MoveRem changes parent and sort order of a Rem.
func (d *DB) MoveRem(id string, targetParentID *string, targetSortOrder int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().UTC()
	_, err := d.sqlDB.Exec(
		"UPDATE rems SET parent_id = ?, sort_order = ?, updated_at = ? WHERE id = ?",
		targetParentID, targetSortOrder, now, id,
	)
	return err
}

// ToggleCollapse flips the collapsed state of a Rem.
func (d *DB) ToggleCollapse(id string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var collapsedInt int
	err := d.sqlDB.QueryRow("SELECT collapsed FROM rems WHERE id = ?", id).Scan(&collapsedInt)
	if err != nil {
		return false, err
	}

	newState := 1
	if collapsedInt == 1 {
		newState = 0
	}

	_, err = d.sqlDB.Exec("UPDATE rems SET collapsed = ?, updated_at = ? WHERE id = ?", newState, time.Now().UTC(), id)
	return newState == 1, err
}

// GetTree returns a hierarchical tree of Rems using a recursive CTE.
func (d *DB) GetTree(rootID *string) ([]*RemTreeNode, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Recursive CTE to select rem tree
	var query string
	var args []interface{}

	if rootID == nil {
		query = `
		WITH RECURSIVE rem_tree AS (
			SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at, 0 as depth,
			       printf('%08d', sort_order) as path
			FROM rems
			WHERE parent_id IS NULL
			UNION ALL
			SELECT r.id, r.parent_id, r.content, r.collapsed, r.sort_order, r.created_at, r.updated_at, t.depth + 1,
			       t.path || '/' || printf('%08d', r.sort_order)
			FROM rems r
			JOIN rem_tree t ON r.parent_id = t.id
		)
		SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at, depth, path
		FROM rem_tree
		ORDER BY path ASC;`
	} else {
		query = `
		WITH RECURSIVE rem_tree AS (
			SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at, 0 as depth,
			       printf('%08d', sort_order) as path
			FROM rems
			WHERE id = ?
			UNION ALL
			SELECT r.id, r.parent_id, r.content, r.collapsed, r.sort_order, r.created_at, r.updated_at, t.depth + 1,
			       t.path || '/' || printf('%08d', r.sort_order)
			FROM rems r
			JOIN rem_tree t ON r.parent_id = t.id
		)
		SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at, depth, path
		FROM rem_tree
		ORDER BY path ASC;`
		args = append(args, *rootID)
	}

	rows, err := d.sqlDB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tree: %w", err)
	}
	defer rows.Close()

	nodesMap := make(map[string]*RemTreeNode)
	var flatNodes []*RemTreeNode

	for rows.Next() {
		var node RemTreeNode
		var parentID sql.NullString
		var collapsedInt int

		err := rows.Scan(
			&node.ID, &parentID, &node.Content, &collapsedInt, &node.SortOrder,
			&node.CreatedAt, &node.UpdatedAt, &node.Depth, &node.Path,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tree row: %w", err)
		}

		if parentID.Valid {
			p := parentID.String
			node.ParentID = &p
		}
		node.Collapsed = collapsedInt == 1
		node.Children = make([]*RemTreeNode, 0)

		nodesMap[node.ID] = &node
		flatNodes = append(flatNodes, &node)
	}

	// Fetch card counts for all rems in this tree
	cardCounts := make(map[string]int)
	cRows, err := d.sqlDB.Query("SELECT rem_id, COUNT(*) FROM cards GROUP BY rem_id")
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var rID string
			var count int
			if err := cRows.Scan(&rID, &count); err == nil {
				cardCounts[rID] = count
			}
		}
	}

	var rootNodes []*RemTreeNode
	for _, node := range flatNodes {
		node.CardCount = cardCounts[node.ID]
		if node.ParentID != nil {
			if parent, exists := nodesMap[*node.ParentID]; exists {
				parent.Children = append(parent.Children, node)
				parent.ChildCount++
				continue
			}
		}
		// If rootID is specified and matches this node, or if parent not in slice
		rootNodes = append(rootNodes, node)
	}

	return rootNodes, nil
}

// GetAncestors returns the chain of parent Rems leading up to root.
func (d *DB) GetAncestors(id string) ([]*Rem, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
	WITH RECURSIVE ancestors AS (
		SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at, 0 as depth
		FROM rems WHERE id = ?
		UNION ALL
		SELECT r.id, r.parent_id, r.content, r.collapsed, r.sort_order, r.created_at, r.updated_at, a.depth + 1
		FROM rems r
		JOIN ancestors a ON r.id = a.parent_id
	)
	SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at
	FROM ancestors
	ORDER BY depth DESC;`

	rows, err := d.sqlDB.Query(query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query ancestors: %w", err)
	}
	defer rows.Close()

	var chain []*Rem
	for rows.Next() {
		var r Rem
		var parentID sql.NullString
		var collapsedInt int
		if err := rows.Scan(&r.ID, &parentID, &r.Content, &collapsedInt, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if parentID.Valid {
			p := parentID.String
			r.ParentID = &p
		}
		r.Collapsed = collapsedInt == 1
		chain = append(chain, &r)
	}
	return chain, nil
}

// Search queries the FTS5 virtual table for lightning-fast matching.
func (d *DB) Search(query string, limit int) ([]*SearchResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	// Format FTS5 query terms with wildcard for prefix matching
	terms := strings.Fields(trimmed)
	var ftsQuery strings.Builder
	for i, t := range terms {
		clean := strings.ReplaceAll(t, `"`, `""`)
		if i > 0 {
			ftsQuery.WriteString(" AND ")
		}
		ftsQuery.WriteString(fmt.Sprintf(`"%s"*`, clean))
	}

	q := `
	SELECT r.id, r.content, snippet(f, 0, '<b>', '</b>', '...', 12) as snippet, r.updated_at
	FROM rems_fts f
	JOIN rems r ON f.rem_id = r.id
	WHERE f MATCH ?
	ORDER BY rank
	LIMIT ?;`

	rows, err := d.sqlDB.Query(q, ftsQuery.String(), limit)
	if err != nil {
		// Fallback to LIKE query if FTS syntax error
		likeQ := `SELECT id, content, content, updated_at FROM rems WHERE content LIKE ? LIMIT ?`
		rows, err = d.sqlDB.Query(likeQ, "%"+trimmed+"%", limit)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		var res SearchResult
		if err := rows.Scan(&res.ID, &res.Content, &res.Snippet, &res.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, &res)
	}

	// Populate breadcrumbs for results
	for _, res := range results {
		ancestors, _ := d.getAncestorsInternal(res.ID)
		for _, a := range ancestors {
			if a.ID != res.ID {
				res.Breadcrumbs = append(res.Breadcrumbs, parser.CleanDelimiters(a.Content))
			}
		}
	}

	return results, nil
}

// GetBacklinks finds all Rems that reference the target by title or ID.
func (d *DB) GetBacklinks(targetIDOrTitle string) ([]*Rem, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	cleanTitle := targetIDOrTitle
	var remContent string
	if err := d.sqlDB.QueryRow("SELECT content FROM rems WHERE id = ?", targetIDOrTitle).Scan(&remContent); err == nil {
		cleanTitle = parser.CleanDelimiters(remContent)
		if parts := strings.SplitN(cleanTitle, "::", 2); len(parts) > 1 {
			cleanTitle = strings.TrimSpace(parts[0])
		}
		if parts := strings.SplitN(cleanTitle, ";;", 2); len(parts) > 1 {
			cleanTitle = strings.TrimSpace(parts[0])
		}
		if parts := strings.SplitN(cleanTitle, "==>", 2); len(parts) > 1 {
			cleanTitle = strings.TrimSpace(parts[0])
		}
	}

	query := `
	SELECT DISTINCT r.id, r.parent_id, r.content, r.collapsed, r.sort_order, r.created_at, r.updated_at
	FROM references_map ref
	JOIN rems r ON ref.source_rem_id = r.id
	WHERE ref.target_rem_id = ? 
	   OR LOWER(ref.target_title) = LOWER(?)
	   OR LOWER(ref.target_title) = LOWER(?)
	   OR ref.target_rem_id IN (SELECT id FROM rems WHERE id = ?)
	ORDER BY r.updated_at DESC;`

	rows, err := d.sqlDB.Query(query, targetIDOrTitle, targetIDOrTitle, cleanTitle, targetIDOrTitle)
	if err != nil {
		return nil, fmt.Errorf("failed to query backlinks: %w", err)
	}
	defer rows.Close()

	var list []*Rem
	for rows.Next() {
		var r Rem
		var pID sql.NullString
		var colInt int
		if err := rows.Scan(&r.ID, &pID, &r.Content, &colInt, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if pID.Valid {
			p := pID.String
			r.ParentID = &p
		}
		r.Collapsed = colInt == 1
		list = append(list, &r)
	}
	return list, nil
}

// Internal helper functions without locking
func (d *DB) getRemInternal(id string) (*Rem, error) {
	var r Rem
	var parentID sql.NullString
	var collapsedInt int

	err := d.sqlDB.QueryRow(
		"SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at FROM rems WHERE id = ?",
		id,
	).Scan(&r.ID, &parentID, &r.Content, &collapsedInt, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if parentID.Valid {
		p := parentID.String
		r.ParentID = &p
	}
	r.Collapsed = collapsedInt == 1
	return &r, nil
}

func (d *DB) getAncestorsInternal(id string) ([]*Rem, error) {
	query := `
	WITH RECURSIVE ancestors AS (
		SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at, 0 as depth
		FROM rems WHERE id = ?
		UNION ALL
		SELECT r.id, r.parent_id, r.content, r.collapsed, r.sort_order, r.created_at, r.updated_at, a.depth + 1
		FROM rems r
		JOIN ancestors a ON r.id = a.parent_id
	)
	SELECT id, parent_id, content, collapsed, sort_order, created_at, updated_at
	FROM ancestors
	ORDER BY depth DESC;`

	rows, err := d.sqlDB.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chain []*Rem
	for rows.Next() {
		var r Rem
		var pID sql.NullString
		var colInt int
		if err := rows.Scan(&r.ID, &pID, &r.Content, &colInt, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if pID.Valid {
			p := pID.String
			r.ParentID = &p
		}
		r.Collapsed = colInt == 1
		chain = append(chain, &r)
	}
	return chain, nil
}
