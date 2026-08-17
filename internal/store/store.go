// Package store is the SQLite userfile (WAL, busy_timeout 5s).
// modernc is pure Go; keep MaxOpenConns modest so writers do not pile up.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB   *sql.DB
	path string
	lock *os.File
}

func Open(path string) (*Store, error) {
	if err := secureDatabaseFiles(path); err != nil {
		return nil, err
	}
	var lock *os.File
	if path != "" && path != ":memory:" {
		f, err := acquireLock(path)
		if err != nil {
			return nil, err
		}
		lock = f
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		if lock != nil {
			_ = lock.Close()
		}
		return nil, err
	}
	db.SetMaxOpenConns(8) // modernc: a high cap plus WAL writers can stall
	s := &Store{DB: db, path: path, lock: lock}
	if err := s.migrate(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := secureDatabaseFiles(path); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func secureDatabaseFiles(path string) error {
	if path == "" || path == ":memory:" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("secure database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close database file: %w", err)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(name, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("secure database file %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	err := s.DB.Close()
	if s.lock != nil {
		_ = s.lock.Close()
		s.lock = nil
	}
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
    handle TEXT PRIMARY KEY,
    password TEXT NOT NULL DEFAULT '',
    global_flags TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    last_seen INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS hosts (
    id INTEGER PRIMARY KEY,
    handle TEXT NOT NULL REFERENCES users(handle) ON DELETE CASCADE,
    mask TEXT NOT NULL,
    UNIQUE(handle, mask)
);

CREATE TABLE IF NOT EXISTS channel_flags (
    handle TEXT NOT NULL REFERENCES users(handle) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    flags TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (handle, channel)
);

CREATE TABLE IF NOT EXISTS channels (
    name TEXT PRIMARY KEY,
    chanset TEXT NOT NULL DEFAULT '',
    topic TEXT NOT NULL DEFAULT '',
    greet TEXT NOT NULL DEFAULT '',
    llm_persona TEXT NOT NULL DEFAULT '',
    need_op TEXT NOT NULL DEFAULT '',
    autojoin INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS bans (
    id INTEGER PRIMARY KEY,
    channel TEXT NOT NULL,
    mask TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    setter TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE(channel, mask)
);

CREATE TABLE IF NOT EXISTS seen (
    nick TEXT PRIMARY KEY,
    handle TEXT NOT NULL DEFAULT '',
    account TEXT NOT NULL DEFAULT '',
    host TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT '',
    event TEXT NOT NULL DEFAULT '',
    last_text TEXT NOT NULL DEFAULT '',
    seen_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS notes (
    id INTEGER PRIMARY KEY,
    to_handle TEXT NOT NULL,
    from_handle TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    read INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS quotes (
    id INTEGER PRIMARY KEY,
    channel TEXT NOT NULL,
    nick TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_audit (
    id INTEGER PRIMARY KEY,
    handle TEXT NOT NULL,
    channel TEXT NOT NULL,
    tool TEXT NOT NULL,
    args TEXT NOT NULL,
    result TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS kv (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func Now() int64 { return time.Now().Unix() }
