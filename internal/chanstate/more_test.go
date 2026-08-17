package chanstate

import (
	"path/filepath"
	"testing"
	"time"

	"eggbot/internal/flags"
	"eggbot/internal/store"
)

func TestMembersBansAndPersona(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st)
	cs, _ := flags.ParseChanSet("+seen")
	s.LoadOrCreate("#room", cs, "hi", "", "chanserv", "k")
	s.SetPersona("#room", "brief")
	s.SetJoined("#room", true)
	s.AddMember("#room", "alice", "a", "h", "acct", "@+")
	s.AddMember("#room", "bob", "b", "h2", "", "+")
	s.SetOp("#room", "bob", true)
	s.SetVoice("#room", "alice", false)
	s.NickChange("alice", "ally")
	c := s.Get("#room")
	if c.Member("ally") == nil || len(c.Nicks()) < 2 {
		t.Fatalf("%+v", c)
	}
	s.RemoveMember("#room", "bob")
	if err := s.AddBan(Ban{Channel: "#room", Mask: "*!*@x", Reason: "no", Setter: "nate", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if len(s.Bans("#room")) != 1 || len(s.AllBans()) != 1 {
		t.Fatal("bans")
	}
	if err := s.DelBan("#room", "*!*@x"); err != nil {
		t.Fatal(err)
	}
	expired := Ban{ExpiresAt: time.Now().Add(-time.Second)}
	if !expired.Expired() {
		t.Fatal("expired")
	}
	if len(s.All()) == 0 {
		t.Fatal("all")
	}
	s.ClearMembers("#room")
	s.SetJoined("#room", false)
	s.SetAutoJoin("#missing", true)
	s.SetPersona("#missing", "x")
}
