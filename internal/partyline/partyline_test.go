package partyline

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestLoginAndCommand(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Log:   slog.Default(),
		fails: map[string]failRec{},
		stop:  make(chan struct{}),
		Authenticate: func(handle, password string) error {
			if handle == "nate" && password == "secret" {
				return nil
			}
			return errString("no")
		},
	}
	got := make(chan string, 4)
	s.OnLine = func(sess *Session, line string) {
		got <- sess.Handle + ":" + line
		sess.Write("ok")
	}
	go s.accept(ln, "telnet")
	defer ln.Close()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(c)
	readUntil(t, r, "handle:")
	if _, err := io.WriteString(c, "nate\n"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, r, "password:")
	if _, err := io.WriteString(c, "secret\n"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, r, "welcome")
	if _, err := io.WriteString(c, ".status\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != "nate:.status" {
			t.Fatalf("got %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for command")
	}
}

func TestLoginStripsTelnetIAC(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Log:   slog.Default(),
		fails: map[string]failRec{},
		stop:  make(chan struct{}),
		Authenticate: func(handle, password string) error {
			if handle == "nate" && password == "secret" {
				return nil
			}
			return errString(handle + "/" + password)
		},
	}
	got := make(chan string, 1)
	s.OnLine = func(sess *Session, line string) { got <- line }
	go s.accept(ln, "telnet")
	defer ln.Close()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(c)
	readUntil(t, r, "handle:")
	// IAC WILL ECHO, then handle
	if _, err := c.Write([]byte{255, 251, 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(c, "nate\r\n"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, r, "password:")
	if _, err := io.WriteString(c, "secret\r\n"); err != nil {
		t.Fatal(err)
	}
	readUntil(t, r, "welcome")
}

func TestReadLineHasHardLimit(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(strings.Repeat("a", 17) + "\n"))
	if _, err := readLine(r, io.Discard, 16); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong, got %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func readUntil(t *testing.T, r *bufio.Reader, needle string) {
	t.Helper()
	var buf strings.Builder
	for buf.Len() < 4096 {
		b, err := r.ReadByte()
		if err != nil {
			t.Fatalf("waiting for %q, have %q: %v", needle, buf.String(), err)
		}
		buf.WriteByte(b)
		if strings.Contains(buf.String(), needle) {
			return
		}
	}
	t.Fatalf("never saw %q in %q", needle, buf.String())
}
