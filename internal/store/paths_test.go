package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMemoryAndMissingParent(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(t.TempDir(), "nope", "eggbot.db")); err == nil {
		t.Fatal("missing parent")
	}
}

func TestMigrateLegacyUsersTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (handle, password, global_flags, created_at, last_seen) VALUES ('old', '', '', 1, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var version int
	if err := st.DB.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Fatalf("version %d", version)
	}
}

func TestPurgeAuditAndProposals(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB.Exec(`INSERT INTO ai_audit (handle, channel, tool, args, result, created_at) VALUES ('n', '#c', 'kick', '{}', 'ok', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO proposals (id, handle, channel, tool, args, created_at, expires_at, status) VALUES ('p', 'n', '#c', 'kick', '{}', 1, 1, 'done')`); err != nil {
		t.Fatal(err)
	}
	if err := st.Purge(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}

func TestSecureDatabaseFilesCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	if err := os.WriteFile(path+"-wal", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
}
