// Package queue implements eggdrop-style putserv / puthelp / putquick send queues.
package queue

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Pri int

const (
	Protocol Pri = iota - 1
	Quick
	Serv
	Help
)

type Item struct {
	Line string
	Pri  Pri
}

type Config struct {
	QuickBurst int
	QuickEvery time.Duration
	ServEvery  time.Duration
	HelpEvery  time.Duration
	MaxLine    int
	MaxItems   int
}

func DefaultConfig() Config {
	return Config{
		QuickBurst: 4,
		QuickEvery: 500 * time.Millisecond,
		ServEvery:  time.Second,
		HelpEvery:  1500 * time.Millisecond,
		MaxLine:    400,
		MaxItems:   2048,
	}
}

// Queue is a three-lane outgoing IRC line queue. Quick never starves behind help.
type Queue struct {
	mu      sync.Mutex
	cfg     Config
	proto   []string
	quick   []string
	serv    []string
	help    []string
	send    func(string)
	stop    chan struct{}
	stopped bool
	gen     uint64
	burst   int
	lastQ   time.Time
	lastS   time.Time
	lastH   time.Time
	drops   int
}

func New(cfg Config, send func(string)) *Queue {
	def := DefaultConfig()
	if cfg.QuickBurst <= 0 {
		cfg.QuickBurst = def.QuickBurst
	}
	if cfg.QuickEvery <= 0 {
		cfg.QuickEvery = def.QuickEvery
	}
	if cfg.ServEvery <= 0 {
		cfg.ServEvery = def.ServEvery
	}
	if cfg.HelpEvery <= 0 {
		cfg.HelpEvery = def.HelpEvery
	}
	if cfg.MaxLine <= 0 {
		cfg.MaxLine = def.MaxLine
	}
	if cfg.MaxItems <= 0 {
		cfg.MaxItems = def.MaxItems
	}
	return &Queue{
		cfg:  cfg,
		send: send,
		stop: make(chan struct{}),
	}
}

func (q *Queue) SetSend(send func(string)) {
	q.mu.Lock()
	q.send = send
	q.mu.Unlock()
}

func (q *Queue) SetMaxLine(n int) {
	if n <= 0 {
		return
	}
	q.mu.Lock()
	q.cfg.MaxLine = n
	q.mu.Unlock()
}

func (q *Queue) SetGeneration(g uint64) {
	q.mu.Lock()
	q.gen = g
	if g > 0 {
		q.proto, q.quick, q.serv, q.help = nil, nil, nil, nil
	}
	q.mu.Unlock()
}

func (q *Queue) Generation() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.gen
}

func (q *Queue) Drops() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.drops
}

func (q *Queue) Push(pri Pri, line string) {
	line = sanitizeLine(line)
	if line == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		q.drops++
		return
	}
	if pri != Protocol {
		line = clipBytes(line, q.cfg.MaxLine)
	}
	if !q.makeRoom(pri) {
		q.drops++
		return
	}
	switch pri {
	case Protocol:
		q.proto = append(q.proto, line)
	case Quick:
		q.quick = append(q.quick, line)
	case Help:
		q.help = append(q.help, line)
	default:
		q.serv = append(q.serv, line)
	}
}

func (q *Queue) makeRoom(pri Pri) bool {
	if len(q.proto)+len(q.quick)+len(q.serv)+len(q.help) < q.cfg.MaxItems {
		return true
	}
	if pri == Protocol {
		switch {
		case len(q.help) > 0:
			q.help = q.help[1:]
		case len(q.serv) > 0:
			q.serv = q.serv[1:]
		case len(q.quick) > 0:
			q.quick = q.quick[1:]
		default:
			return false
		}
		return true
	}
	switch pri {
	case Quick:
		switch {
		case len(q.help) > 0:
			q.help = q.help[1:]
		case len(q.serv) > 0:
			q.serv = q.serv[1:]
		case len(q.quick) > 0:
			q.quick = q.quick[1:]
		default:
			return false
		}
		return true
	case Serv:
		if len(q.help) > 0 {
			q.help = q.help[1:]
			return true
		}
	}
	return false
}

func (q *Queue) PushText(pri Pri, prefix, text string) {
	prefix = sanitizeLine(prefix)
	if prefix == "" || len(prefix) >= q.cfg.MaxLine {
		return
	}
	for _, part := range Split(text, q.cfg.MaxLine-len(prefix)) {
		q.Push(pri, prefix+part)
	}
}

func (q *Queue) Pending() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.proto) + len(q.quick) + len(q.serv) + len(q.help)
	out := make([]string, 0, n)
	out = append(out, q.proto...)
	out = append(out, q.quick...)
	out = append(out, q.serv...)
	out = append(out, q.help...)
	return out
}

func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.proto) + len(q.quick) + len(q.serv) + len(q.help)
}

func (q *Queue) Size(pri Pri) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch pri {
	case Quick:
		return len(q.quick)
	case Help:
		return len(q.help)
	default:
		return len(q.serv)
	}
}

func (q *Queue) Start() {
	q.mu.Lock()
	stop := q.stop
	q.mu.Unlock()
	go q.loop(stop)
}

func (q *Queue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stopped = true
	select {
	case <-q.stop:
	default:
		if q.stop != nil {
			close(q.stop)
		}
	}
}

func (q *Queue) Restart() {
	q.mu.Lock()
	select {
	case <-q.stop:
		q.stop = make(chan struct{})
		q.stopped = false
		stop := q.stop
		q.mu.Unlock()
		go q.loop(stop)
	default:
		q.stopped = false
		q.mu.Unlock()
	}
}

func (q *Queue) loop(stop <-chan struct{}) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-tick.C:
			q.drain(now)
		}
	}
}

func (q *Queue) drain(now time.Time) {
	q.mu.Lock()
	var send []string
	for len(q.proto) > 0 {
		line := q.proto[0]
		q.proto = q.proto[1:]
		send = append(send, line)
	}
	// quick: burst then paced
	for len(q.quick) > 0 {
		if q.burst < q.cfg.QuickBurst || now.Sub(q.lastQ) >= q.cfg.QuickEvery {
			line := q.quick[0]
			q.quick = q.quick[1:]
			q.burst++
			q.lastQ = now
			send = append(send, line)
			if q.burst >= q.cfg.QuickBurst && now.Sub(q.lastQ) < q.cfg.QuickEvery {
				break
			}
			continue
		}
		break
	}
	if len(q.quick) == 0 {
		q.burst = 0
	}
	if len(q.serv) > 0 && now.Sub(q.lastS) >= q.cfg.ServEvery {
		line := q.serv[0]
		q.serv = q.serv[1:]
		q.lastS = now
		send = append(send, line)
	}
	if len(q.help) > 0 && now.Sub(q.lastH) >= q.cfg.HelpEvery {
		// never send help if quick is backed up
		if len(q.quick) == 0 {
			line := q.help[0]
			q.help = q.help[1:]
			q.lastH = now
			send = append(send, line)
		}
	}
	q.mu.Unlock()
	for _, line := range send {
		q.send(line)
	}
}

// Split wraps text on word boundaries into pieces of at most max bytes.
func Split(s string, max int) []string {
	s = sanitizeLine(s)
	if max <= 0 {
		max = 400
	}
	if s == "" {
		return nil
	}
	var out []string
	for len(s) > 0 {
		if len(s) <= max {
			out = append(out, s)
			break
		}
		cut := max
		// don't split a utf-8 rune
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = max
		}
		// prefer last space
		if i := strings.LastIndexByte(s[:cut], ' '); i > max/4 {
			cut = i
			out = append(out, s[:cut])
			s = strings.TrimLeft(s[cut+1:], " ")
			continue
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	return out
}

// Sanitize is the exported form of sanitizeLine for other packages.
func Sanitize(s string) string { return sanitizeLine(s) }

// sanitizeLine guarantees that one queue item is exactly one IRC protocol
// line. Text from models and plugins is untrusted and must never be able to
// introduce a second command through CR, LF, or NUL.
func sanitizeLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', 0:
			return ' '
		default:
			return r
		}
	}, s)
	return strings.TrimSpace(s)
}

func clipBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		return ""
	}
	return s[:cut]
}
