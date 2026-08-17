package chanstate

import (
	"path/filepath"
	"testing"

	"eggbot/internal/flags"
	"eggbot/internal/store"
)

func TestAutoJoinPersist(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	cs, _ := flags.ParseChanSet("+seen +ai")
	s.LoadOrCreate("#lobbytest", cs, "", "", "", "")
	s.SetAutoJoin("#lobbytest", true)
	s.LoadOrCreate("#skip", cs, "", "", "", "")

	s2 := New(st)
	names := s2.AutoJoinNames()
	if len(names) != 1 || names[0] != "#lobbytest" {
		t.Fatalf("autojoin=%v", names)
	}
	s2.LoadOrCreate("#lobbytest", cs, "", "", "", "")
	c := s2.Get("#lobbytest")
	if c == nil || !c.AutoJoin {
		t.Fatal("expected autojoin loaded")
	}
	s2.Drop("#lobbytest")
	if len(s2.AutoJoinNames()) != 0 {
		t.Fatal("drop should clear autojoin")
	}
}

func TestChannelSnapshotsAreIsolated(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	cs, _ := flags.ParseChanSet("+seen")
	s.LoadOrCreate("#safe", cs, "", "", "", "")
	s.AddMember("#safe", "alice", "u", "h", "", "@")

	snapshot := s.Get("#safe")
	snapshot.Topic = "updated"
	snapshot.Chanset["ai"] = struct{}{}
	snapshot.Member("alice").Op = false

	current := s.Get("#safe")
	if current.Topic != "" || current.Chanset.Has("ai") || !current.Member("alice").Op {
		t.Fatal("mutating a snapshot changed shared channel state")
	}
	if err := s.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	current = s.Get("#safe")
	if current.Topic != "updated" || !current.Chanset.Has("ai") {
		t.Fatal("Save did not merge persistent snapshot fields")
	}
	if !current.Member("alice").Op {
		t.Fatal("Save must not overwrite live member state")
	}
}

func TestPersistedEmptyChansetOverridesDefaults(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	def, _ := flags.ParseChanSet("+seen +ai")
	s := New(st)
	s.LoadOrCreate("#off", def, "", "", "", "")
	if _, err := s.ApplyChanSet("#off", "-seen -ai"); err != nil {
		t.Fatal(err)
	}

	reloaded := New(st)
	c := reloaded.LoadOrCreate("#off", def, "", "", "", "")
	if c.Chanset.Has("seen") || c.Chanset.Has("ai") {
		t.Fatalf("persisted empty chanset was replaced by defaults: %v", c.Chanset)
	}
}
