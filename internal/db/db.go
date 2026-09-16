package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darsheee/remgo/internal/srs"
	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection with thread-safety and multi-tenant business logic.
type DB struct {
	sqlDB *sql.DB
	srs   *srs.FSRS
	mu    sync.RWMutex
}

// Rem represents a bullet node in the outliner tree.
type Rem struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id,omitempty"`
	ParentID  *string   `json:"parent_id,omitempty"`
	Content   string    `json:"content"`
	Collapsed bool      `json:"collapsed"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RemTreeNode represents a Rem in the hierarchical tree structure.
type RemTreeNode struct {
	Rem
	Depth      int            `json:"depth"`
	Path       string         `json:"path"`
	CardCount  int            `json:"card_count"`
	ChildCount int            `json:"child_count"`
	Children   []*RemTreeNode `json:"children"`
}

// CardWithRem represents a flashcard with associated Rem content and breadcrumbs.
type CardWithRem struct {
	ID             string                 `json:"id"`
	UserID         string                 `json:"user_id,omitempty"`
	RemID          string                 `json:"rem_id"`
	RemContent     string                 `json:"rem_content"`
	CardType       string                 `json:"card_type"`
	Front          string                 `json:"front"`
	Back           string                 `json:"back"`
	ClozeIndex     int                    `json:"cloze_index,omitempty"`
	Hint           string                 `json:"hint,omitempty"`
	State          srs.State              `json:"state"`
	StateName      string                 `json:"state_name"`
	Stability      float64                `json:"stability"`
	Difficulty     float64                `json:"difficulty"`
	Reps           int                    `json:"reps"`
	Lapses         int                    `json:"lapses"`
	LastReviewedAt *time.Time             `json:"last_reviewed_at,omitempty"`
	DueAt          time.Time              `json:"due_at"`
	CreatedAt      time.Time              `json:"created_at"`
	Breadcrumbs    []string               `json:"breadcrumbs,omitempty"`
	NextPreviews   *srs.NextStatesPreview `json:"next_previews,omitempty"`
}

// SearchResult represents a full-text search hit with context breadcrumbs.
type SearchResult struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`
	Snippet     string    `json:"snippet"`
	Breadcrumbs []string  `json:"breadcrumbs"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CardStats provides aggregate SRS statistics.
type CardStats struct {
	TotalCards    int `json:"total_cards"`
	NewCards      int `json:"new_cards"`
	LearningCards int `json:"learning_cards"`
	ReviewCards   int `json:"review_cards"`
	DueToday      int `json:"due_today"`
	ReviewedToday int `json:"reviewed_today"`
}

// Open initializes or connects to an embedded SQLite database with WAL mode enabled.
func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=recursive_triggers(ON)&_pragma=synchronous(NORMAL)", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// SQLite WAL mode handles concurrent readers with serialized writers
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(0)

	// 1. Create base tables
	if _, err := sqlDB.Exec(SchemaTablesSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to execute tables schema: %w", err)
	}

	// 2. Migrate existing tables (add user_id column if missing)
	if err := migrateSchema(sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	// 3. Create indexes (now guaranteed that user_id column exists)
	if _, err := sqlDB.Exec(SchemaIndexesSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to execute indexes schema: %w", err)
	}

	return &DB{
		sqlDB: sqlDB,
		srs:   srs.New(),
	}, nil
}

// Close closes the underlying SQLite database.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sqlDB.Close()
}

// IsEmpty returns true if the database contains no rems across all users.
func (d *DB) IsEmpty() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var count int
	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM rems").Scan(&count)
	return count == 0
}

// migrateSchema handles upgrades from single-user legacy databases.
func migrateSchema(s *sql.DB) error {
	// 1. Ensure default user exists
	_, err := s.Exec(`
		INSERT OR IGNORE INTO users(id, username, email, password_hash, role, created_at, updated_at)
		VALUES ('usr_default', 'default', 'default@remgo.local', '', 'admin', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
	`)
	if err != nil {
		return fmt.Errorf("failed to insert default user: %w", err)
	}

	// Helper to check if a column exists in a table
	hasColumn := func(tableName, colName string) (bool, error) {
		rows, err := s.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
		if err != nil {
			return false, err
		}
		defer rows.Close()

		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return false, err
			}
			if strings.EqualFold(name, colName) {
				return true, nil
			}
		}
		return false, nil
	}

	// Migrate tables if user_id is missing
	tables := []string{"rems", "cards", "card_reviews", "references_map"}
	for _, table := range tables {
		hasCol, err := hasColumn(table, "user_id")
		if err != nil {
			return fmt.Errorf("failed to inspect %s: %w", table, err)
		}
		if !hasCol {
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN user_id TEXT NOT NULL DEFAULT 'usr_default';", table)
			if _, err := s.Exec(alterSQL); err != nil {
				return fmt.Errorf("failed to add user_id to %s: %w", table, err)
			}
		}
	}

	return nil
}
