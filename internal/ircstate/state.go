// Package ircstate is the ordered live IRC user/channel reducer.
// Persisted channel configuration stays in chanstate.
package ircstate

import (
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/irccase"
	"eggbot/internal/ircx"
	"eggbot/internal/origin"
)

type User struct {
	Nick     string
	User     string
	Host     string
	Account  string
	Realname string
	Away     bool
	Channels map[string]struct{}
}

type Member struct {
	Nick     string
	Prefixes string
}

type Channel struct {
	Name    string
	Topic   string
	TopicBy string
	TopicAt time.Time
	Modes   string
	Key     string
	Joined  bool
	Members map[string]*Member
	Syncing bool
}

type Network struct {
	mu       sync.RWMutex
	isupport *ircx.ISupport
	parser   ircx.ModeParser
	self     string
	gen      uint64
	users    map[string]*User
	channels map[string]*Channel
}

func New(is *ircx.ISupport) *Network {
	if is == nil {
		is = ircx.NewISupport()
	}
	return &Network{
		isupport: is,
		parser:   ircx.ParserFromISupport(is),
		users:    map[string]*User{},
		channels: map[string]*Channel{},
	}
}

func (n *Network) Mapper() irccase.Mapper {
	return n.isupport.Mapper()
}

func (n *Network) SetSelf(nick string) {
	n.mu.Lock()
	n.self = nick
	n.mu.Unlock()
}

func (n *Network) Self() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.self
}

func (n *Network) Reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.users = map[string]*User{}
	n.channels = map[string]*Channel{}
	n.gen++
	n.parser = ircx.ParserFromISupport(n.isupport)
}

func (n *Network) Generation() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.gen
}

func (n *Network) foldNick(s string) string { return n.isupport.Mapper().Fold(s) }
func (n *Network) foldChan(s string) string { return n.isupport.Mapper().Fold(s) }

func (n *Network) Apply(msg ircmsg.Message) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.parser = ircx.ParserFromISupport(n.isupport)
	switch msg.Command {
	case "001":
		if len(msg.Params) > 0 {
			n.self = msg.Params[0]
		}
	case "NICK":
		n.onNick(msg)
	case "JOIN":
		n.onJoin(msg)
	case "PART":
		n.onPart(msg)
	case "QUIT", "KILL":
		n.onQuit(msg)
	case "KICK":
		n.onKick(msg)
	case "MODE":
		n.onMode(msg)
	case "TOPIC":
		n.onTopic(msg)
	case "331":
		if ch, ok := origin.Parse366(msg.Params); ok {
			if c := n.channelLocked(ch); c != nil {
				c.Topic = ""
			}
		}
	case "332":
		if ch, topic, ok := origin.Parse332(msg.Params); ok {
			c := n.ensureChannelLocked(ch)
			c.Topic = topic
		}
	case "333":
		n.on333(msg)
	case "353":
		n.on353(msg)
	case "366":
		if ch, ok := origin.Parse366(msg.Params); ok {
			if c := n.channelLocked(ch); c != nil {
				c.Syncing = false
			}
		}
	case "324":
		n.on324(msg)
	case "352":
		n.on352(msg)
	case "354":
		n.on354(msg)
	case "ACCOUNT":
		n.onAccount(msg)
	case "CHGHOST":
		n.onChghost(msg)
	case "AWAY":
		n.onAway(msg)
	case "SETNAME":
		n.onSetname(msg)
	}
}

func (n *Network) onJoin(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	ch := origin.Param(msg, 0)
	if ch == "" {
		ch = origin.Last(msg)
	}
	account := o.Account
	realname := ""
	if len(msg.Params) >= 3 {
		account = msg.Params[1]
		if account == "*" {
			account = ""
		}
		realname = msg.Params[2]
	}
	c := n.ensureChannelLocked(ch)
	self := n.isSelfLocked(o.Nick)
	if self {
		c.Joined = true
		c.Members = map[string]*Member{}
		c.Syncing = true
	}
	n.upsertUserLocked(o.Nick, o.User, o.Host, account, realname)
	n.addMemberLocked(ch, o.Nick, "")
}

func (n *Network) onPart(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	ch := origin.Param(msg, 0)
	n.removeMemberLocked(ch, o.Nick)
	if n.isSelfLocked(o.Nick) {
		if c := n.channelLocked(ch); c != nil {
			c.Joined = false
			c.Members = map[string]*Member{}
		}
	}
}

func (n *Network) onQuit(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	u := n.users[n.foldNick(o.Nick)]
	if u == nil {
		return
	}
	for ch := range u.Channels {
		n.removeMemberLocked(ch, o.Nick)
	}
	delete(n.users, n.foldNick(o.Nick))
}

func (n *Network) onKick(msg ircmsg.Message) {
	if len(msg.Params) < 2 {
		return
	}
	ch, victim := msg.Params[0], msg.Params[1]
	n.removeMemberLocked(ch, victim)
	if n.isSelfLocked(victim) {
		if c := n.channelLocked(ch); c != nil {
			c.Joined = false
			c.Members = map[string]*Member{}
		}
	}
}

func (n *Network) onNick(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	neu := origin.Last(msg)
	oldF, newF := n.foldNick(o.Nick), n.foldNick(neu)
	if u := n.users[oldF]; u != nil {
		u.Nick = neu
		delete(n.users, oldF)
		n.users[newF] = u
		for ch := range u.Channels {
			if c := n.channelLocked(ch); c != nil {
				if m := c.Members[oldF]; m != nil {
					delete(c.Members, oldF)
					m.Nick = neu
					c.Members[newF] = m
				}
			}
		}
	}
	if n.isSelfLocked(o.Nick) {
		n.self = neu
	}
}

func (n *Network) onMode(msg ircmsg.Message) {
	if len(msg.Params) < 2 {
		return
	}
	target := msg.Params[0]
	if !ircx.IsChannelTarget(target, n.isupport.ChanTypes()) {
		return
	}
	args := []string{}
	if len(msg.Params) > 2 {
		args = msg.Params[2:]
	}
	changes := n.parser.Parse(msg.Params[1], args)
	c := n.ensureChannelLocked(target)
	for _, chg := range changes {
		if chg.Kind == ircx.ModePrefix && chg.Arg != "" {
			m := n.memberLocked(target, chg.Arg)
			if m == nil {
				n.addMemberLocked(target, chg.Arg, "")
				m = n.memberLocked(target, chg.Arg)
			}
			if m == nil {
				continue
			}
			pref := string(chg.Prefix)
			if chg.Add {
				if !strings.Contains(m.Prefixes, pref) {
					m.Prefixes += pref
				}
			} else {
				m.Prefixes = strings.ReplaceAll(m.Prefixes, pref, "")
			}
		}
		if chg.Mode == 'k' {
			if chg.Add {
				c.Key = chg.Arg
			} else {
				c.Key = ""
			}
		}
	}
}

func (n *Network) onTopic(msg ircmsg.Message) {
	if len(msg.Params) < 1 {
		return
	}
	c := n.ensureChannelLocked(msg.Params[0])
	c.Topic = origin.Last(msg)
	c.TopicBy = origin.FromMessage(msg).Nick
	c.TopicAt = time.Now()
}

func (n *Network) on333(msg ircmsg.Message) {
	// 333 nick #chan setter ts
	var ch, setter, ts string
	switch len(msg.Params) {
	case 3:
		ch, setter, ts = msg.Params[0], msg.Params[1], msg.Params[2]
	case 4:
		ch, setter, ts = msg.Params[1], msg.Params[2], msg.Params[3]
	default:
		return
	}
	c := n.ensureChannelLocked(ch)
	c.TopicBy = setter
	if unix, err := parseUnix(ts); err == nil {
		c.TopicAt = time.Unix(unix, 0)
	}
}

func (n *Network) on353(msg ircmsg.Message) {
	ch, names, ok := origin.Parse353(msg.Params)
	if !ok {
		return
	}
	c := n.ensureChannelLocked(ch)
	_, prefixChars := n.isupport.Prefix()
	for _, tok := range strings.Fields(names) {
		prefs, rest := ircx.StripPrefixes(tok, prefixChars)
		nick, user, host := ircx.ParseUserhostName(rest)
		if nick == "" {
			continue
		}
		n.upsertUserLocked(nick, user, host, "", "")
		n.addMemberLocked(ch, nick, prefs)
	}
	_ = c
}

func (n *Network) on324(msg ircmsg.Message) {
	if len(msg.Params) < 3 {
		return
	}
	ch := msg.Params[1]
	if !ircx.IsChannelTarget(ch, n.isupport.ChanTypes()) {
		ch = msg.Params[0]
	}
	c := n.ensureChannelLocked(ch)
	c.Modes = strings.Join(msg.Params[2:], " ")
}

func (n *Network) on352(msg ircmsg.Message) {
	// 352 me chan user host server nick flags :hop realname
	if len(msg.Params) < 8 {
		return
	}
	ch, user, host, nick := msg.Params[1], msg.Params[2], msg.Params[3], msg.Params[5]
	flags := msg.Params[6]
	real := origin.Last(msg)
	n.upsertUserLocked(nick, user, host, "", real)
	_, prefixChars := n.isupport.Prefix()
	prefs, _ := ircx.StripPrefixes(flags, prefixChars+"HGH*")
	n.addMemberLocked(ch, nick, prefs)
}

func (n *Network) on354(msg ircmsg.Message) {
	// WHOX %cuhnfas -> chan user host nick flags account realname
	if len(msg.Params) < 7 {
		return
	}
	ch := msg.Params[1]
	user := msg.Params[2]
	host := msg.Params[3]
	nick := msg.Params[4]
	account := ""
	real := origin.Last(msg)
	if len(msg.Params) >= 8 {
		account = msg.Params[6]
	}
	if account == "0" || account == "*" {
		account = ""
	}
	n.upsertUserLocked(nick, user, host, account, real)
	n.addMemberLocked(ch, nick, "")
}

func (n *Network) onAccount(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	acc := origin.Last(msg)
	if acc == "*" {
		acc = ""
	}
	if u := n.users[n.foldNick(o.Nick)]; u != nil {
		u.Account = acc
	}
}

func (n *Network) onChghost(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	if len(msg.Params) < 2 {
		return
	}
	if u := n.users[n.foldNick(o.Nick)]; u != nil {
		u.User, u.Host = msg.Params[0], msg.Params[1]
	}
}

func (n *Network) onAway(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	if u := n.users[n.foldNick(o.Nick)]; u != nil {
		u.Away = origin.Last(msg) != "" && len(msg.Params) > 0
		if len(msg.Params) == 0 {
			u.Away = false
		}
	}
}

func (n *Network) onSetname(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	if u := n.users[n.foldNick(o.Nick)]; u != nil {
		u.Realname = origin.Last(msg)
	}
}

func (n *Network) upsertUserLocked(nick, user, host, account, real string) *User {
	f := n.foldNick(nick)
	u := n.users[f]
	if u == nil {
		u = &User{Channels: map[string]struct{}{}}
		n.users[f] = u
	}
	u.Nick = nick
	if user != "" {
		u.User = user
	}
	if host != "" {
		u.Host = host
	}
	if account == "*" {
		u.Account = ""
	} else if account != "" {
		u.Account = account
	}
	if real != "" {
		u.Realname = real
	}
	return u
}

func (n *Network) addMemberLocked(channel, nick, prefixes string) {
	c := n.ensureChannelLocked(channel)
	f := n.foldNick(nick)
	m := c.Members[f]
	if m == nil {
		m = &Member{}
		c.Members[f] = m
	}
	m.Nick = nick
	if prefixes != "" {
		m.Prefixes = prefixes
	}
	if u := n.users[f]; u != nil {
		u.Channels[n.foldChan(channel)] = struct{}{}
	}
}

func (n *Network) removeMemberLocked(channel, nick string) {
	c := n.channelLocked(channel)
	if c == nil {
		return
	}
	f := n.foldNick(nick)
	delete(c.Members, f)
	if u := n.users[f]; u != nil {
		delete(u.Channels, n.foldChan(channel))
		if len(u.Channels) == 0 {
			delete(n.users, f)
		}
	}
}

func (n *Network) ensureChannelLocked(name string) *Channel {
	f := n.foldChan(name)
	c := n.channels[f]
	if c == nil {
		c = &Channel{Name: name, Members: map[string]*Member{}}
		n.channels[f] = c
	}
	return c
}

func (n *Network) channelLocked(name string) *Channel {
	return n.channels[n.foldChan(name)]
}

func (n *Network) memberLocked(channel, nick string) *Member {
	c := n.channelLocked(channel)
	if c == nil {
		return nil
	}
	return c.Members[n.foldNick(nick)]
}

func (n *Network) isSelfLocked(nick string) bool {
	return n.isupport.Mapper().Equal(nick, n.self)
}

func (n *Network) Channel(name string) *Channel {
	n.mu.RLock()
	defer n.mu.RUnlock()
	c := n.channelLocked(name)
	if c == nil {
		return nil
	}
	out := *c
	out.Members = make(map[string]*Member, len(c.Members))
	for k, m := range c.Members {
		cp := *m
		out.Members[k] = &cp
	}
	return &out
}

func (n *Network) User(nick string) *User {
	n.mu.RLock()
	defer n.mu.RUnlock()
	u := n.users[n.foldNick(nick)]
	if u == nil {
		return nil
	}
	cp := *u
	cp.Channels = make(map[string]struct{}, len(u.Channels))
	for k := range u.Channels {
		cp.Channels[k] = struct{}{}
	}
	return &cp
}

func (n *Network) Joined(channel string) bool {
	c := n.Channel(channel)
	return c != nil && c.Joined
}

func (n *Network) HasMember(channel, nick string) bool {
	c := n.Channel(channel)
	return c != nil && c.Members[n.Mapper().Fold(nick)] != nil
}

func (n *Network) IsOp(channel, nick string) bool {
	c := n.Channel(channel)
	if c == nil {
		return false
	}
	m := c.Members[n.Mapper().Fold(nick)]
	if m == nil {
		return false
	}
	return strings.Contains(m.Prefixes, "@") || strings.ContainsAny(m.Prefixes, "o")
}

func (n *Network) IsVoice(channel, nick string) bool {
	c := n.Channel(channel)
	if c == nil {
		return false
	}
	m := c.Members[n.Mapper().Fold(nick)]
	return m != nil && (strings.Contains(m.Prefixes, "+") || strings.Contains(m.Prefixes, "v"))
}

func (n *Network) Nicks(channel string) []string {
	c := n.Channel(channel)
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.Members))
	for _, m := range c.Members {
		out = append(out, m.Nick)
	}
	return out
}

func parseUnix(s string) (int64, error) {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errNotUnix
		}
		n = n*10 + int64(r-'0')
	}
	return n, nil
}

var errNotUnix = errString("not unix")

type errString string

func (e errString) Error() string { return string(e) }
