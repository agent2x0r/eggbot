package bot

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"eggbot/internal/chanstate"
	"eggbot/internal/config"
	"eggbot/internal/flags"
	"eggbot/internal/irccase"
	"eggbot/internal/origin"
	"eggbot/internal/partyline"
	"eggbot/internal/script"
	"eggbot/internal/store"
	"eggbot/internal/userfile"
	"eggbot/internal/version"
)

func (b *Bot) onPartyline(s *partyline.Session, line string) {
	if line == "" {
		return
	}
	switch line[0] {
	case '.':
		cmd, args, _ := strings.Cut(strings.TrimSpace(line[1:]), " ")
		b.dispatchParty(s, strings.ToLower(cmd), strings.TrimSpace(args))
	case ',':
		b.PL.Broadcast(fmt.Sprintf("[owners] <%s> %s", s.Handle, strings.TrimSpace(line[1:])), true, func(h string) bool {
			u, err := b.Users.Get(h)
			return err == nil && u.Global.Has(flags.Owner)
		})
	case '\'':
		s.Write(fmt.Sprintf("<%s> %s", s.Handle, strings.TrimSpace(line[1:])))
	default:
		if s.ChatOn {
			b.PL.Broadcast(fmt.Sprintf("<%s> %s", s.Handle, line), false, nil)
		}
	}
}

func (b *Bot) dispatchParty(s *partyline.Session, cmd, args string) {
	u, err := b.Users.Get(s.Handle)
	if err != nil {
		s.Write("your user record vanished")
		return
	}
	// script dcc binds first
	b.Scripts.Dispatch("dcc", scriptEvent(s.Handle, cmd, args))
	type fn func(*partyline.Session, *userfile.User, string)
	cmds := map[string]struct {
		need string
		fn   fn
		help string
	}{
		"help":     {"-", b.cmdHelp, "list commands"},
		"who":      {"p", b.cmdWho, "who's on the partyline"},
		"whom":     {"p", b.cmdWho, "who's on the partyline"},
		"status":   {"p", b.cmdStatus, "bot status"},
		"uptime":   {"p", b.cmdStatus, "bot status"},
		"version":  {"p", b.cmdVersion, "print version"},
		"match":    {"m", b.cmdMatch, ".match <nick|mask>"},
		"+user":    {"m", b.cmdAddUser, ".+user <handle>"},
		"adduser":  {"m", b.cmdAddUser, ".adduser <handle> [host]"},
		"-user":    {"m", b.cmdDelUser, ".-user <handle>"},
		"chattr":   {"m", b.cmdChattr, ".chattr <handle> <flags> [channel]"},
		"+host":    {"m", b.cmdAddHost, ".+host <handle> <mask>"},
		"-host":    {"m", b.cmdDelHost, ".-host <handle> <mask>"},
		"chpass":   {"p", b.cmdChpass, ".chpass [handle] <newpass>"},
		"console":  {"p", b.cmdConsole, ".console [+jpk...]"},
		"chat":     {"p", b.cmdChat, ".chat [on|off]"},
		"op":       {"o", b.cmdOp, ".op <channel> [nick]"},
		"deop":     {"o", b.cmdDeop, ".deop <channel> <nick>"},
		"voice":    {"o", b.cmdVoice, ".voice <channel> <nick>"},
		"devoice":  {"o", b.cmdDevoice, ".devoice <channel> <nick>"},
		"kick":     {"o", b.cmdKick, ".kick <channel> <nick> [reason]"},
		"kickban":  {"o", b.cmdKickban, ".kickban <channel> <nick> [reason]"},
		"+ban":     {"o", b.cmdAddBan, ".+ban <mask> <channel> [reason]"},
		"-ban":     {"o", b.cmdDelBan, ".-ban <mask> <channel>"},
		"bans":     {"o", b.cmdBans, ".bans <channel>"},
		"join":     {"m", b.cmdJoin, ".join #channel"},
		"part":     {"m", b.cmdPart, ".part #channel"},
		"+chan":    {"m", b.cmdPlusChan, ".+chan #channel"},
		"-chan":    {"m", b.cmdMinusChan, ".-chan #channel"},
		"chan":     {"m", b.cmdChan, ".chan [#channel]"},
		"chans":    {"m", b.cmdChan, ".chans"},
		"chanset":  {"m", b.cmdChanset, ".chanset <channel> [+flag|-flag]"},
		"channel":  {"m", b.cmdChaninfo, ".channel <channel>"},
		"chaninfo": {"m", b.cmdChaninfo, ".chaninfo <channel>"},
		"say":      {"o", b.cmdSay, ".say <target> <text>"},
		"act":      {"o", b.cmdAct, ".act <target> <text>"},
		"msg":      {"o", b.cmdSay, ".msg <target> <text>"},
		"notice":   {"o", b.cmdNotice, ".notice <target> <text>"},
		"topic":    {"o", b.cmdTopic, ".topic <channel> <text>"},
		"jump":     {"n", b.cmdJump, ".jump"},
		"save":     {"n", b.cmdSave, ".save"},
		"rehash":   {"n", b.cmdRehash, ".rehash"},
		"reload":   {"n", b.cmdReload, ".reload"},
		"backup":   {"n", b.cmdBackup, ".backup [path]"},
		"confirm":  {"o", b.cmdConfirm, ".confirm <id>"},
		"reject":   {"o", b.cmdReject, ".reject <id>"},
		"die":      {"n", b.cmdDie, ".die"},
		"note":     {"p", b.cmdNote, ".note <handle> <text>"},
		"notes":    {"p", b.cmdNotes, ".notes"},
		"seen":     {"p", b.cmdSeen, ".seen <nick>"},
		"quote":    {"p", b.cmdQuote, ".quote [search]"},
		"+quote":   {"m", b.cmdAddQuote, ".+quote <channel> <text>"},
		"-quote":   {"m", b.cmdDelQuote, ".-quote <id>"},
		"ai":       {"n", b.cmdAI, ".ai [prompt] or .ai enable|disable|model <name>"},
		"llm":      {"n", b.cmdAI, "alias for .ai"},
		"binds":    {"n", b.cmdBinds, ".binds"},
		"bind":     {"n", b.cmdBinds, ".binds"},
		"quit":     {"p", b.cmdQuit, ".quit"},
	}
	ent, ok := cmds[cmd]
	if !ok {
		s.Write("unknown command. .help")
		return
	}
	need := ent.need
	if need != "-" && need != "" && !canParty(u, need) {
		s.Write("you don't have access to that command")
		return
	}
	ent.fn(s, u, args)
}

// canParty is eggdrop-style: +n implies +m +o +p +everything.
func canParty(u *userfile.User, need string) bool {
	if u == nil {
		return need == "-" || need == ""
	}
	if u.Global.Has(flags.Owner) {
		return true
	}
	if u.Global.Has(flags.Master) && need != "n" {
		return true
	}
	switch need {
	case "m":
		return u.Global.Has(flags.Master)
	case "o":
		return u.Global.Has(flags.Op) || u.Global.Has(flags.Master)
	case "p":
		return u.Global.Has(flags.Party) || u.Global.Has(flags.Master)
	case "n":
		return u.Global.Has(flags.Owner)
	default:
		return flags.MatchAttr(u.Global, nil, need)
	}
}

func privilegeRank(set flags.Set) int {
	switch {
	case set.Has(flags.Owner):
		return 3
	case set.Has(flags.Master):
		return 2
	case set.Has(flags.Op):
		return 1
	default:
		return 0
	}
}

// canAdminister prevents a caller from modifying an equal-or-higher account.
// Owner-to-owner changes are deliberately denied; each owner changes their own
// password, and recovery for another owner is an out-of-band operation.
func canAdminister(actor, target *userfile.User) bool {
	if actor == nil || target == nil {
		return false
	}
	return privilegeRank(actor.Global) > privilegeRank(target.Global)
}

func canChangeFlags(actor, target *userfile.User, next flags.Set) bool {
	if !canAdminister(actor, target) {
		return false
	}
	if privilegeRank(actor.Global) == 3 {
		return privilegeRank(next) <= 3
	}
	return privilegeRank(next) < privilegeRank(actor.Global)
}

func scriptEvent(handle, cmd, args string) script.Event {
	return script.Event{Handle: handle, Text: cmd, Raw: args}
}

func (b *Bot) cmdHelp(s *partyline.Session, u *userfile.User, args string) {
	if args != "" {
		s.Write("see .help for the command list")
		return
	}
	s.Write("partyline commands: .help .who .status .version .match .+user .-user .chattr .+host .-host .chpass")
	s.Write("  .console .chat .join .part .+chan .-chan .chan .chanset .channel")
	s.Write("  .op .deop .voice .devoice .kick .kickban .+ban .-ban .bans")
	s.Write("  .say .act .notice .topic .jump .save .reload .die .note .notes .seen .quote .+quote .-quote")
	s.Write("  .ai .binds .quit")
	s.Write("flags: n=owner m=master o=op v=voice f=friend p=party a=autoop g=autovoice d=deop k=autokick")
	_ = u
}

func (b *Bot) cmdWho(s *partyline.Session, _ *userfile.User, _ string) {
	for _, o := range b.PL.Sessions() {
		s.Printf("  %-12s %s %s", o.Handle, o.Kind, o.Remote)
	}
}

func (b *Bot) cmdVersion(s *partyline.Session, _ *userfile.User, _ string) {
	s.Write(version.String())
}

func (b *Bot) cmdStatus(s *partyline.Session, _ *userfile.User, _ string) {
	up := time.Since(b.Started).Truncate(time.Second)
	s.Printf("%s  nick=%s  server=%s  state=%s  up=%s  users=%d  scripts=%d  queue=%d",
		version.String(), b.IRC.Nick(), b.Cfg.ServerAddr(), b.IRC.State(), up, countUsers(b), len(b.Scripts.Binds()), b.IRC.Queue().Len())
	s.Printf("llm enabled=%v model=%s", b.Cfg.LLM.Enabled && b.Cfg.LLM.APIKey != "", b.Cfg.LLM.Model)
	for _, c := range b.Chans.All() {
		s.Printf("  %s  %s  members=%d", c.Name, c.Chanset.String(), len(c.Members))
	}
}

func countUsers(b *Bot) int {
	list, _ := b.Users.List()
	return len(list)
}

func (b *Bot) cmdMatch(s *partyline.Session, _ *userfile.User, args string) {
	if args == "" {
		s.Write("usage: .match <nick|mask>")
		return
	}
	if u := b.Users.FindByHost(args); u != nil {
		s.Write(formatUser(u))
		return
	}
	if u, err := b.Users.Get(args); err == nil {
		s.Write(formatUser(u))
		return
	}
	s.Write("no match")
}

func formatUser(u *userfile.User) string {
	hosts := strings.Join(u.Hosts, ", ")
	if hosts == "" {
		hosts = "(no hosts)"
	}
	return fmt.Sprintf("%s +%s  %s", u.Handle, u.Global.String(), hosts)
}

func (b *Bot) cmdAddUser(s *partyline.Session, _ *userfile.User, args string) {
	handle, rest, _ := strings.Cut(args, " ")
	handle = strings.TrimSpace(handle)
	if handle == "" {
		s.Write("usage: .+user <handle> [hostmask]")
		return
	}
	if err := b.Users.Add(handle, ""); err != nil {
		s.Write(err.Error())
		return
	}
	if rest = strings.TrimSpace(rest); rest != "" {
		_ = b.Users.AddHost(handle, rest)
	}
	s.Write("added " + irccase.Fold(handle))
}

func (b *Bot) cmdDelUser(s *partyline.Session, actor *userfile.User, args string) {
	if args == "" {
		s.Write("usage: .-user <handle>")
		return
	}
	target, err := b.Users.Get(args)
	if err != nil {
		s.Write(err.Error())
		return
	}
	if !canAdminister(actor, target) {
		s.Write("cannot delete an equal-or-higher user")
		return
	}
	if err := b.Users.Delete(args); err != nil {
		s.Write(err.Error())
		return
	}
	s.Write("deleted " + args)
}

func (b *Bot) cmdChattr(s *partyline.Session, actor *userfile.User, args string) {
	f := strings.Fields(args)
	if len(f) == 1 {
		u, err := b.Users.Get(f[0])
		if err != nil {
			s.Write(err.Error())
			return
		}
		s.Write(formatUser(u))
		for ch, fl := range u.ChanFlags {
			if !fl.Empty() {
				s.Printf("  %s +%s", ch, fl.String())
			}
		}
		return
	}
	if len(f) < 2 {
		s.Write("usage: .chattr <handle> [flags] [channel]")
		return
	}
	ch := ""
	if len(f) >= 3 {
		ch = f[2]
	}
	target, err := b.Users.Get(f[0])
	if err != nil {
		s.Write(err.Error())
		return
	}
	next := target.Global.Clone()
	if ch != "" {
		next = flags.Set{}
		if current := target.ChanFlags[irccase.Fold(ch)]; current != nil {
			next = current.Clone()
		}
	}
	if err := next.Apply(f[1]); err != nil {
		s.Write(err.Error())
		return
	}
	if !canChangeFlags(actor, target, next) {
		s.Write("cannot grant your rank or modify an equal-or-higher user")
		return
	}
	u, err := b.Users.Chattr(f[0], f[1], ch)
	if err != nil {
		s.Write(err.Error())
		return
	}
	if ch == "" {
		s.Printf("%s now +%s", u.Handle, u.Global.String())
		return
	}
	s.Printf("%s %s +%s", u.Handle, ch, u.ChanFlags[irccase.Fold(ch)].String())
}

func (b *Bot) cmdAddHost(s *partyline.Session, actor *userfile.User, args string) {
	handle, mask, ok := strings.Cut(args, " ")
	if !ok {
		s.Write("usage: .+host <handle> <mask>")
		return
	}
	target, err := b.Users.Get(handle)
	if err != nil {
		s.Write(err.Error())
		return
	}
	if !irccase.Equal(actor.Handle, target.Handle) && !canAdminister(actor, target) {
		s.Write("cannot modify an equal-or-higher user")
		return
	}
	if err := b.Users.AddHost(handle, strings.TrimSpace(mask)); err != nil {
		s.Write(err.Error())
		return
	}
	s.Write("ok")
}

func (b *Bot) cmdDelHost(s *partyline.Session, actor *userfile.User, args string) {
	handle, mask, ok := strings.Cut(args, " ")
	if !ok {
		s.Write("usage: .-host <handle> <mask>")
		return
	}
	target, err := b.Users.Get(handle)
	if err != nil {
		s.Write(err.Error())
		return
	}
	if !irccase.Equal(actor.Handle, target.Handle) && !canAdminister(actor, target) {
		s.Write("cannot modify an equal-or-higher user")
		return
	}
	if err := b.Users.DelHost(handle, strings.TrimSpace(mask)); err != nil {
		s.Write(err.Error())
		return
	}
	s.Write("ok")
}

func (b *Bot) cmdChpass(s *partyline.Session, u *userfile.User, args string) {
	f := strings.Fields(args)
	if len(f) == 1 {
		if err := b.Users.SetPass(u.Handle, f[0]); err != nil {
			s.Write(err.Error())
			return
		}
		s.Write("password set")
		return
	}
	if len(f) == 2 {
		target, err := b.Users.Get(f[0])
		if err != nil {
			s.Write(err.Error())
			return
		}
		if !canAdminister(u, target) {
			s.Write("cannot reset an equal-or-higher user's password")
			return
		}
		if err := b.Users.SetPass(f[0], f[1]); err != nil {
			s.Write(err.Error())
			return
		}
		s.Write("password set for " + f[0])
		return
	}
	s.Write("usage: .chpass [handle] <newpass>")
}

func (b *Bot) cmdConsole(s *partyline.Session, _ *userfile.User, args string) {
	args = strings.TrimSpace(args)
	if args == "" {
		s.Write("console +" + s.Console)
		return
	}
	mode := '+'
	var out []byte
	cur := []byte(s.Console)
	apply := func(r byte, on bool) {
		has := strings.IndexByte(string(cur), r) >= 0
		if on && !has {
			cur = append(cur, r)
		}
		if !on && has {
			n := strings.IndexByte(string(cur), r)
			cur = append(cur[:n], cur[n+1:]...)
		}
	}
	if !strings.ContainsAny(args, "+-") {
		s.Console = strings.TrimLeft(args, "+")
		s.Write("console +" + s.Console)
		return
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '+':
			mode = '+'
		case '-':
			mode = '-'
		default:
			apply(args[i], mode == '+')
		}
	}
	s.Console = string(cur)
	_ = out
	s.Write("console +" + s.Console)
}

func (b *Bot) cmdChat(s *partyline.Session, _ *userfile.User, args string) {
	switch strings.ToLower(strings.TrimSpace(args)) {
	case "off", "0":
		s.ChatOn = false
		s.Write("chat off")
	default:
		s.ChatOn = true
		s.Write("chat on")
	}
}

func split2(args string) (a, b string) {
	a, b, _ = strings.Cut(strings.TrimSpace(args), " ")
	return a, strings.TrimSpace(b)
}

func (b *Bot) cmdOp(s *partyline.Session, u *userfile.User, args string) {
	ch, nick := split2(args)
	if ch == "" {
		s.Write("usage: .op <channel> [nick]")
		return
	}
	if nick == "" {
		nick = s.Handle
	}
	if !b.need(u, "o", ch) && !u.Global.Has(flags.Owner) && !u.Global.Has(flags.Master) {
		s.Write("need +o on that channel")
		return
	}
	b.IRC.Mode(ch, "+o", nick)
	s.Write("opped " + nick + " on " + ch)
}

func (b *Bot) cmdDeop(s *partyline.Session, _ *userfile.User, args string) {
	ch, nick := split2(args)
	if ch == "" || nick == "" {
		s.Write("usage: .deop <channel> <nick>")
		return
	}
	b.IRC.Mode(ch, "-o", nick)
}

func (b *Bot) cmdVoice(s *partyline.Session, _ *userfile.User, args string) {
	ch, nick := split2(args)
	if ch == "" || nick == "" {
		s.Write("usage: .voice <channel> <nick>")
		return
	}
	b.IRC.Mode(ch, "+v", nick)
}

func (b *Bot) cmdDevoice(s *partyline.Session, _ *userfile.User, args string) {
	ch, nick := split2(args)
	if ch == "" || nick == "" {
		s.Write("usage: .devoice <channel> <nick>")
		return
	}
	b.IRC.Mode(ch, "-v", nick)
}

func (b *Bot) cmdKick(s *partyline.Session, _ *userfile.User, args string) {
	f := strings.Fields(args)
	if len(f) < 2 {
		s.Write("usage: .kick <channel> <nick> [reason]")
		return
	}
	reason := "kick"
	if len(f) > 2 {
		reason = strings.Join(f[2:], " ")
	}
	b.IRC.Kick(f[0], f[1], reason)
}

func (b *Bot) cmdKickban(s *partyline.Session, u *userfile.User, args string) {
	f := strings.Fields(args)
	if len(f) < 2 {
		s.Write("usage: .kickban <channel> <nick> [reason]")
		return
	}
	reason := "kb"
	if len(f) > 2 {
		reason = strings.Join(f[2:], " ")
	}
	mask := f[1] + "!*@*"
	if c := b.Chans.Get(f[0]); c != nil {
		if m := c.Member(f[1]); m != nil && m.Host != "" {
			mask = "*!*@" + m.Host
		}
	}
	_ = b.Chans.AddBan(chanstate.Ban{Channel: f[0], Mask: mask, Reason: reason, Setter: u.Handle, CreatedAt: time.Now()})
	b.IRC.Mode(f[0], "+b", mask)
	b.IRC.Kick(f[0], f[1], reason)
}

func (b *Bot) cmdAddBan(s *partyline.Session, u *userfile.User, args string) {
	f := strings.Fields(args)
	if len(f) < 2 {
		s.Write("usage: .+ban <mask> <channel> [reason]")
		return
	}
	reason := strings.Join(f[2:], " ")
	exp := time.Time{}
	if len(f) > 2 {
		if d, err := time.ParseDuration(f[2]); err == nil {
			exp = time.Now().Add(d)
			reason = strings.Join(f[3:], " ")
		}
	}
	if err := b.Chans.AddBan(chanstate.Ban{Channel: f[1], Mask: f[0], Reason: reason, Setter: u.Handle, CreatedAt: time.Now(), ExpiresAt: exp}); err != nil {
		s.Write(err.Error())
		return
	}
	b.IRC.Mode(f[1], "+b", f[0])
	s.Write("banned " + f[0])
}

func (b *Bot) cmdDelBan(s *partyline.Session, _ *userfile.User, args string) {
	f := strings.Fields(args)
	if len(f) < 2 {
		s.Write("usage: .-ban <mask> <channel>")
		return
	}
	if err := b.Chans.DelBan(f[1], f[0]); err != nil {
		s.Write("no such ban")
		return
	}
	b.IRC.Mode(f[1], "-b", f[0])
	s.Write("unbanned " + f[0])
}

func (b *Bot) cmdBans(s *partyline.Session, _ *userfile.User, args string) {
	if args == "" {
		s.Write("usage: .bans <channel>")
		return
	}
	for _, ban := range b.Chans.Bans(args) {
		s.Printf("  %s  %s  (%s)", ban.Mask, ban.Reason, ban.Setter)
	}
}

func normalizeChan(raw string) (string, error) {
	ch := strings.TrimSpace(raw)
	if i := strings.IndexAny(ch, " \t"); i >= 0 {
		ch = ch[:i]
	}
	if ch == "" {
		return "", fmt.Errorf("which channel?")
	}
	if !origin.IsChannel(ch) {
		ch = "#" + ch
	}
	return ch, nil
}

func defaultNewChanSet() flags.ChanSet {
	cs, _ := flags.ParseChanSet("+seen")
	return cs
}

func (b *Bot) cmdJoin(s *partyline.Session, _ *userfile.User, args string) {
	ch, err := normalizeChan(args)
	if err != nil {
		s.Write("usage: .join #channel")
		return
	}
	c := b.Chans.Get(ch)
	if c == nil {
		c = b.Chans.LoadOrCreate(ch, defaultNewChanSet(), "", "", "", "")
	}
	b.Chans.SetAutoJoin(ch, true)
	b.IRC.JoinKey(c.Name, c.Key)
	s.Write("JOIN sent for " + c.Name + " — .chan to see on/off")
}

func (b *Bot) cmdPart(s *partyline.Session, _ *userfile.User, args string) {
	ch, extra := split2(args)
	ch, err := normalizeChan(ch)
	if err != nil {
		s.Write("usage: .part #channel")
		return
	}
	c := b.Chans.Get(ch)
	if c == nil {
		s.Write("i'm not managing " + ch)
		return
	}
	b.Chans.SetAutoJoin(ch, false)
	b.Chans.SetJoined(ch, false)
	b.Chans.ClearMembers(ch)
	b.IRC.Part(c.Name)
	_ = extra
	s.Write("parting " + c.Name + " (still in .chan list; .-chan to drop)")
}

func (b *Bot) cmdPlusChan(s *partyline.Session, u *userfile.User, args string) {
	b.cmdJoin(s, u, args)
}

func (b *Bot) cmdMinusChan(s *partyline.Session, _ *userfile.User, args string) {
	ch, err := normalizeChan(args)
	if err != nil {
		s.Write("usage: .-chan #channel")
		return
	}
	c := b.Chans.Get(ch)
	name := ch
	if c != nil {
		name = c.Name
	}
	b.IRC.Part(name)
	b.Chans.Drop(ch)
	s.Write("removed " + name)
}

func (b *Bot) cmdChan(s *partyline.Session, u *userfile.User, args string) {
	if strings.TrimSpace(args) != "" {
		b.cmdChaninfo(s, u, args)
		return
	}
	all := b.Chans.All()
	if len(all) == 0 {
		s.Write("no channels — .join #chan or .+chan #chan")
		return
	}
	for _, c := range all {
		on := "off"
		if b.Botonchan(c.Name) {
			on = "on"
		}
		aj := ""
		if c.AutoJoin {
			aj = " autojoin"
		}
		s.Printf("  %-16s %-4s %s%s  members=%d", c.Name, on, c.Chanset.String(), aj, len(c.Members))
	}
}

func (b *Bot) cmdChanset(s *partyline.Session, _ *userfile.User, args string) {
	ch, spec := split2(args)
	if ch == "" {
		s.Write("usage: .chanset <channel> [+flag|-flag]")
		return
	}
	c := b.Chans.Get(ch)
	if c == nil {
		s.Write("i'm not on that channel")
		return
	}
	if spec == "" {
		s.Write(c.Name + " " + c.Chanset.String())
		return
	}
	if _, err := b.Chans.ApplyChanSet(ch, spec); err != nil {
		s.Write(err.Error())
		return
	}
	s.Write(c.Name + " " + c.Chanset.String())
}

func (b *Bot) cmdChaninfo(s *partyline.Session, _ *userfile.User, args string) {
	if args == "" {
		for _, c := range b.Chans.All() {
			s.Printf("%s %s topic=%q", c.Name, c.Chanset.String(), c.Topic)
		}
		return
	}
	c := b.Chans.Get(args)
	if c == nil {
		s.Write("unknown channel")
		return
	}
	s.Printf("%s %s", c.Name, c.Chanset.String())
	s.Printf("topic: %s", c.Topic)
	s.Printf("members: %s", strings.Join(c.Nicks(), ", "))
}

func (b *Bot) cmdSay(s *partyline.Session, _ *userfile.User, args string) {
	t, text := split2(args)
	if t == "" || text == "" {
		s.Write("usage: .say <target> <text>")
		return
	}
	b.IRC.Privmsg(t, text)
}

func (b *Bot) cmdAct(s *partyline.Session, _ *userfile.User, args string) {
	t, text := split2(args)
	if t == "" || text == "" {
		s.Write("usage: .act <target> <text>")
		return
	}
	b.IRC.Action(t, text)
}

func (b *Bot) cmdNotice(s *partyline.Session, _ *userfile.User, args string) {
	t, text := split2(args)
	if t == "" || text == "" {
		s.Write("usage: .notice <target> <text>")
		return
	}
	b.IRC.Notice(t, text)
}

func (b *Bot) cmdTopic(s *partyline.Session, _ *userfile.User, args string) {
	ch, text := split2(args)
	if ch == "" {
		s.Write("usage: .topic <channel> <text>")
		return
	}
	b.IRC.Topic(ch, text)
}

func (b *Bot) cmdJump(s *partyline.Session, _ *userfile.User, _ string) {
	s.Write("jumping...")
	b.IRC.Reconnect()
}

func (b *Bot) cmdSave(s *partyline.Session, _ *userfile.User, _ string) {
	failed := 0
	for _, c := range b.Chans.All() {
		if err := b.Chans.Save(c); err != nil {
			failed++
			b.Log.Error("save channel", "channel", c.Name, "err", err)
		}
	}
	if failed > 0 {
		s.Printf("save failed for %d channel(s)", failed)
		return
	}
	b.saveChatHist()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := b.Store.Checkpoint(ctx, store.CheckpointTruncate)
	if err != nil {
		s.Write("saved, wal checkpoint failed: " + err.Error())
		return
	}
	if res.Blocked != 0 {
		s.Printf("saved (wal checkpoint blocked, %d/%d frames)", res.Checkpointed, res.Log)
		return
	}
	s.Printf("saved (wal checkpointed %d frames)", res.Checkpointed)
}

func (b *Bot) cmdReload(s *partyline.Session, _ *userfile.User, _ string) {
	failed := false
	if err := b.Lua.LoadDir(b.Cfg.Scripts.LuaDir); err != nil {
		s.Write("lua: " + err.Error())
		failed = true
	}
	if b.Cfg.Scripts.PythonEnabled {
		if err := b.Py.LoadDir(b.Cfg.Scripts.PythonDir); err != nil {
			s.Write("python: " + err.Error())
			failed = true
		}
	}
	if failed {
		s.Write("reload completed with errors")
		return
	}
	s.Write("scripts reloaded")
}

func (b *Bot) cmdRehash(s *partyline.Session, _ *userfile.User, _ string) {
	if b.Cfg.Path == "" {
		s.Write("config path unknown")
		return
	}
	next, err := config.Load(b.Cfg.Path)
	if err != nil {
		s.Write("rehash failed: " + err.Error())
		return
	}
	diff := config.Diff(b.Cfg, next)
	for _, c := range diff {
		if !c.Hot {
			s.Write("cold setting " + c.Path + " requires restart")
			return
		}
	}
	b.Cfg = next
	if b.LLM != nil {
		b.LLM.ReplaceConfig(next)
	}
	b.cmdReload(s, nil, "")
	s.Write("rehashed")
	for _, c := range diff {
		s.Write("  " + c.Path + " updated")
	}
}

func (b *Bot) cmdBackup(s *partyline.Session, _ *userfile.User, args string) {
	dest := strings.TrimSpace(args)
	if dest == "" {
		dest = strings.TrimSuffix(b.Cfg.Store.Path, ".db") + "-" + time.Now().Format("20060102-150405") + ".db"
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(b.Cfg.Store.Path), dest)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.Store.Backup(ctx, dest); err != nil {
		s.Write(err.Error())
		return
	}
	s.Write("backed up to " + dest)
}

func (b *Bot) cmdConfirm(s *partyline.Session, u *userfile.User, args string) {
	id := strings.TrimSpace(args)
	if id == "" {
		s.Write("usage: .confirm <id>")
		return
	}
	res, err := b.ConfirmProposal(id, u.Handle)
	if err != nil {
		s.Write(err.Error())
		return
	}
	s.Write(res)
}

func (b *Bot) cmdReject(s *partyline.Session, u *userfile.User, args string) {
	id := strings.TrimSpace(args)
	if id == "" {
		s.Write("usage: .reject <id>")
		return
	}
	if err := b.RejectProposal(id, u.Handle); err != nil {
		s.Write(err.Error())
		return
	}
	s.Write("rejected")
}

func (b *Bot) cmdDie(s *partyline.Session, _ *userfile.User, _ string) {
	s.Write("goodbye")
	go func() {
		time.Sleep(200 * time.Millisecond)
		b.Close()
	}()
}

func (b *Bot) cmdNote(s *partyline.Session, u *userfile.User, args string) {
	to, body := split2(args)
	if to == "" || body == "" {
		s.Write("usage: .note <handle> <text>")
		return
	}
	if err := b.Notes.Add(to, u.Handle, body); err != nil {
		s.Write(err.Error())
		return
	}
	s.Write("noted")
}

func (b *Bot) cmdNotes(s *partyline.Session, u *userfile.User, _ string) {
	ns := b.Notes.Pending(u.Handle)
	if len(ns) == 0 {
		s.Write("no notes")
		return
	}
	for _, n := range ns {
		s.Printf("  from %s: %s", n.From, n.Body)
	}
	b.Notes.MarkRead(u.Handle)
}

func (b *Bot) cmdSeen(s *partyline.Session, _ *userfile.User, args string) {
	if args == "" {
		s.Write("usage: .seen <nick>")
		return
	}
	r, err := b.Seen.Lookup(args)
	if err != nil {
		s.Write(err.Error())
		return
	}
	s.Write(b.Seen.Format(args, r))
}

func (b *Bot) cmdQuote(s *partyline.Session, _ *userfile.User, args string) {
	ch, search := split2(args)
	if ch == "" {
		s.Write("usage: .quote <channel> [search]")
		return
	}
	q, err := b.Quotes.Random(ch, search)
	if err != nil {
		s.Write(err.Error())
		return
	}
	if q == nil {
		s.Write("no quotes")
		return
	}
	s.Write(q.Format())
}

func (b *Bot) cmdAddQuote(s *partyline.Session, u *userfile.User, args string) {
	ch, body := split2(args)
	if ch == "" || body == "" {
		s.Write("usage: .+quote <channel> <text>")
		return
	}
	id, err := b.Quotes.Add(ch, u.Handle, body)
	if err != nil {
		s.Write(err.Error())
		return
	}
	s.Printf("quote #%d", id)
}

func (b *Bot) cmdDelQuote(s *partyline.Session, _ *userfile.User, args string) {
	id, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
	if err != nil {
		s.Write("usage: .-quote <id>")
		return
	}
	if err := b.Quotes.Delete(id); err != nil {
		s.Write("no such quote")
		return
	}
	s.Write("deleted")
}

func (b *Bot) cmdAI(s *partyline.Session, u *userfile.User, args string) {
	f := strings.Fields(args)
	if len(f) == 0 {
		s.Printf("llm enabled=%v model=%s base=%s key=%v",
			b.Cfg.LLM.Enabled, b.Cfg.LLM.Model, b.Cfg.LLM.BaseURL, b.Cfg.LLM.APIKey != "")
		return
	}
	switch strings.ToLower(f[0]) {
	case "enable", "on":
		b.Cfg.LLM.Enabled = true
		s.Write("llm enabled")
	case "disable", "off":
		b.Cfg.LLM.Enabled = false
		s.Write("llm disabled")
	case "model":
		if len(f) < 2 {
			s.Write("usage: .ai model <name>")
			return
		}
		b.Cfg.LLM.Model = f[1]
		b.LLM.Client.Model = f[1]
		s.Write("model " + f[1])
	default:
		go func() {
			reply, err := b.LLM.Ask("", s.Handle, u.Handle, u, "", args, false)
			if err != nil {
				s.Write("ai: " + err.Error())
				return
			}
			s.Write(reply)
		}()
	}
}

func (b *Bot) cmdBinds(s *partyline.Session, _ *userfile.User, _ string) {
	for _, bind := range b.Scripts.Binds() {
		s.Printf("  %-6s %-6s %-16s %s (%s)", bind.Type, bind.Flags, bind.Mask, bind.Name, bind.File)
	}
}

func (b *Bot) cmdQuit(s *partyline.Session, _ *userfile.User, _ string) {
	s.Write("bye")
	s.Conn.Close()
}
