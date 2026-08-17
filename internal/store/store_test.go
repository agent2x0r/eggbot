package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRestrictsDatabasePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eggbot.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("database permissions = %o, want 600", got)
	}
}

func TestMigrationsAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eggbot.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := st.DB.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Fatalf("version %d", version)
	}
	dest := filepath.Join(dir, "backup.db")
	if err := st.Backup(context.Background(), dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup perm %o", info.Mode().Perm())
	}
	st.Close()
	st2, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if err := st2.Integrity(); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeAndSingleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eggbot.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO seen (nick, handle, account, host, channel, event, last_text, seen_at) VALUES ('old','','','','#c','saying','x', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := st.Purge(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM seen`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("purge left %d", n)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("second open should fail while locked")
	}
	st.Close()
}

func TestMigrateFromV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DELETE FROM schema_migrations WHERE version > 1`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	var version int
	if err := st2.DB.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Fatalf("expected v2 after reopen, got %d", version)
	}
}

func TestPingTxAndBackupErrors(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO kv (key, value) VALUES ('a', 'b')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(*sql.Tx) error {
		return errors.New("nope")
	}); err == nil {
		t.Fatal("rollback")
	}
	if err := st.Backup(context.Background(), ""); err == nil {
		t.Fatal("empty dest")
	}
	if err := st.Backup(context.Background(), st.path); err == nil {
		t.Fatal("existing dest")
	}
	if err := st.Purge(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}
