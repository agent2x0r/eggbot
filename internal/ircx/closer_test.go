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

func TestISupportOdditiesAndTextLimit(t *testing.T) {
	is := NewISupport()
	is.ApplyTokens([]string{"PREFIX=(ov)@", "CHANMODES=b,k", "LINELEN=50", "NICKLEN="})
	_, _ = is.Prefix()
	_, _, _, _ = is.ChanModes()
	_ = is.Int("NICKLEN", 16)
	is.ApplyTokens([]string{"PREFIX=()", "LINELEN=600"})
	_, _ = is.Prefix()
	c := New(config.Defaults(), nil)
	c.isupport = is
	_ = c.textLimit()
	is.ApplyTokens([]string{"LINELEN=50"})
	c.isupport = is
	_ = c.textLimit()
	_ = SplitCTCP("ACTION", strings.Repeat("x", 80), 20)
	_ = SplitCTCP("ACTION", "hi", 0)
	_ = SplitCTCP("ACTION", strings.Repeat("y", 40), 10)
	_, _ = capParams([]string{"foo", "bar"})
	_ = ValidNick("\x01egg", 16)
	_ = ValidNick(string(rune(0x2028)), 16)
	_, _, _ = ParseUserhostName("nick!user")
	_ = TextBudget("PRIVMSG", strings.Repeat("t", 40), 20)
	p := ParserFromISupport(NewISupport())
	_ = p.Kind('z')
	_ = p.takesArg(ModeUnknown, true)
	_, _ = SplitStatusMsg(string([]byte{0xff})+"#c", "@", "#")
	_ = parseCapList("= =foo")
	_ = pickSASLMech(nil, false, false)
	_ = pickSASLMech([]string{"PLAIN"}, true, false)
	caps := NewCaps()
	_ = caps.SASLMechs()
	_ = SelectRequests(map[string]string{"echo-message": "", "account-tag": ""}, []string{"echo-message", "account-tag", "account-tag"}, false)
}

func TestNextNickTruncationAndNilDialer(t *testing.T) {
	cfg := config.Defaults()
	cfg.Nick = "abcdefghijklmnop"
	cfg.AltNicks = []string{"a", "b"}
	c := NewWithDialer(cfg, nil, nil)
	_ = c.nextNick()
	_ = c.nextNick()
	got := c.nextNick()
	if !strings.Contains(got, "_") {
		t.Fatalf("synth %q", got)
	}
	c.SendMsg(queue.Help, ircmsg.MakeMessage(nil, "", "PRIVMSG", "#c", "hi"))
}

func TestTLSConfigClientCert(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Host = "irc.example.net"
	dir := t.TempDir()
	cfg.Server.ClientCert = filepath.Join(dir, "c.pem")
	cfg.Server.ClientKey = filepath.Join(dir, "k.pem")
	if err := os.WriteFile(cfg.Server.ClientCert, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Server.ClientKey, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsConfig(cfg); err == nil {
		t.Fatal("bad client cert")
	}
}

func TestHandshakeSASLOptionalEndCap(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "c.pem")
	key := filepath.Join(dir, "k.pem")
	if err := os.WriteFile(cert, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
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
				errc <- nil
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "CAP LS"):
				_, _ = io.WriteString(c, ":test. CAP * LS :account-tag\r\n")
			case strings.HasPrefix(line, "CAP REQ"):
				_, _ = io.WriteString(c, ":test. CAP * ACK :account-tag\r\n")
			case strings.HasPrefix(line, "CAP END"):
				_, _ = io.WriteString(c, ":test. 001 eggbot :welcome\r\n")
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
	cfg.Server.ClientCert = cert
	cfg.Server.ClientKey = key
	c := New(cfg, nil)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	defer c.Quit()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timeout")
	}
}
