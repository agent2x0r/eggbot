package protect

import (
	"fmt"
	"testing"
	"time"

	"eggbot/internal/flags"
	"eggbot/internal/origin"
	"eggbot/internal/userfile"
)

type fakeSend struct{ kicks int }

func (f *fakeSend) Mode(string, string, ...string) {}
func (f *fakeSend) Kick(string, string, string)    { f.kicks++ }
func (f *fakeSend) Join(string)                    {}
func (f *fakeSend) Nick() string                   { return "eggbot" }

type fakeHost struct{ cs flags.ChanSet }

func (f fakeHost) UserOn(o origin.Origin) *userfile.User { return nil }
func (f fakeHost) ChanSet(string) flags.ChanSet          { return f.cs }
func (f fakeHost) IsBanned(string, string) (bool, string) {
	return false, ""
}
func (f fakeHost) MemberOp(string, string) bool     { return false }
func (f fakeHost) AutoJoin(string) bool             { return true }
func (f fakeHost) ChannelsWithNick(string) []string { return nil }

func TestQuietChannelNoFloodKick(t *testing.T) {
	send := &fakeSend{}
	cs, _ := flags.ParseChanSet("+seen +ai")
	e := New(send, fakeHost{cs: cs}, DefaultLimits())
	o := origin.Origin{Nick: "flooder", User: "u", Host: "h.example"}
	for i := 0; i < 10; i++ {
		e.OnPrivmsg("#lobby", o)
	}
	if send.kicks != 0 {
		t.Fatalf("quiet channel should not kick, got %d", send.kicks)
	}
}

func TestProtectedChannelFloodKick(t *testing.T) {
	send := &fakeSend{}
	cs, _ := flags.ParseChanSet("+enforcebans")
	e := New(send, fakeHost{cs: cs}, DefaultLimits())
	o := origin.Origin{Nick: "flooder", User: "u", Host: "h.example"}
	for i := 0; i < 10; i++ {
		e.OnPrivmsg("#lobby", o)
	}
	if send.kicks == 0 {
		t.Fatal("expected a flood kick when +enforcebans")
	}
}

func TestWindow(t *testing.T) {
	now := time.Now()
	times, hit := OverLimit(nil, now, time.Second, 2)
	if hit {
		t.Fatal("first should not hit")
	}
	times, hit = OverLimit(times, now.Add(100*time.Millisecond), time.Second, 2)
	if hit {
		t.Fatal("second should not hit (limit 2 means 3rd hits)")
	}
	_, hit = OverLimit(times, now.Add(200*time.Millisecond), time.Second, 2)
	if !hit {
		t.Fatal("third should hit")
	}
}

func TestFloodWindowMapIsBounded(t *testing.T) {
	e := New(&fakeSend{}, fakeHost{}, DefaultLimits())
	for i := 0; i < maxTrackedWindows+100; i++ {
		e.hit(e.lines, fmt.Sprintf("user-%d", i), time.Minute, 10)
	}
	if len(e.lines) > maxTrackedWindows {
		t.Fatalf("tracked windows = %d, max = %d", len(e.lines), maxTrackedWindows)
	}
}
