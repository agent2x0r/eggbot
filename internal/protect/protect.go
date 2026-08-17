// Package protect enforces chansets: flood, auto-op/voice, bitch, protectops.
package protect

import (
	"sync"
	"time"

	"eggbot/internal/flags"
	"eggbot/internal/irccase"
	"eggbot/internal/origin"
	"eggbot/internal/userfile"
)

type Sender interface {
	Mode(channel string, modes string, args ...string)
	Kick(channel, nick, reason string)
	Join(channel string)
	Nick() string
}

type Host interface {
	UserOn(o origin.Origin) *userfile.User
	ChanSet(channel string) flags.ChanSet
	IsBanned(channel, nuh string) (bool, string)
	MemberOp(channel, nick string) bool
	AutoJoin(channel string) bool
	ChannelsWithNick(nick string) []string
}

type Limits struct {
	Lines      int
	LineWin    time.Duration
	Joins      int
	JoinWin    time.Duration
	Nicks      int
	NickWin    time.Duration
	KickBanFor time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		Lines:      6,
		LineWin:    10 * time.Second,
		Joins:      4,
		JoinWin:    8 * time.Second,
		Nicks:      4,
		NickWin:    10 * time.Second,
		KickBanFor: 10 * time.Minute,
	}
}

type Engine struct {
	send            Sender
	host            Host
	lim             Limits
	mu              sync.Mutex
	lines           map[string]*window // nuh or nick
	joins           map[string]*window // host
	nicks           map[string]*window // host
	lastWindowPrune time.Time
}

const maxTrackedWindows = 4096

func New(send Sender, host Host, lim Limits) *Engine {
	if lim.Lines == 0 {
		lim = DefaultLimits()
	}
	return &Engine{
		send:  send,
		host:  host,
		lim:   lim,
		lines: map[string]*window{},
		joins: map[string]*window{},
		nicks: map[string]*window{},
	}
}

func (e *Engine) OnJoin(channel string, o origin.Origin) {
	cs := e.host.ChanSet(channel)
	u := e.host.UserOn(o)
	if banned, reason := e.host.IsBanned(channel, o.NUH()); banned && cs.Has("enforcebans") {
		if !friend(u) {
			e.send.Kick(channel, o.Nick, reason)
			return
		}
	}
	if u != nil {
		if u.Global.Has(flags.AutoKick) || e.chanHas(u, channel, flags.AutoKick) {
			e.send.Kick(channel, o.Nick, "auto-kick")
			return
		}
		if cs.Has("autoop") && (u.Global.Has(flags.AutoOp) || e.chanHas(u, channel, flags.AutoOp) || u.Global.Has(flags.Op) || e.chanHas(u, channel, flags.Op)) {
			e.send.Mode(channel, "+o", o.Nick)
		}
		if cs.Has("autovoice") && !u.Global.Has(flags.Quiet) && (u.Global.Has(flags.AutoVoice) || e.chanHas(u, channel, flags.AutoVoice) || u.Global.Has(flags.Voice) || e.chanHas(u, channel, flags.Voice)) {
			e.send.Mode(channel, "+v", o.Nick)
		}
	}
	if !friend(u) && e.hit(e.joins, o.Host, e.lim.JoinWin, e.lim.Joins) {
		e.punish(channel, o, "join flood")
	}
}

func (e *Engine) OnPrivmsg(channel string, o origin.Origin) {
	u := e.host.UserOn(o)
	if friend(u) {
		return
	}
	if e.hit(e.lines, o.NUH(), e.lim.LineWin, e.lim.Lines) {
		e.punish(channel, o, "flood")
	}
}

func (e *Engine) OnNick(o origin.Origin, newNick string) {
	u := e.host.UserOn(o)
	if friend(u) {
		return
	}
	if e.hit(e.nicks, o.Host, e.lim.NickWin, e.lim.Nicks) {
		for _, channel := range e.host.ChannelsWithNick(o.Nick) {
			e.punish(channel, o, "nick flood")
		}
		for _, channel := range e.host.ChannelsWithNick(newNick) {
			e.punish(channel, origin.Origin{Nick: newNick, User: o.User, Host: o.Host, Handle: o.Handle}, "nick flood")
		}
	}
}

func (e *Engine) OnMode(channel, setter, modes string, args []string) {
	cs := e.host.ChanSet(channel)
	// parse simple +o/-o +v/-v
	ai := 0
	sign := byte(0)
	for i := 0; i < len(modes); i++ {
		m := modes[i]
		if m == '+' || m == '-' {
			sign = m
			continue
		}
		takesNick := m == 'o' || m == 'v' || m == 'h' || m == 'b'
		var arg string
		if takesNick && ai < len(args) {
			arg = args[ai]
			ai++
		}
		if sign == '-' && m == 'o' && arg != "" && (cs.Has("protectops") || cs.Has("protectfriends")) {
			victim := e.host.UserOn(origin.Origin{Nick: arg})
			protected := e.host.MemberOp(channel, arg) ||
				(victim != nil && (victim.Global.Has(flags.Op) || victim.Global.Has(flags.Owner) || victim.Global.Has(flags.Master) ||
					(cs.Has("protectfriends") && victim.Global.Has(flags.Friend))))
			if protected {
				setterU := e.host.UserOn(origin.Origin{Nick: setter})
				if setterU == nil || !(setterU.Global.Has(flags.Master) || setterU.Global.Has(flags.Owner)) {
					if arg != e.send.Nick() {
						e.send.Mode(channel, "+o", arg)
					}
				}
			}
		}
		if sign == '+' && m == 'o' && arg != "" && cs.Has("bitch") {
			victim := e.host.UserOn(origin.Origin{Nick: arg})
			allowed := victim != nil && (victim.Global.Has(flags.Op) || victim.Global.Has(flags.Owner) || victim.Global.Has(flags.Master) || e.chanHas(victim, channel, flags.Op))
			setterU := e.host.UserOn(origin.Origin{Nick: setter})
			if !allowed && (setterU == nil || !(setterU.Global.Has(flags.Master) || setterU.Global.Has(flags.Owner))) {
				if arg != e.send.Nick() {
					e.send.Mode(channel, "-o", arg)
				}
			}
		}
		if sign == '+' && m == 'o' && arg != "" {
			victim := e.host.UserOn(origin.Origin{Nick: arg})
			if victim != nil && (victim.Global.Has(flags.Deop) || e.chanHas(victim, channel, flags.Deop)) {
				e.send.Mode(channel, "-o", arg)
			}
		}
	}
}

func (e *Engine) OnKick(channel, kicker, victim, reason string) {
	_ = kicker
	_ = reason
	if irccase.Equal(victim, e.send.Nick()) && e.host.AutoJoin(channel) {
		e.send.Join(channel)
	}
}

func (e *Engine) punish(channel string, o origin.Origin, reason string) {
	cs := e.host.ChanSet(channel)
	// Quiet channels: record floods but do not kick unless protection is on.
	if !cs.Has("enforcebans") && !cs.Has("dynamicbans") {
		return
	}
	if cs.Has("dontkickops") && e.host.MemberOp(channel, o.Nick) {
		return
	}
	mask := "*!*@" + o.Host
	if o.Host == "" {
		mask = o.Nick + "!*@*"
	}
	if cs.Has("dynamicbans") || cs.Has("enforcebans") {
		e.send.Mode(channel, "+b", mask)
	}
	e.send.Kick(channel, o.Nick, reason)
}

func (e *Engine) hit(m map[string]*window, key string, win time.Duration, limit int) bool {
	if key == "" || limit <= 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	w := m[key]
	if w == nil {
		now := time.Now()
		if len(m) >= maxTrackedWindows {
			if now.Sub(e.lastWindowPrune) >= time.Second {
				for tracked, candidate := range m {
					if candidate.prune(now, win) == 0 {
						delete(m, tracked)
					}
				}
				e.lastWindowPrune = now
			}
			if len(m) >= maxTrackedWindows {
				return true
			}
		}
		w = &window{}
		m[key] = w
	}
	return w.add(time.Now(), win, limit)
}

func (e *Engine) chanHas(u *userfile.User, channel string, r rune) bool {
	if u == nil {
		return false
	}
	if cf := u.ChanFlags[irccase.Fold(channel)]; cf != nil {
		return cf.Has(r)
	}
	return false
}

func friend(u *userfile.User) bool {
	if u == nil {
		return false
	}
	return u.Global.Has(flags.Friend) || u.Global.Has(flags.Owner) || u.Global.Has(flags.Master) || u.Global.Has(flags.Op)
}

type window struct {
	times []time.Time
}

func (w *window) add(now time.Time, d time.Duration, limit int) bool {
	w.prune(now, d)
	w.times = append(w.times, now)
	return len(w.times) > limit
}

func (w *window) prune(now time.Time, d time.Duration) int {
	cut := now.Add(-d)
	kept := w.times[:0]
	for _, t := range w.times {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	w.times = kept
	return len(w.times)
}

// OverLimit is exported for tests.
func OverLimit(times []time.Time, now time.Time, d time.Duration, limit int) ([]time.Time, bool) {
	w := &window{times: times}
	hit := w.add(now, d, limit)
	return w.times, hit
}
