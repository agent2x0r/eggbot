package chanstate

import (
	"path/filepath"
	"testing"

	"eggbot/internal/flags"
	"eggbot/internal/store"
)

func TestApplyChanSetAndMissing(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	if _, err := s.ApplyChanSet("#nope", "+seen"); err == nil {
		t.Fatal("missing")
	}
	cs, _ := flags.ParseChanSet("+seen")
	s.LoadOrCreate("#room", cs, "", "", "", "")
	if _, err := s.ApplyChanSet("#room", "+notathing"); err == nil {
		t.Fatal("bad spec")
	}
	got, err := s.ApplyChanSet("#room", "+greet")
	if err != nil || !got.Chanset.Has("greet") {
		t.Fatal(err, got)
	}
	s.SetOp("#missing", "n", true)
	s.SetVoice("#missing", "n", true)
	if err := s.DelBan("#room", "*!*@no"); err == nil {
		t.Fatal("missing ban")
	}
}
