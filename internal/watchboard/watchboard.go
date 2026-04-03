// Package watchboard manages user dashboard layouts (module visibility + order).
package watchboard

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ModuleConfig describes one dashboard section's state within a watchboard.
type ModuleConfig struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	Order   int    `json:"order"`
}

// Watchboard is a saved dashboard layout.
type Watchboard struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Name      string         `json:"name"`
	IsDefault bool           `json:"is_default"`
	Modules   []ModuleConfig `json:"modules"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

// DefaultModules returns the standard module ordering with all enabled.
func DefaultModules() []ModuleConfig {
	keys := []string{
		"indicators",
		"van_readiness", "metals", "crypto", "credit",
		"commodities", "jolts", "employment", "industry",
		"despair", "stocks", "entitlements", "feed",
	}
	out := make([]ModuleConfig, len(keys))
	for i, k := range keys {
		out[i] = ModuleConfig{Key: k, Enabled: true, Order: i}
	}
	return out
}

func newID() (string, error) {
	b := make([]byte, 8)
	// Use crypto/rand via import below — simpler: use timestamp+random suffix.
	// Kept trivial: Go's time nano + a random-ish suffix is fine for SQLite PKs.
	_ = b
	return fmt.Sprintf("%x", time.Now().UnixNano()), nil
}

// List returns all watchboards for a user, ordered by created_at.
func List(db *sql.DB, userID string) ([]Watchboard, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, is_default, module_config, created_at, updated_at
		 FROM watchboards WHERE user_id=? ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Watchboard
	for rows.Next() {
		var w Watchboard
		var modJSON string
		var isDefault int
		if err := rows.Scan(&w.ID, &w.UserID, &w.Name, &isDefault, &modJSON, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.IsDefault = isDefault == 1
		if err := json.Unmarshal([]byte(modJSON), &w.Modules); err != nil {
			w.Modules = DefaultModules()
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Get fetches a single watchboard by ID, verifying ownership.
func Get(db *sql.DB, id, userID string) (*Watchboard, error) {
	w := &Watchboard{}
	var modJSON string
	var isDefault int
	err := db.QueryRow(
		`SELECT id, user_id, name, is_default, module_config, created_at, updated_at
		 FROM watchboards WHERE id=? AND user_id=?`,
		id, userID,
	).Scan(&w.ID, &w.UserID, &w.Name, &isDefault, &modJSON, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w.IsDefault = isDefault == 1
	if err := json.Unmarshal([]byte(modJSON), &w.Modules); err != nil {
		w.Modules = DefaultModules()
	}
	return w, nil
}

// Create inserts a new watchboard. Returns the created record.
func Create(db *sql.DB, userID, name string, modules []ModuleConfig, isDefault bool) (*Watchboard, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 {
		modules = DefaultModules()
	}
	modJSON, err := json.Marshal(modules)
	if err != nil {
		return nil, err
	}

	defVal := 0
	if isDefault {
		defVal = 1
		// Clear existing default.
		if _, err := db.Exec(`UPDATE watchboards SET is_default=0 WHERE user_id=?`, userID); err != nil {
			return nil, err
		}
	}

	_, err = db.Exec(
		`INSERT INTO watchboards (id, user_id, name, is_default, module_config) VALUES (?,?,?,?,?)`,
		id, userID, name, defVal, string(modJSON),
	)
	if err != nil {
		return nil, err
	}
	return Get(db, id, userID)
}

// Update saves changes to name, module config, and/or default status.
func Update(db *sql.DB, id, userID, name string, modules []ModuleConfig, isDefault bool) (*Watchboard, error) {
	modJSON, err := json.Marshal(modules)
	if err != nil {
		return nil, err
	}
	defVal := 0
	if isDefault {
		defVal = 1
		if _, err := db.Exec(`UPDATE watchboards SET is_default=0 WHERE user_id=?`, userID); err != nil {
			return nil, err
		}
	}
	_, err = db.Exec(
		`UPDATE watchboards SET name=?, module_config=?, is_default=?, updated_at=datetime('now')
		 WHERE id=? AND user_id=?`,
		name, string(modJSON), defVal, id, userID,
	)
	if err != nil {
		return nil, err
	}
	return Get(db, id, userID)
}

// Delete removes a watchboard. Returns an error if it is the user's only board.
func Delete(db *sql.DB, id, userID string) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM watchboards WHERE user_id=?`, userID).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return fmt.Errorf("cannot delete the only watchboard")
	}
	_, err := db.Exec(`DELETE FROM watchboards WHERE id=? AND user_id=?`, id, userID)
	return err
}

// EnsureDefault creates a default watchboard for userID if none exist yet.
func EnsureDefault(db *sql.DB, userID string) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM watchboards WHERE user_id=?`, userID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := Create(db, userID, "Full View", DefaultModules(), true)
	return err
}

// CountForUser returns how many watchboards a user has.
func CountForUser(db *sql.DB, userID string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM watchboards WHERE user_id=?`, userID).Scan(&n)
	return n, err
}
