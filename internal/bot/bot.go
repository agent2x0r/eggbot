// Package bot is the process core: IRC events, partyline commands, and LLM asks.
// +n implies master/op/partyline for command access (see canParty).
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/chanstate"
	"eggbot/internal/config"
	"eggbot/internal/dcc"
	"eggbot/internal/flags"
	"eggbot/internal/github"
	"eggbot/internal/hostmask"
	"eggbot/internal/identity"
	"eggbot/internal/irccase"
	"eggbot/internal/ircstate"
	"eggbot/internal/ircx"
	"eggbot/internal/llm"
	"eggbot/internal/notes"
	"eggbot/internal/origin"
	"eggbot/internal/partyline"
	"eggbot/internal/protect"
	"eggbot/internal/queue"
	"eggbot/internal/quotes"
	"eggbot/internal/script"
	scriplua "eggbot/internal/script/lua"
	scriptpy "eggbot/internal/script/python"
	"eggbot/internal/seen"
	"eggbot/internal/sensitive"
	"eggbot/internal/store"
	"eggbot/internal/userfile"
	"eggbot/internal/version"
)

var Version = version.UserAgent()

type Bot struct {
	Cfg     *config.Config
	Log     *slog.Logger
	Store   *store.Store
	Users   *userfile.File
	Chans   *chanstate.State
	Net     *ircstate.Network
	Ident   *identity.Registry
	IRC     *ircx.Client
	PL      *partyline.Server
	Protect *protect.Engine
	Seen    *seen.Module
	Notes   *notes.Module
	Quotes  *quotes.Module
	Scripts *script.Engine
	Lua     *scriplua.Host
	Py      *scriptpy.Host
	LLM     *llm.Brain
	GitHub  *github.Client
	Started time.Time

	mu    sync.Mutex
	dying bool

	authAttempts map[string][]time.Time
	helloHits    map[string][]time.Time
	dccSlots     chan struct{}
	closeOnce    sync.Once
	workers      sync.WaitGroup
	cancel       context.CancelFunc
	ctx          context.Context
	proposals    map[string]*Proposal
}

func New(cfg *config.Config, log *slog.Logger) (*Bot, error) {
	st, err := store.Open(cfg.Store.Path)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	b := &Bot{
		Cfg:          cfg,
		Log:          log,
		Store:        st,
		Users:        userfile.New(st, cfg.Owners.Handles),
		Chans:        chanstate.New(st),
		Ident:        identity.New(st),
		IRC:          ircx.New(cfg, log),
		Seen:         seen.New(st),
		Notes:        notes.New(st),
		Quotes:       quotes.New(st),
		Started:      time.Now(),
		authAttempts: make(map[string][]time.Time),
		helloHits:    make(map[string][]time.Time),
		dccSlots:     make(chan struct{}, 2),
		proposals:    map[string]*Proposal{},
	}
	b.Net = ircstate.New(b.IRC.ISupport())
	if err := b.Users.SeedOwners(); err != nil {
		_ = st.Close()
		return nil, err
	}
	for _, name := range cfg.ChannelNames() {
		cc := cfg.Channel[name]
		cs, _ := flags.ParseChanSet(cc.Chanset)
		b.Chans.LoadOrCreate(name, cs, cc.Greet, cc.LLMPersona, cc.NeedOp, cc.Key)
		b.Chans.SetAutoJoin(name, true)
		// toml is source of truth when the channel is configured
		b.Chans.SetPersona(name, cc.LLMPersona)
	}
	def, _ := flags.ParseChanSet("+seen")
	for _, name := range b.Chans.AutoJoinNames() {
		b.Chans.LoadOrCreate(name, def, "", "", "", "")
	}
	b.Protect = protect.New(b.IRC, b, protect.DefaultLimits())
	b.Scripts = script.New(b, log, cfg.Scripts.Trusted)
	b.Lua = scriplua.New(b.Scripts)
	sdk := cfg.Scripts.PythonDir
	b.Py = scriptpy.New(b.Scripts, cfg.Scripts.PythonBin, sdk)
	b.LLM = llm.NewBrain(cfg, log, b, b.toolDefs())
	b.GitHub = github.NewClient("", "", 8*time.Second)
	b.GitHub.UserAgent = Version
	b.PL = &partyline.Server{
		Listen:    cfg.Partyline.Listen,
		TLSListen: cfg.Partyline.TLSListen,
		TLSCert:   cfg.Partyline.TLSCert,
		TLSKey:    cfg.Partyline.TLSKey,
		Log:       log,
		Authenticate: func(handle, password string) error {
			u, err := b.Users.Get(handle)
			if err != nil {
				return fmt.Errorf("no such handle — /msg %s hello first", b.IRC.Nick())
			}
			if !u.Global.Has(flags.Party) && !u.Global.Has(flags.Owner) && !u.Global.Has(flags.Master) {
				return fmt.Errorf("need +p — an owner must .chattr %s +p", u.Handle)
			}
			if !u.HasPass() {
				if b.Cfg.Learn.ProductionLock {
					return fmt.Errorf("production profile: set the owner password with eggbot -bootstrap-owner")
				}
				if !b.Users.IsPermanentOwner(u.Handle) {
					return fmt.Errorf("no password set — /msg %s pass <password>", b.IRC.Nick())
				}
				if strings.TrimSpace(password) == "" {
					return fmt.Errorf("choose a password (first login as owner)")
				}
				return b.Users.SetPass(u.Handle, password)
			}
			if !b.Users.CheckPass(handle, password) {
				return fmt.Errorf("bad password")
			}
			return nil
		},
		OnLine: b.onPartyline,
		OnJoin: func(s *partyline.Session) {
			b.PL.Broadcast(fmt.Sprintf("*** %s joined the partyline", s.Handle), false, nil)
			if notes := b.Notes.Pending(s.Handle); len(notes) > 0 {
				s.Printf("you have %d note(s). .notes to read.", len(notes))
			}
		},
		OnLeave: func(s *partyline.Session) {
			b.PL.Broadcast(fmt.Sprintf("*** %s left the partyline", s.Handle), false, nil)
		},
	}
	return b, nil
}

func (b *Bot) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.ctx = ctx
	b.cancel = cancel
	b.mu.Unlock()
	if err := b.PL.Start(); err != nil {
		return err
	}
	b.Scripts.Start()
	if err := b.Lua.LoadDir(b.Cfg.Scripts.LuaDir); err != nil {
		b.Log.Warn("lua dir", "err", err)
	}
	if b.Cfg.Scripts.PythonEnabled {
		if err := b.Py.LoadDir(b.Cfg.Scripts.PythonDir); err != nil {
			b.Log.Warn("python dir", "err", err)
		}
	}
	b.IRC.On(b.onIRC)
	b.IRC.OnDisconnect(func() {
		b.Net.Reset()
	})
	b.IRC.OnReady(func() {
		b.Net.SetSelf(b.IRC.Nick())
	})
	b.goWork(func() { b.expireLoop(ctx) })
	if err := b.IRC.Connect(); err != nil {
		return fmt.Errorf("irc connect: %w", err)
	}
	b.IRC.Loop()
	return nil
}

func (b *Bot) Close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.dying = true
		cancel := b.cancel
		b.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		b.IRC.Quit()
		b.PL.Stop()
		b.Scripts.Stop()
		b.Lua.Close()
		b.Py.Close()
		if b.LLM != nil {
			b.LLM.Close()
		}
		done := make(chan struct{})
		go func() {
			b.workers.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
		}
		_ = b.Store.Close()
	})
}

func (b *Bot) Ready() bool {
	return b.IRC != nil && b.IRC.Ready() && b.Store != nil
}

func (b *Bot) goWork(fn func()) {
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		fn()
	}()
}

func (b *Bot) authorize(handle, expr, channel string) error {
	if expr == "" || expr == "-" {
		return nil
	}
	if handle == "" {
		return fmt.Errorf("not authorized")
	}
	u, err := b.Users.Get(handle)
	if err != nil {
		return fmt.Errorf("not authorized")
	}
	if len(expr) == 1 {
		need := rune(expr[0])
		combined := flags.Combined(u.Global, b.Users.ChannelFlags(u, channel))
		if !flags.StrongerOrEqual(combined, need) {
			return fmt.Errorf("not authorized")
		}
		return nil
	}
	if !b.Users.MatchAttr(u, expr, channel) {
		return fmt.Errorf("not authorized")
	}
	return nil
}

func (b *Bot) onIRC(msg ircmsg.Message) {
	b.mu.Lock()
	dying := b.dying
	b.mu.Unlock()
	if dying {
		return
	}
	b.Net.Apply(msg)
	if sensitive.IRCMessage(msg, b.IRC.Nick()) {
		if strings.EqualFold(msg.Command, "PRIVMSG") {
			b.onPrivmsgSensitive(msg)
		}
		return
	}
	switch msg.Command {
	case "001":
		b.Net.SetSelf(b.IRC.Nick())
		b.onWelcome()
	case "PRIVMSG":
		b.onPrivmsg(msg)
	case "NOTICE":
		b.onNotice(msg)
	case "JOIN":
		b.onJoin(msg)
	case "PART":
		b.onPart(msg)
	case "QUIT":
		b.onQuit(msg)
	case "KICK":
		b.onKick(msg)
	case "MODE":
		b.onMode(msg)
	case "NICK":
		b.onNick(msg)
	case "TOPIC":
		b.onTopic(msg)
	case "353":
		b.onNames(msg)
	case "366":
		if ch, ok := origin.Parse366(msg.Params); ok {
			b.maybeNeedOp(ch)
		}
	case "332":
		if ch, topic, ok := origin.Parse332(msg.Params); ok {
			if c := b.Chans.Get(ch); c != nil {
				c.Topic = topic
				if err := b.Chans.Save(c); err != nil {
					b.Log.Error("save topic", "channel", ch, "err", err)
				}
			}
		}
	case "331":
		if ch, ok := origin.Parse366(msg.Params); ok {
			if c := b.Chans.Get(ch); c != nil {
				c.Topic = ""
				if err := b.Chans.Save(c); err != nil {
					b.Log.Error("clear topic", "channel", ch, "err", err)
				}
			}
		}
	case "INVITE":
		if len(msg.Params) >= 2 && irccase.Equal(msg.Params[0], b.IRC.Nick()) {
			if c := b.Chans.Get(msg.Params[1]); c != nil && c.AutoJoin {
				b.IRC.JoinKey(c.Name, c.Key)
			}
		}
	}
	ev := script.Event{
		Raw:     msg.Command,
		Text:    origin.Last(msg),
		Nick:    origin.FromMessage(msg).Nick,
		Host:    origin.FromMessage(msg).NUH(),
		Account: origin.FromMessage(msg).Account,
		Args:    append([]string{}, msg.Params...),
	}
	b.Scripts.Dispatch("raw", ev)
}

func (b *Bot) onNotice(msg ircmsg.Message) {
	if len(msg.Params) < 2 {
		return
	}
	o := origin.FromMessage(msg)
	text := msg.Params[1]
	if cmd, arg, ok := origin.IsCTCP(text); ok {
		_ = cmd
		_ = arg
		return
	}
	b.PL.Consolef('m', "[notice] <%s> %s", o.Nick, text)
}

func (b *Bot) onWelcome() {
	for _, c := range b.Chans.All() {
		if !c.AutoJoin {
			continue
		}
		b.IRC.JoinKey(c.Name, c.Key)
	}
}

func (b *Bot) resolve(o origin.Origin) (*userfile.User, origin.Origin) {
	network := b.Cfg.NetworkID()
	if handle, mech := b.Ident.Resolve(network, o.Account, o.CertFP); handle != "" {
		if u, err := b.Users.Get(handle); err == nil {
			o.Handle = u.Handle
			b.touchUser(u.Handle)
			_ = mech
			return u, o
		}
	}
	if u := b.Users.FindByHost(o.NUH()); u != nil {
		o.Handle = u.Handle
		b.touchUser(u.Handle)
		if o.Account != "" && o.Account != "*" {
			_ = b.Ident.Bind(u.Handle, network, identity.MechAccount, o.Account)
		}
		if o.CertFP != "" {
			_ = b.Ident.Bind(u.Handle, network, identity.MechCertFP, o.CertFP)
		}
		return u, o
	}
	for _, mask := range b.Cfg.Owners.AutoOwnerHosts {
		if hostmask.Match(mask, o.NUH()) {
			handle := irccase.HandleFold(o.Nick)
			if !b.Cfg.IsOwnerHandle(handle) {
				continue
			}
			if _, err := b.Users.Get(handle); err != nil {
				_ = b.Users.Add(handle, "")
			}
			_, _ = b.Users.Chattr(handle, "+np", "")
			_ = b.Users.AddHost(handle, o.NUH())
			if o.Account != "" && o.Account != "*" {
				_ = b.Ident.Bind(handle, network, identity.MechAccount, o.Account)
			}
			if o.CertFP != "" {
				_ = b.Ident.Bind(handle, network, identity.MechCertFP, o.CertFP)
			}
			u, _ := b.Users.Get(handle)
			if u != nil {
				o.Handle = u.Handle
			}
			return u, o
		}
	}
	return nil, o
}

func (b *Bot) onPrivmsgSensitive(msg ircmsg.Message) {
	if len(msg.Params) < 2 {
		return
	}
	o := origin.FromMessage(msg)
	u, o := b.resolve(o)
	target, text := msg.Params[0], msg.Params[1]
	if origin.IsChannel(target) {
		b.Protect.OnPrivmsg(target, o)
		b.IRC.PrivmsgHelp(target, o.Nick+": send password commands in a private message")
		return
	}
	b.PL.Consolef('m', "[msg] <%s> %s", o.Nick, sensitive.Redact(text))
	b.handleMsg(o, u, text)
}

func (b *Bot) onPrivmsg(msg ircmsg.Message) {
	if len(msg.Params) < 2 {
		return
	}
	o := origin.FromMessage(msg)
	_, rest := ircx.SplitStatusMsg(msg.Params[0], b.IRC.ISupport().StatusMsg(), b.IRC.ISupport().ChanTypes())
	if rest != "" {
		msg.Params[0] = rest
	}
	u, o := b.resolve(o)
	target := msg.Params[0]
	text := msg.Params[1]
	if irccase.Equal(o.Nick, b.IRC.Nick()) && b.channelSetting(target, "honorackmsg") {
		return
	}
	if cmd, arg, ok := origin.IsCTCP(text); ok {
		if cmd == "ACTION" {
			b.onAction(o, u, target, arg)
			return
		}
		b.onCTCP(o, u, target, cmd, arg)
		return
	}
	if origin.IsChannel(target) {
		if sensitive.Public(b.IRC.Nick(), text) {
			b.Protect.OnPrivmsg(target, o)
			b.IRC.PrivmsgHelp(target, o.Nick+": send password commands in a private message")
			return
		}
		b.PL.Consolef('m', "[%s] <%s> %s", target, o.Nick, text)
		if c := b.Chans.Get(target); c != nil {
			if c.Chanset.Has("seen") {
				b.recordSeen(o.Nick, o.Handle, o.Account, o.NUH(), target, "saying", text)
			}
			if c.Chanset.Has("ai") {
				b.LLM.Remember(target, o.Nick, text)
			}
		}
		b.Protect.OnPrivmsg(target, o)
		b.Scripts.Dispatch("pub", script.Event{
			Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Account: o.Account,
			Channel: target, Text: text,
		})
		b.Scripts.Dispatch("pubm", script.Event{
			Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Channel: target, Text: text,
		})
		b.handlePublic(o, u, target, text)
		return
	}
	b.PL.Consolef('m', "[msg] <%s> %s", o.Nick, text)
	b.Scripts.Dispatch("msg", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Text: text,
	})
	b.handleMsg(o, u, text)
}

func (b *Bot) onAction(o origin.Origin, u *userfile.User, target, text string) {
	if !origin.IsChannel(target) {
		return
	}
	b.PL.Consolef('m', "[%s] * %s %s", target, o.Nick, text)
	if c := b.Chans.Get(target); c != nil && c.Chanset.Has("ai") {
		b.LLM.Remember(target, o.Nick, "/me "+text)
	}
	b.Protect.OnPrivmsg(target, o)
	b.Scripts.Dispatch("pubm", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Channel: target, Text: "\x01ACTION " + text + "\x01",
	})
	_ = u
}

func publicCredentialCommand(botNick, text string) bool {
	return sensitive.Public(botNick, text)
}

func redactQuery(text string) string {
	return sensitive.Redact(text)
}

func (b *Bot) onCTCP(o origin.Origin, u *userfile.User, target, cmd, arg string) {
	if irccase.Equal(o.Nick, b.IRC.Nick()) {
		return
	}
	switch cmd {
	case "VERSION":
		b.IRC.CTCPReply(o.Nick, "VERSION", Version)
	case "PING":
		b.IRC.CTCPReply(o.Nick, "PING", arg)
	case "FINGER":
		b.IRC.CTCPReply(o.Nick, "FINGER", "an egg with a brain")
	case "DCC":
		if !b.Cfg.Partyline.DCC {
			return
		}
		if u == nil || !(u.Global.Has(flags.Party) || u.Global.Has(flags.Owner)) {
			return
		}
		ip, port, err := dcc.ParseCHAT(arg)
		if err != nil {
			return
		}
		select {
		case b.dccSlots <- struct{}{}:
		default:
			return
		}
		go func() {
			defer func() { <-b.dccSlots }()
			conn, err := dcc.DialCHAT(ip, port)
			if err != nil {
				b.Log.Info("dcc chat failed", "err", err)
				return
			}
			b.PL.ServeConn(conn, "dcc")
		}()
	}
	_ = target
}

func (b *Bot) channelSetting(channel, setting string) bool {
	c := b.Chans.Get(channel)
	return c != nil && c.Chanset.Has(setting)
}

func (b *Bot) seenEnabledForNick(nick string) bool {
	for _, c := range b.Chans.All() {
		if c.Chanset.Has("seen") && c.Member(nick) != nil {
			return true
		}
	}
	return false
}

func (b *Bot) recordSeen(nick, handle, account, host, channel, event, text string) {
	if b.Seen == nil {
		return
	}
	if err := b.Seen.Record(nick, handle, account, host, channel, event, text); err != nil && b.Log != nil {
		b.Log.Error("seen record", "nick", nick, "channel", channel, "event", event, "err", err)
	}
}

func (b *Bot) touchUser(handle string) {
	if b.Users == nil || handle == "" {
		return
	}
	if err := b.Users.Touch(handle); err != nil && b.Log != nil {
		b.Log.Error("last_seen", "handle", handle, "err", err)
	}
}

func (b *Bot) onJoin(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	u, o := b.resolve(o)
	ch := origin.Last(msg)
	if len(msg.Params) > 0 {
		ch = msg.Params[0]
	}
	if irccase.Equal(o.Nick, b.IRC.Nick()) {
		b.Chans.SetJoined(ch, true)
		b.Chans.ClearMembers(ch)
		if !b.channelSetting(ch, "nodesynch") {
			b.IRC.WHO(ch)
		}
		return
	}
	b.Chans.AddMember(ch, o.Nick, o.User, o.Host, o.Account, "")
	b.PL.Consolef('j', "[%s] join %s (%s)", ch, o.Nick, o.NUH())
	if b.channelSetting(ch, "seen") {
		b.recordSeen(o.Nick, o.Handle, o.Account, o.NUH(), ch, "joining", "")
	}
	b.Protect.OnJoin(ch, o)
	if c := b.Chans.Get(ch); c != nil && c.Chanset.Has("greet") && c.Greet != "" && u != nil {
		g := strings.ReplaceAll(c.Greet, "%n", o.Nick)
		g = strings.ReplaceAll(g, "%h", u.Handle)
		b.IRC.Privmsg(ch, g)
	}
	if u != nil {
		if pending := b.Notes.Pending(u.Handle); len(pending) > 0 {
			b.IRC.Notice(o.Nick, fmt.Sprintf("you have %d note(s). /msg %s notes", len(pending), b.IRC.Nick()))
		}
	}
	b.Scripts.Dispatch("join", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Channel: ch,
	})
}

func (b *Bot) onPart(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	_, o = b.resolve(o)
	ch := ""
	if len(msg.Params) > 0 {
		ch = msg.Params[0]
	}
	if irccase.Equal(o.Nick, b.IRC.Nick()) {
		b.Chans.SetJoined(ch, false)
	}
	recordSeen := b.channelSetting(ch, "seen")
	b.Chans.RemoveMember(ch, o.Nick)
	b.PL.Consolef('p', "[%s] part %s", ch, o.Nick)
	if recordSeen {
		b.recordSeen(o.Nick, o.Handle, o.Account, o.NUH(), ch, "parting", origin.Last(msg))
	}
	b.Scripts.Dispatch("part", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Channel: ch, Text: origin.Last(msg),
	})
}

func (b *Bot) onQuit(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	_, o = b.resolve(o)
	b.PL.Consolef('p', "quit %s (%s)", o.Nick, origin.Last(msg))
	if b.seenEnabledForNick(o.Nick) {
		b.recordSeen(o.Nick, o.Handle, o.Account, o.NUH(), "", "quitting", origin.Last(msg))
	}
	for _, c := range b.Chans.All() {
		b.Chans.RemoveMember(c.Name, o.Nick)
	}
	b.Scripts.Dispatch("sign", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Text: origin.Last(msg),
	})
}

func (b *Bot) onKick(msg ircmsg.Message) {
	if len(msg.Params) < 2 {
		return
	}
	o := origin.FromMessage(msg)
	ch, victim, reason := msg.Params[0], msg.Params[1], origin.Last(msg)
	b.Chans.RemoveMember(ch, victim)
	b.PL.Consolef('k', "[%s] %s kicked %s (%s)", ch, o.Nick, victim, reason)
	b.Protect.OnKick(ch, o.Nick, victim, reason)
	if b.channelSetting(ch, "seen") {
		b.recordSeen(victim, "", "", "", ch, "kicked", reason)
	}
	b.Scripts.Dispatch("kick", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Channel: ch, Text: victim + " " + reason,
	})
}

func (b *Bot) onMode(msg ircmsg.Message) {
	if len(msg.Params) < 2 {
		return
	}
	o := origin.FromMessage(msg)
	ch := msg.Params[0]
	modes := msg.Params[1]
	args := []string{}
	if len(msg.Params) > 2 {
		args = msg.Params[2:]
	}
	b.PL.Consolef('o', "[%s] mode %s %s %s", ch, o.Nick, modes, strings.Join(args, " "))
	b.applyModeCache(ch, modes, args)
	b.Protect.OnMode(ch, o.Nick, modes, args)
	b.Scripts.Dispatch("mode", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Channel: ch, Text: modes + " " + strings.Join(args, " "),
	})
}

func (b *Bot) applyModeCache(ch, modes string, args []string) {
	changes := ircx.ParserFromISupport(b.IRC.ISupport()).Parse(modes, args)
	for _, c := range changes {
		if c.Kind != ircx.ModePrefix || c.Arg == "" {
			continue
		}
		switch c.Mode {
		case 'o':
			b.Chans.SetOp(ch, c.Arg, c.Add)
		case 'v':
			b.Chans.SetVoice(ch, c.Arg, c.Add)
		}
	}
}

func (b *Bot) onNick(msg ircmsg.Message) {
	o := origin.FromMessage(msg)
	_, o = b.resolve(o)
	neu := origin.Last(msg)
	recordSeen := b.seenEnabledForNick(o.Nick)
	b.Chans.NickChange(o.Nick, neu)
	b.PL.Consolef('n', "nick %s -> %s", o.Nick, neu)
	if recordSeen {
		b.recordSeen(neu, o.Handle, o.Account, hostmask.Normalize(neu, o.User, o.Host), "", "nicking", o.Nick)
	}
	b.Protect.OnNick(o, neu)
	b.Scripts.Dispatch("nick", script.Event{
		Nick: o.Nick, Host: o.NUH(), Handle: o.Handle, Text: neu,
	})
}

func (b *Bot) onTopic(msg ircmsg.Message) {
	if len(msg.Params) < 1 {
		return
	}
	ch := msg.Params[0]
	topic := origin.Last(msg)
	if c := b.Chans.Get(ch); c != nil {
		c.Topic = topic
		if err := b.Chans.Save(c); err != nil {
			b.Log.Error("save topic", "channel", ch, "err", err)
		}
	}
	b.PL.Consolef('t', "[%s] topic: %s", ch, topic)
	b.Scripts.Dispatch("topc", script.Event{Channel: ch, Text: topic})
}

func (b *Bot) onNames(msg ircmsg.Message) {
	ch, names, ok := origin.Parse353(msg.Params)
	if !ok {
		return
	}
	if b.Chans.Get(ch) == nil {
		b.Chans.LoadOrCreate(ch, flags.ChanSet{}, "", "", "", "")
	}
	_, prefixChars := b.IRC.ISupport().Prefix()
	for _, tok := range strings.Fields(names) {
		pref, rest := ircx.StripPrefixes(tok, prefixChars)
		nick, user, host := ircx.ParseUserhostName(rest)
		if nick == "" {
			continue
		}
		b.Chans.AddMember(ch, nick, user, host, "", pref)
	}
}

func (b *Bot) maybeNeedOp(channel string) {
	c := b.Chans.Get(channel)
	if c == nil || strings.TrimSpace(c.NeedOp) == "" {
		return
	}
	if b.Botisop(channel) {
		return
	}
	b.PL.Consolef('o', "[%s] need-op: %s", channel, c.NeedOp)
	target := strings.Fields(c.NeedOp)[0]
	b.IRC.PrivmsgHelp(target, "need op on "+channel)
}

// --- protect.Host ---

func (b *Bot) UserOn(o origin.Origin) *userfile.User {
	if o.Handle != "" {
		u, err := b.Users.Get(o.Handle)
		if err == nil {
			return u
		}
	}
	if o.Nick != "" || o.Host != "" {
		if u := b.Users.FindByHost(o.NUH()); u != nil {
			return u
		}
	}
	if o.Nick != "" && o.User == "" && o.Host == "" {
		// look up member host from any channel
		for _, c := range b.Chans.All() {
			if m := c.Member(o.Nick); m != nil {
				return b.Users.FindByHost(hostmask.Normalize(m.Nick, m.User, m.Host))
			}
		}
	}
	return nil
}

func (b *Bot) ChanSet(channel string) flags.ChanSet {
	if c := b.Chans.Get(channel); c != nil {
		return c.Chanset
	}
	return flags.ChanSet{}
}

func (b *Bot) IsBanned(channel, nuh string) (bool, string) {
	for _, ban := range b.Chans.Bans(channel) {
		if hostmask.Match(ban.Mask, nuh) {
			reason := ban.Reason
			if reason == "" {
				reason = "banned"
			}
			return true, reason
		}
	}
	return false, ""
}

func (b *Bot) MemberOp(channel, nick string) bool {
	c := b.Chans.Get(channel)
	if c == nil {
		return false
	}
	m := c.Member(nick)
	return m != nil && m.Op
}

func (b *Bot) AutoJoin(channel string) bool {
	c := b.Chans.Get(channel)
	return c != nil && c.AutoJoin
}

func (b *Bot) ChannelsWithNick(nick string) []string {
	var out []string
	if b.Net != nil {
		for _, name := range []string{} {
			_ = name
		}
	}
	for _, c := range b.Chans.All() {
		if c.Member(nick) != nil || (b.Net != nil && b.Net.HasMember(c.Name, nick)) {
			out = append(out, c.Name)
		}
	}
	return out
}

// --- script.API ---

func (b *Bot) Say(target, text string)    { b.IRC.Privmsg(target, text) }
func (b *Bot) Act(target, text string)    { b.IRC.Action(target, text) }
func (b *Bot) Notice(target, text string) { b.IRC.Notice(target, text) }
func (b *Bot) Kick(channel, nick, reason string) {
	b.IRC.Kick(channel, nick, reason)
}
func (b *Bot) Putserv(line string)  { b.IRC.Send(queue.Serv, line) }
func (b *Bot) Puthelp(line string)  { b.IRC.Send(queue.Help, line) }
func (b *Bot) Putquick(line string) { b.IRC.Send(queue.Quick, line) }
func (b *Bot) Putlog(line string)   { b.Log.Info(line) }

func (b *Bot) MatchAttr(handle, fl, channel string) bool {
	u, err := b.Users.Get(handle)
	if err != nil {
		return flags.MatchAttr(nil, nil, fl)
	}
	return b.Users.MatchAttr(u, fl, channel)
}

func (b *Bot) Chattr(handle, spec, channel string) error {
	_, err := b.Users.Chattr(handle, spec, channel)
	return err
}

func (b *Bot) Finduser(nuh string) string {
	if u := b.Users.FindByHost(nuh); u != nil {
		return u.Handle
	}
	return ""
}

func (b *Bot) Validuser(handle string) bool {
	_, err := b.Users.Get(handle)
	return err == nil
}

func (b *Bot) Onchan(nick, channel string) bool {
	if b.Net != nil && b.Net.HasMember(channel, nick) {
		return true
	}
	c := b.Chans.Get(channel)
	return c != nil && c.Member(nick) != nil
}

func (b *Bot) Isop(nick, channel string) bool {
	if b.Net != nil && b.Net.IsOp(channel, nick) {
		return true
	}
	c := b.Chans.Get(channel)
	if c == nil {
		return false
	}
	m := c.Member(nick)
	return m != nil && m.Op
}

func (b *Bot) Isvoice(nick, channel string) bool {
	if b.Net != nil && b.Net.IsVoice(channel, nick) {
		return true
	}
	c := b.Chans.Get(channel)
	if c == nil {
		return false
	}
	m := c.Member(nick)
	return m != nil && m.Voice
}

func (b *Bot) Botonchan(channel string) bool {
	if b.Net != nil && b.Net.Joined(channel) {
		return true
	}
	if c := b.Chans.Get(channel); c != nil && c.Joined {
		return true
	}
	return b.Onchan(b.IRC.Nick(), channel)
}

func (b *Bot) Botisop(channel string) bool {
	return b.Isop(b.IRC.Nick(), channel)
}

func (b *Bot) Topic(channel string) string {
	return b.channelTopic(channel)
}

func (b *Bot) channelTopic(channel string) string {
	if channel == "" {
		return ""
	}
	if live := b.Net.Channel(channel); live != nil && strings.TrimSpace(live.Topic) != "" {
		return live.Topic
	}
	if c := b.Chans.Get(channel); c != nil {
		return c.Topic
	}
	return ""
}

func (b *Bot) Chanlist(channel string) []string {
	if c := b.Chans.Get(channel); c != nil {
		return c.Nicks()
	}
	return nil
}

func (b *Bot) BotNick() string { return b.IRC.Nick() }

func (b *Bot) Getuser(handle string) *userfile.User {
	u, err := b.Users.Get(handle)
	if err != nil {
		return nil
	}
	return u
}

func (b *Bot) need(u *userfile.User, expr, channel string) bool {
	if u == nil {
		return flags.MatchAttr(nil, nil, expr)
	}
	return b.Users.MatchAttr(u, expr, channel)
}
