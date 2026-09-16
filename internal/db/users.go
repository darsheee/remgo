package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darsheee/remgo/internal/auth"
	"github.com/google/uuid"
)

const (
	DefaultUserID   = "usr_default"
	DefaultUsername = "default"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid username/email or password")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidInput       = errors.New("invalid user input")
)

// User represents a registered RemGo user account.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"` // "admin" or "user"
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Session represents an active login session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	UserAgent string    `json:"user_agent,omitempty"`
	IPAddress string    `json:"ip_address,omitempty"`
}

// APIKey represents a Personal Access Token (PAT).
type APIKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	KeyHash    string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// CreateUser registers a new user with bcrypt password hashing.
func (d *DB) CreateUser(username, email, password, role string) (*User, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	username = strings.TrimSpace(username)
	email = strings.TrimSpace(strings.ToLower(email))
	if len(username) < 3 || len(username) > 50 {
		return nil, fmt.Errorf("%w: username must be between 3 and 50 characters", ErrInvalidInput)
	}
	if len(email) < 3 || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%w: invalid email address", ErrInvalidInput)
	}
	if len(password) < 6 {
		return nil, fmt.Errorf("%w: password must be at least 6 characters", ErrInvalidInput)
	}

	// Check if this will be the first non-default user
	var existingCount int
	_ = d.sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id != ?", DefaultUserID).Scan(&existingCount)
	if existingCount == 0 {
		role = "admin"
	} else if role == "" {
		role = "user"
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now().UTC()
	userID := "usr_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO users(id, username, email, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, username, email, hash, role, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.username") {
			return nil, ErrUsernameTaken
		}
		if strings.Contains(err.Error(), "UNIQUE constraint failed: users.email") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("failed to insert user: %w", err)
	}

	// If this is the first human admin user, migrate any starter notes from DefaultUserID
	if existingCount == 0 {
		_, _ = tx.Exec("UPDATE rems SET user_id = ? WHERE user_id = ?", userID, DefaultUserID)
		_, _ = tx.Exec("UPDATE cards SET user_id = ? WHERE user_id = ?", userID, DefaultUserID)
		_, _ = tx.Exec("UPDATE card_reviews SET user_id = ? WHERE user_id = ?", userID, DefaultUserID)
		_, _ = tx.Exec("UPDATE references_map SET user_id = ? WHERE user_id = ?", userID, DefaultUserID)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit user creation: %w", err)
	}

	return &User{
		ID:           userID,
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// GetUserByID fetches a user by primary ID.
func (d *DB) GetUserByID(id string) (*User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var u User
	err := d.sqlDB.QueryRow(
		`SELECT id, username, email, password_hash, role, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// GetUserByUsernameOrEmail finds a user by username or email (case-insensitive).
func (d *DB) GetUserByUsernameOrEmail(identifier string) (*User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	trimmed := strings.TrimSpace(identifier)
	var u User
	err := d.sqlDB.QueryRow(
		`SELECT id, username, email, password_hash, role, created_at, updated_at
		 FROM users WHERE username = ? OR email = ? COLLATE NOCASE LIMIT 1`,
		trimmed, strings.ToLower(trimmed),
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// AuthenticateUser verifies user credentials and returns the User record.
func (d *DB) AuthenticateUser(identifier, password string) (*User, error) {
	user, err := d.GetUserByUsernameOrEmail(identifier)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}

	if !auth.CheckPassword(user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// CountUsers returns the number of registered human users (excluding DefaultUserID).
func (d *DB) CountUsers() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.sqlDB.QueryRow("SELECT COUNT(*) FROM users WHERE id != ?", DefaultUserID).Scan(&count)
	return count, err
}

// HasUsers checks if any human user exists.
func (d *DB) HasUsers() (bool, error) {
	count, err := d.CountUsers()
	return count > 0, err
}

// ListUsers returns all users in the system.
func (d *DB) ListUsers() ([]*User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.sqlDB.Query(
		`SELECT id, username, email, password_hash, role, created_at, updated_at
		 FROM users WHERE id != ? ORDER BY created_at ASC`, DefaultUserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, nil
}

// DeleteUser removes a user and cascades all their notes, cards, and keys.
func (d *DB) DeleteUser(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.sqlDB.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

// CreateSession creates a cryptographic session token stored as a SHA-256 hash.
func (d *DB) CreateSession(userID, userAgent, ipAddress string, duration time.Duration) (string, *Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rawToken, err := auth.GenerateSecureToken(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	tokenHash := auth.HashToken(rawToken)

	now := time.Now().UTC()
	expiresAt := now.Add(duration)
	sessionID := "sess_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]

	_, err = d.sqlDB.Exec(
		`INSERT INTO sessions(id, user_id, token_hash, expires_at, created_at, user_agent, ip_address)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, userID, tokenHash, expiresAt, now, userAgent, ipAddress,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to insert session: %w", err)
	}

	session := &Session{
		ID:        sessionID,
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UserAgent: userAgent,
		IPAddress: ipAddress,
	}
	return rawToken, session, nil
}

// ValidateSession verifies a raw session token against the database.
func (d *DB) ValidateSession(rawToken string) (*User, *Session, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	tokenHash := auth.HashToken(rawToken)
	now := time.Now().UTC()

	var u User
	var s Session

	query := `
	SELECT s.id, s.user_id, s.token_hash, s.expires_at, s.created_at, s.user_agent, s.ip_address,
	       u.id, u.username, u.email, u.password_hash, u.role, u.created_at, u.updated_at
	FROM sessions s
	JOIN users u ON s.user_id = u.id
	WHERE s.token_hash = ? AND s.expires_at > ?
	LIMIT 1;`

	var ua, ip sql.NullString
	err := d.sqlDB.QueryRow(query, tokenHash, now).Scan(
		&s.ID, &s.UserID, &s.TokenHash, &s.ExpiresAt, &s.CreatedAt, &ua, &ip,
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, auth.ErrInvalidToken
		}
		return nil, nil, err
	}

	if ua.Valid {
		s.UserAgent = ua.String
	}
	if ip.Valid {
		s.IPAddress = ip.String
	}

	return &u, &s, nil
}

// DeleteSession invalidates a specific session.
func (d *DB) DeleteSession(rawToken string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tokenHash := auth.HashToken(rawToken)
	_, err := d.sqlDB.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

// DeleteUserSessions invalidates all sessions for a user (e.g. on password reset).
func (d *DB) DeleteUserSessions(userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.sqlDB.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

// CreateAPIKey generates a Personal Access Token (remgo_pat_...) for programmatic and MCP access.
func (d *DB) CreateAPIKey(userID, name string, expiresAt *time.Time) (string, *APIKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		name = "Personal Access Token"
	}

	rawPAT, err := auth.GeneratePAT()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate PAT: %w", err)
	}

	keyHash := auth.HashToken(rawPAT)
	keyPrefix := rawPAT[:14] + "..." // e.g. "remgo_pat_a1b2..."
	id := "key_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	now := time.Now().UTC()

	_, err = d.sqlDB.Exec(
		`INSERT INTO api_keys(id, user_id, name, key_prefix, key_hash, created_at, last_used_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL, ?)`,
		id, userID, name, keyPrefix, keyHash, now, expiresAt,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to insert api key: %w", err)
	}

	apiKey := &APIKey{
		ID:        id,
		UserID:    userID,
		Name:      name,
		KeyPrefix: keyPrefix,
		KeyHash:   keyHash,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	return rawPAT, apiKey, nil
}

// ValidateAPIKey verifies a Personal Access Token, updates last_used_at, and returns the User.
func (d *DB) ValidateAPIKey(rawKey string) (*User, *APIKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	keyHash := auth.HashToken(rawKey)
	now := time.Now().UTC()

	var u User
	var k APIKey
	var lastUsed, expires sql.NullTime

	query := `
	SELECT k.id, k.user_id, k.name, k.key_prefix, k.key_hash, k.created_at, k.last_used_at, k.expires_at,
	       u.id, u.username, u.email, u.password_hash, u.role, u.created_at, u.updated_at
	FROM api_keys k
	JOIN users u ON k.user_id = u.id
	WHERE k.key_hash = ? AND (k.expires_at IS NULL OR k.expires_at > ?)
	LIMIT 1;`

	err := d.sqlDB.QueryRow(query, keyHash, now).Scan(
		&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.KeyHash, &k.CreatedAt, &lastUsed, &expires,
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, auth.ErrInvalidToken
		}
		return nil, nil, err
	}

	if lastUsed.Valid {
		t := lastUsed.Time
		k.LastUsedAt = &t
	}
	if expires.Valid {
		t := expires.Time
		k.ExpiresAt = &t
	}

	// Update last_used_at asynchronously/inline
	_, _ = d.sqlDB.Exec("UPDATE api_keys SET last_used_at = ? WHERE id = ?", now, k.ID)
	k.LastUsedAt = &now

	return &u, &k, nil
}

// ListAPIKeys returns all active API keys for a user (without exposing key hashes).
func (d *DB) ListAPIKeys(userID string) ([]*APIKey, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.sqlDB.Query(
		`SELECT id, user_id, name, key_prefix, created_at, last_used_at, expires_at
		 FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		var k APIKey
		var lastUsed, expires sql.NullTime
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &lastUsed, &expires); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			k.LastUsedAt = &t
		}
		if expires.Valid {
			t := expires.Time
			k.ExpiresAt = &t
		}
		keys = append(keys, &k)
	}
	return keys, nil
}

// DeleteAPIKey revokes an API key.
func (d *DB) DeleteAPIKey(userID, keyID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.sqlDB.Exec("DELETE FROM api_keys WHERE id = ? AND user_id = ?", keyID, userID)
	return err
}
