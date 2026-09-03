// Package store is the SQLite userfile (WAL, busy_timeout 5s).
// modernc is pure Go; keep MaxOpenConns modest so writers do not pile up.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const (
	CheckpointPassive  = "PASSIVE"
	CheckpointTruncate = "TRUNCATE"
)

type Store struct {
	DB        *sql.DB
	path      string
	lock      *os.File
	writeErrs atomic.Uint64
}

type CheckpointResult struct {
	Blocked      int
	Log          int
	Checkpointed int
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
	if s.DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = s.Checkpoint(ctx, CheckpointTruncate)
		cancel()
	}
	err := s.DB.Close()
	if s.lock != nil {
		_ = s.lock.Close()
		s.lock = nil
	}
	return err
}

func (s *Store) NoteWriteError() {
	if s == nil {
		return
	}
	s.writeErrs.Add(1)
}

func (s *Store) WriteErrors() uint64 {
	if s == nil {
		return 0
	}
	return s.writeErrs.Load()
}

// Checkpoint copies WAL frames into the main database file. PASSIVE never
// waits; TRUNCATE resets the WAL when nothing else holds it. :memory: is a no-op.
func (s *Store) Checkpoint(ctx context.Context, mode string) (CheckpointResult, error) {
	var out CheckpointResult
	if s == nil {
		return out, fmt.Errorf("store closed")
	}
	if s.path == "" || s.path == ":memory:" {
		return out, nil
	}
	if s.DB == nil {
		return out, fmt.Errorf("store closed")
	}
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case CheckpointTruncate:
		mode = CheckpointTruncate
	case "RESTART":
		mode = "RESTART"
	case "FULL":
		mode = "FULL"
	default:
		mode = CheckpointPassive
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := s.DB.QueryRowContext(ctx, "PRAGMA wal_checkpoint("+mode+")").Scan(&out.Blocked, &out.Log, &out.Checkpointed)
	if err != nil {
		return out, fmt.Errorf("wal checkpoint %s: %w", mode, err)
	}
	return out, nil
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

func (s *Store) PutKV(key, value string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("store closed")
	}
	_, err := s.DB.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *Store) GetKV(key string) (string, error) {
	if s == nil || s.DB == nil {
		return "", fmt.Errorf("store closed")
	}
	var v string
	err := s.DB.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) DelKV(key string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("store closed")
	}
	_, err := s.DB.Exec(`DELETE FROM kv WHERE key = ?`, key)
	return err
}
