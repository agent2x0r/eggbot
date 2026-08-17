package ircstate

import (
	"testing"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/ircx"
)

func TestJoinHydratesAndSelfJoined(t *testing.T) {
	n := New(ircx.NewISupport())
	n.SetSelf("eggbot")
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	if !n.Joined("#chan") {
		t.Fatal("self join should mark joined")
	}
	n.Apply(ircmsg.MakeMessage(nil, "alice!a@host", "JOIN", "#chan", "aliceacct", "Alice"))
	u := n.User("alice")
	if u == nil || u.Account != "aliceacct" || u.Host != "host" {
		t.Fatalf("%+v", u)
	}
	n.Apply(ircmsg.MakeMessage(nil, "alice!a@host", "ACCOUNT", "*"))
	if n.User("alice").Account != "" {
		t.Fatal("logout")
	}
	n.Apply(ircmsg.MakeMessage(nil, "alice!a@host", "CHGHOST", "newu", "newh"))
	if n.User("alice").Host != "newh" {
		t.Fatal(n.User("alice").Host)
	}
}

func TestNamesUserhostAndDisconnectClear(t *testing.T) {
	n := New(ircx.NewISupport())
	n.SetSelf("eggbot")
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "srv", "353", "eggbot", "=", "#chan", "@bob!b@bh +carol!c@ch"))
	if !n.IsOp("#chan", "bob") || !n.IsVoice("#chan", "carol") {
		t.Fatal("prefixes")
	}
	if n.User("bob") == nil || n.User("bob").Host != "bh" {
		t.Fatal("userhost-in-names")
	}
	n.Reset()
	if n.Joined("#chan") || n.User("bob") != nil {
		t.Fatal("reset must drop live state")
	}
}

func TestKickSelfClearsJoined(t *testing.T) {
	n := New(ircx.NewISupport())
	n.SetSelf("eggbot")
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "op!o@h", "KICK", "#chan", "eggbot", "bye"))
	if n.Joined("#chan") {
		t.Fatal("self kick must unjoin")
	}
}

func TestModePrefixUpdate(t *testing.T) {
	n := New(ircx.NewISupport())
	n.SetSelf("eggbot")
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "op!o@h", "MODE", "#chan", "+o", "bob"))
	if !n.IsOp("#chan", "bob") {
		t.Fatal("op")
	}
	n.Apply(ircmsg.MakeMessage(nil, "op!o@h", "MODE", "#chan", "-o", "bob"))
	if n.IsOp("#chan", "bob") {
		t.Fatal("deop")
	}
}

func TestAccountTagAndAway(t *testing.T) {
	n := New(ircx.NewISupport())
	n.SetSelf("eggbot")
	n.Apply(ircmsg.MakeMessage(nil, "eggbot!u@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "JOIN", "#chan"))
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "AWAY", "gone"))
	if u := n.User("bob"); u == nil || !u.Away {
		t.Fatal("away")
	}
	n.Apply(ircmsg.MakeMessage(nil, "bob!b@h", "NICK", "robert"))
	if n.User("robert") == nil {
		t.Fatal("nick change")
	}
	n.Apply(ircmsg.MakeMessage(nil, "robert!b@h", "QUIT", "bye"))
	if n.HasMember("#chan", "robert") {
		t.Fatal("quit")
	}
}
