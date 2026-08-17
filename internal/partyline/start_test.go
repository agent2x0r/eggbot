package partyline

import (
	"log/slog"
	"testing"
	"time"
)

func TestStartStopBroadcast(t *testing.T) {
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
	s.Broadcast("hello", false, nil)
	s.Broadcast("owners", true, func(string) bool { return false })
	s.Consolef('m', "mode %s", "x")
	if n := len(s.Sessions()); n != 0 {
		t.Fatalf("sessions %d", n)
	}
	s2 := &Server{Log: slog.Default()}
	if err := s2.Start(); err != nil {
		t.Fatal(err)
	}
	s2.Stop()
	s2.Stop()
	_ = time.Second
}
