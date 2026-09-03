package bot

import (
	"strings"
	"sync"
	"time"

	"eggbot/internal/irccase"
	"eggbot/internal/llm"
	"eggbot/internal/origin"
	"eggbot/internal/userfile"
)

const (
	stickyIdle = 120 * time.Second
	maxSticky  = 4096
)

type stickyKind int

const (
	stickyNone stickyKind = iota
	stickyYes
	stickyMiss
	stickyClose
)

type stickySess struct {
	deadline time.Time
	held     string
	retryAt  time.Time
	warned   bool
	gen      uint64
	origin   origin.Origin
	handle   string
}

type stickyMap struct {
	mu   sync.Mutex
	sess map[string]*stickySess
}

func newSticky() *stickyMap {
	return &stickyMap{sess: map[string]*stickySess{}}
}

func stickyKey(channel, nick string) string {
	return irccase.Fold(channel) + "\x00" + irccase.Fold(nick)
}

func (m *stickyMap) open(channel, nick string, o origin.Origin, handle string, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropExpiredLocked(now)
	if len(m.sess) >= maxSticky {
		return
	}
	key := stickyKey(channel, nick)
	s := m.sess[key]
	if s == nil {
		s = &stickySess{}
		m.sess[key] = s
	}
	s.deadline = now.Add(stickyIdle)
	if s.held == "" {
		s.warned = false
	}
	s.gen++
	s.origin = o
	s.handle = handle
}

func (m *stickyMap) consider(channel, nick, text string, now time.Time) (string, stickyKind) {
	if m == nil {
		return "", stickyNone
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := stickyKey(channel, nick)
	s := m.sess[key]
	if s == nil {
		return "", stickyNone
	}
	if s.held == "" && now.After(s.deadline) {
		delete(m.sess, key)
		return "", stickyNone
	}
	if strings.TrimSpace(text) == "" {
		return "", stickyMiss
	}
	s.deadline = now.Add(stickyIdle)
	return text, stickyYes
}

func (m *stickyMap) pause(channel, nick, prompt string, o origin.Origin, handle string, retryAt, now time.Time) (warn bool) {
	if m == nil || strings.TrimSpace(prompt) == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := stickyKey(channel, nick)
	s := m.sess[key]
	if s == nil {
		if len(m.sess) >= maxSticky {
			return false
		}
		s = &stickySess{}
		m.sess[key] = s
	}
	already := s.held != ""
	s.held = prompt
	if !already || s.retryAt.IsZero() {
		s.retryAt = retryAt
	}
	s.origin = o
	if handle != "" {
		s.handle = handle
	}
	s.gen++
	if already {
		return false
	}
	if s.warned {
		return false
	}
	s.warned = true
	return true
}

func (m *stickyMap) takeHold(channel, nick string, gen uint64, now time.Time) (prompt string, o origin.Origin, handle string, ok bool) {
	if m == nil {
		return "", origin.Origin{}, "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := stickyKey(channel, nick)
	s := m.sess[key]
	if s == nil || s.held == "" {
		return "", origin.Origin{}, "", false
	}
	if gen != 0 && s.gen != gen {
		return "", origin.Origin{}, "", false
	}
	prompt = s.held
	o = s.origin
	handle = s.handle
	s.held = ""
	s.retryAt = time.Time{}
	s.warned = false
	s.deadline = now.Add(stickyIdle)
	return prompt, o, handle, true
}

func (m *stickyMap) dueHolds(now time.Time) []stickyResume {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []stickyResume
	for key, s := range m.sess {
		if s.held == "" || s.retryAt.After(now) {
			continue
		}
		channel, nick, _ := strings.Cut(key, "\x00")
		out = append(out, stickyResume{
			Channel: channel, Nick: nick, Prompt: s.held,
			Origin: s.origin, Handle: s.handle, Gen: s.gen,
		})
	}
	return out
}

type stickyResume struct {
	Channel string
	Nick    string
	Prompt  string
	Origin  origin.Origin
	Handle  string
	Gen     uint64
}

func (m *stickyMap) touchReply(channel, nick string, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess[stickyKey(channel, nick)]
	if s == nil {
		return
	}
	s.deadline = now.Add(stickyIdle)
}

func (m *stickyMap) onNick(old, neu string) {
	if m == nil || old == "" || neu == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	oldF, neuF := irccase.Fold(old), irccase.Fold(neu)
	if oldF == neuF {
		return
	}
	next := make(map[string]*stickySess, len(m.sess))
	for key, s := range m.sess {
		ch, nick, _ := strings.Cut(key, "\x00")
		if nick == oldF {
			s.origin.Nick = neu
			next[ch+"\x00"+neuF] = s
			continue
		}
		next[key] = s
	}
	m.sess = next
}

func (m *stickyMap) drop(channel, nick string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.sess, stickyKey(channel, nick))
	m.mu.Unlock()
}

func (m *stickyMap) dropNick(nick string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	nf := irccase.Fold(nick)
	for key := range m.sess {
		_, n, _ := strings.Cut(key, "\x00")
		if n == nf {
			delete(m.sess, key)
		}
	}
}

func (m *stickyMap) dropExpiredLocked(now time.Time) {
	for key, s := range m.sess {
		if s.held == "" && now.After(s.deadline) {
			delete(m.sess, key)
		}
	}
}

func (m *stickyMap) holding(channel, nick string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sess[stickyKey(channel, nick)]
	return s != nil && s.held != ""
}

func (b *Bot) now() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

func (b *Bot) openSticky(channel string, o origin.Origin, u *userfile.User) {
	if b.sticky == nil || b.Cfg == nil || !b.Cfg.LLM.Sticky || !b.Cfg.LLM.Enabled {
		return
	}
	handle := ""
	if u != nil {
		handle = u.Handle
	}
	b.sticky.open(channel, o.Nick, o, handle, b.now())
}

func (b *Bot) resumeDueSticky(now time.Time) {
	if b.sticky == nil || b.LLM == nil || b.Cfg == nil || !b.Cfg.LLM.Sticky {
		return
	}
	for _, r := range b.sticky.dueHolds(now) {
		prompt, o, handle, ok := b.sticky.takeHold(r.Channel, r.Nick, r.Gen, now)
		if !ok || strings.TrimSpace(prompt) == "" {
			continue
		}
		u := b.Getuser(handle)
		if u == nil {
			u, o = b.resolve(o)
		}
		if o.Nick == "" {
			o.Nick = r.Nick
		}
		ch, orig, user, p := r.Channel, o, u, prompt
		b.goWork(func() { b.askLLM(ch, orig, user, p, llm.DestChannel) })
	}
}
