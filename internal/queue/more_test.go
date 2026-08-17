package queue

import (
	"testing"
	"time"
)

func TestQueueControlsAndPushText(t *testing.T) {
	var got []string
	q := New(DefaultConfig(), func(s string) { got = append(got, s) })
	q.SetSend(func(s string) { got = append(got, s) })
	q.SetMaxLine(80)
	q.SetGeneration(3)
	if q.Generation() != 3 {
		t.Fatal(q.Generation())
	}
	q.PushText(Serv, "PRIVMSG #c :", "hello there this is a line")
	if q.Size(Serv) == 0 && q.Len() == 0 {
		t.Fatal("pushtext")
	}
	_ = q.Size(Quick)
	_ = q.Size(Help)
	q.Start()
	time.Sleep(20 * time.Millisecond)
	q.Restart()
	time.Sleep(20 * time.Millisecond)
	q.Stop()
}
