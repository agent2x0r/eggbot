package ircx

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"eggbot/internal/config"
)

func TestStateString(t *testing.T) {
	for _, s := range []State{StateDisconnected, StateDialing, StateNegotiating, StateAuthenticating, StateRegistering, StateReady, StateDraining} {
		if s.String() == "" {
			t.Fatal(s)
		}
	}
}

func TestCAPNAKThenRegister(t *testing.T) {
	playTranscript(t, []transcriptStep{
		{expectPrefix: "CAP LS", send: []string{":test. CAP * LS :account-tag"}},
		{expectPrefix: "CAP REQ", send: []string{":test. CAP * NAK :account-tag"}},
		{expectPrefix: "CAP END", send: []string{":test. 001 eggbot :welcome"}},
	}, nil)
}

func TestReadLoopAfterReady(t *testing.T) {
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
		_ = c.SetDeadline(time.Now().Add(15 * time.Second))
		r := bufio.NewReader(c)
		registered := false
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
				registered = true
				_, _ = io.WriteString(c, ":test. 005 eggbot NICKLEN=16 :are supported\r\n")
				_, _ = io.WriteString(c, "PING :keep\r\n")
				_, _ = io.WriteString(c, ":test. CAP * NEW :server-time\r\n")
				_, _ = io.WriteString(c, ":test. CAP * DEL :account-tag\r\n")
				_, _ = io.WriteString(c, ":test. CAP * ACK :server-time\r\n")
				_, _ = io.WriteString(c, ":test. CAP * NAK :echo-message\r\n")
				_, _ = io.WriteString(c, ":eggbot!u@h NICK eggbot2\r\n")
				_, _ = io.WriteString(c, "this is not irc\r\n")
				_, _ = io.WriteString(c, ":bob!b@h PRIVMSG #c :hi\r\n")
				time.Sleep(50 * time.Millisecond)
				_, _ = io.WriteString(c, "ERROR :closing\r\n")
			case strings.HasPrefix(line, "PING") && registered:
				_, _ = io.WriteString(c, ":test. PONG :x\r\n")
			}
		}
	}()
	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Server.TLS = false
	c := New(cfg, nil)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for !c.Ready() {
		if time.Now().After(deadline) {
			c.Quit()
			t.Fatal("not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	c.Quit()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Wait")
	}
	select {
	case <-errc:
	case <-time.After(time.Second):
	}
}
