package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darsheee/remgo/internal/srs"
	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection with thread-safety and business logic.
type DB struct {
	sqlDB *sql.DB
	srs   *srs.FSRS
	mu    sync.RWMutex
}

// Rem represents a bullet node in the outliner tree.
type Rem struct {
	ID        string    `json:"id"`
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

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// SQLite WAL mode handles concurrent readers with serialized writers
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(0)

	if _, err := sqlDB.Exec(SchemaSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to execute schema: %w", err)
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
