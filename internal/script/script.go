// Package script is the bind engine shared by Lua and Python frontends.
package script

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"eggbot/internal/hostmask"
	"eggbot/internal/irccase"
	"eggbot/internal/userfile"
)

type Event struct {
	Type    string
	Nick    string
	Host    string
	Handle  string
	Account string
	Channel string
	Text    string
	Raw     string
	Args    []string
}

type API interface {
	Say(target, text string)
	Act(target, text string)
	Notice(target, text string)
	Kick(channel, nick, reason string)
	Putserv(line string)
	Puthelp(line string)
	Putquick(line string)
	Putlog(line string)
	MatchAttr(handle, flags, channel string) bool
	Chattr(handle, spec, channel string) error
	Finduser(nuh string) string
	Validuser(handle string) bool
	Onchan(nick, channel string) bool
	Isop(nick, channel string) bool
	Isvoice(nick, channel string) bool
	Botonchan(channel string) bool
	Botisop(channel string) bool
	Topic(channel string) string
	Chanlist(channel string) []string
	BotNick() string
	Getuser(handle string) *userfile.User
}

type Bind struct {
	Type    string
	Flags   string
	Mask    string
	Name    string
	Engine  string
	File    string
	Trusted bool
	Call    func(ev Event) (cont bool)
}

type Engine struct {
	API     API
	Log     *slog.Logger
	Trusted []string

	mu     sync.Mutex
	binds  []*Bind
	timers []*Timer
	seq    int
	stop   chan struct{}
	jobs   chan dispatchJob
	async  bool
}

type dispatchJob struct {
	typ string
	ev  Event
}

type Timer struct {
	ID     int
	At     time.Time
	Every  time.Duration
	Repeat bool
	Name   string
	File   string
	Call   func()
}

func New(api API, log *slog.Logger, trusted []string) *Engine {
	return &Engine{API: api, Log: log, Trusted: trusted, stop: make(chan struct{})}
}

func (e *Engine) IsTrusted(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	candidates := []string{path}
	for i, r := range path {
		if r == '/' && i+1 < len(path) {
			candidates = append(candidates, path[i+1:])
		}
	}
	for _, pattern := range e.Trusted {
		pattern = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(pattern)), "./")
		for _, candidate := range candidates {
			ok, err := filepath.Match(pattern, candidate)
			if err == nil && ok {
				return true
			}
		}
	}
	return false
}

func (e *Engine) AddBind(b *Bind) {
	e.mu.Lock()
	e.binds = append(e.binds, b)
	e.mu.Unlock()
}

func (e *Engine) ClearFile(path string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.binds[:0]
	for _, b := range e.binds {
		if b.File != path {
			out = append(out, b)
		}
	}
	e.binds = out
	timers := e.timers[:0]
	for _, timer := range e.timers {
		if timer.File != path {
			timers = append(timers, timer)
		}
	}
	e.timers = timers
}

// ReplaceFile atomically removes a file's old binds/timers and promotes a
// successfully staged replacement.
func (e *Engine) ReplaceFile(staged, path string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	binds := e.binds[:0]
	for _, bind := range e.binds {
		if bind.File == path {
			continue
		}
		if bind.File == staged {
			bind.File = path
		}
		binds = append(binds, bind)
	}
	e.binds = binds
	timers := e.timers[:0]
	for _, timer := range e.timers {
		if timer.File == path {
			continue
		}
		if timer.File == staged {
			timer.File = path
		}
		timers = append(timers, timer)
	}
	e.timers = timers
}

func (e *Engine) Binds() []*Bind {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*Bind, len(e.binds))
	copy(out, e.binds)
	return out
}

func (e *Engine) Dispatch(typ string, ev Event) {
	if e.async && e.jobs != nil {
		select {
		case e.jobs <- dispatchJob{typ: typ, ev: ev}:
		default:
			if e.Log != nil {
				e.Log.Warn("script queue full; dropping event", "type", typ)
			}
		}
		return
	}
	e.dispatchNow(typ, ev)
}

func (e *Engine) dispatchNow(typ string, ev Event) {
	ev.Type = typ
	deadline := time.Now().Add(8 * time.Second)
	for _, b := range e.Binds() {
		if time.Now().After(deadline) {
			if e.Log != nil {
				e.Log.Warn("script dispatch deadline", "type", typ)
			}
			return
		}
		if !strings.EqualFold(b.Type, typ) {
			continue
		}
		if !matchBind(b, ev) {
			continue
		}
		if b.Flags != "" && b.Flags != "-" && e.API != nil {
			if ev.Handle == "" || !e.API.MatchAttr(ev.Handle, b.Flags, ev.Channel) {
				continue
			}
		}
		cont := true
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.Log.Error("script bind panic", "bind", b.Name, "err", r)
				}
			}()
			if b.Call != nil {
				cont = b.Call(ev)
			}
		}()
		if !cont {
			return
		}
	}
}

func matchBind(b *Bind, ev Event) bool {
	mask := b.Mask
	if mask == "" || mask == "*" {
		return true
	}
	switch strings.ToLower(b.Type) {
	case "pub":
		cmd, _, _ := strings.Cut(strings.TrimSpace(ev.Text), " ")
		return irccase.Equal(cmd, mask) || strings.EqualFold(cmd, mask)
	case "dcc":
		return strings.EqualFold(mask, ev.Text) || irccase.Equal(mask, ev.Text)
	case "pubm", "msgm", "filt":
		return hostmask.Match(mask, ev.Text) || wildcardContains(mask, ev.Text)
	case "join", "part", "sign", "kick", "nick":
		return hostmask.Match(mask, ev.Host) || mask == "*"
	case "raw":
		return strings.EqualFold(mask, ev.Raw) || mask == "*"
	default:
		return hostmask.Match(mask, ev.Text) || strings.EqualFold(mask, ev.Text) || mask == "*"
	}
}

func wildcardContains(pat, s string) bool {
	if !strings.ContainsAny(pat, "*?") {
		return strings.Contains(strings.ToLower(s), strings.ToLower(pat))
	}
	return hostmask.Match(pat, s)
}

func (e *Engine) AddTimer(every time.Duration, repeat bool, name string, call func()) int {
	return e.AddTimerFor("", every, repeat, name, call)
}

func (e *Engine) AddTimerFor(file string, every time.Duration, repeat bool, name string, call func()) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if every < 100*time.Millisecond {
		every = 100 * time.Millisecond
	}
	if len(e.timers) >= 4096 {
		return 0
	}
	e.seq++
	t := &Timer{ID: e.seq, At: time.Now().Add(every), Every: every, Repeat: repeat, Name: name, File: file, Call: call}
	e.timers = append(e.timers, t)
	return t.ID
}

func (e *Engine) KillTimer(id int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, t := range e.timers {
		if t.ID == id {
			e.timers = append(e.timers[:i], e.timers[i+1:]...)
			return true
		}
	}
	return false
}

// RawAllowed protects credentials and connection lifecycle from scripts that
// were not explicitly trusted. The queue enforces single-line framing too.
func RawAllowed(trusted bool, line string) bool {
	if strings.ContainsAny(line, "\r\n\x00") {
		return false
	}
	if trusted {
		return true
	}
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(line)))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "OPER", "PASS", "AUTHENTICATE", "QUIT", "REHASH", "RESTART", "DIE":
		return false
	case "PRIVMSG", "NOTICE":
		if len(fields) >= 2 {
			target := strings.TrimPrefix(fields[1], ":")
			if target == "NICKSERV" || target == "NS" {
				return false
			}
		}
	}
	return true
}

func (e *Engine) Start() {
	e.mu.Lock()
	if e.jobs == nil {
		e.jobs = make(chan dispatchJob, 256)
	}
	e.async = true
	e.mu.Unlock()
	go e.dispatchLoop()
	go e.timerLoop()
}

func (e *Engine) dispatchLoop() {
	for {
		select {
		case <-e.stop:
			return
		case job, ok := <-e.jobs:
			if !ok {
				return
			}
			e.dispatchNow(job.typ, job.ev)
		}
	}
}

func (e *Engine) Stop() {
	select {
	case <-e.stop:
	default:
		close(e.stop)
	}
}

func (e *Engine) timerLoop() {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-e.stop:
			return
		case now := <-tick.C:
			var due []*Timer
			e.mu.Lock()
			remain := e.timers[:0]
			for _, t := range e.timers {
				if !now.Before(t.At) {
					due = append(due, t)
					if t.Repeat {
						t.At = now.Add(t.Every)
						remain = append(remain, t)
					}
				} else {
					remain = append(remain, t)
				}
			}
			e.timers = remain
			e.mu.Unlock()
			for _, t := range due {
				func() {
					defer func() { _ = recover() }()
					if t.Call != nil {
						t.Call()
					}
				}()
			}
		}
	}
}
