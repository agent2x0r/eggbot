package ircx

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"eggbot/internal/config"
	"eggbot/internal/queue"
)

func TestWHOXKickEmptyAndCaps(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Host = "irc.example.net"
	cfg.Server.RequestCaps = []string{"account-tag"}
	cfg.Username = ""
	cfg.Realname = ""
	c := New(cfg, nil)
	c.ISupport().ApplyTokens([]string{"WHOX", "LINELEN=200"})
	c.WHO("#c")
	c.Kick("#c", "eve", "")
	c.Kick("#c", "eve", strings.Repeat("x", 600))
	c.writeRaw("")
	c.sendText(queue.Help, "PRIVMSG", "#c", "hi")
	_ = c.preferredCaps()
	_ = c.textLimit()
	_ = c.nextNick()
	_ = c.nextNick()
	_ = c.nextNick()
	if c.Queue().Len() == 0 {
		t.Fatal("queued")
	}
	_, _ = capParams(nil)
	_, _ = capLS(nil)
	_ = TextBudget("PRIVMSG", "#c", 0)
	_ = ValidUTF8("\xff")
	_, _ = EncodeLine(Make("PRIVMSG", "#c", "hi"), 0)
}

func TestTranscriptNickSynthesisAndPING(t *testing.T) {
	playTranscript(t, []transcriptStep{
		{expectPrefix: "CAP LS", send: []string{"PING :cap", ":test. CAP * LS :account-tag"}},
		{expectPrefix: "CAP REQ", send: []string{":test. CAP * ACK :account-tag"}},
		{expectPrefix: "CAP END", send: []string{":test. 433 * eggbot :in use"}},
		{expectPrefix: "NICK eggbot_", send: []string{":test. 433 * eggbot_ :in use"}},
		{expectPrefix: "NICK eggbot_1", send: []string{":test. 001 eggbot_1 :welcome"}},
	}, func(c *Client) {
		if c.Nick() != "eggbot_1" {
			t.Fatalf("nick %s", c.Nick())
		}
	})
}

func TestConnectFallbackEndpointsAndPassword(t *testing.T) {
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
			case strings.HasPrefix(line, "PASS "):
			case strings.HasPrefix(line, "CAP LS"):
				_, _ = io.WriteString(c, ":test. CAP * LS :\r\n")
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
	cfg.Server.Password = "s3cret"
	cfg.Server.Endpoints = []string{"127.0.0.1:1", cfg.ServerAddr()}
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

func TestDefaultDialerPlainAndTLSFail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		time.Sleep(50 * time.Millisecond)
	}()
	cfg := config.Defaults()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Server.TLS = false
	cfg.Server.DialTimeoutSec = 0
	d := defaultDialer(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := d(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	cfg.Server.TLS = true
	d = defaultDialer(cfg)
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	go func() {
		c, err := ln2.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		time.Sleep(20 * time.Millisecond)
	}()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if _, err := d(ctx2, "tcp", ln2.Addr().String()); err == nil {
		t.Fatal("expected tls handshake failure")
	}
}

func TestModesIsChannelAndValidNick(t *testing.T) {
	if IsChannelTarget("", "#") || !IsChannelTarget("#c", "") {
		t.Fatal("channel")
	}
	if ValidNick("", 16) || ValidNick("bad.nick", 16) || ValidNick("#egg", 16) {
		t.Fatal("nick")
	}
	p := ParserFromISupport(NewISupport())
	_ = p.Kind('b')
	_ = p.Kind('k')
	_ = p.Kind('l')
	_ = p.Kind('i')
	_ = p.Kind('o')
	_ = p.ModeForPrefix('x')
}
