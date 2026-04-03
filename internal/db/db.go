// Package db opens and migrates the SQLite database.
// Uses modernc.org/sqlite (pure Go, no CGO) so it runs in Alpine / Fly.io.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Open opens (or creates) the SQLite file at path, applies the schema, and
// returns a ready-to-use *sql.DB.  WAL mode and foreign-key enforcement are
// enabled via pragma query parameters.
func Open(path string) (*sql.DB, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db open %q: %w", path, err)
	}
	// SQLite is single-writer; one connection avoids lock contention.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("db migrate: %w", err)
	}
	return db, nil
}
