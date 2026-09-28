// Package store is the SQLite persistence layer: the schema, its migrations,
// and every query the app runs, grouped by domain (one file each).
package store

import (
	"database/sql"
	"errors"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a lookup by id matches no row.
var ErrNotFound = errors.New("not found")

// Store wraps the SQLite database and all queries the app needs.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and brings its schema
// up to date.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// modernc SQLite is happiest with a single writer; keep the pool tight.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON;`); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// HasData reports whether any user data has been recorded.
func (s *Store) HasData() (bool, error) {
	var n int
	err := s.db.QueryRow(`
SELECT (SELECT COUNT(1) FROM contacts) + (SELECT COUNT(1) FROM reminders) +
       (SELECT COUNT(1) FROM interactions) + (SELECT COUNT(1) FROM notes) +
       (SELECT COUNT(1) FROM chat_threads)`).Scan(&n)
	return n > 0, err
}

// nullID maps the app's "0 = no contact" convention onto a SQL NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// likeArg wraps a search term for a case-insensitive LIKE, escaping the
// wildcard characters so "50%" matches literally. Use with ESCAPE '\'.
func likeArg(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(q)) + "%"
}

// notFound turns sql.ErrNoRows into ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// checkAffected returns ErrNotFound when an UPDATE/DELETE touched no row.
func checkAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}
