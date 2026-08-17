package script

import (
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatchStartAndTimers(t *testing.T) {
	var hits atomic.Int32
	e := New(nil, slog.Default(), nil)
	e.AddBind(&Bind{Type: "pubm", Mask: "*", Call: func(Event) bool {
		hits.Add(1)
		return true
	}})
	e.AddBind(&Bind{Type: "pub", Mask: "!ping", Call: func(Event) bool {
		hits.Add(1)
		return false
	}})
	e.AddBind(&Bind{Type: "join", Mask: "*!*@h", Call: func(Event) bool { return true }})
	e.Dispatch("pubm", Event{Text: "hello world"})
	e.Dispatch("pub", Event{Text: "!ping extra"})
	e.Dispatch("join", Event{Host: "n!u@h"})
	e.AddBind(&Bind{Type: "pubm", Mask: "*hello*", Call: func(Event) bool {
		hits.Add(1)
		return true
	}})
	e.Dispatch("pubm", Event{Text: "say hello there"})
	e.Start()
	defer e.Stop()
	id := e.AddTimer(100*time.Millisecond, false, "once", func() { hits.Add(1) })
	if id == 0 {
		t.Fatal("timer")
	}
	if !e.KillTimer(id) {
		t.Fatal("kill")
	}
	fired := make(chan struct{}, 1)
	e.AddTimer(120*time.Millisecond, false, "fire", func() { fired <- struct{}{} })
	e.Dispatch("pubm", Event{Text: "async"})
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("timer loop")
	}
	if e.KillTimer(999) {
		t.Fatal("missing")
	}
	_ = e.Binds()
	e.ClearFile("missing")
}
