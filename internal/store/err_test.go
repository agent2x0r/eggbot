package store

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestErrorPathsAndPurgeDroppedTables(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "bad.db")
	if err := os.WriteFile(garbage, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(garbage); err == nil {
		t.Fatal("garbage")
	}

	lockDir := filepath.Join(t.TempDir(), "x.db")
	if err := os.Mkdir(lockDir+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(lockDir); err == nil {
		t.Fatal("lock dir")
	}

	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DROP TABLE seen`); err != nil {
		t.Fatal(err)
	}
	if err := st.Purge(context.Background(), 1); err == nil {
		t.Fatal("seen drop")
	}
	if err := st.Backup(context.Background(), t.TempDir()); err == nil {
		t.Fatal("backup dir")
	}
	tx, err := st.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := currentVersion(tx); err == nil {
		t.Fatal("current version")
	}
	_ = tx.Rollback()
	st.Close()
	if err := st.Integrity(); err == nil {
		t.Fatal("closed integrity")
	}
	if err := st.Tx(context.Background(), func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("closed tx")
	}
}

func TestPurgeDroppedAuditAndProposals(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB.Exec(`INSERT INTO seen (nick, handle, account, host, channel, event, last_text, seen_at) VALUES ('n','','','','#c','x','',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DROP TABLE ai_audit`); err != nil {
		t.Fatal(err)
	}
	if err := st.Purge(context.Background(), 1); err == nil {
		t.Fatal("audit drop")
	}
}

func TestPurgeDroppedProposals(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.DB.Exec(`DROP TABLE proposals`); err != nil {
		t.Fatal(err)
	}
	if err := st.Purge(context.Background(), 1); err == nil {
		t.Fatal("proposals drop")
	}
}

func TestLegacyStampAndSchemaView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DELETE FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := st2.DB.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Fatalf("version %d", version)
	}
	st2.Close()

	path2 := filepath.Join(t.TempDir(), "v.db")
	st, err = Open(path2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`CREATE VIEW schema_migrations AS SELECT 1 AS version, 'x' AS checksum, 1 AS applied_at`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := Open(path2); err == nil {
		t.Fatal("schema view")
	}
}

func TestBackupMissingParent(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Backup(context.Background(), filepath.Join(t.TempDir(), "nope", "b.db")); err == nil {
		t.Fatal("missing parent")
	}
}

func TestBadMigration(t *testing.T) {
	prev := migrations
	t.Cleanup(func() { migrations = prev })
	migrations = append(migrations, migration{99, "THIS IS NOT SQL"})
	if _, err := Open(filepath.Join(t.TempDir(), "t.db")); err == nil {
		t.Fatal("bad migration")
	}
	migrations = append(prev, migration{99, `CREATE TABLE extra (id INTEGER); INSERT INTO schema_migrations (version, checksum, applied_at) VALUES (99, 'x', 0);`})
	if _, err := Open(filepath.Join(t.TempDir(), "u.db")); err == nil {
		t.Fatal("dup migration row")
	}
}

func TestStampInsertTriggerAndCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DELETE FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`CREATE TRIGGER no_stamp BEFORE INSERT ON schema_migrations BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := Open(path); err == nil {
		t.Fatal("trigger")
	}

	path3 := filepath.Join(t.TempDir(), "corrupt.db")
	st, err = Open(path3)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	f, err := os.OpenFile(path3, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(4096, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(bytes.Repeat([]byte{0xaa}, 200)); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_, _ = Open(path3)
}
