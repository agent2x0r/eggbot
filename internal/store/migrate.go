package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

type migration struct {
	Version int
	SQL     string
}

var migrations = []migration{
	{1, schemaV1},
	{2, schemaV2},
}

func (s *Store) migrate() error {
	if err := s.Integrity(); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		checksum TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	current, err := currentVersion(tx)
	if err != nil {
		return err
	}
	if current == 0 {
		var name string
		err := tx.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&name)
		if err == nil {
			sum := checksum(schemaV1)
			if _, err := tx.Exec(`INSERT INTO schema_migrations (version, checksum, applied_at) VALUES (1, ?, ?)`, sum, Now()); err != nil {
				return err
			}
			current = 1
		} else if err != sql.ErrNoRows {
			return err
		}
	}
	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			return fmt.Errorf("migration %d: %w", m.Version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, checksum, applied_at) VALUES (?, ?, ?)`,
			m.Version, checksum(m.SQL), Now()); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.Integrity()
}

func currentVersion(tx *sql.Tx) (int, error) {
	var v int
	err := tx.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func checksum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (s *Store) Integrity() error {
	var r string
	if err := s.DB.QueryRow(`PRAGMA quick_check`).Scan(&r); err != nil {
		return fmt.Errorf("integrity: %w", err)
	}
	if r != "ok" {
		return fmt.Errorf("sqlite integrity: %s", r)
	}
	return nil
}

func (s *Store) Backup(ctx context.Context, dest string) error {
	if dest == "" || dest == ":memory:" {
		return fmt.Errorf("invalid backup destination")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination already exists")
	}
	_, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, dest)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return os.Chmod(dest, 0o600)
}

func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.DB.PingContext(ctx)
}

func (s *Store) Purge(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM seen WHERE seen_at < ?`, cutoff); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM ai_audit WHERE created_at < ?`, cutoff); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM proposals WHERE expires_at < ? AND status != 'pending'`, cutoff); err != nil {
			return err
		}
		return nil
	})
}

const schemaV1 = schema

const schemaV2 = `
CREATE TABLE IF NOT EXISTS identities (
    id INTEGER PRIMARY KEY,
    handle TEXT NOT NULL REFERENCES users(handle) ON DELETE CASCADE,
    network TEXT NOT NULL,
    mechanism TEXT NOT NULL,
    external_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE(network, mechanism, external_id)
);
CREATE INDEX IF NOT EXISTS idx_identities_handle ON identities(handle);
CREATE INDEX IF NOT EXISTS idx_hosts_mask ON hosts(mask);
CREATE INDEX IF NOT EXISTS idx_seen_at ON seen(seen_at);
CREATE INDEX IF NOT EXISTS idx_notes_to ON notes(to_handle, read);
CREATE INDEX IF NOT EXISTS idx_ai_audit_created ON ai_audit(created_at);

CREATE TABLE IF NOT EXISTS proposals (
    id TEXT PRIMARY KEY,
    handle TEXT NOT NULL,
    channel TEXT NOT NULL,
    tool TEXT NOT NULL,
    args TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
);

CREATE TABLE IF NOT EXISTS hello_rate (
    key TEXT PRIMARY KEY,
    n INTEGER NOT NULL,
    window_start INTEGER NOT NULL
);
`
