package queue

import (
	"strings"
	"testing"
	"time"
)

func TestProtocolLaneDrainsFirst(t *testing.T) {
	var got []string
	q := New(DefaultConfig(), func(s string) { got = append(got, s) })
	q.Push(Help, "help")
	q.Push(Protocol, "PONG :x")
	q.drain(time.Now())
	if len(got) == 0 || got[0] != "PONG :x" {
		t.Fatalf("%q", got)
	}
}

func TestStoppedQueueDrops(t *testing.T) {
	q := New(DefaultConfig(), func(string) {})
	q.Stop()
	q.Push(Serv, "PRIVMSG #c :hi")
	if q.Len() != 0 || q.Drops() == 0 {
		t.Fatal("stopped queue should drop")
	}
}

func TestSanitizeExport(t *testing.T) {
	if strings.ContainsAny(Sanitize("a\r\nb"), "\r\n") {
		t.Fatal("sanitize")
	}
}
