package queue

import (
	"strings"
	"testing"
)

func TestMakeRoomAndClipBytes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxItems = 3
	cfg.MaxLine = 8
	q := New(cfg, func(string) {})
	q.Push(Help, "help1")
	q.Push(Serv, "serv1")
	q.Push(Quick, "quick1")
	q.Push(Protocol, "PONG :x")
	if q.Len() > 3 && q.Drops() == 0 {
		t.Fatal("expected drop or eviction")
	}
	q2 := New(cfg, func(string) {})
	q2.PushText(Serv, "PRIVMSG #c :", strings.Repeat("x", 50)+"🙂"+strings.Repeat("y", 50))
	if clipBytes("🙂x", 1) != "" && clipBytes("ab", 8) != "ab" {
		t.Fatal("clip")
	}
	_ = clipBytes("abc", 2)
	_ = clipBytes("🙂🙂", 3)
}

func TestMakeRoomEvictsLowerLanes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxItems = 1
	q := New(cfg, func(string) {})
	q.Push(Help, "h")
	q.Push(Protocol, "PONG")
	q.Push(Serv, "s")
	q.Push(Quick, "q")
	q.Push(Help, "h2")
}
