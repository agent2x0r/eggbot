package quotes

import (
	"path/filepath"
	"testing"

	"eggbot/internal/store"
)

func TestQuotesRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st)
	id, err := m.Add("#chan", "nate", "hello world")
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	q, err := m.Random("#chan", "hello")
	if err != nil || q == nil || q.Body != "hello world" {
		t.Fatal(q, err)
	}
	if err := m.Delete(id); err != nil {
		t.Fatal(err)
	}
}
