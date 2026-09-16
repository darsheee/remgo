package db

const SchemaSQL = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS rems (
    id TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES rems(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    collapsed INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rems_parent ON rems(parent_id, sort_order);
CREATE INDEX IF NOT EXISTS idx_rems_updated ON rems(updated_at DESC);

CREATE TABLE IF NOT EXISTS cards (
    id TEXT PRIMARY KEY,
    rem_id TEXT NOT NULL REFERENCES rems(id) ON DELETE CASCADE,
    card_type TEXT NOT NULL,
    front TEXT NOT NULL,
    back TEXT NOT NULL,
    cloze_index INTEGER NOT NULL DEFAULT 0,
    hint TEXT NOT NULL DEFAULT '',
    state INTEGER NOT NULL DEFAULT 0,
    stability REAL NOT NULL DEFAULT 0,
    difficulty REAL NOT NULL DEFAULT 0,
    reps INTEGER NOT NULL DEFAULT 0,
    lapses INTEGER NOT NULL DEFAULT 0,
    last_reviewed_at DATETIME,
    due_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cards_rem_id ON cards(rem_id);
CREATE INDEX IF NOT EXISTS idx_cards_due ON cards(due_at ASC, state DESC);

CREATE TABLE IF NOT EXISTS card_reviews (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    rating INTEGER NOT NULL,
    state INTEGER NOT NULL,
    stability REAL NOT NULL,
    difficulty REAL NOT NULL,
    elapsed_days REAL NOT NULL,
    scheduled_days REAL NOT NULL,
    reviewed_at DATETIME NOT NULL,
    is_cram INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_reviews_card_id ON card_reviews(card_id, reviewed_at DESC);

CREATE TABLE IF NOT EXISTS references_map (
    id TEXT PRIMARY KEY,
    source_rem_id TEXT NOT NULL REFERENCES rems(id) ON DELETE CASCADE,
    target_title TEXT NOT NULL,
    target_rem_id TEXT REFERENCES rems(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_refs_source ON references_map(source_rem_id);
CREATE INDEX IF NOT EXISTS idx_refs_title ON references_map(target_title);
CREATE INDEX IF NOT EXISTS idx_refs_target ON references_map(target_rem_id);

-- FTS5 Virtual Table for full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS rems_fts USING fts5(
    content,
    rem_id UNINDEXED,
    tokenize = 'porter unicode61'
);

-- Triggers to keep FTS5 synchronized with rems table
CREATE TRIGGER IF NOT EXISTS rems_fts_ai AFTER INSERT ON rems BEGIN
    INSERT INTO rems_fts(rowid, content, rem_id) VALUES (new.rowid, new.content, new.id);
END;

CREATE TRIGGER IF NOT EXISTS rems_fts_ad AFTER DELETE ON rems BEGIN
    DELETE FROM rems_fts WHERE rowid = old.rowid;
END;

CREATE TRIGGER IF NOT EXISTS rems_fts_au AFTER UPDATE ON rems BEGIN
    DELETE FROM rems_fts WHERE rowid = old.rowid;
    INSERT INTO rems_fts(rowid, content, rem_id) VALUES (new.rowid, new.content, new.id);
END;
`
