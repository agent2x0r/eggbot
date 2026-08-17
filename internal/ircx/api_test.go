package ircx

import (
	"bufio"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/config"
	"eggbot/internal/queue"
)

func TestClientAPIQueues(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Host = "irc.example.net"
	c := New(cfg, nil)
	c.On(func(ircmsg.Message) {})
	c.OnReady(func() {})
	c.OnDisconnect(func() {})
	c.SetNick("eggbot2")
	c.Send(queue.Help, "PRIVMSG #c :x")
	c.Privmsg("#c", "hi")
	c.PrivmsgHelp("#c", "help")
	c.Notice("bob", "n")
	c.NoticeRaw("bob", "n")
	c.Action("#c", "dances")
	c.CTCPReply("bob", "VERSION", "eggbot")
	c.Mode("#c", "+o", "bob")
	c.Kick("#c", "eve", "bye")
	c.Topic("#c", "t")
	c.Join("#c")
	c.JoinKey("#c", "secret")
	c.Part("#c")
	c.Invite("bob", "#c")
	c.Raw("VERSION")
	c.RawQuick("TIME")
	c.WHO("#c")
	if c.Queue().Len() == 0 {
		t.Fatal("expected queued commands")
	}
	if c.Connected() {
		t.Fatal("not connected")
	}
	_ = c.ISupport()
	_ = c.State()
	_ = c.Caps()
	if !irccaseEqual("A", "a") {
		t.Fatal("irccase")
	}
	c.Close()
}

func TestCapsLifecycle(t *testing.T) {
	c := NewCaps()
	c.AddLS("sasl=PLAIN account-tag", false)
	if !c.LSDone() {
		t.Fatal("ls")
	}
	c.ACK("sasl account-tag")
	c.NAK("echo-message")
	c.NEW("chghost")
	lost := c.DEL("sasl")
	if len(lost) != 1 {
		t.Fatal(lost)
	}
	if len(c.OfferedNames()) == 0 || len(c.EnabledNames()) == 0 {
		t.Fatal("names")
	}
}

func TestISupportHelpers(t *testing.T) {
	is := NewISupport()
	is.ApplyTokens([]string{"NICKLEN=20", "CHANTYPES=#", "STATUSMSG=@"})
	if !is.Has("NICKLEN") || is.NickLen() != 20 {
		t.Fatal("nicklen")
	}
	if is.ChanTypes() != "#" || is.StatusMsg() != "@" {
		t.Fatal("chantypes")
	}
}

func TestModesHelpers(t *testing.T) {
	is := NewISupport()
	p := ParserFromISupport(is)
	if p.ModeForPrefix('@') != 'o' {
		t.Fatal("prefix")
	}
	if !IsChannelOrStatus("@#c", "#", "@") {
		t.Fatal("status")
	}
	if !ValidNick("eggbot", 16) || ValidNick("bad nick", 16) {
		t.Fatal("nick")
	}
	if ValidUTF8("hi") != "hi" {
		t.Fatal("utf8")
	}
	_ = saslEXTERNAL()
}

func TestTLSConfigFiles(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Host = "irc.example.net"
	cfg.Server.TLS = true
	tc, err := tlsConfig(cfg)
	if err != nil || tc.ServerName != "irc.example.net" {
		t.Fatalf("%v %+v", err, tc)
	}
	dir := t.TempDir()
	cfg.Server.CAFile = filepath.Join(dir, "missing.pem")
	if _, err := tlsConfig(cfg); err == nil {
		t.Fatal("missing ca")
	}
	_ = os.WriteFile(cfg.Server.CAFile, []byte("not a cert"), 0o600)
	if _, err := tlsConfig(cfg); err == nil {
		t.Fatal("bad ca")
	}
}

func TestSASLFailureIsFatal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	errc := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errc <- err
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(8 * time.Second))
		r := bufio.NewReader(c)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				errc <- err
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "CAP LS"):
				_, _ = io.WriteString(c, ":test. CAP * LS :sasl=PLAIN\r\n")
			case strings.HasPrefix(line, "CAP REQ"):
				_, _ = io.WriteString(c, ":test. CAP * ACK :sasl\r\n")
			case line == "AUTHENTICATE PLAIN":
				_, _ = io.WriteString(c, ":test. AUTHENTICATE +\r\n")
			case strings.HasPrefix(line, "AUTHENTICATE ") && line != "AUTHENTICATE PLAIN":
				_, _ = io.WriteString(c, ":test. 904 eggbot :SASL fail\r\n")
				errc <- nil
				return
			}
		}
	}()
	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Server.TLS = false
	cfg.Server.SASLAccount = "eggbot"
	cfg.Server.SASLPassword = "nope"
	c := New(cfg, nil)
	err = c.Connect()
	if err == nil {
		c.Quit()
		t.Fatal("expected SASL failure")
	}
}

func TestMultilineCAPLS(t *testing.T) {
	playTranscript(t, []transcriptStep{
		{expectPrefix: "CAP LS", send: []string{":test. CAP * LS * :account-tag", ":test. CAP * LS :server-time"}},
		{expectPrefix: "CAP REQ", send: []string{":test. CAP * ACK :account-tag server-time"}},
		{expectPrefix: "CAP END", send: []string{":test. 001 eggbot :welcome"}},
	}, func(c *Client) {
		if !c.Ready() || !c.Caps().Enabled("account-tag") {
			t.Fatal("caps")
		}
		c.Reconnect()
		time.Sleep(20 * time.Millisecond)
	})
}

func TestClassifyAndProfile(t *testing.T) {
	if Classify("PRIVMSG").Disposition != Handled {
		t.Fatal("privmsg")
	}
	if Classify("CAP").Disposition != Handled {
		t.Fatal("cap")
	}
	if Classify("ZYX9").Disposition != Surfaced {
		t.Fatal("unknown")
	}
	if n := len(MustClassifyAll()); n < 50 {
		t.Fatalf("inventory %d", n)
	}
	p := ProductionProfile()
	if p.CAPVersion != 302 || len(p.Preferred) == 0 {
		t.Fatalf("%+v", p)
	}
	offered := map[string]string{"sasl": "PLAIN", "account-tag": "", "echo-message": ""}
	reqs := SelectRequests(offered, nil, true)
	if len(reqs) == 0 {
		t.Fatal("select")
	}
}

func TestISupportResetAndSnapshot(t *testing.T) {
	is := NewISupport()
	is.ApplyTokens([]string{"FOO=bar", "-NICKLEN", "bad token with space", "LINELEN=32", "MODES=nope"})
	if is.Has("NICKLEN") {
		t.Fatal("removed")
	}
	if is.Int("MODES", 4) != 4 {
		t.Fatal("bad int")
	}
	if is.LineLen() != 64 {
		t.Fatal("min linelen")
	}
	snap := is.Snapshot()
	if snap["FOO"] != "bar" {
		t.Fatal(snap)
	}
	is.Reset()
	if !is.Has("NICKLEN") {
		t.Fatal("defaults")
	}
	_ = saslPLAIN("a", "b")
	if pickSASLMech([]string{"PLAIN"}, false, true) != "PLAIN" {
		t.Fatal("plain")
	}
	if pickSASLMech([]string{"EXTERNAL"}, true, false) != "EXTERNAL" {
		t.Fatal("external")
	}
	if pickSASLMech(nil, false, true) != "PLAIN" {
		t.Fatal("empty offered")
	}
}

func TestModeParseAndHijack(t *testing.T) {
	p := ParserFromISupport(NewISupport())
	chgs := p.Parse("+ov-b", []string{"a", "b", "*!*@x"})
	if len(chgs) != 3 {
		t.Fatal(chgs)
	}
	st, rest := SplitStatusMsg("@#chan", "@", "#")
	if st != "@" || rest != "#chan" {
		t.Fatal(st, rest)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := New(config.Defaults(), nil)
	c.HijackConn(a)
	if !c.Connected() {
		t.Fatal("hijack")
	}
}

func TestCapsSASLMechs(t *testing.T) {
	c := NewCaps()
	c.AddLS("sasl", false)
	if len(c.SASLMechs()) != 2 {
		t.Fatal(c.SASLMechs())
	}
	c.Reset()
	c.AddLS("sasl=EXTERNAL,PLAIN", true)
	if c.LSDone() {
		t.Fatal("more")
	}
	c.MarkLSDone()
	if !c.LSDone() {
		t.Fatal("marked")
	}
}
