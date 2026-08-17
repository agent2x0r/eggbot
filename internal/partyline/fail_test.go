package partyline

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestServeConnAndFailedLogin(t *testing.T) {
	s := &Server{
		Listen: "127.0.0.1:0",
		Log:    slog.Default(),
		Authenticate: func(handle, password string) error {
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
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(c)
	readUntil(t, r, "handle:")
	_, _ = io.WriteString(c, "eve\n")
	readUntil(t, r, "password:")
	_, _ = io.WriteString(c, "wrong\n")
	time.Sleep(50 * time.Millisecond)

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	s.ServeConn(a, "dcc")
	time.Sleep(20 * time.Millisecond)
	s.recordFail(c.RemoteAddr().String())
	s.recordFail(c.RemoteAddr().String())
	s.recordFail(c.RemoteAddr().String())
	_ = s.blocked(c.RemoteAddr().String())
}
