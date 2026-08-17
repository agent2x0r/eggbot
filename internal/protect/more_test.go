package protect

import (
	"testing"

	"eggbot/internal/flags"
	"eggbot/internal/origin"
	"eggbot/internal/userfile"
)

type richHost struct {
	cs     flags.ChanSet
	users  map[string]*userfile.User
	banned bool
	chans  []string
	op     bool
}

func (f richHost) UserOn(o origin.Origin) *userfile.User { return f.users[o.Nick] }
func (f richHost) ChanSet(string) flags.ChanSet          { return f.cs }
func (f richHost) IsBanned(string, string) (bool, string) {
	return f.banned, "banned"
}
func (f richHost) MemberOp(string, string) bool     { return f.op }
func (f richHost) AutoJoin(string) bool             { return true }
func (f richHost) ChannelsWithNick(string) []string { return f.chans }

func TestOnJoinAutoOpVoiceBan(t *testing.T) {
	send := &fakeSend{}
	cs, _ := flags.ParseChanSet("+autoop +autovoice +enforcebans")
	op := &userfile.User{Handle: "alice", Global: flags.Parse("o")}
	h := richHost{cs: cs, users: map[string]*userfile.User{"alice": op}}
	e := New(send, h, DefaultLimits())
	e.OnJoin("#c", origin.Origin{Nick: "alice", User: "a", Host: "h"})
	kicker := &fakeSend{}
	banned := richHost{cs: cs, users: map[string]*userfile.User{}, banned: true}
	e2 := New(kicker, banned, DefaultLimits())
	e2.OnJoin("#c", origin.Origin{Nick: "eve", User: "e", Host: "bad"})
	if kicker.kicks == 0 {
		t.Fatal("enforcebans kick")
	}
	ak := &userfile.User{Handle: "kickme", Global: flags.Parse("k")}
	e3 := New(&fakeSend{}, richHost{cs: cs, users: map[string]*userfile.User{"kickme": ak}}, DefaultLimits())
	e3.OnJoin("#c", origin.Origin{Nick: "kickme", User: "k", Host: "h"})
}

func TestOnNickModeKick(t *testing.T) {
	send := &fakeSend{}
	cs, _ := flags.ParseChanSet("+enforcebans +protectops +protectfriends +bitch")
	op := &userfile.User{Handle: "alice", Global: flags.Parse("of")}
	h := richHost{
		cs:    cs,
		users: map[string]*userfile.User{"alice": op, "eve": nil},
		chans: []string{"#c"},
		op:    true,
	}
	e := New(send, h, DefaultLimits())
	o := origin.Origin{Nick: "flooder", User: "u", Host: "h.example"}
	for i := 0; i < 10; i++ {
		e.OnNick(o, "n"+string(rune('a'+i)))
	}
	e.OnMode("#c", "eve", "-o+o", []string{"alice", "eve"})
	e.OnKick("#c", "eve", "eggbot", "out")
	deop := &userfile.User{Handle: "d", Global: flags.Parse("d")}
	h.users["d"] = deop
	e.OnMode("#c", "eve", "+o", []string{"d"})
}
