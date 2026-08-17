package ircx

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/config"
	"eggbot/internal/queue"
)

type transcriptStep struct {
	expectPrefix string
	send         []string
}

func playTranscript(t *testing.T, steps []transcriptStep, ready func(*Client)) {
	t.Helper()
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
		i := 0
		for i < len(steps) {
			line, err := r.ReadString('\n')
			if err != nil {
				errc <- err
				return
			}
			line = strings.TrimSpace(line)
			step := steps[i]
			if step.expectPrefix != "" && !strings.HasPrefix(line, step.expectPrefix) {
				continue
			}
			for _, s := range step.send {
				if _, err := io.WriteString(c, s+"\r\n"); err != nil {
					errc <- err
					return
				}
			}
			i++
		}
		errc <- nil
	}()

	cfg := config.Defaults()
	cfg.Nick = "eggbot"
	cfg.AltNicks = []string{"eggbot_"}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port
	cfg.Server.TLS = false
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
		t.Fatal("transcript timeout")
	}
	if ready != nil {
		ready(c)
	}
}

func TestTranscriptLegacyNoCAP(t *testing.T) {
	playTranscript(t, []transcriptStep{
		{expectPrefix: "CAP LS", send: []string{":test. 421 eggbot CAP :Unknown command"}},
		{expectPrefix: "CAP END", send: []string{":test. 001 eggbot :welcome"}},
	}, func(c *Client) {
		if !c.Ready() {
			t.Fatal("ready")
		}
	})
}

func TestTranscriptSASLSuccess(t *testing.T) {
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
				_, _ = io.WriteString(c, ":test. CAP * LS :sasl=PLAIN account-tag\r\n")
			case strings.HasPrefix(line, "CAP REQ"):
				_, _ = io.WriteString(c, ":test. CAP * ACK :sasl account-tag\r\n")
			case line == "AUTHENTICATE PLAIN":
				_, _ = io.WriteString(c, ":test. AUTHENTICATE +\r\n")
			case strings.HasPrefix(line, "AUTHENTICATE ") && line != "AUTHENTICATE PLAIN":
				_, _ = io.WriteString(c, ":test. 903 eggbot :SASL authentication successful\r\n")
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
	cfg.Server.SASLAccount = "eggbot"
	cfg.Server.SASLPassword = "secret"
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
	if !c.Ready() || !c.Caps().Enabled("sasl") {
		t.Fatal("sasl not ready")
	}
}

func TestTranscriptNickCollision(t *testing.T) {
	playTranscript(t, []transcriptStep{
		{expectPrefix: "CAP LS", send: []string{":test. CAP * LS :"}},
		{expectPrefix: "CAP END", send: []string{":test. 433 * eggbot :in use"}},
		{expectPrefix: "NICK eggbot_", send: []string{":test. 001 eggbot_ :welcome"}},
	}, func(c *Client) {
		if c.Nick() != "eggbot_" {
			t.Fatalf("nick %s", c.Nick())
		}
	})
}

func TestSendMsgTyped(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Host = "irc.example.net"
	c := New(cfg, nil)
	c.SendMsg(queue.Help, ircmsg.MakeMessage(nil, "", "PRIVMSG", "#c", "hi"))
	if c.Queue().Len() == 0 {
		t.Fatal("typed send")
	}
}

func TestProductionProfile(t *testing.T) {
	p := ProductionProfile()
	if p.CAPVersion != 302 || p.EchoMessage {
		t.Fatalf("%+v", p)
	}
	if Classify("001").Disposition != Handled {
		t.Fatal("001")
	}
	seen := map[string]struct{}{}
	for _, c := range MustClassifyAll() {
		if c.Name == "" {
			t.Fatal("empty class")
		}
		seen[c.Name] = struct{}{}
	}
	for _, name := range []string{"PRIVMSG", "NOTICE", "001", "005", "433", "903"} {
		if _, ok := seen[name]; !ok {
			t.Fatal(name)
		}
	}
}

func FuzzEncodeLine(f *testing.F) {
	f.Add("PRIVMSG", "#c", "hello")
	f.Fuzz(func(t *testing.T, cmd, a, b string) {
		_, _ = EncodeLine(Make(cmd, a, b), 512)
		_, _ = ParseLine(":" + a + " " + cmd + " :" + b)
	})
}
