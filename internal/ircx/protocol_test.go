package ircx

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"eggbot/internal/config"
)

func TestISupportAndModes(t *testing.T) {
	is := NewISupport()
	is.ApplyTokens([]string{"CASEMAPPING=ascii", "PREFIX=(qaohv)~&@%+", "CHANMODES=beI,k,l,imnt", "CHANTYPES=#", "LINELEN=4096"})
	if is.Mapper().Name() != "ascii" {
		t.Fatal(is.Mapper().Name())
	}
	p := ParserFromISupport(is)
	ch := p.Parse("+o-l+k", []string{"bob", "secret"})
	if len(ch) != 3 {
		t.Fatalf("%+v", ch)
	}
	if !ch[0].Add || ch[0].Arg != "bob" || ch[0].Kind != ModePrefix {
		t.Fatalf("%+v", ch[0])
	}
	if ch[1].Add || ch[1].Arg != "" || ch[1].Kind != ModeSetArg {
		t.Fatalf("-l must not consume an argument: %+v", ch[1])
	}
	if !ch[2].Add || ch[2].Arg != "secret" {
		t.Fatalf("%+v", ch[2])
	}
}

func TestSelectRequests(t *testing.T) {
	offered := map[string]string{"sasl": "PLAIN", "account-tag": "", "echo-message": ""}
	got := SelectRequests(offered, nil, true)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "sasl") || !strings.Contains(joined, "account-tag") {
		t.Fatal(got)
	}
	if strings.Contains(joined, "echo-message") {
		t.Fatal("echo-message must stay disabled")
	}
	if len(SelectRequests(offered, nil, false)) != 1 {
		t.Fatal("sasl omitted when unconfigured")
	}
}

func TestClassify(t *testing.T) {
	if Classify("PRIVMSG").Disposition != Handled {
		t.Fatal("privmsg")
	}
	if Classify("SQUIT").Disposition != NotApplicable {
		t.Fatal("squit")
	}
	if Classify("WALLOPS").Disposition != Surfaced {
		t.Fatal("wallops")
	}
	if Classify("ACCOUNT").Disposition != Handled {
		t.Fatal("account")
	}
}

func TestSplitCTCP(t *testing.T) {
	parts := SplitCTCP("ACTION", strings.Repeat("x", 80), 40)
	if len(parts) < 2 {
		t.Fatal(parts)
	}
	for _, p := range parts {
		if !strings.HasPrefix(p, "\x01ACTION ") || !strings.HasSuffix(p, "\x01") {
			t.Fatalf("bad fragment %q", p)
		}
	}
}

func TestRegistrationEmptyCAPLS(t *testing.T) {
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
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(c)
		sawREQ := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				errc <- err
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "CAP LS"):
				_, _ = io.WriteString(c, ":test. CAP * LS :\r\n")
			case strings.HasPrefix(line, "CAP REQ"):
				sawREQ = true
			case strings.HasPrefix(line, "CAP END"):
				if sawREQ {
					errc <- fmt.Errorf("CAP REQ on empty LS: %s", line)
					return
				}
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
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if !c.Ready() {
		t.Fatal("not ready")
	}
}

func TestNoticeIsNotice(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.Host = "irc.example.net"
	c := New(cfg, nil)
	c.Notice("bob", "hi")
	c.CTCPReply("bob", "VERSION", "eggbot-0.1.0")
	var sawNotice bool
	for _, line := range c.Queue().Pending() {
		if strings.Contains(line, "PRIVMSG bob") {
			t.Fatalf("NOTICE rewritten as PRIVMSG: %q", line)
		}
		if strings.Contains(line, "NOTICE bob") {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Fatalf("expected NOTICE, got %v", c.Queue().Pending())
	}
}

func TestEncodeLineRejectsInject(t *testing.T) {
	line, err := EncodeLine(Make("PRIVMSG", "#c", "hi\r\nQUIT"), 512)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(line, "\r\n") {
		t.Fatal(line)
	}
}

func TestCapParams(t *testing.T) {
	sub, rest := capParams([]string{"*", "LS", "sasl account-tag"})
	if !strings.EqualFold(sub, "LS") || rest != "sasl account-tag" {
		t.Fatal(sub, rest)
	}
	list, more := capLS([]string{"*", "LS", "*", "multi-prefix sasl"})
	if !more || list != "multi-prefix sasl" {
		t.Fatal(list, more)
	}
}

func TestUserhostParse(t *testing.T) {
	prefs, rest := StripPrefixes("@+bob!b@bh", "@%+")
	n, u, h := ParseUserhostName(rest)
	if prefs != "@+" || n != "bob" || u != "b" || h != "bh" {
		t.Fatal(prefs, n, u, h)
	}
	_ = ircmsg.Message{}
}

func FuzzISupport(f *testing.F) {
	f.Add("CASEMAPPING=rfc1459 PREFIX=(ov)@+ CHANMODES=b,k,l,imnt")
	f.Fuzz(func(t *testing.T, s string) {
		is := NewISupport()
		is.ApplyTokens(strings.Fields(s))
		_ = ParserFromISupport(is).Parse("+ovl-b", []string{"a", "b", "c"})
		_ = SelectRequests(is.Snapshot(), nil, true)
	})
}
