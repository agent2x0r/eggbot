package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"eggbot/internal/chanlog"
	"eggbot/internal/flags"
	"eggbot/internal/llm"
	"eggbot/internal/origin"
	"eggbot/internal/userfile"
)

func (b *Bot) handleMsg(o origin.Origin, u *userfile.User, text string) {
	cmd, args := origin.SplitToken(text)
	cmd = strings.ToLower(cmd)
	switch cmd {
	case "hello":
		b.msgHello(o, u)
	case "pass", "password":
		b.msgPass(o, u, args)
	case "ident", "auth":
		b.msgIdent(o, args)
	case "op":
		b.msgOp(o, u, args)
	case "invite":
		b.msgInvite(o, u, args)
	case "seen":
		b.pubSeen(o.Nick, args, func(s string) { b.IRC.Notice(o.Nick, s) })
	case "note":
		b.msgNote(o, u, args)
	case "notes":
		b.msgNotes(o, u)
	case "chat":
		b.msgChat(o, u)
	case "catchup", "summary", "recap":
		ch := args
		if ch == "" {
			if r, _ := b.Seen.Lookup(o.Nick); r != nil {
				ch = r.Channel
			}
		}
		c := b.Chans.Get(ch)
		if c == nil || !c.Chanset.Has("ai") || c.Member(o.Nick) == nil {
			b.IRC.Notice(o.Nick, "catchup is only available while you are in an AI-enabled channel")
			return
		}
		b.goWork(func() { b.askLLM(ch, o, u, "catch me up", llm.DestCatchup) })
	case "help":
		for _, line := range queryHelpLines(b.IRC.Nick()) {
			b.IRC.Notice(o.Nick, line)
		}
	case "whoami":
		if u == nil {
			b.IRC.Notice(o.Nick, "i don't know you. /msg "+b.IRC.Nick()+" hello")
			return
		}
		b.IRC.Notice(o.Nick, fmt.Sprintf("you are %s +%s", u.Handle, u.Global.String()))
	default:
		if b.Cfg.LLM.AllowPrivmsg && b.Cfg.LLM.Enabled {
			b.goWork(func() { b.askLLM("", o, u, text, llm.DestChannel) })
		}
	}
}

func (b *Bot) handlePublic(o origin.Origin, u *userfile.User, channel, text string) {
	c := b.Chans.Get(channel)
	if cmd, args, ok := origin.Bang(text); ok {
		switch cmd {
		case "seen":
			if c != nil && c.Chanset.Has("seen") {
				b.pubSeen(o.Nick, args, func(s string) { b.IRC.PrivmsgHelp(channel, s) })
			}
		case "quote":
			q, err := b.Quotes.Random(channel, args)
			if err != nil {
				return
			}
			if q == nil {
				b.IRC.PrivmsgHelp(channel, "no quotes")
				return
			}
			b.IRC.PrivmsgHelp(channel, q.Format())
		case "note":
			if u == nil {
				b.IRC.PrivmsgHelp(channel, o.Nick+": i don't know you")
				return
			}
			to, body, _ := strings.Cut(args, " ")
			if err := b.Notes.Add(to, u.Handle, strings.TrimSpace(body)); err != nil {
				b.IRC.PrivmsgHelp(channel, o.Nick+": "+err.Error())
				return
			}
			b.IRC.PrivmsgHelp(channel, o.Nick+": noted")
		case "ask", "ai":
			if c != nil && c.Chanset.Has("ai") && c.Member(o.Nick) != nil {
				b.openSticky(channel, o, u)
				b.goWork(func() { b.askLLM(channel, o, u, args, llm.DestChannel) })
			}
		case "search":
			if c != nil && c.Chanset.Has("ai") && c.Member(o.Nick) != nil {
				b.goWork(func() { b.askLLM(channel, o, u, args, llm.DestSearch) })
			}
		case "history":
			if c != nil && c.Chanset.Has("ai") && c.Member(o.Nick) != nil {
				b.goWork(func() { b.askHistory(channel, o, u, args) })
			}
		case "catchup", "summary", "recap":
			if c != nil && c.Chanset.Has("ai") {
				b.goWork(func() { b.askLLM(channel, o, u, args, llm.DestCatchup) })
			}
		case "help":
			b.sendPublicHelp(channel)
		}
		return
	}
	if c == nil || !c.Chanset.Has("ai") {
		return
	}
	if body, ok := origin.Addressed(b.IRC.Nick(), text); ok {
		cmd, _ := origin.SplitToken(body)
		switch strings.ToLower(cmd) {
		case "hello", "pass", "password", "ident", "auth", "whoami", "chat", "note", "notes":
			b.handleMsg(o, u, body)
			return
		case "help":
			b.sendPublicHelp(channel)
			return
		}
		if strings.TrimSpace(body) == "" {
			return
		}
		b.openSticky(channel, o, u)
		b.goWork(func() { b.askLLM(channel, o, u, body, llm.DestChannel) })
		return
	}
	if b.Cfg == nil || !b.Cfg.LLM.Sticky || !b.Cfg.LLM.Enabled || b.sticky == nil {
		return
	}
	prompt, kind := b.sticky.consider(channel, o.Nick, text, b.now())
	if kind != stickyYes {
		return
	}
	if b.sticky.holding(channel, o.Nick) {
		handle := ""
		if u != nil {
			handle = u.Handle
		}
		b.sticky.pause(channel, o.Nick, prompt, o, handle, time.Time{}, b.now())
		return
	}
	b.goWork(func() { b.askLLMOpts(channel, o, u, prompt, llm.DestChannel, true) })
}

func (b *Bot) say(channel, text string) {
	b.IRC.PrivmsgHelp(channel, text)
	if origin.IsChannel(channel) && b.LLM != nil {
		b.LLM.Remember(channel, b.IRC.Nick(), text)
		b.logChat(channel, b.IRC.Nick(), text)
	}
}

func (b *Bot) logChat(channel, nick, text string) {
	if b.ChanLog == nil {
		return
	}
	if err := b.ChanLog.Add(channel, nick, text); err != nil {
		if b.Log != nil {
			b.Log.Error("chanlog", "channel", channel, "err", err)
		}
		if b.Store != nil {
			b.Store.NoteWriteError()
		}
	}
}

func (b *Bot) sendPublicHelp(channel string) {
	for _, line := range publicHelpLines(b.IRC.Nick()) {
		b.IRC.PrivmsgHelp(channel, line)
	}
}

func publicHelpLines(nick string) []string {
	return []string{
		"say " + nick + ": <text> (or !ask <text>) and I'll remember you on subsequent replies",
		"!search <question>     live web/X lookup",
		"!history <words>       search saved chat (optional time: last 2 days, 3 weeks ago)",
		"!note <handle> <text>  leave a note for a registered handle (not a nick)",
		"/msg " + nick + " notes       read notes left for you",
		"!seen <nick>   !quote   !catchup",
	}
}

func queryHelpLines(nick string) []string {
	return []string{
		"query: hello  pass  ident  whoami  seen  note  notes  chat  catchup  help",
		"in channel: " + nick + ": <text>  !ask  !search  !history  !note  !seen  !quote  !catchup  !help",
	}
}

func (b *Bot) askHistory(channel string, o origin.Origin, u *userfile.User, query string) {
	if b.ChanLog == nil {
		b.say(channel, o.Nick+": no channel history yet")
		return
	}
	lines, err := b.ChanLog.Search(channel, query, b.now())
	if err != nil {
		b.say(channel, o.Nick+": "+err.Error())
		return
	}
	if len(lines) == 0 {
		b.say(channel, o.Nick+": no matching history")
		return
	}
	prompt := "Question: " + strings.TrimSpace(query) +
		"\n\nSaved channel lines:\n" + chanlog.Format(lines)
	b.askLLM(channel, o, u, prompt, llm.DestHistory)
}

func (b *Bot) pubSeen(from, who string, reply func(string)) {
	if who == "" {
		reply("seen who?")
		return
	}
	r, err := b.Seen.Lookup(who)
	if err != nil {
		reply(err.Error())
		return
	}
	_ = from
	reply(b.Seen.Format(who, r))
}

func (b *Bot) msgHello(o origin.Origin, u *userfile.User) {
	if b.Cfg.Learn.ProductionLock || !b.Cfg.Learn.Hello {
		b.IRC.Notice(o.Nick, "learning is disabled")
		return
	}
	if !b.allowHello(o.NUH(), time.Now()) {
		b.IRC.Notice(o.Nick, "too many registrations from your host")
		return
	}
	if u != nil {
		b.IRC.Notice(o.Nick, "i already know you as "+u.Handle)
		return
	}
	handle := o.Nick
	if err := b.Users.Add(handle, ""); err != nil {
		b.IRC.Notice(o.Nick, err.Error())
		return
	}
	_ = b.Users.AddHost(handle, o.NUH())
	if b.Cfg.Learn.HelloFlags != "" {
		_, _ = b.Users.Chattr(handle, b.Cfg.Learn.HelloFlags, "")
	}
	msg := "hi, your handle is " + handle + ". set a password: /msg " + b.IRC.Nick() + " pass <password>"
	if b.Cfg.IsOwnerHandle(handle) {
		_, _ = b.Users.Chattr(handle, "+np", "")
		msg += " — you are an owner, then telnet 127.0.0.1:3333"
	} else {
		msg += " — you still need +p from an owner to use the partyline"
	}
	b.IRC.Notice(o.Nick, msg)
}

func (b *Bot) msgPass(o origin.Origin, u *userfile.User, args string) {
	if u == nil {
		b.IRC.Notice(o.Nick, "say hello first")
		return
	}
	if args == "" {
		b.IRC.Notice(o.Nick, "usage: pass <newpassword> (or pass <current> <newpassword>)")
		return
	}
	newPassword := args
	authKey := ""
	if u.HasPass() {
		current, next := origin.SplitToken(args)
		if next == "" {
			b.IRC.Notice(o.Nick, "password already set; use: pass <current> <newpassword>")
			return
		}
		if err := userfile.ValidatePassword(next); err != nil {
			b.IRC.Notice(o.Nick, err.Error())
			return
		}
		authKey = strings.ToLower(o.NUH())
		if !b.allowAuthAttempt(authKey, time.Now()) {
			b.IRC.Notice(o.Nick, "too many authentication attempts; try again later")
			return
		}
		if !b.Users.CheckPass(u.Handle, current) {
			b.IRC.Notice(o.Nick, "current password is incorrect")
			return
		}
		newPassword = next
	}
	if err := b.Users.SetPass(u.Handle, newPassword); err != nil {
		b.IRC.Notice(o.Nick, err.Error())
		return
	}
	if authKey != "" {
		b.clearAuthAttempts(authKey)
	}
	if u.Global.Has(flags.Party) || u.Global.Has(flags.Owner) || b.Cfg.IsOwnerHandle(u.Handle) {
		b.IRC.Notice(o.Nick, "password set. telnet 127.0.0.1:3333  handle "+u.Handle)
		return
	}
	b.IRC.Notice(o.Nick, "password set. you still need +p from an owner for the partyline")
}

func (b *Bot) msgIdent(o origin.Origin, args string) {
	handle, pw := origin.SplitToken(args)
	if handle == "" || pw == "" {
		b.IRC.Notice(o.Nick, "usage: ident <handle> <password>")
		return
	}
	authKey := strings.ToLower(o.NUH())
	if !b.allowAuthAttempt(authKey, time.Now()) {
		b.IRC.Notice(o.Nick, "too many authentication attempts; try again later")
		return
	}
	if !b.Users.CheckPass(handle, pw) {
		b.IRC.Notice(o.Nick, "nope")
		return
	}
	b.clearAuthAttempts(authKey)
	_ = b.Users.AddHost(handle, o.NUH())
	b.IRC.Notice(o.Nick, "recognized. added host "+o.NUH())
}

func (b *Bot) allowHello(key string, now time.Time) bool {
	limit := b.Cfg.Learn.HelloPerHour
	if limit <= 0 {
		limit = 10
	}
	cutoff := now.Add(-time.Hour)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.helloHits == nil {
		b.helloHits = map[string][]time.Time{}
	}
	hits := b.helloHits[key]
	keep := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= limit {
		b.helloHits[key] = keep
		return false
	}
	b.helloHits[key] = append(keep, now)
	return true
}

func (b *Bot) allowAuthAttempt(key string, now time.Time) bool {
	const (
		window       = time.Minute
		maxAttempts  = 5
		maxTrackedID = 4096
	)
	cutoff := now.Add(-window)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.authAttempts == nil {
		b.authAttempts = make(map[string][]time.Time)
	}
	for id, attempts := range b.authAttempts {
		keep := attempts[:0]
		for _, attempt := range attempts {
			if attempt.After(cutoff) {
				keep = append(keep, attempt)
			}
		}
		if len(keep) == 0 {
			delete(b.authAttempts, id)
		} else {
			b.authAttempts[id] = keep
		}
	}
	attempts := b.authAttempts[key]
	if len(attempts) >= maxAttempts {
		return false
	}
	if len(attempts) == 0 && len(b.authAttempts) >= maxTrackedID {
		return false
	}
	b.authAttempts[key] = append(attempts, now)
	return true
}

func (b *Bot) clearAuthAttempts(key string) {
	b.mu.Lock()
	delete(b.authAttempts, key)
	b.mu.Unlock()
}

func (b *Bot) msgOp(o origin.Origin, u *userfile.User, args string) {
	if u == nil {
		b.IRC.Notice(o.Nick, "i don't know you")
		return
	}
	ch := strings.TrimSpace(args)
	if ch == "" {
		b.IRC.Notice(o.Nick, "usage: op #channel")
		return
	}
	if !b.need(u, "o", ch) && !u.Global.Has(flags.Owner) && !u.Global.Has(flags.Master) {
		b.IRC.Notice(o.Nick, "you need +o")
		return
	}
	b.IRC.Mode(ch, "+o", o.Nick)
}

func (b *Bot) msgInvite(o origin.Origin, u *userfile.User, args string) {
	if u == nil {
		return
	}
	ch := strings.TrimSpace(args)
	if ch == "" {
		return
	}
	if !b.need(u, "o", ch) && !u.Global.Has(flags.Owner) {
		return
	}
	b.IRC.Invite(o.Nick, ch)
}

func (b *Bot) msgNote(o origin.Origin, u *userfile.User, args string) {
	if u == nil {
		b.IRC.Notice(o.Nick, "say hello first")
		return
	}
	to, body, _ := strings.Cut(args, " ")
	if err := b.Notes.Add(to, u.Handle, strings.TrimSpace(body)); err != nil {
		b.IRC.Notice(o.Nick, err.Error())
		return
	}
	b.IRC.Notice(o.Nick, "noted")
}

func (b *Bot) msgNotes(o origin.Origin, u *userfile.User) {
	if u == nil {
		return
	}
	ns := b.Notes.Pending(u.Handle)
	if len(ns) == 0 {
		b.IRC.Notice(o.Nick, "no notes")
		return
	}
	for _, n := range ns {
		b.IRC.Notice(o.Nick, fmt.Sprintf("from %s: %s", n.From, n.Body))
	}
	b.Notes.MarkRead(u.Handle)
}

func (b *Bot) msgChat(o origin.Origin, u *userfile.User) {
	if u == nil || !(u.Global.Has(flags.Party) || u.Global.Has(flags.Owner)) {
		b.IRC.Notice(o.Nick, "you need +p")
		return
	}
	addr := b.Cfg.Partyline.Listen
	b.IRC.Notice(o.Nick, "telnet "+addr+"  (handle "+u.Handle+")")
}

func (b *Bot) askLLM(channel string, o origin.Origin, u *userfile.User, prompt string, dest string) {
	b.askLLMOpts(channel, o, u, prompt, dest, false)
}

// askLLMOpts is askLLM; followUp marks a sticky line sent without a prefix,
// the only kind of turn where the model may stay quiet.
func (b *Bot) logAskQuiet(what, channel, nick, dest, reply string) {
	if b.Log != nil {
		b.Log.Info("llm "+what, "channel", channel, "nick", nick, "dest", dest, "reply", reply)
	}
}

func (b *Bot) askLLMOpts(channel string, o origin.Origin, u *userfile.User, prompt string, dest string, followUp bool) {
	if strings.TrimSpace(prompt) == "" && dest != llm.DestCatchup {
		if dest == llm.DestSearch && channel != "" {
			b.say(channel, o.Nick+": search what?")
		}
		return
	}
	if dest == "" {
		dest = llm.DestChannel
	}
	forceSearch := dest == llm.DestSearch
	persona := ""
	ops := false
	if c := b.Chans.Get(channel); c != nil {
		persona = c.LLMPersona
		ops = c.Chanset.Has("aitools")
	}
	handle := ""
	if u != nil {
		handle = u.Handle
	}
	req := llm.AskReq{
		Channel: channel, Nick: o.Nick, Handle: handle, User: u,
		Persona: persona, Prompt: prompt, Topic: b.channelTopic(channel), Dest: dest, FollowUp: followUp,
		Helpful: dest != llm.DestCatchup && dest != llm.DestSearch && dest != llm.DestHistory, Ops: ops && dest != llm.DestCatchup && dest != llm.DestSearch && dest != llm.DestHistory,
	}
	if err := b.LLM.Admit(&req); err != nil {
		if (dest == llm.DestChannel || dest == llm.DestSearch) && channel != "" {
			if rl, ok := llm.IsRateLimited(err); ok {
				handle := ""
				if u != nil {
					handle = u.Handle
				}
				if b.sticky != nil && b.Cfg.LLM.Sticky {
					warn := b.sticky.pause(channel, o.Nick, prompt, o, handle, rl.RetryAt, b.now())
					if warn {
						b.IRC.Notice(o.Nick, "too fast — I'll get to that when I can")
					}
					return
				}
			}
		}
		if channel != "" {
			b.say(channel, o.Nick+": "+err.Error())
		} else {
			b.IRC.PrivmsgHelp(o.Nick, err.Error())
		}
		return
	}
	search := false
	if forceSearch && b.Cfg.LLM.Search {
		search = true
	} else if dest != llm.DestCatchup && dest != llm.DestDM && dest != llm.DestSearch && dest != llm.DestHistory && b.Cfg.LLM.Search {
		search = b.LLM.Route(prompt, b.LLM.RouteContext(channel, req.Topic, 8)) == llm.RouteSearch
	}
	req.Search = search
	done := make(chan struct{})
	onStart := func() {
		if !(search && channel != "" && (dest == llm.DestChannel || dest == llm.DestSearch)) {
			return
		}
		go func() {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Second):
			}
			select {
			case <-done:
				return
			default:
			}
			if b.LLM.ClaimSearchAck(handle, o.Nick) {
				b.say(channel, o.Nick+": Searching...")
			}
		}()
	}
	req.OnStart = onStart
	reply, err := b.LLM.AskReq(req)
	close(done)
	if err != nil {
		if errors.Is(err, llm.ErrStale) {
			return
		}
		if channel != "" {
			b.say(channel, o.Nick+": "+err.Error())
		} else {
			b.IRC.PrivmsgHelp(o.Nick, err.Error())
		}
		return
	}
	if forceSearch && (llm.SilentReply(reply) || llm.DropContext(reply) || strings.TrimSpace(reply) == "") {
		b.say(channel, o.Nick+": nothing useful came back")
		return
	}
	quiet := llm.DropContext(reply) || llm.SilentReply(reply)
	if quiet && !followUp && dest != llm.DestCatchup {
		// Someone asked directly; never swallow it without a word.
		b.logAskQuiet("explicit ask got no reply", channel, o.Nick, dest, reply)
		if channel != "" {
			b.say(channel, o.Nick+": I didn't get an answer for that, try again")
		} else {
			b.IRC.PrivmsgHelp(o.Nick, "I didn't get an answer for that, try again")
		}
		return
	}
	if llm.DropContext(reply) {
		b.logAskQuiet("sticky window dropped", channel, o.Nick, dest, reply)
		if b.sticky != nil && channel != "" {
			b.sticky.drop(channel, o.Nick)
		}
		return
	}
	if llm.SilentReply(reply) {
		b.logAskQuiet("follow-up stayed silent", channel, o.Nick, dest, reply)
		return
	}
	switch dest {
	case llm.DestDM:
		b.IRC.PrivmsgHelp(o.Nick, reply)
	default:
		if channel != "" {
			b.say(channel, o.Nick+": "+reply)
			if dest == llm.DestChannel || dest == llm.DestSearch || dest == llm.DestHistory {
				b.openSticky(channel, o, u)
				if b.sticky != nil {
					b.sticky.touchReply(channel, o.Nick, b.now())
				}
			}
		} else {
			b.IRC.PrivmsgHelp(o.Nick, reply)
		}
	}
}
