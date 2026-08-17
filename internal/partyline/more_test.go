package partyline

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBroadcastToLoggedInSession(t *testing.T) {
	s := &Server{
		Listen: "127.0.0.1:0",
		Log:    slog.Default(),
		Authenticate: func(handle, password string) error {
			if handle == "nate" && password == "secret" {
				return nil
			}
			return errString("no")
		},
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	c, err := net.Dial("tcp", s.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	r := bufio.NewReader(c)
	readUntil(t, r, "handle:")
	_, _ = io.WriteString(c, "nate\n")
	readUntil(t, r, "password:")
	_, _ = io.WriteString(c, "secret\n")
	readUntil(t, r, "welcome")
	deadline := time.Now().Add(2 * time.Second)
	for len(s.Sessions()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("session")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Sessions()[0].ChatOn = false
	s.Broadcast("muted", false, nil)
	s.Sessions()[0].ChatOn = true
	s.Broadcast("hello", false, nil)
	s.Broadcast("owners", true, func(h string) bool { return h == "nate" })
	s.Consolef('j', "join %s", "x")
	sess := &Session{out: make(chan string)}
	sess.Write("drop")
}

func TestSkipIACSequencesAndBackspace(t *testing.T) {
	payload := []byte{
		iac, iac,
		iac, sb, 1, 2, iac, se,
		iac, will, 1,
		iac, do, 1,
		iac, 241,
		'a', 0x08, 'b', '\n',
	}
	r := bufio.NewReader(strings.NewReader(string(payload)))
	var buf strings.Builder
	line, err := readLine(r, &writeDiscard{&buf}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if line != "b" {
		t.Fatalf("got %q", line)
	}
}

func TestServeNilAuthAndBusy(t *testing.T) {
	s := &Server{Log: slog.Default(), slots: make(chan struct{}, 1)}
	s.slots <- struct{}{}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s.ServeConn(a, "dcc")
	s2 := &Server{Log: slog.Default()}
	c, d := net.Pipe()
	defer c.Close()
	defer d.Close()
	go s2.ServeConn(c, "dcc")
	rd := bufio.NewReader(d)
	_ = d.SetDeadline(time.Now().Add(2 * time.Second))
	readUntil(t, rd, "handle:")
}

type writeDiscard struct{ b *strings.Builder }

func (w *writeDiscard) Write(p []byte) (int, error) {
	w.b.Write(p)
	return len(p), nil
}
