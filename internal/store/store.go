// Package store persists credentials, accounts and upload jobs in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

const maxBackups = 5

// Store wraps a single-connection writer and a reader pool over one database file.
type Store struct {
	path string
	w    *sql.DB
	r    *sql.DB
}

// Open opens (creating if needed) the database at path and applies pending migrations,
// backing up a populated database first.
func Open(path string) (*Store, error) {
	return openWith(path, migrations)
}

func openWith(path string, migs []migration) (*Store, error) {
	const pragmas = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	w, err := sql.Open("sqlite", path+pragmas+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", path+pragmas)
	if err != nil {
		w.Close()
		return nil, err
	}
	s := &Store{path: path, w: w, r: r}
	if err := s.migrate(migs); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Close closes both handles.
func (s *Store) Close() error {
	return errors.Join(s.r.Close(), s.w.Close())
}

func (s *Store) version() (int, error) {
	var v int
	err := s.w.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

func (s *Store) migrate(migs []migration) error {
	cur, err := s.version()
	if err != nil {
		return err
	}
	if cur >= len(migs) {
		return nil
	}
	if cur > 0 {
		if err := s.backup(cur); err != nil {
			return fmt.Errorf("back up database before migrating: %w", err)
		}
	}
	for i := cur; i < len(migs); i++ {
		tx, err := s.w.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migs[i].sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", migs[i].name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// backup writes a consistent copy with VACUUM INTO and keeps the newest maxBackups files.
func (s *Store) backup(fromVersion int) error {
	dir := filepath.Join(filepath.Dir(s.path), "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := filepath.Join(dir, fmt.Sprintf("social-mcp-%d-v%d.db", time.Now().UnixNano(), fromVersion))
	if _, err := s.w.Exec("VACUUM INTO ?", name); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // names embed a nanosecond timestamp, so lexical order is age order
	for len(names) > maxBackups {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

// GetMeta reads a key from the meta table.
func (s *Store) GetMeta(key string) (string, bool, error) {
	var v string
	err := s.r.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetMeta writes a key to the meta table.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.w.Exec("INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// ms converts a time to unix milliseconds, with the zero time stored as NULL.
func ms(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func fromMs(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64)
}

func nowMs() int64 { return time.Now().UnixMilli() }
