package notes

import (
	"path/filepath"
	"testing"

	"eggbot/internal/store"
)

func TestNotes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st)
	if err := m.Add("bob", "nate", "hello there"); err != nil {
		t.Fatal(err)
	}
	ns := m.Pending("bob")
	if len(ns) != 1 || ns[0].From != "nate" || ns[0].Body != "hello there" {
		t.Fatalf("%+v", ns)
	}
	m.MarkRead("bob")
	if len(m.Pending("bob")) != 0 {
		t.Fatal("expected empty")
	}
}
