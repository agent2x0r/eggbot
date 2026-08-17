package ircx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/ergochat/irc-go/ircreader"

	"eggbot/internal/config"
	"eggbot/internal/queue"
)

type State int

const (
	StateDisconnected State = iota
	StateDialing
	StateNegotiating
	StateAuthenticating
	StateRegistering
	StateReady
	StateDraining
)

func (s State) String() string {
	switch s {
	case StateDialing:
		return "dialing"
	case StateNegotiating:
		return "negotiating"
	case StateAuthenticating:
		return "authenticating"
	case StateRegistering:
		return "registering"
	case StateReady:
		return "ready"
	case StateDraining:
		return "draining"
	default:
		return "disconnected"
	}
}

var (
	ErrFatalAuth     = errors.New("irc authentication failed")
	ErrBanned        = errors.New("banned from server")
	ErrNotRegistered = errors.New("registration timed out")
)

const registrationTimeout = 45 * time.Second

// Client is the eggbot IRC session: CAP/SASL registration, reconnect, and
// a bounded outbound queue. Every inbound line is dispatched.
type Client struct {
	cfg  *config.Config
	log  *slog.Logger
	q    *queue.Queue
	dial DialFunc

	mu         sync.RWMutex
	nick       string
	wantedNick string
	altIndex   int
	handlers   []func(ircmsg.Message)
	onReady    []func()
	onDrop     []func()
	state      State
	gen        uint64
	caps       *Caps
	isupport   *ISupport
	conn       net.Conn
	reader     *ircreader.Reader
	ready      bool
	connected  bool
	stopOnce   sync.Once
	cancel     context.CancelFunc
	loopDone   chan struct{}
	writeMu    sync.Mutex
	metrics    Metrics
	supervised atomic.Bool
}

type Metrics struct {
	Reconnects atomic.Uint64
	SendFails  atomic.Uint64
	Drops      atomic.Uint64
}

func New(cfg *config.Config, log *slog.Logger) *Client {
	return NewWithDialer(cfg, log, nil)
}

func NewWithDialer(cfg *config.Config, log *slog.Logger, dial DialFunc) *Client {
	if log == nil {
		log = slog.Default()
	}
	c := &Client{
		cfg:        cfg,
		log:        log,
		nick:       cfg.Nick,
		wantedNick: cfg.Nick,
		caps:       NewCaps(),
		isupport:   NewISupport(),
		dial:       dial,
		loopDone:   make(chan struct{}),
	}
	if c.dial == nil {
		c.dial = defaultDialer(cfg)
	}
	c.q = queue.New(queue.DefaultConfig(), func(line string) {
		if err := c.writeRaw(line); err != nil {
			c.metrics.SendFails.Add(1)
			c.log.Debug("IRC send failed", "err", err)
		}
	})
	return c
}

func (c *Client) On(fn func(ircmsg.Message)) {
	c.mu.Lock()
	c.handlers = append(c.handlers, fn)
	c.mu.Unlock()
}

func (c *Client) OnReady(fn func()) {
	c.mu.Lock()
	c.onReady = append(c.onReady, fn)
	c.mu.Unlock()
}

func (c *Client) OnDisconnect(fn func()) {
	c.mu.Lock()
	c.onDrop = append(c.onDrop, fn)
	c.mu.Unlock()
}

func (c *Client) Queue() *queue.Queue { return c.q }

func (c *Client) ISupport() *ISupport { return c.isupport }

func (c *Client) Caps() *Caps { return c.caps }

func (c *Client) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Client) Generation() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gen
}

func (c *Client) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

func (c *Client) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *Client) Nick() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nick
}

func (c *Client) SetNick(n string) {
	n = strings.TrimSpace(n)
	if n == "" {
		return
	}
	c.mu.Lock()
	c.wantedNick = n
	c.mu.Unlock()
	c.SendProtocol("NICK " + n)
}

func (c *Client) Connect() error {
	c.q.Start()
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	if err := c.connectOnce(ctx); err != nil {
		cancel()
		return err
	}
	c.supervised.Store(true)
	go c.supervise(ctx)
	return nil
}

func (c *Client) Loop() {
	c.mu.RLock()
	done := c.loopDone
	c.mu.RUnlock()
	<-done
}

func (c *Client) Quit() {
	c.stopOnce.Do(func() {
		c.setState(StateDraining)
		c.SendProtocol("QUIT :gone fishing")
		c.q.Stop()
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.mu.Unlock()
		if !c.supervised.Load() {
			select {
			case <-c.loopDone:
			default:
				close(c.loopDone)
			}
		}
	})
}

func (c *Client) Reconnect() {
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.mu.Unlock()
}

func (c *Client) supervise(ctx context.Context) {
	defer close(c.loopDone)
	backoff := time.Second
	for {
		err := c.readLoop(ctx)
		if ctx.Err() != nil {
			c.teardown("shutdown")
			return
		}
		c.log.Warn("irc disconnected", "err", err)
		c.teardown("disconnected")
		c.metrics.Reconnects.Add(1)
		wait := backoff + time.Duration(c.metrics.Reconnects.Load()%3)*200*time.Millisecond
		if wait > 2*time.Minute {
			wait = 2 * time.Minute
		}
		c.log.Info("irc reconnect", "in", wait.String())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := c.connectOnce(ctx); err != nil {
			if errors.Is(err, ErrFatalAuth) || errors.Is(err, ErrBanned) || errors.Is(err, context.Canceled) {
				c.log.Error("irc session ended", "err", err)
				return
			}
			c.log.Warn("irc session", "err", err)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (c *Client) connectOnce(ctx context.Context) error {
	var last error
	for _, addr := range c.cfg.ServerEndpoints() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.dialAndRegister(ctx, addr); err != nil {
			last = err
			if errors.Is(err, ErrFatalAuth) || errors.Is(err, ErrBanned) {
				return err
			}
			c.log.Warn("endpoint failed", "addr", addr, "err", err)
			c.teardown("endpoint failed")
			continue
		}
		return nil
	}
	if last == nil {
		last = fmt.Errorf("no IRC endpoints")
	}
	return last
}

func (c *Client) dialAndRegister(ctx context.Context, addr string) error {
	c.setState(StateDialing)
	c.caps.Reset()
	c.isupport.Reset()
	c.mu.Lock()
	c.altIndex = 0
	c.nick = c.wantedNick
	c.ready = false
	c.mu.Unlock()

	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, err := c.dial(dialCtx, "tcp", addr)
	cancel()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.gen++
	gen := c.gen
	c.conn = conn
	c.connected = true
	c.mu.Unlock()
	c.q.SetGeneration(gen)
	c.q.SetSend(func(line string) {
		if err := c.writeRaw(line); err != nil {
			c.metrics.SendFails.Add(1)
			c.log.Debug("IRC send failed", "err", err)
		}
	})
	c.mu.Lock()
	c.reader = ircreader.NewIRCReader(conn)
	c.mu.Unlock()

	if pw := c.cfg.Server.Password; pw != "" {
		if err := c.writeRaw("PASS " + pw); err != nil {
			return err
		}
	}
	if err := c.writeRaw("CAP LS 302"); err != nil {
		return err
	}
	nick := c.Nick()
	user := c.cfg.Username
	if user == "" {
		user = nick
	}
	real := c.cfg.Realname
	if real == "" {
		real = nick
	}
	if err := c.writeRaw("NICK " + nick); err != nil {
		return err
	}
	if err := c.writeRaw(fmt.Sprintf("USER %s 0 * :%s", user, queue.Sanitize(real))); err != nil {
		return err
	}

	c.setState(StateNegotiating)
	regCtx, stop := context.WithTimeout(ctx, registrationTimeout)
	defer stop()
	return c.handshake(regCtx)
}

func (c *Client) handshake(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	reader := c.reader
	c.mu.RUnlock()
	if conn == nil || reader == nil {
		return io.EOF
	}
	wantSASL := c.cfg.Server.SASLAccount != "" || (c.cfg.Server.ClientCert != "" && c.cfg.Server.ClientKey != "")
	saslDone := !wantSASL
	capEnded := false
	authenticated := false

	endCap := func() error {
		if capEnded {
			return nil
		}
		capEnded = true
		c.setState(StateRegistering)
		return c.writeRaw("CAP END")
	}

	startSASL := func() error {
		c.setState(StateAuthenticating)
		mechs := c.caps.SASLMechs()
		haveCert := c.cfg.Server.ClientCert != "" && c.cfg.Server.ClientKey != ""
		havePlain := c.cfg.Server.SASLAccount != "" && c.cfg.Server.SASLPassword != ""
		mech := pickSASLMech(mechs, haveCert, havePlain)
		if mech == "" {
			if wantSASL && c.cfg.Server.SASLAccount != "" {
				return fmt.Errorf("%w: no mutually supported SASL mechanism", ErrFatalAuth)
			}
			saslDone = true
			return endCap()
		}
		return c.writeRaw("AUTHENTICATE " + mech)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrNotRegistered, err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		line, err := reader.ReadLine()
		if err != nil {
			return err
		}
		msg, err := ircmsg.ParseLine(string(line))
		if err != nil {
			continue
		}
		c.dispatch(msg)

		switch msg.Command {
		case "PING":
			pong := "PONG"
			if len(msg.Params) > 0 {
				pong += " :" + msg.Params[0]
			}
			if err := c.writeRaw(pong); err != nil {
				return err
			}
		case "CAP":
			sub, rest := capParams(msg.Params)
			switch strings.ToUpper(sub) {
			case "LS":
				list, more := capLS(msg.Params)
				c.caps.AddLS(list, more)
				if !more {
					reqs := SelectRequests(c.caps.OfferedMap(), c.preferredCaps(), wantSASL)
					if len(reqs) == 0 {
						if err := endCap(); err != nil {
							return err
						}
						break
					}
					if err := c.writeRaw("CAP REQ :" + strings.Join(reqs, " ")); err != nil {
						return err
					}
				}
			case "ACK":
				c.caps.ACK(rest)
				if wantSASL && c.caps.Enabled("sasl") && !authenticated {
					if err := startSASL(); err != nil {
						return err
					}
				} else if err := endCap(); err != nil {
					return err
				}
			case "NAK":
				c.caps.NAK(rest)
				if wantSASL && strings.Contains(strings.ToLower(rest), "sasl") {
					return fmt.Errorf("%w: SASL not acknowledged", ErrFatalAuth)
				}
				if err := endCap(); err != nil {
					return err
				}
			case "NEW":
				c.caps.NEW(rest)
			case "DEL":
				c.caps.DEL(rest)
			}
		case "AUTHENTICATE":
			token := ""
			if len(msg.Params) > 0 {
				token = msg.Params[0]
			}
			if token == "+" {
				var chunks []string
				if c.cfg.Server.ClientCert != "" && c.caps.Enabled("sasl") && pickSASLMech(c.caps.SASLMechs(), true, false) == "EXTERNAL" {
					chunks = saslEXTERNAL()
				} else {
					chunks = saslPLAIN(c.cfg.Server.SASLAccount, c.cfg.Server.SASLPassword)
				}
				for _, ch := range chunks {
					if err := c.writeRaw("AUTHENTICATE " + ch); err != nil {
						return err
					}
				}
			}
		case "900", "903":
			authenticated = true
			saslDone = true
			if err := endCap(); err != nil {
				return err
			}
		case "902", "904", "905", "906", "907":
			if wantSASL && c.cfg.Server.SASLAccount != "" {
				return fmt.Errorf("%w: SASL numeric %s", ErrFatalAuth, msg.Command)
			}
			saslDone = true
			if err := endCap(); err != nil {
				return err
			}
		case "421":
			if len(msg.Params) >= 2 && strings.EqualFold(msg.Params[1], "CAP") {
				c.caps.MarkLSDone()
				if err := endCap(); err != nil {
					return err
				}
			}
		case "433", "432", "436", "437":
			next := c.nextNick()
			if next == "" {
				return fmt.Errorf("no alternate nicknames remaining")
			}
			if err := c.writeRaw("NICK " + next); err != nil {
				return err
			}
		case "464":
			return fmt.Errorf("%w: server password rejected", ErrFatalAuth)
		case "465":
			return ErrBanned
		case "001":
			if len(msg.Params) > 0 {
				c.mu.Lock()
				c.nick = msg.Params[0]
				c.mu.Unlock()
			}
			if !capEnded {
				_ = endCap()
			}
			_ = saslDone
			c.markReady()
			return nil
		case "005":
			if len(msg.Params) >= 2 {
				c.isupport.ApplyTokens(msg.Params[1 : len(msg.Params)-1])
				c.q.SetMaxLine(c.textLimit())
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	reader := c.reader
	c.mu.RUnlock()
	if conn == nil || reader == nil {
		return io.EOF
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Minute))
		line, err := reader.ReadLine()
		if err != nil {
			return err
		}
		msg, err := ircmsg.ParseLine(string(line))
		if err != nil {
			continue
		}
		switch msg.Command {
		case "PING":
			pong := "PONG"
			if len(msg.Params) > 0 {
				pong += " :" + msg.Params[0]
			}
			_ = c.writeRaw(pong)
		case "005":
			if len(msg.Params) >= 2 {
				c.isupport.ApplyTokens(msg.Params[1 : len(msg.Params)-1])
				c.q.SetMaxLine(c.textLimit())
			}
		case "CAP":
			sub, rest := capParams(msg.Params)
			switch strings.ToUpper(sub) {
			case "NEW":
				c.caps.NEW(rest)
				wantSASL := c.cfg.Server.SASLAccount != ""
				reqs := SelectRequests(c.caps.OfferedMap(), c.preferredCaps(), wantSASL)
				if len(reqs) > 0 {
					_ = c.writeRaw("CAP REQ :" + strings.Join(reqs, " "))
				}
			case "DEL":
				c.caps.DEL(rest)
			case "ACK":
				c.caps.ACK(rest)
			case "NAK":
				c.caps.NAK(rest)
			}
		case "NICK":
			if irccaseEqual(msg.Nick(), c.Nick()) && len(msg.Params) > 0 {
				c.mu.Lock()
				c.nick = msg.Params[len(msg.Params)-1]
				c.mu.Unlock()
			}
		case "ERROR":
			c.dispatch(msg)
			return fmt.Errorf("server error: %s", strings.Join(msg.Params, " "))
		}
		c.dispatch(msg)
	}
}

func (c *Client) markReady() {
	c.mu.Lock()
	c.ready = true
	c.state = StateReady
	hooks := append([]func(){}, c.onReady...)
	c.mu.Unlock()
	c.log.Info("registered", "nick", c.Nick())
	for _, h := range hooks {
		h()
	}
}

func (c *Client) teardown(reason string) {
	c.mu.Lock()
	c.ready = false
	c.connected = false
	c.state = StateDisconnected
	c.reader = nil
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	hooks := append([]func(){}, c.onDrop...)
	c.mu.Unlock()
	c.q.SetGeneration(c.Generation() + 1)
	c.caps.Reset()
	c.isupport.Reset()
	for _, h := range hooks {
		h()
	}
	_ = reason
}

func (c *Client) dispatch(msg ircmsg.Message) {
	c.mu.RLock()
	hs := append([]func(ircmsg.Message){}, c.handlers...)
	c.mu.RUnlock()
	for _, h := range hs {
		h(msg)
	}
}

func (c *Client) writeRaw(line string) error {
	line = queue.Sanitize(line)
	if line == "" {
		return nil
	}
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		c.metrics.Drops.Add(1)
		return net.ErrClosed
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, err := io.WriteString(conn, line+"\r\n")
	return err
}

func (c *Client) SendProtocol(line string) { c.q.Push(queue.Protocol, line) }

func (c *Client) Send(pri queue.Pri, line string) { c.q.Push(pri, line) }

func (c *Client) sendText(pri queue.Pri, command, target, text string) {
	text = queue.Sanitize(text)
	budget := TextBudget(command, target, c.textLimit())
	for _, part := range queue.Split(text, budget) {
		line, err := CommandLine(command, target, part)
		if err != nil {
			c.q.Push(pri, command+" "+target+" :"+part)
			continue
		}
		c.q.Push(pri, line)
	}
}

func (c *Client) Privmsg(target, text string) {
	c.sendText(queue.Serv, "PRIVMSG", target, text)
}

func (c *Client) PrivmsgHelp(target, text string) {
	c.sendText(queue.Help, "PRIVMSG", target, text)
}

func (c *Client) Notice(target, text string) {
	c.sendText(queue.Help, "NOTICE", target, text)
}

func (c *Client) NoticeRaw(target, text string) {
	c.Notice(target, text)
}

func (c *Client) Action(target, text string) {
	budget := TextBudget("PRIVMSG", target, c.textLimit())
	for _, body := range SplitCTCP("ACTION", text, budget) {
		c.q.Push(queue.Serv, "PRIVMSG "+target+" :"+body)
	}
}

func (c *Client) CTCPReply(target, tag, arg string) {
	body := "\x01" + tag
	if arg != "" {
		body += " " + arg
	}
	body += "\x01"
	c.sendText(queue.Help, "NOTICE", target, body)
}

func (c *Client) Mode(channel, modes string, args ...string) {
	line := "MODE " + channel + " " + modes
	if len(args) > 0 {
		line += " " + strings.Join(args, " ")
	}
	c.q.Push(queue.Quick, line)
}

func (c *Client) Kick(channel, nick, reason string) {
	if reason == "" {
		reason = "bye"
	}
	line, err := CommandLine("KICK", channel, nick, reason)
	if err != nil {
		c.q.Push(queue.Quick, fmt.Sprintf("KICK %s %s :%s", channel, nick, queue.Sanitize(reason)))
		return
	}
	c.q.Push(queue.Quick, line)
}

func (c *Client) Topic(channel, topic string) {
	c.sendText(queue.Serv, "TOPIC", channel, topic)
}

func (c *Client) Join(channel string) {
	c.q.Push(queue.Quick, "JOIN "+channel)
}

func (c *Client) JoinKey(channel, key string) {
	if key == "" {
		c.Join(channel)
		return
	}
	c.q.Push(queue.Quick, "JOIN "+channel+" "+key)
}

func (c *Client) Part(channel string) {
	c.q.Push(queue.Serv, "PART "+channel)
}

func (c *Client) Invite(nick, channel string) {
	c.q.Push(queue.Serv, "INVITE "+nick+" "+channel)
}

func (c *Client) Raw(line string)      { c.q.Push(queue.Serv, line) }
func (c *Client) RawQuick(line string) { c.q.Push(queue.Quick, line) }

func (c *Client) WHO(channel string) {
	if _, ok := c.isupport.Get("WHOX"); ok {
		c.q.Push(queue.Quick, "WHO "+channel+" %cuhnfas")
		return
	}
	c.q.Push(queue.Quick, "WHO "+channel)
}

func (c *Client) textLimit() int {
	n := c.isupport.LineLen() - 2
	if n > 400 && n > 80 {
		// leave room for tags/prefix on inbound; outbound body is smaller
		if n > 512 {
			n = 400
		} else {
			n = n - 80
		}
	}
	if n < 80 {
		n = 80
	}
	return n
}

func (c *Client) preferredCaps() []string {
	if len(c.cfg.Server.RequestCaps) > 0 {
		return c.cfg.Server.RequestCaps
	}
	return DefaultPreferredCaps
}

func (c *Client) nextNick() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	alts := c.cfg.AltNicks
	if c.altIndex < len(alts) {
		n := alts[c.altIndex]
		c.altIndex++
		c.nick = n
		return n
	}
	// synthesize nick_N bounded by NICKLEN
	max := c.isupport.NickLen()
	base := c.wantedNick
	if len(base) >= max-2 {
		base = base[:max-2]
	}
	n := fmt.Sprintf("%s_%d", base, c.altIndex)
	c.altIndex++
	c.nick = n
	return n
}

func (c *Client) setState(st State) {
	c.mu.Lock()
	c.state = st
	c.mu.Unlock()
}

func capParams(params []string) (sub, rest string) {
	if len(params) == 0 {
		return "", ""
	}
	rest = params[len(params)-1]
	for i := 0; i < len(params)-1; i++ {
		switch strings.ToUpper(params[i]) {
		case "LS", "ACK", "NAK", "NEW", "DEL", "LIST", "REQ", "END":
			return params[i], rest
		}
	}
	return params[0], rest
}

func capLS(params []string) (list string, more bool) {
	if len(params) == 0 {
		return "", false
	}
	list = params[len(params)-1]
	if len(params) >= 3 && params[len(params)-2] == "*" {
		return list, true
	}
	return list, false
}

func irccaseEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}

func (c *Client) Wait() { c.Loop() }

func (c *Client) Close() { c.Quit() }

// ConnFile is used by tests that wrap an already-accepted connection.
func (c *Client) HijackConn(conn net.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.reader = ircreader.NewIRCReader(conn)
	c.mu.Unlock()
}
