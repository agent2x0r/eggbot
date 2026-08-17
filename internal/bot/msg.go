package bot

import (
	"fmt"
	"strings"
	"time"

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
		b.IRC.Notice(o.Nick, "hello pass ident op invite seen note notes chat whoami help catchup")
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
			if c != nil && c.Chanset.Has("ai") {
				b.goWork(func() { b.askLLM(channel, o, u, args, llm.DestChannel) })
			}
		case "catchup", "summary", "recap":
			if c != nil && c.Chanset.Has("ai") {
				b.goWork(func() { b.askLLM(channel, o, u, args, llm.DestCatchup) })
			}
		}
	}
	if c == nil || !c.Chanset.Has("ai") {
		return
	}
	if body, ok := origin.Addressed(b.IRC.Nick(), text); ok {
		cmd, _ := origin.SplitToken(body)
		switch strings.ToLower(cmd) {
		case "hello", "pass", "password", "ident", "auth", "help", "whoami", "chat", "note", "notes":
			b.handleMsg(o, u, body)
			return
		}
		b.goWork(func() { b.askLLM(channel, o, u, body, llm.DestChannel) })
	}
}

func (b *Bot) say(channel, text string) {
	b.IRC.PrivmsgHelp(channel, text)
	if origin.IsChannel(channel) && b.LLM != nil {
		b.LLM.Remember(channel, b.IRC.Nick(), text)
	}
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
	if strings.TrimSpace(prompt) == "" && dest != llm.DestCatchup {
		return
	}
	if dest == "" {
		dest = llm.DestChannel
	}
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
		Persona: persona, Prompt: prompt, Topic: b.channelTopic(channel), Dest: dest,
		Helpful: dest != llm.DestCatchup, Ops: ops && dest != llm.DestCatchup,
	}
	if err := b.LLM.Admit(&req); err != nil {
		if channel != "" {
			b.say(channel, o.Nick+": "+err.Error())
		} else {
			b.IRC.PrivmsgHelp(o.Nick, err.Error())
		}
		return
	}
	search := false
	if dest != llm.DestCatchup && dest != llm.DestDM && b.Cfg.LLM.Search {
		search = b.LLM.Route(prompt, b.LLM.RouteContext(channel, req.Topic, 8)) == llm.RouteSearch
	}
	req.Search = search
	done := make(chan struct{})
	onStart := func() {
		if !(search && channel != "" && dest == llm.DestChannel) {
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
		if channel != "" {
			b.say(channel, o.Nick+": "+err.Error())
		} else {
			b.IRC.PrivmsgHelp(o.Nick, err.Error())
		}
		return
	}
	switch dest {
	case llm.DestDM:
		b.IRC.PrivmsgHelp(o.Nick, reply)
	default:
		if channel != "" {
			b.say(channel, o.Nick+": "+reply)
		} else {
			b.IRC.PrivmsgHelp(o.Nick, reply)
		}
	}
}
