// Package auth handles magic-link authentication and session management.
package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	CookieName   = "van_session"

	// Scout tier limits
	ScoutMaxWatchboards    = 2
	ScoutMaxAlerts         = 5
	ScoutMaxConditions     = 3
	ScoutMaxFiresPerMonth  = 10
)

var (
	ErrTokenNotFound = errors.New("token not found")
	ErrTokenExpired  = errors.New("token expired")
	ErrTokenUsed     = errors.New("token already used")
	ErrRateLimited   = errors.New("a sign-in link was sent recently; check your email")
)

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RequestMagicLink creates a magic link token for email.
// Returns ErrRateLimited if one was sent within the last 5 minutes.
func RequestMagicLink(db *sql.DB, email string) (token string, err error) {
	// Rate-limit: one link per email per 5 minutes.
	var recent int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM magic_links
		 WHERE email=? AND created_at > datetime('now','-5 minutes') AND used_at IS NULL`,
		email,
	).Scan(&recent)
	if err != nil {
		return "", fmt.Errorf("rate-limit check: %w", err)
	}
	if recent > 0 {
		return "", ErrRateLimited
	}

	token, err = randomHex(32)
	if err != nil {
		return "", err
	}
	// Store expires_at via SQLite datetime expression — avoids Go/SQLite format mismatch.
	_, err = db.Exec(
		`INSERT INTO magic_links (token, email, expires_at)
		 VALUES (?, ?, datetime('now', '+15 minutes'))`,
		token, email,
	)
	return token, err
}

// VerifyMagicLink validates the token, upserts the user, creates a session,
// and returns the session token.
func VerifyMagicLink(db *sql.DB, token string) (sessionToken string, err error) {
	var email string
	var usedAt sql.NullString

	// Expiry is checked in SQL using SQLite's own clock — no Go time parsing needed.
	err = db.QueryRow(
		`SELECT email, used_at FROM magic_links
		 WHERE token=? AND expires_at > datetime('now')`,
		token,
	).Scan(&email, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Distinguish between "never existed" and "expired / already used".
		var exists int
		_ = db.QueryRow(`SELECT COUNT(*) FROM magic_links WHERE token=?`, token).Scan(&exists)
		if exists == 0 {
			return "", ErrTokenNotFound
		}
		return "", ErrTokenExpired
	}
	if err != nil {
		return "", err
	}
	if usedAt.Valid {
		return "", ErrTokenUsed
	}

	// Mark used.
	if _, err = db.Exec(
		`UPDATE magic_links SET used_at=datetime('now') WHERE token=?`, token,
	); err != nil {
		return "", err
	}

	// Upsert user.
	userID, err := randomHex(16)
	if err != nil {
		return "", err
	}
	_, err = db.Exec(
		`INSERT INTO users (id, email) VALUES (?,?)
		 ON CONFLICT(email) DO NOTHING`,
		userID, email,
	)
	if err != nil {
		return "", err
	}
	// Fetch real user ID (may differ if already existed).
	if err = db.QueryRow(`SELECT id FROM users WHERE email=?`, email).Scan(&userID); err != nil {
		return "", err
	}

	// Create session — 30-day expiry stored via SQLite datetime expression.
	sessionToken, err = randomHex(32)
	if err != nil {
		return "", err
	}
	_, err = db.Exec(
		`INSERT INTO sessions (token, user_id, expires_at)
		 VALUES (?, ?, datetime('now', '+30 days'))`,
		sessionToken, userID,
	)
	return sessionToken, err
}

// ValidateSession returns the user ID for a valid, unexpired session token.
func ValidateSession(db *sql.DB, token string) (userID string, err error) {
	err = db.QueryRow(
		`SELECT user_id FROM sessions WHERE token=? AND expires_at > datetime('now')`,
		token,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTokenNotFound
	}
	return userID, err
}

// DeleteSession removes a session (logout).
func DeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}

// PruneExpired removes expired sessions and magic links (call periodically).
func PruneExpired(db *sql.DB) error {
	if _, err := db.Exec(`DELETE FROM sessions WHERE expires_at <= datetime('now')`); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM magic_links WHERE expires_at <= datetime('now')`)
	return err
}

// User is a minimal user record.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Tier      string `json:"tier"`
	CreatedAt string `json:"created_at"`
}

// GetUser fetches a user by ID.
func GetUser(db *sql.DB, userID string) (*User, error) {
	u := &User{}
	err := db.QueryRow(
		`SELECT id, email, tier, created_at FROM users WHERE id=?`, userID,
	).Scan(&u.ID, &u.Email, &u.Tier, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}
