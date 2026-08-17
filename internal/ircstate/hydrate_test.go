package ircstate

import (
	"testing"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/ircx"
)

func TestHydrationNumericsAndSetname(t *testing.T) {
	n := New(ircx.NewISupport())
	n.SetSelf("eggbot")
	n.Apply(ircmsg.MakeMessage(nil, "srv", "001", "eggbot", "welcome"))
	if n.Self() != "eggbot" {
		t.Fatal(n.Self())
	}
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "srv", "332", "eggbot", "#chan", "hello topic"))
	n.Apply(ircmsg.MakeMessage(nil, "srv", "333", "eggbot", "#chan", "alice", "1700000000"))
	n.Apply(ircmsg.MakeMessage(nil, "srv", "324", "eggbot", "#chan", "+nt"))
	n.Apply(ircmsg.MakeMessage(nil, "srv", "352", "eggbot", "#chan", "b", "h", "srv", "bob", "H@", "0 Bob"))
	n.Apply(ircmsg.MakeMessage(nil, "srv", "354", "eggbot", "#chan", "c", "ch", "carol", "H", "carolacct", "Carol"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "SETNAME", "Robert"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "TOPIC", "#chan", "newer"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "PART", "#chan", "bye"))
	if n.HasMember("#chan", "bob") {
		t.Fatal("part")
	}
	if u := n.User("carol"); u == nil || u.Account != "carolacct" {
		t.Fatalf("%+v", u)
	}
	if n.Nicks("#chan") == nil {
		t.Fatal("nicks")
	}
	_ = n.Generation()
	_ = n.Mapper()
	if ch := n.Channel("#chan"); ch == nil || ch.Topic == "" {
		t.Fatal("topic")
	}
	n.Apply(ircmsg.MakeMessage(nil, "op!o@h", "MODE", "#chan", "+k", "secret"))
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "PART", "#chan", "out"))
	if n.Joined("#chan") {
		t.Fatal("self part")
	}
	n.Reset()
	if n.Generation() == 0 {
		t.Fatal("generation")
	}
}

func TestAccountChghostAwayKick(t *testing.T) {
	n := New(ircx.NewISupport())
	n.SetSelf("eggbot")
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@old", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@old", "ACCOUNT", "bobacct"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@old", "CHGHOST", "nb", "new.host"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!nb@new.host", "AWAY", "brb"))
	n.Apply(ircmsg.MakeMessage(nil, "srv", "331", "eggbot", "#chan", "no topic"))
	n.Apply(ircmsg.MakeMessage(nil, "op!o@h", "KICK", "#chan", "bob", "out"))
	if n.HasMember("#chan", "bob") {
		t.Fatal("kick")
	}
	n.Apply(ircmsg.MakeMessage(nil, "carol!c@h", "NICK", "caroline"))
	n.Apply(ircmsg.MakeMessage(nil, "eve!e@h", "QUIT", "gone"))
	_, _ = parseUnix("not-a-number")
	if _, err := parseUnix("1700000000"); err != nil {
		t.Fatal(err)
	}
}
