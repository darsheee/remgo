package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/darsheee/remgo/internal/parser"
	"github.com/darsheee/remgo/internal/srs"
	"github.com/google/uuid"
)

// syncCardsAndRefsTx synchronizes parsed cards and references inside an active transaction.
func (d *DB) syncCardsAndRefsTx(tx *sql.Tx, rem *Rem) error {
	parseResult := parser.ParseContent(rem.Content)
	now := time.Now().UTC()

	// 1. Sync References
	if _, err := tx.Exec("DELETE FROM references_map WHERE source_rem_id = ?", rem.ID); err != nil {
		return fmt.Errorf("failed to clear old references: %w", err)
	}

	for _, ref := range parseResult.References {
		var targetRemID sql.NullString
		_ = tx.QueryRow(
			"SELECT id FROM rems WHERE LOWER(content) LIKE LOWER(?) LIMIT 1",
			ref.TargetTitle+"%",
		).Scan(&targetRemID)

		refID := "ref_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
		var targetIDVal *string
		if targetRemID.Valid {
			v := targetRemID.String
			targetIDVal = &v
		}

		_, err := tx.Exec(
			"INSERT INTO references_map(id, source_rem_id, target_title, target_rem_id, created_at) VALUES (?, ?, ?, ?, ?)",
			refID, rem.ID, ref.TargetTitle, targetIDVal, now,
		)
		if err != nil {
			return fmt.Errorf("failed to insert reference: %w", err)
		}
	}

	// 2. Sync Flashcards
	type existingCard struct {
		id         string
		cardType   string
		front      string
		clozeIndex int
	}

	existingMap := make(map[string]existingCard)
	rows, err := tx.Query(
		"SELECT id, card_type, front, cloze_index FROM cards WHERE rem_id = ?",
		rem.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to query existing cards: %w", err)
	}

	for rows.Next() {
		var ec existingCard
		if err := rows.Scan(&ec.id, &ec.cardType, &ec.front, &ec.clozeIndex); err != nil {
			rows.Close()
			return err
		}
		key := fmt.Sprintf("%s:%s:%d", ec.cardType, ec.front, ec.clozeIndex)
		existingMap[key] = ec
	}
	rows.Close()

	matchedCardIDs := make(map[string]bool)

	for _, pc := range parseResult.Cards {
		key := fmt.Sprintf("%s:%s:%d", pc.Type, pc.Front, pc.ClozeIndex)
		if ec, found := existingMap[key]; found {
			// Update back & hint, preserve SRS history
			_, err := tx.Exec(
				"UPDATE cards SET back = ?, hint = ? WHERE id = ?",
				pc.Back, pc.Hint, ec.id,
			)
			if err != nil {
				return fmt.Errorf("failed to update existing card: %w", err)
			}
			matchedCardIDs[ec.id] = true
		} else {
			// Insert new card
			cardID := "card_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
			initStab := d.srs.InitialStability(srs.RatingGood)
			initDiff := d.srs.InitialDifficulty(srs.RatingGood)

			_, err := tx.Exec(
				`INSERT INTO cards(
					id, rem_id, card_type, front, back, cloze_index, hint,
					state, stability, difficulty, reps, lapses, last_reviewed_at, due_at, created_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
				cardID, rem.ID, string(pc.Type), pc.Front, pc.Back, pc.ClozeIndex, pc.Hint,
				int(srs.StateNew), initStab, initDiff, 0, 0, now, now,
			)
			if err != nil {
				return fmt.Errorf("failed to insert new card: %w", err)
			}
		}
	}

	// Delete obsolete cards that no longer exist in parsed content
	for _, ec := range existingMap {
		if !matchedCardIDs[ec.id] {
			if _, err := tx.Exec("DELETE FROM cards WHERE id = ?", ec.id); err != nil {
				return fmt.Errorf("failed to delete obsolete card: %w", err)
			}
		}
	}

	return nil
}

// GetDueCards returns flashcards that are due for review.
func (d *DB) GetDueCards(limit int) ([]*CardWithRem, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	now := time.Now().UTC()
	query := `
	SELECT c.id, c.rem_id, r.content, c.card_type, c.front, c.back, c.cloze_index, c.hint,
	       c.state, c.stability, c.difficulty, c.reps, c.lapses, c.last_reviewed_at, c.due_at, c.created_at
	FROM cards c
	JOIN rems r ON c.rem_id = r.id
	WHERE c.due_at <= ?
	ORDER BY c.state DESC, c.due_at ASC
	LIMIT ?;`

	rows, err := d.sqlDB.Query(query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query due cards: %w", err)
	}
	defer rows.Close()

	var cards []*CardWithRem
	for rows.Next() {
		var c CardWithRem
		var lastRev sql.NullTime
		var stateInt int

		err := rows.Scan(
			&c.ID, &c.RemID, &c.RemContent, &c.CardType, &c.Front, &c.Back, &c.ClozeIndex, &c.Hint,
			&stateInt, &c.Stability, &c.Difficulty, &c.Reps, &c.Lapses, &lastRev, &c.DueAt, &c.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan due card: %w", err)
		}

		c.State = srs.State(stateInt)
		c.StateName = c.State.String()
		if lastRev.Valid {
			t := lastRev.Time
			c.LastReviewedAt = &t
		}

		// Calculate interval previews for 1, 2, 3, 4
		cardRec := srs.CardRecord{
			ID:             c.ID,
			State:          c.State,
			Stability:      c.Stability,
			Difficulty:     c.Difficulty,
			Reps:           c.Reps,
			Lapses:         c.Lapses,
			LastReviewedAt: c.LastReviewedAt,
			DueAt:          c.DueAt,
		}
		previews := d.srs.PreviewRatings(cardRec, now)
		c.NextPreviews = &previews

		// Breadcrumbs
		ancestors, _ := d.getAncestorsInternal(c.RemID)
		for _, a := range ancestors {
			if a.ID != c.RemID {
				c.Breadcrumbs = append(c.Breadcrumbs, parser.CleanDelimiters(a.Content))
			}
		}

		cards = append(cards, &c)
	}

	return cards, nil
}

// GetCramCards returns cards for on-demand practice without due date constraints.
func (d *DB) GetCramCards(remID *string, limit int) ([]*CardWithRem, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	now := time.Now().UTC()
	var query string
	var args []interface{}

	if remID == nil {
		query = `
		SELECT c.id, c.rem_id, r.content, c.card_type, c.front, c.back, c.cloze_index, c.hint,
		       c.state, c.stability, c.difficulty, c.reps, c.lapses, c.last_reviewed_at, c.due_at, c.created_at
		FROM cards c
		JOIN rems r ON c.rem_id = r.id
		ORDER BY RANDOM()
		LIMIT ?;`
		args = append(args, limit)
	} else {
		query = `
		WITH RECURSIVE rem_sub AS (
			SELECT id FROM rems WHERE id = ?
			UNION ALL
			SELECT r.id FROM rems r JOIN rem_sub s ON r.parent_id = s.id
		)
		SELECT c.id, c.rem_id, r.content, c.card_type, c.front, c.back, c.cloze_index, c.hint,
		       c.state, c.stability, c.difficulty, c.reps, c.lapses, c.last_reviewed_at, c.due_at, c.created_at
		FROM cards c
		JOIN rems r ON c.rem_id = r.id
		WHERE c.rem_id IN (SELECT id FROM rem_sub)
		ORDER BY RANDOM()
		LIMIT ?;`
		args = append(args, *remID, limit)
	}

	rows, err := d.sqlDB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query cram cards: %w", err)
	}
	defer rows.Close()

	var cards []*CardWithRem
	for rows.Next() {
		var c CardWithRem
		var lastRev sql.NullTime
		var stateInt int

		err := rows.Scan(
			&c.ID, &c.RemID, &c.RemContent, &c.CardType, &c.Front, &c.Back, &c.ClozeIndex, &c.Hint,
			&stateInt, &c.Stability, &c.Difficulty, &c.Reps, &c.Lapses, &lastRev, &c.DueAt, &c.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cram card: %w", err)
		}

		c.State = srs.State(stateInt)
		c.StateName = c.State.String()
		if lastRev.Valid {
			t := lastRev.Time
			c.LastReviewedAt = &t
		}

		cardRec := srs.CardRecord{
			ID:             c.ID,
			State:          c.State,
			Stability:      c.Stability,
			Difficulty:     c.Difficulty,
			Reps:           c.Reps,
			Lapses:         c.Lapses,
			LastReviewedAt: c.LastReviewedAt,
			DueAt:          c.DueAt,
		}
		previews := d.srs.PreviewRatings(cardRec, now)
		c.NextPreviews = &previews

		ancestors, _ := d.getAncestorsInternal(c.RemID)
		for _, a := range ancestors {
			if a.ID != c.RemID {
				c.Breadcrumbs = append(c.Breadcrumbs, parser.CleanDelimiters(a.Content))
			}
		}

		cards = append(cards, &c)
	}

	return cards, nil
}

// ReviewCard processes user recall rating and saves the result to the database.
func (d *DB) ReviewCard(cardID string, rating srs.Rating, isCram bool) (*srs.ReviewResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var rec srs.CardRecord
	var stateInt int
	var lastRev sql.NullTime

	err := d.sqlDB.QueryRow(
		"SELECT id, state, stability, difficulty, reps, lapses, last_reviewed_at, due_at FROM cards WHERE id = ?",
		cardID,
	).Scan(&rec.ID, &stateInt, &rec.Stability, &rec.Difficulty, &rec.Reps, &rec.Lapses, &lastRev, &rec.DueAt)
	if err != nil {
		return nil, fmt.Errorf("card not found: %s", cardID)
	}

	rec.State = srs.State(stateInt)
	if lastRev.Valid {
		t := lastRev.Time
		rec.LastReviewedAt = &t
	}

	now := time.Now().UTC()
	result := d.srs.Review(rec, rating, now)

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin review tx: %w", err)
	}
	defer tx.Rollback()

	reviewLogID := "rev_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	cramInt := 0
	if isCram {
		cramInt = 1
	}

	// Insert review log
	_, err = tx.Exec(
		`INSERT INTO card_reviews(
			id, card_id, rating, state, stability, difficulty,
			elapsed_days, scheduled_days, reviewed_at, is_cram
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reviewLogID, cardID, int(rating), int(result.Card.State), result.Card.Stability, result.Card.Difficulty,
		result.ElapsedDays, result.ScheduledDays, now, cramInt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to record review log: %w", err)
	}

	// If NOT cram mode, persist updated SRS state to the card
	if !isCram {
		_, err = tx.Exec(
			`UPDATE cards SET state = ?, stability = ?, difficulty = ?, reps = ?, lapses = ?, last_reviewed_at = ?, due_at = ?
			WHERE id = ?`,
			int(result.Card.State), result.Card.Stability, result.Card.Difficulty,
			result.Card.Reps, result.Card.Lapses, now, result.Card.DueAt, cardID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to update card srs state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit review tx: %w", err)
	}

	return &result, nil
}

// GetCardStats calculates aggregate statistics on cards and reviews.
func (d *DB) GetCardStats() (*CardStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var stats CardStats
	now := time.Now().UTC()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	endOfToday := startOfToday.Add(24 * time.Hour)

	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM cards").Scan(&stats.TotalCards)
	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM cards WHERE state = ?", int(srs.StateNew)).Scan(&stats.NewCards)
	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM cards WHERE state IN (?, ?)", int(srs.StateLearning), int(srs.StateRelearning)).Scan(&stats.LearningCards)
	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM cards WHERE state = ?", int(srs.StateReview)).Scan(&stats.ReviewCards)
	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM cards WHERE due_at <= ?", endOfToday).Scan(&stats.DueToday)
	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM card_reviews WHERE reviewed_at >= ? AND reviewed_at < ?", startOfToday, endOfToday).Scan(&stats.ReviewedToday)

	return &stats, nil
}
