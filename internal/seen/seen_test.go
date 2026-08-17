package seen

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eggbot/internal/store"
)

func TestSeen(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st)
	m.Record("Nate", "nate", "", "n!u@h", "#cool", "saying", "brb")
	r, err := m.Lookup("nate")
	if err != nil || r == nil {
		t.Fatalf("%+v %v", r, err)
	}
	got := m.Format("nate", r)
	if got == "" || !strings.Contains(got, "brb") {
		t.Fatal(got)
	}
	if r2, _ := m.Lookup("nobody"); r2 != nil {
		t.Fatal("expected miss")
	}
	_ = Search("  nate  ")
	_ = human(30 * time.Second)
	_ = human(5 * time.Minute)
	_ = human(3 * time.Hour)
	_ = human(90 * 24 * time.Hour)
}
